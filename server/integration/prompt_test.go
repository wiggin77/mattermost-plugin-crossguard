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

// promptChannelStatus mirrors the on-wire shape of
// ChannelStatusResponse in server/service.go. Only the fields the tests
// assert on are modeled; unknown fields are ignored.
type promptChannelStatus struct {
	TeamConnections []promptConnectionStatus `json:"team_connections"`
}

type promptConnectionStatus struct {
	Name      string `json:"name"`
	Direction string `json:"direction"`
	Linked    bool   `json:"linked"`
}

// TestPromptChannel drives the first-time-link prompt flow at the channel
// level. Mirrors docker-prompt-test. Each sub-test uses a unique channel
// name so prompt KV state from previous runs (or the other sub-test) does
// not mask the new prompt.
//
// The team-level low-to-high link must already exist on B (RequireSmokeLinkage
// covers it). Only the *channel* side is unlinked at the start of each
// sub-test, which is what causes the prompt.
func TestPromptChannel(t *testing.T) {
	h := NewHarness(t)
	RequireSmokeLinkage(t, h)

	useraClient := h.ClientAs(t, h.A, "usera", "password")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	adminB, _, err := h.AdminB.GetUserByUsername(ctx, "admin", "")
	if err != nil {
		t.Fatalf("GetUserByUsername(admin) on B: %v", err)
	}

	type row struct {
		name        string
		nameSuffix  string
		action      string // "accept" or "block"
		expectRelay bool
	}
	cases := []row{
		{name: "Accept", nameSuffix: "prompt-accept", action: "accept", expectRelay: true},
		{name: "Block", nameSuffix: "prompt-block", action: "block", expectRelay: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			channelName := fmt.Sprintf("%s-%d-%d", tc.nameSuffix, time.Now().Unix(), os.Getpid())
			channelA := h.EnsureChannel(t, h.A, "test", channelName, channelName)
			channelB := h.EnsureChannel(t, h.B, "test", channelName, channelName)
			h.AddChannelMemberByUsername(t, h.A, channelA.Id, "usera")

			// Link on A only. Receiver-side prompt fires because B has the
			// team linked but the channel arrives unlinked.
			h.ExecSlash(t, h.A, channelA.Id, "/crossguard init-channel outbound:low-to-high")

			triggerMarker := fmt.Sprintf("prompt-trigger:%s", channelName)
			CreatePost(t, useraClient, channelA.Id, triggerMarker)

			// The trigger should not relay until accepted; a prompt post
			// should appear on B's channel. Match the shell test's
			// substring check.
			Eventually(t, 20*time.Second, "prompt post on B for "+channelName, func() (struct{}, bool) {
				return struct{}{}, findPromptPost(t, h.AdminB, channelB.Id, "low-to-high") != nil
			})

			// Call the plugin's prompt endpoint.
			adminBClient := h.AdminB
			body := map[string]any{
				"user_id": adminB.Id,
				"context": map[string]any{
					"channel_id": channelB.Id,
					"conn_name":  "low-to-high",
				},
			}
			PluginPOST(t, adminBClient, h.B, "/prompt/channel/"+tc.action, body, nil)

			if tc.expectRelay {
				// Wait until the channel reports linked on B.
				Eventually(t, 10*time.Second, "channel linked on B after accept", func() (struct{}, bool) {
					var status promptChannelStatus
					PluginGET(t, adminBClient, h.B, "/channels/"+channelB.Id+"/status", &status)
					for _, c := range status.TeamConnections {
						if c.Name == "low-to-high" && c.Direction == "inbound" && c.Linked {
							return struct{}{}, true
						}
					}
					return struct{}{}, false
				})

				// Follow-up post should relay.
				followMarker := fmt.Sprintf("prompt-follow:%s", channelName)
				CreatePost(t, useraClient, channelA.Id, followMarker)
				h.FindRelayedPost(t, h.B, channelB.Id, followMarker, 20*time.Second)
				return
			}

			// Block path: follow-up should NOT relay. Wait the same 10s
			// the shell test waited, then confirm absence.
			followMarker := fmt.Sprintf("prompt-follow:%s", channelName)
			CreatePost(t, useraClient, channelA.Id, followMarker)
			time.Sleep(10 * time.Second)

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			posts, _, err := h.AdminB.GetPostsForChannel(ctx, channelB.Id, 0, 50, "", false, false)
			if err != nil {
				t.Fatalf("GetPostsForChannel: %v", err)
			}
			for _, p := range posts.Posts {
				if strings.Contains(p.Message, followMarker) {
					t.Fatalf("block path leaked: follow-up %s relayed to B (post id %s)", followMarker, p.Id)
				}
			}
		})
	}
}

// findPromptPost returns the most recent prompt post in the channel that
// references the named inbound connection, or nil if none has arrived.
// The plugin's prompt text is "...inbound Cross Guard connection..." and
// includes the connection name. Caller polls until non-nil.
func findPromptPost(t *testing.T, client *model.Client4, channelID, connName string) *model.Post {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	posts, _, err := client.GetPostsForChannel(ctx, channelID, 0, 50, "", false, false)
	if err != nil {
		return nil
	}
	for _, p := range posts.Posts {
		if strings.Contains(p.Message, "inbound Cross Guard connection") && strings.Contains(p.Message, connName) {
			return p
		}
	}
	return nil
}
