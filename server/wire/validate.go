package wire

import "regexp"

// Validation patterns mirror the constrained wire schema verbatim.
// The constrained schema is the authoritative compliance contract; the
// wire package validates upstream-supplied values against these
// patterns before emission so the producer never serializes a non-
// conforming envelope.
//
// keep in sync with schema/crossguard.xsd
var (
	// usernamePattern mirrors UsernameType:
	//   <xs:pattern value="[a-z0-9][a-z0-9_\-\.]{0,63}"/>
	// keep in sync with schema/crossguard.xsd
	usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_\-.]{0,63}$`)

	// emailPattern mirrors EmailType:
	//   <xs:pattern value="[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}"/>
	// keep in sync with schema/crossguard.xsd
	emailPattern = regexp.MustCompile(`^[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}$`)
)

// WireLogger is the wire package's logging seam. The package owns no
// reference to plugintest or the upstream pluginapi, so the plugin
// passes an adapter that forwards Warn/Error events to its
// p.API.LogWarn/LogError. Tests inject a recording mock.
//
// Implementations must accept zero or more key-value pairs (slog-style:
// alternating string keys and any-typed values), the same shape that
// pluginapi.Log* methods consume.
type WireLogger interface {
	Warn(code int, kv ...any)
	Error(code int, kv ...any)
}

// nopLogger is a WireLogger that discards every call. The production
// outbound producer always supplies a real plugin adapter; this exists
// purely as the fallback used when a nil logger is passed (defensive,
// not the intended path). Tests should not use it: they should use
// RecordingLogger so the audit-log contract is exercised explicitly
// rather than silently swallowed.
type nopLogger struct{}

func (nopLogger) Warn(int, ...any)  {}
func (nopLogger) Error(int, ...any) {}

// NopLogger returns a WireLogger that drops every event. Defensive
// fallback only; prefer RecordingLogger in tests.
func NopLogger() WireLogger { return nopLogger{} }

// LogEntry captures one Warn or Error call for assertion in tests.
type LogEntry struct {
	Code int
	KV   []any
}

// RecordingLogger is a WireLogger that captures every call so tests
// can assert which audit events fired. Use it (not nil and not
// NopLogger) in tests so the "never silently drop" contract is
// verified at each call site.
type RecordingLogger struct {
	Warns  []LogEntry
	Errors []LogEntry
}

// NewRecordingLogger returns a fresh RecordingLogger.
func NewRecordingLogger() *RecordingLogger { return &RecordingLogger{} }

func (r *RecordingLogger) Warn(code int, kv ...any) {
	r.Warns = append(r.Warns, LogEntry{Code: code, KV: append([]any(nil), kv...)})
}

func (r *RecordingLogger) Error(code int, kv ...any) {
	r.Errors = append(r.Errors, LogEntry{Code: code, KV: append([]any(nil), kv...)})
}

// WarnCodes returns the codes of every Warn call, in order.
func (r *RecordingLogger) WarnCodes() []int {
	out := make([]int, len(r.Warns))
	for i, e := range r.Warns {
		out[i] = e.Code
	}
	return out
}

// ErrorCodes returns the codes of every Error call, in order.
func (r *RecordingLogger) ErrorCodes() []int {
	out := make([]int, len(r.Errors))
	for i, e := range r.Errors {
		out[i] = e.Code
	}
	return out
}
