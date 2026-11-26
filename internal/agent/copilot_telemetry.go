package agent

import (
	"os"
	"strings"
)

// SupportsRunRelay reports whether ynh points backend's own telemetry at a
// relay under `ynh run`, where the vendor CLI is a child of ynh and the relay
// lives as long as the session. Claude Code, Copilot CLI and Codex; Cursor
// and OpenCode cannot send their telemetry to a relay (vendor parity, L19).
func SupportsRunRelay(backend string) bool {
	return backend == "claude" || backend == "copilot" || backend == "codex"
}

// RunRelayArgs is what `ynh run` adds to the vendor's command line to send
// backend's telemetry to the relay at endpoint. Codex takes its settings as
// -c overrides, not from the environment; they are the ones `ynh agent run`
// gives it (codexTelemetryArgs), content and logs off. Claude Code gets the
// same settings as `ynh agent run` as --settings JSON, which outranks the
// user's own settings files (claudeSettingsArg). Others take none.
func RunRelayArgs(backend, endpoint string) []string {
	switch backend {
	case "codex":
		return codexTelemetryArgs(endpoint)
	case "claude":
		return []string{"--settings", claudeSettingsArg(endpoint)}
	}
	return nil
}

// RunRelayEnv is what `ynh run` adds to its own environment, which the vendor
// child inherits, to send backend's telemetry to the relay at endpoint.
//
// Copilot CLI 1.0.106 reads OTLP settings from the environment (`copilot help
// monitoring`). The exporter is pinned to OTLP/HTTP with JSON bodies, the
// combination verified against a real ynr relay, and content capture is
// pinned off (ynr ADR-006, rule 10). Copilot does not join TRACEPARENT: its
// spans start their own trace.
//
// Claude Code gets the settings `ynh agent run` gives it
// (claudeTelemetrySettings), content switches off, in the environment as well
// as in --settings (RunRelayArgs).
func RunRelayEnv(backend, endpoint string) [][2]string {
	if backend == "claude" {
		return claudeTelemetrySettings(endpoint)
	}
	if backend != "copilot" {
		return nil
	}
	return [][2]string{
		{"COPILOT_OTEL_ENABLED", "true"},
		{"COPILOT_OTEL_EXPORTER_TYPE", "otlp-http"},
		{"OTEL_EXPORTER_OTLP_ENDPOINT", endpoint},
		{"OTEL_EXPORTER_OTLP_PROTOCOL", "http/json"},
		{"OTEL_INSTRUMENTATION_GENAI_CAPTURE_CONTENT", "NO_CONTENT"},
		{"OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT", "false"},
	}
}

// RunRelayUnset names the variables `ynh run` removes from its environment
// before adding RunRelayEnv, because they would send Copilot's telemetry to a
// file instead of the relay. Operator OTEL_EXPORTER_OTLP_* never reach here:
// they mean the telemetry is the operator's, and no relay starts.
//
// For Claude Code it is every variable the relay's configuration owns that
// ynh's environment already carries (claudeTelemetryOwned), so an inherited
// OTEL_LOG_USER_PROMPTS=1 cannot turn content back on.
func RunRelayUnset(backend string) []string {
	switch backend {
	case "copilot":
		return []string{"COPILOT_OTEL_FILE_EXPORTER_PATH"}
	case "claude":
		var names []string
		for _, kv := range os.Environ() {
			if name, _, _ := strings.Cut(kv, "="); claudeTelemetryOwned(name) {
				names = append(names, name)
			}
		}
		return names
	}
	return nil
}
