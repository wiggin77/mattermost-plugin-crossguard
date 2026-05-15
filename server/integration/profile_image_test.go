//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// TestProfileImage uploads a content-unique PNG as the avatar of a
// dedicated user (userg) on Server A and verifies the framework propagates
// the change to the synced-in user on Server B by watching
// last_picture_update advance. Mirrors docker-profile-image-test.
//
// Why a dedicated user: the framework keys profile image sync on the
// source-side user id. Re-using usera would interleave with other tests
// that change usera's picture, so the shell test introduced userg. We
// preserve that choice.
//
// Why content-unique PNG: Mattermost dedupes byte-identical uploads, which
// would leave last_picture_update unchanged. See png.go for the generator.
func TestProfileImage(t *testing.T) {
	h := NewHarness(t)
	linkage := RequireSmokeLinkage(t, h)

	userg := h.EnsureUser(t, h.A, "userg", "userg@example.com", "test")
	h.AddChannelMemberByUsername(t, h.A, linkage.LowToHighA, "userg")

	usergClient := h.ClientAs(t, h.A, "userg", "password")

	// Warmup post: triggers framework user sync, which creates the
	// userg:* sync user on Server B.
	warmupMarker := fmt.Sprintf("profile-warmup:%d-%d", time.Now().UnixNano(), os.Getpid())
	CreatePost(t, usergClient, linkage.LowToHighA, warmupMarker)
	h.FindRelayedPost(t, h.B, linkage.LowToHighB, warmupMarker, 30*time.Second)

	// Locate the sync user on B by username prefix.
	syncUser := h.FindUserWithUsernamePrefix(t, h.B, "userg:", 30*time.Second)
	initialLPU := syncUser.LastPictureUpdate
	t.Logf("sync user on B: id=%s username=%s last_picture_update=%d",
		syncUser.Id, syncUser.Username, initialLPU)

	// Upload a new, content-unique avatar on A.
	SetProfileImage(t, usergClient, userg.Id, UniquePNG(t))

	// Poll B for last_picture_update advance. The shell test allowed 90s.
	Eventually(t, 90*time.Second, "last_picture_update advance on B sync user", func() (struct{}, bool) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		u, _, err := h.AdminB.GetUser(ctx, syncUser.Id, "")
		if err != nil {
			return struct{}{}, false
		}
		return struct{}{}, u.LastPictureUpdate > initialLPU
	})
}
