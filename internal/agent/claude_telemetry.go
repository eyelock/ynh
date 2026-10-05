package agent

import (
	"encoding/json"
	"strings"
)

// SupportsTelemetryRelay reports whether ynh knows how to point backend's own
// telemetry at a relay. Only Claude Code, whose settings were verified
// against a real CLI (ynr ADR-004). Codex is configured through its
// config.toml rather than the environment, and that is unverified, so it
// gets nothing for now; so does Cursor.
func SupportsTelemetryRelay(backend string) bool {
	return backend == "claude"
}

// claudeTelemetrySettings is what Claude Code is given when the run has a
// relay: exactly the settings ynr ADR-004 records as verified with Claude
// Code 2.1.289, then every content switch Claude Code documents, forced off
// (ynr ADR-006, rule 10: vendor CLIs run with their content options off).
//
// TRACEPARENT is not here: the run's telemetry already gives it to the
// worker whenever telemetry is on.
func claudeTelemetrySettings(endpoint string) [][2]string {
	return [][2]string{
		{"CLAUDE_CODE_ENABLE_TELEMETRY", "1"},
		{"OTEL_TRACES_EXPORTER", "otlp"},
		{"OTEL_METRICS_EXPORTER", "otlp"},
		{"OTEL_LOGS_EXPORTER", "otlp"},
		{"OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf"},
		{"OTEL_EXPORTER_OTLP_ENDPOINT", endpoint},
		// Without it Claude Code sends no spans.
		{"CLAUDE_CODE_ENHANCED_TELEMETRY_BETA", "1"},
		// Content, off. Prompts and the system prompt, and the model's
		// output under detailed tracing.
		{"OTEL_LOG_USER_PROMPTS", "0"},
		// Falls back to OTEL_LOG_USER_PROMPTS when unset; pinned anyway.
		{"OTEL_LOG_ASSISTANT_RESPONSES", "0"},
		// Bash commands, tool arguments and raw error strings.
		{"OTEL_LOG_TOOL_DETAILS", "0"},
		// File contents, command output and fetched pages.
		{"OTEL_LOG_TOOL_CONTENT", "0"},
		// Whole Messages API request and response bodies.
		{"OTEL_LOG_RAW_API_BODIES", "0"},
		// The organisation's managed settings, redacted.
		{"OTEL_LOG_MANAGED_SETTINGS", "0"},
		// Detailed beta tracing adds content attributes, and with
		// BETA_TRACING_ENDPOINT sends traces and logs somewhere else.
		{"ENABLE_BETA_TRACING_DETAILED", "0"},
	}
}

// claudeTelemetryOwned reports whether name is a variable the relay's
// configuration owns. Any such variable already in the worker's
// environment, from env_passthrough, is dropped before ynh's are added, so
// a passed-through OTEL_LOG_USER_PROMPTS=1 or a per-signal endpoint cannot
// turn content on or send the telemetry elsewhere.
func claudeTelemetryOwned(name string) bool {
	switch {
	case strings.HasPrefix(name, "OTEL_EXPORTER_OTLP_"),
		strings.HasPrefix(name, "OTEL_LOG_"),
		name == "BETA_TRACING_ENDPOINT",
		name == "ENABLE_ENHANCED_TELEMETRY_BETA":
		return true
	}
	for _, kv := range claudeTelemetrySettings("") {
		if kv[0] == name {
			return true
		}
	}
	return false
}

// withRelayEnv returns env with the relay's configuration for Claude Code:
// the variables it owns removed, then its settings added.
func withRelayEnv(env []string, endpoint string) []string {
	settings := claudeTelemetrySettings(endpoint)
	out := make([]string, 0, len(env)+len(settings))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if !claudeTelemetryOwned(name) {
			out = append(out, kv)
		}
	}
	for _, kv := range settings {
		out = append(out, kv[0]+"="+kv[1])
	}
	return out
}

// claudeSettingsArg is the --settings JSON that carries the same settings.
//
// The environment alone is not enough. Claude Code applies a settings
// file's env block over a variable exported in the environment it was
// started from, so a user's ~/.claude/settings.json could turn prompt
// logging back on or send the telemetry elsewhere. --settings ranks above
// the user, project and local files and below only the organisation's
// managed settings, and its env block applies over the launch environment
// too (code.claude.com/docs/en/settings, "Settings precedence";
// code.claude.com/docs/en/settings-reference, "env").
func claudeSettingsArg(endpoint string) string {
	env := map[string]string{}
	for _, kv := range claudeTelemetrySettings(endpoint) {
		env[kv[0]] = kv[1]
	}
	data, _ := json.Marshal(map[string]any{"env": env})
	return string(data)
}
