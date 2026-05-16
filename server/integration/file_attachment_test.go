//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestFileAttachment uploads a file (PDF and DOCX, table-driven) and posts
// it on Server A's low-to-high channel, then verifies the relayed post on
// Server B carries at least one file_id. Mirrors the file relay block of
// the shell docker-integration-test target.
func TestFileAttachment(t *testing.T) {
	t.Skip("blocked on Mattermost server PR #36592: gob-encoding bug in " +
		"apiRPCServer.ReceiveSharedChannelAttachmentSyncMsg breaks the " +
		"plugin<->server RPC connection on the first attachment receive, " +
		"causing this test (and any subsequent tests that depend on B's " +
		"plugin) to fail. Re-enable once the fix is in the dev image.")

	h := NewHarness(t)
	linkage := RequireSmokeLinkage(t, h)

	type row struct {
		name    string
		relPath string
	}
	cases := []row{
		{name: "PDF", relPath: "testdata/sample.pdf"},
		{name: "DOCX", relPath: "testdata/sample.docx"},
	}

	useraClient := h.ClientAs(t, h.A, "usera", "password")

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			marker := fmt.Sprintf("file-test:%d-%d-%s",
				time.Now().UnixNano(), os.Getpid(), strings.ToLower(tc.name))

			fileID := UploadFile(t, useraClient, linkage.LowToHighA, tc.relPath)
			CreatePostWithFile(t, useraClient, linkage.LowToHighA, marker, fileID)

			relayed := h.FindRelayedPost(t, h.B, linkage.LowToHighB, marker, 30*time.Second)

			// The relayed post must carry the attachment. The shell test
			// asserted len(file_ids) > 0; do the same and also surface the
			// file metadata for easier debugging on failure.
			Eventually(t, 30*time.Second, "file_ids on relayed post "+relayed.Id, func() (struct{}, bool) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				p, _, err := h.AdminB.GetPost(ctx, relayed.Id, "")
				if err != nil {
					return struct{}{}, false
				}
				return struct{}{}, len(p.FileIds) > 0
			})
		})
	}
}
