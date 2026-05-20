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

// TestServiceBus exercises the azure-servicebus provider end-to-end
// against the local Service Bus emulator. Mirrors
// docker-servicebus-smoke-test.
//
// Emulator readiness is the responsibility of the make layer: the
// docker-integration-test-go target depends on servicebus-probe-run,
// which brings up the emulator container and AMQP-PeekMessages until it
// is reachable. This test assumes the emulator is ready and fails (not
// skips) if it is not.
//
// The first relay after a fresh provider link can take noticeably longer
// than NATS (Service Bus client must establish AMQP, the elector must
// subscribe, the queue may be cold). The shell test allowed 60s; we
// match.
//
// Uses a dedicated user (userf) to avoid the framework's RemoteID
// conflict across providers.
func TestServiceBus(t *testing.T) {
	const (
		channelName = "servicebus-test"
		username    = "userf"
	)
	connName := devbaseline.ServiceBusLowToHighName

	h := NewHarness(t)

	h.EnsureUser(t, h.A, username, username+"@example.com", "test")

	channelA := h.EnsureChannel(t, h.A, "test", channelName, "Service Bus Test")
	channelB := h.EnsureChannel(t, h.B, "test", channelName, "Service Bus Test")
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

	userfClient := h.ClientAs(t, h.A, username, "password")

	t.Run("Message", func(t *testing.T) {
		marker := fmt.Sprintf("servicebus-smoke-test:%d-%d", time.Now().UnixNano(), os.Getpid())
		CreatePost(t, userfClient, channelA.Id, marker)
		h.FindRelayedPost(t, h.B, channelB.Id, marker, 90*time.Second)
	})

	t.Run("File", func(t *testing.T) {
		marker := fmt.Sprintf("servicebus-file-test:%d-%d", time.Now().UnixNano(), os.Getpid())
		fileID := UploadFile(t, userfClient, channelA.Id, "testdata/sample.pdf")
		CreatePostWithFile(t, userfClient, channelA.Id, marker, fileID)
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
