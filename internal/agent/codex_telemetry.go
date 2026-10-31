package agent

import "strings"

// codexTelemetryArgs are the -c overrides that point a codex exec at the
// run's relay, as verified with codex-cli 0.166.0 against a real ynr relay
// (ynw-aob):
//
//   - Traces and metrics go to the relay over OTLP/HTTP protobuf ("binary"),
//     each with its full signal path: Codex's otlp-http endpoint is the URL
//     it posts to, not a base. Codex reads TRACEPARENT (and TRACESTATE) from
//     its environment and parents its spans on it, so the run's span, which
//     the worker's environment already carries, is the parent of codex.exec.
//   - Logs are off (otel.exporter = "none"). Every Codex log record carries
//     the signed-in account's email address and account id as attributes,
//     with no switch to leave them out, and contract rule 10 lets people
//     appear only by their handle, never by email.
//   - Every content switch Codex has is forced off. With logs off they have
//     nothing to act on; they are pinned so turning logs on later cannot
//     carry content.
//
// A -c override outranks ~/.codex/config.toml and its profiles, and each
// exporter is given as a whole inline table, so a user's [otel] block
// cannot move an endpoint, add headers or turn content on. An
// OTEL_EXPORTER_OTLP_*_ENDPOINT in the environment did not move them either.
func codexTelemetryArgs(endpoint string) []string {
	exporter := func(path string) string {
		return `{otlp-http={endpoint="` + endpoint + path + `",protocol="binary"}}`
	}
	return []string{
		"-c", "otel.trace_exporter=" + exporter("/v1/traces"),
		"-c", "otel.metrics_exporter=" + exporter("/v1/metrics"),
		"-c", `otel.exporter="none"`,
		"-c", "otel.log_user_prompt=false",
		"-c", "otel.log_agent_responses=false",
		"-c", "otel.log_guardian_assessments=false",
	}
}

// withCodexRelayEnv returns env without the OTEL_EXPORTER_OTLP_* variables
// a harness's env_passthrough would otherwise hand to Codex. The -c
// overrides already win over them for the endpoints; dropping them keeps an
// operator's headers, timeouts or compression from riding along to the
// relay. Nothing is added: Codex takes its settings on the command line.
func withCodexRelayEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if !strings.HasPrefix(kv, "OTEL_EXPORTER_OTLP_") {
			out = append(out, kv)
		}
	}
	return out
}
