package main

import (
	"github.com/mattermost/mattermost/server/public/plugin"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/wire"
)

// pluginWireLogger adapts plugin.API logging to the wire.WireLogger
// interface so the wire package can emit validation audit events
// (UserFromModel ladder, per-prop drops) without depending on the
// plugin's API surface.
//
// The adapter carries a base K/V context (typically conn_name,
// channel_id, post_id) so a single log entry from inside UserFromModel
// already correlates the dropped record with the envelope that
// triggered it. This is the "never silently drop" audit signal: each
// event records both the validation failure and the operational
// context.
type pluginWireLogger struct {
	api plugin.API
	ctx []any
}

func newPluginWireLogger(api plugin.API, ctx ...any) wire.WireLogger {
	return &pluginWireLogger{api: api, ctx: ctx}
}

// withContext returns a derived logger that appends additional context
// pairs. Used to layer post-level context on top of envelope-level
// context.
func (l *pluginWireLogger) withContext(extra ...any) *pluginWireLogger {
	merged := make([]any, 0, len(l.ctx)+len(extra))
	merged = append(merged, l.ctx...)
	merged = append(merged, extra...)
	return &pluginWireLogger{api: l.api, ctx: merged}
}

func (l *pluginWireLogger) Warn(code int, kv ...any) {
	args := make([]any, 0, 2+len(l.ctx)+len(kv))
	args = append(args, "error_code", code)
	args = append(args, l.ctx...)
	args = append(args, kv...)
	l.api.LogWarn("wire-layer validation event", args...)
}

func (l *pluginWireLogger) Error(code int, kv ...any) {
	args := make([]any, 0, 2+len(l.ctx)+len(kv))
	args = append(args, "error_code", code)
	args = append(args, l.ctx...)
	args = append(args, kv...)
	l.api.LogError("wire-layer validation event", args...)
}

// withPostContext derives a logger with per-post context appended.
// When the input is a *pluginWireLogger it appends to the existing
// context; otherwise (e.g., a recording test mock, or NopLogger) it
// returns the input unchanged so foreign WireLogger implementations
// keep their original semantics.
func withPostContext(log wire.WireLogger, postID string) wire.WireLogger {
	if pl, ok := log.(*pluginWireLogger); ok {
		return pl.withContext("post_id", postID)
	}
	return log
}
