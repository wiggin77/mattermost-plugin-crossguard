//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
)

// postMetadataFiles fetches the relayed post via GetPostsForChannel (which
// includes Metadata.Files in the response, unlike GetPost) and returns the
// FileInfo list. Returns an empty slice if the post is no longer in the
// page-sized window. Fails the test on a transport error.
func postMetadataFiles(t *testing.T, h *Harness, channelID, postID string) []*model.FileInfo {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	posts, _, err := h.AdminB.GetPostsForChannel(ctx, channelID, 0, 50, "", false, false)
	if err != nil {
		t.Fatalf("GetPostsForChannel: %v", err)
	}
	p, ok := posts.Posts[postID]
	if !ok || p == nil || p.Metadata == nil {
		return nil
	}
	return p.Metadata.Files
}

// TestFileFilter exercises the connection-level file_filter_mode / file_filter_types
// settings on both the sender (Server A outbound) and receiver (Server B
// inbound) sides. Mirrors docker-file-filter-test.
//
// Three sub-tests, each runs serially (config is global) and restores the
// connection on cleanup so subsequent tests see an unfiltered link:
//
//   - SenderDenyPDF       Server A outbound, filter .pdf  -> PDF dropped, body relays
//   - SenderDenyTXT       Server A outbound, filter .txt  -> PDF still relays (negative case)
//   - ReceiverDenyPDF     Server B inbound,  filter .pdf  -> PDF dropped on receive, body relays
//
// Sub-test C from the implementation plan (per-connection max_file_size) is
// covered by Go unit tests in hooks_test.go and is not implementable here:
// the plugin reads the server's global FileSettings.MaxFileSize, not a
// per-connection field.
func TestFileFilter(t *testing.T) {
	h := NewHarness(t)
	linkage := RequireSmokeLinkage(t, h)

	useraClient := h.ClientAs(t, h.A, "usera", "password")

	type row struct {
		name      string
		patchSide Server
		direction ConnectionDirection
		mode      string
		types     string
		marker    string
		// expectFiles is whether the relayed post should retain file_ids.
		expectFiles bool
	}
	cases := []row{
		{name: "SenderDenyPDF", patchSide: h.A, direction: Outbound, mode: "deny", types: ".pdf", marker: "filter-sender-deny-pdf", expectFiles: false},
		{name: "SenderDenyTXT", patchSide: h.A, direction: Outbound, mode: "deny", types: ".txt", marker: "filter-sender-deny-txt", expectFiles: true},
		{name: "ReceiverDenyPDF", patchSide: h.B, direction: Inbound, mode: "deny", types: ".pdf", marker: "filter-receiver-deny-pdf", expectFiles: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			restore := h.PatchConnection(t, tc.patchSide, tc.direction, "low-to-high", func(conn map[string]any) {
				conn["file_filter_mode"] = tc.mode
				conn["file_filter_types"] = tc.types
			})
			t.Cleanup(restore)

			marker := fmt.Sprintf("%s:%d-%d", tc.marker, time.Now().UnixNano(), os.Getpid())
			fileID := UploadFile(t, useraClient, linkage.LowToHighA, "testdata/sample.pdf")
			CreatePostWithFile(t, useraClient, linkage.LowToHighA, marker, fileID)

			relayed := h.FindRelayedPost(t, h.B, linkage.LowToHighB, marker, 30*time.Second)

			// The relay always carries the post body. The filter only
			// drops attachment content, so we assert on Metadata.Files
			// (the materialized FileInfo list on the receiver) rather
			// than FileIds, which can linger on the post record.
			// Mirrors the shell test's `metadata.files` check.
			Eventually(t, 10*time.Second, fmt.Sprintf("metadata.files on relayed %s (expect files=%v)", relayed.Id, tc.expectFiles), func() (struct{}, bool) {
				files := postMetadataFiles(t, h, linkage.LowToHighB, relayed.Id)
				has := len(files) > 0
				return struct{}{}, has == tc.expectFiles
			})

			files := postMetadataFiles(t, h, linkage.LowToHighB, relayed.Id)
			if (len(files) > 0) != tc.expectFiles {
				names := make([]string, 0, len(files))
				for _, f := range files {
					names = append(names, f.Id)
				}
				t.Fatalf("filter mismatch on %s: expected files=%v, got metadata.files=%s",
					tc.name, tc.expectFiles, strings.Join(names, ","))
			}
		})
	}
}
