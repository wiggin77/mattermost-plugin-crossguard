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
	"github.com/mattermost/mattermost/server/public/model"
)

// TestAzureBlob exercises the azure-blob (batched WAL) provider end-to-end
// against the local Azurite emulator. Mirrors docker-azure-blob-smoke-test.
//
// Unlike azure-queue, azure-blob buffers locally and flushes on an
// interval (flush_interval_seconds=5 in this fixture). First-delivery
// latency therefore includes the flush window plus the receiver's
// 1-second poll cadence. We allow 60s for both sub-tests to absorb cold
// start.
//
// Uses a dedicated user (usere) to avoid the framework's RemoteID conflict
// across providers.
func TestAzureBlob(t *testing.T) {
	const (
		channelName = "azure-blob-test"
		username    = "usere"
	)
	connName := devbaseline.AzureBlobLowToHighName

	h := NewHarness(t)

	h.EnsureUser(t, h.A, username, username+"@example.com", "test")

	channelA := h.EnsureChannel(t, h.A, "test", channelName, "Azure Blob Test")
	channelB := h.EnsureChannel(t, h.B, "test", channelName, "Azure Blob Test")
	h.AddChannelMemberByUsername(t, h.A, channelA.Id, username)
	h.AddChannelMemberByUsername(t, h.B, channelB.Id, "userb")

	// The connection itself is pre-configured at deploy time (see
	// build/devbaseline). These slash commands are idempotent.
	h.ExecSlash(t, h.A, channelA.Id, "/crossguard init-team outbound:"+connName)
	h.ExecSlash(t, h.B, channelB.Id, "/crossguard init-team inbound:"+connName)
	h.ExecSlash(t, h.A, channelA.Id, "/crossguard init-channel outbound:"+connName)
	h.ExecSlash(t, h.B, channelB.Id, "/crossguard init-channel inbound:"+connName)

	// Wait for the framework to mark the remote online for this channel
	// on the sending side before the first post; otherwise the first
	// sync attempts can be dropped during the plugin's cold-start window.
	h.WaitForChannelRemotesOnline(t, h.A, channelA.Id, 60*time.Second)

	usereClient := h.ClientAs(t, h.A, username, "password")

	t.Run("Message", func(t *testing.T) {
		marker := fmt.Sprintf("azure-blob-smoke-test:%d-%d", time.Now().UnixNano(), os.Getpid())
		CreatePost(t, usereClient, channelA.Id, marker)
		h.FindRelayedPost(t, h.B, channelB.Id, marker, 90*time.Second)
	})

	t.Run("File", func(t *testing.T) {
		marker := fmt.Sprintf("azure-blob-file-test:%d-%d", time.Now().UnixNano(), os.Getpid())
		fileID := UploadFile(t, usereClient, channelA.Id, "testdata/sample.pdf")
		CreatePostWithFile(t, usereClient, channelA.Id, marker, fileID)
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

		// FileIds may appear on the post record before the corresponding
		// FileInfo metadata is fully materialized on the receiver. Poll
		// until at least one FileInfo is queryable.
		files := Eventually(t, 90*time.Second, "materialized files on "+relayed.Id, func() ([]*model.FileInfo, bool) {
			f := postMetadataFiles(t, h, channelB.Id, relayed.Id)
			return f, len(f) > 0
		})
		if len(files) == 0 {
			t.Fatalf("expected at least one materialized file on %s, got none (file_ids=%s)",
				relayed.Id, strings.Join(relayedFileIDs(h, channelB.Id, relayed.Id), ","))
		}
	})
}
