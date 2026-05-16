//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/build/devbaseline"
)

// TestAzureQueue exercises the azure-queue provider end-to-end against the
// local Azurite emulator. Mirrors docker-azure-smoke-test.
//
// Two sub-tests:
//
//   - Message: post a body, verify relay
//   - File:    post a body with sample.pdf attached, verify relay carries
//     the materialized FileInfo
//
// Uses a dedicated user (userd). Each provider test uses its own user to
// avoid the shared-channels framework's RemoteID conflict when one user
// is the sync source for two distinct connections.
func TestAzureQueue(t *testing.T) {
	const (
		channelName = "azure-test"
		username    = "userd"
	)
	connName := devbaseline.AzureQueueLowToHighName

	h := NewHarness(t)

	h.EnsureUser(t, h.A, username, username+"@example.com", "test")

	channelA := h.EnsureChannel(t, h.A, "test", channelName, "Azure Queue Test")
	channelB := h.EnsureChannel(t, h.B, "test", channelName, "Azure Queue Test")
	h.AddChannelMemberByUsername(t, h.A, channelA.Id, username)
	h.AddChannelMemberByUsername(t, h.B, channelB.Id, "userb")

	// Link team + channel on both sides. The connection itself is
	// pre-configured at deploy time (see build/devbaseline). These slash
	// commands are idempotent.
	h.ExecSlash(t, h.A, channelA.Id, "/crossguard init-team outbound:"+connName)
	h.ExecSlash(t, h.B, channelB.Id, "/crossguard init-team inbound:"+connName)
	h.ExecSlash(t, h.A, channelA.Id, "/crossguard init-channel outbound:"+connName)
	h.ExecSlash(t, h.B, channelB.Id, "/crossguard init-channel inbound:"+connName)

	userdClient := h.ClientAs(t, h.A, username, "password")

	t.Run("Message", func(t *testing.T) {
		// 90s timeout absorbs the inbound-elector cold start. A stale
		// lease from a prior run (or sibling node) can hold for up to
		// the TTL (45s, see server/inbound_election.go) before the new
		// elector can acquire and subscribe; layered on that the
		// elector's tick cadence is 15s. Subsequent messages on the same
		// connection are sub-second once the subscription is live, see
		// the File sub-test below.
		marker := fmt.Sprintf("azure-smoke-test:%d-%d", time.Now().UnixNano(), os.Getpid())
		CreatePost(t, userdClient, channelA.Id, marker)
		h.FindRelayedPost(t, h.B, channelB.Id, marker, 90*time.Second)
	})

	t.Run("File", func(t *testing.T) {
		t.Skip("blocked on Mattermost server PR #36592: gob-encoding bug " +
			"in apiRPCServer.ReceiveSharedChannelAttachmentSyncMsg breaks " +
			"the plugin<->server RPC. Re-enable once the fix is in the " +
			"dev image.")
		marker := fmt.Sprintf("azure-file-test:%d-%d", time.Now().UnixNano(), os.Getpid())
		fileID := UploadFile(t, userdClient, channelA.Id, "testdata/sample.pdf")
		CreatePostWithFile(t, userdClient, channelA.Id, marker, fileID)
		relayed := h.FindRelayedPost(t, h.B, channelB.Id, marker, 90*time.Second)

		Eventually(t, 90*time.Second, "file_ids on relayed "+relayed.Id, func() (struct{}, bool) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			p, _, err := h.AdminB.GetPost(ctx, relayed.Id, "")
			if err != nil {
				return struct{}{}, false
			}
			return struct{}{}, len(p.FileIds) > 0
		})

		files := postMetadataFiles(t, h, channelB.Id, relayed.Id)
		if len(files) == 0 {
			t.Fatalf("expected at least one materialized file on %s, got none (file_ids=%s)",
				relayed.Id, strings.Join(relayedFileIDs(h, channelB.Id, relayed.Id), ","))
		}
	})
}

// relayedFileIDs returns the FileIds field of the post for debug logging.
func relayedFileIDs(h *Harness, channelID, postID string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	posts, _, err := h.AdminB.GetPostsForChannel(ctx, channelID, 0, 50, "", false, false)
	if err != nil {
		return nil
	}
	if p, ok := posts.Posts[postID]; ok && p != nil {
		return p.FileIds
	}
	return nil
}
