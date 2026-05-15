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

// TestPostLifecycle exercises post create, edit, react, unreact, and delete
// across the relay. It is table-driven over a `transport` axis so the
// Phase 4 follow-up (Azure Queue, Azure Blob, Service Bus) can add rows
// without reshaping the test. At migration time the table has a single row
// because the underlying makefile target tested NATS only.
func TestPostLifecycle(t *testing.T) {
	h := NewHarness(t)

	type row struct {
		name        string
		channelName string
		setup       func(t *testing.T, h *Harness)
	}
	cases := []row{
		{
			name:        "NATS",
			channelName: "low-to-high",
			setup: func(t *testing.T, h *Harness) {
				RequireSmokeLinkage(t, h)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.setup(t, h)
			runPostLifecycle(t, h, tc.channelName)
		})
	}
}

// runPostLifecycle is the body of TestPostLifecycle, factored so future
// transport rows can call it after their per-transport setup is done.
func runPostLifecycle(t *testing.T, h *Harness, channelName string) {
	t.Helper()

	channelA := h.EnsureChannel(t, h.A, "test", channelName, "Low To High")
	channelB := h.EnsureChannel(t, h.B, "test", channelName, "Low To High")

	useraClient := h.ClientAs(t, h.A, "usera", "password")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	usera, _, err := h.AdminA.GetUserByUsername(ctx, "usera", "")
	if err != nil {
		t.Fatalf("GetUserByUsername(usera) on A: %v", err)
	}

	marker := fmt.Sprintf("lifecycle-test:%d-%d", time.Now().UnixNano(), os.Getpid())

	// Step 1: post the base message on A.
	postA := CreatePost(t, useraClient, channelA.Id, marker)
	t.Logf("posted %s on A (id=%s)", marker, postA.Id)

	// Step 2: poll B for the relayed post.
	postB := h.FindRelayedPost(t, h.B, channelB.Id, marker, 20*time.Second)
	t.Logf("relay landed on B (id=%s)", postB.Id)

	// Step 3: edit on A and wait for the update to propagate.
	editedMarker := marker + " edited"
	EditPostMessage(t, useraClient, postA.Id, editedMarker)
	Eventually(t, 20*time.Second, "edit propagation for "+postB.Id, func() (struct{}, bool) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		p, _, err := h.AdminB.GetPost(ctx, postB.Id, "")
		if err != nil {
			return struct{}{}, false
		}
		return struct{}{}, strings.Contains(p.Message, "edited") && p.UpdateAt > p.CreateAt
	})

	// Step 4: add reaction on A and wait for it to appear on B.
	React(t, useraClient, usera.Id, postA.Id, "thumbsup")
	Eventually(t, 20*time.Second, "reaction add for "+postB.Id, func() (struct{}, bool) {
		return struct{}{}, h.HasReaction(t, h.B, postB.Id, "thumbsup")
	})

	// Step 5: remove reaction on A and wait for it to disappear on B.
	Unreact(t, useraClient, usera.Id, postA.Id, "thumbsup")
	Eventually(t, 20*time.Second, "reaction remove for "+postB.Id, func() (struct{}, bool) {
		return struct{}{}, !h.HasReaction(t, h.B, postB.Id, "thumbsup")
	})

	// Step 6: delete on A and wait for the soft-delete to land on B.
	DeletePostID(t, useraClient, postA.Id)
	Eventually(t, 20*time.Second, "delete for "+postB.Id, func() (struct{}, bool) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		p, _, err := h.AdminB.GetPostIncludeDeleted(ctx, postB.Id, "")
		if err != nil {
			return struct{}{}, false
		}
		return struct{}{}, p.DeleteAt > 0
	})
}
