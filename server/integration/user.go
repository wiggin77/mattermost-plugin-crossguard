//go:build integration

package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
)

// EnsureTeam guarantees a team with the given name exists on the given
// server and that the listed usernames are members. Creation goes through
// mmctl --local because admin signup may be disabled in the dev environment.
// Idempotent.
func (h *Harness) EnsureTeam(t *testing.T, s Server, name, displayName string, members ...string) *model.Team {
	t.Helper()

	h.Compose.MMCtlOrAlreadyExists(t, s.Container,
		"team", "create",
		"--name", name,
		"--display-name", displayName,
	)
	for _, u := range members {
		r := h.Compose.MMCtl(t, s.Container, "team", "users", "add", name, u)
		if r.Err != nil && !IsAlreadyExists(r) {
			// "user not found" is not benign and should fail; keep
			// failing only on non-membership errors.
			t.Fatalf("mmctl team users add %s %s on %s: %v\n%s",
				name, u, s.Name, r.Err, r.Combined())
		}
	}

	client := h.AdminFor(s)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	team, _, err := client.GetTeamByName(ctx, name, "")
	if err != nil {
		t.Fatalf("GetTeamByName(%s) on %s after create: %v", name, s.Name, err)
	}
	return team
}

// EnsureUser guarantees a user with the given username exists on the given
// server and is a member of the named team. The password is fixed to
// "password" to match the existing dev fixtures. Idempotent.
func (h *Harness) EnsureUser(t *testing.T, s Server, username, email, teamName string) *model.User {
	t.Helper()

	h.Compose.MMCtlOrAlreadyExists(t, s.Container,
		"user", "create",
		"--email", email,
		"--username", username,
		"--password", "password",
	)
	if teamName != "" {
		r := h.Compose.MMCtl(t, s.Container, "team", "users", "add", teamName, username)
		if r.Err != nil && !IsAlreadyExists(r) {
			t.Fatalf("mmctl team users add %s %s on %s: %v\n%s",
				teamName, username, s.Name, r.Err, r.Combined())
		}
	}

	client := h.AdminFor(s)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	user, _, err := client.GetUserByUsername(ctx, username, "")
	if err != nil {
		t.Fatalf("GetUserByUsername(%s) on %s after create: %v", username, s.Name, err)
	}
	return user
}

// FindUserWithUsernamePrefix scans Server B for a user whose username starts
// with the given prefix (e.g. "userg:" for crossguard sync users). Used by
// the profile image test to locate the synced-in user without having to
// guess the exact suffix. Returns the user when found; fails the test on
// timeout.
func (h *Harness) FindUserWithUsernamePrefix(t *testing.T, s Server, prefix string, timeout time.Duration) *model.User {
	t.Helper()
	client := h.AdminFor(s)
	return Eventually(t, timeout, fmt.Sprintf("user with username prefix %q on %s", prefix, s.Name), func() (*model.User, bool) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		users, _, err := client.GetUsers(ctx, 0, 200, "")
		if err != nil {
			return nil, false
		}
		for _, u := range users {
			if len(u.Username) >= len(prefix) && u.Username[:len(prefix)] == prefix {
				return u, true
			}
		}
		return nil, false
	})
}
