package main

import (
	"net/http"

	"github.com/mattermost/mattermost/server/public/model"
)

// getUser wraps p.API.GetUser, normalizing the (nil, nil) return that the
// generated plugin->server RPC client produces when the RPC layer itself
// fails (gob encoding error, severed pipe, etc.).
//
// Background: every method on the generated apiRPCClient catches RPC errors
// with `log.Printf` and falls through to `return _returns.A, _returns.B`,
// which is (nil, nil) for the zero-value response struct. Code that
// dereferences the returned pointer crashes. See
// mattermost/server/public/plugin/client_rpc_generated.go.
//
// The wrapper returns *model.AppError so most callers do not need to
// change beyond swapping the API call site. When user is nil with no
// upstream error, we synthesize a 5xx AppError describing the RPC failure
// so the caller's existing `if appErr != nil` branch handles it.
func (p *Plugin) getUser(userID string) (*model.User, *model.AppError) {
	user, appErr := p.API.GetUser(userID)
	if appErr != nil {
		return nil, appErr
	}
	if user == nil {
		return nil, model.NewAppError(
			"Plugin.getUser",
			"plugin.api.nil_return",
			nil,
			"plugin API returned nil user for id "+userID+" (likely RPC layer failure)",
			http.StatusInternalServerError,
		)
	}
	return user, nil
}
