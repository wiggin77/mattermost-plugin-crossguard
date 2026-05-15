//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
)

// LookupTeam returns the team by name on the given server, failing the test
// if it does not exist.
func (h *Harness) LookupTeam(t *testing.T, s Server, name string) *model.Team {
	t.Helper()
	client := h.AdminFor(s)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	team, _, err := client.GetTeamByName(ctx, name, "")
	if err != nil {
		t.Fatalf("GetTeamByName(%s) on %s: %v", name, s.Name, err)
	}
	return team
}

// EnsureChannel returns the channel by name on the given server's team,
// creating it as type Open if it does not exist. Idempotent and safe to
// call from suite setup.
func (h *Harness) EnsureChannel(t *testing.T, s Server, teamName, channelName, displayName string) *model.Channel {
	t.Helper()
	client := h.AdminFor(s)
	team := h.LookupTeam(t, s, teamName)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if ch, _, err := client.GetChannelByName(ctx, channelName, team.Id, ""); err == nil {
		return ch
	}

	ch := &model.Channel{
		TeamId:      team.Id,
		Name:        channelName,
		DisplayName: displayName,
		Type:        model.ChannelTypeOpen,
	}
	created, _, err := client.CreateChannel(ctx, ch)
	if err != nil {
		// Race: a parallel test may have created it. Retry the lookup.
		if got, _, err2 := client.GetChannelByName(ctx, channelName, team.Id, ""); err2 == nil {
			return got
		}
		t.Fatalf("CreateChannel(%s) on %s: %v", channelName, s.Name, err)
	}
	return created
}

// AddChannelMemberByUsername resolves the username on the given server, then
// adds them to the channel. "Already a member" errors are ignored.
func (h *Harness) AddChannelMemberByUsername(t *testing.T, s Server, channelID, username string) {
	t.Helper()
	client := h.AdminFor(s)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	user, _, err := client.GetUserByUsername(ctx, username, "")
	if err != nil {
		t.Fatalf("GetUserByUsername(%s) on %s: %v", username, s.Name, err)
	}
	if _, _, err := client.AddChannelMember(ctx, channelID, user.Id); err != nil {
		if isAlreadyMemberErr(err) {
			return
		}
		t.Fatalf("AddChannelMember(%s, %s) on %s: %v", channelID, username, s.Name, err)
	}
}

// ExecSlash executes a slash command in the given channel as admin. It
// returns the response (rarely needed) and fails the test on transport
// errors. Some crossguard slash commands respond with status:"OK" inside the
// post text rather than a non-200; callers should inspect the response if
// they need stronger assertions.
func (h *Harness) ExecSlash(t *testing.T, s Server, channelID, command string) *model.CommandResponse {
	t.Helper()
	client := h.AdminFor(s)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	resp, _, err := client.ExecuteCommand(ctx, channelID, command)
	if err != nil {
		t.Fatalf("ExecuteCommand(%s) in channel %s on %s: %v", command, channelID, s.Name, err)
	}
	return resp
}

func isAlreadyMemberErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already") && strings.Contains(msg, "member")
}
