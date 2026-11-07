package agent

// SupportsRunRelay reports whether ynh points backend's own telemetry at a
// relay under `ynh run`, where the vendor CLI is a child of ynh and the relay
// lives as long as the session. Copilot CLI only: Claude Code and Codex are
// configured for `ynh agent run` (SupportsTelemetryRelay) and not yet here.
func SupportsRunRelay(backend string) bool {
	return backend == "copilot"
}

// RunRelayEnv is what `ynh run` adds to its own environment, which the vendor
// child inherits, to send backend's telemetry to the relay at endpoint.
//
// Copilot CLI 1.0.106 reads OTLP settings from the environment (`copilot help
// monitoring`). The exporter is pinned to OTLP/HTTP with JSON bodies, the
// combination verified against a real ynr relay, and content capture is
// pinned off (ynr ADR-006, rule 10). Copilot does not join TRACEPARENT: its
// spans start their own trace.
func RunRelayEnv(backend, endpoint string) [][2]string {
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
func RunRelayUnset(backend string) []string {
	if backend != "copilot" {
		return nil
	}
	return []string{"COPILOT_OTEL_FILE_EXPORTER_PATH"}
}
