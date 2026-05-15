//go:build integration

package integration

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// TestXMLRelay exercises the XML wire-format transport configured by
// `make deploy` as outbound:xml-low-to-high on A / inbound:xml-low-to-high
// on B. It mirrors the xml-test block of the shell docker-integration-test
// target.
//
// Table-driven on the `format` axis for symmetry with TestPostLifecycle,
// even though there is only one row at migration time: the underlying
// makefile test exercised XML only.
func TestXMLRelay(t *testing.T) {
	h := NewHarness(t)
	// xml-low-to-high is independent of the smoke linkage, but the smoke
	// channels make for a useful sanity check upstream; do not require it.

	type row struct {
		name        string
		channelName string
		outConn     string
		inConn      string
		marker      string
	}
	cases := []row{
		{
			name:        "XML",
			channelName: "xml-test",
			outConn:     "outbound:xml-low-to-high",
			inConn:      "inbound:xml-low-to-high",
			marker:      "xml-test",
		},
	}

	// Dedicated poster so this test does not interfere with usera's sync
	// ownership on the smoke linkage.
	h.EnsureUser(t, h.A, "userc", "userc@example.com", "test")

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			channelA := h.EnsureChannel(t, h.A, "test", tc.channelName, "XML Test")
			channelB := h.EnsureChannel(t, h.B, "test", tc.channelName, "XML Test")

			h.AddChannelMemberByUsername(t, h.A, channelA.Id, "userc")
			h.AddChannelMemberByUsername(t, h.B, channelB.Id, "userb")

			h.ExecSlash(t, h.A, channelA.Id, "/crossguard init-team "+tc.outConn)
			h.ExecSlash(t, h.B, channelB.Id, "/crossguard init-team "+tc.inConn)
			h.ExecSlash(t, h.A, channelA.Id, "/crossguard init-channel "+tc.outConn)
			h.ExecSlash(t, h.B, channelB.Id, "/crossguard init-channel "+tc.inConn)

			usercClient := h.ClientAs(t, h.A, "userc", "password")
			marker := fmt.Sprintf("%s:%d-%d", tc.marker, time.Now().UnixNano(), os.Getpid())

			CreatePost(t, usercClient, channelA.Id, marker)
			h.FindRelayedPost(t, h.B, channelB.Id, marker, 20*time.Second)
		})
	}
}
