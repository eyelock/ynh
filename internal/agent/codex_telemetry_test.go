package agent

import (
	"slices"
	"testing"
)

// wantCodexRelayArgs is what a codex exec gets with a relay: traces and
// metrics to it over OTLP/HTTP protobuf with full signal paths, logs off
// (they carry the account's email), and every content switch off. Verified
// with codex-cli 0.166.0 against a real ynr relay.
var wantCodexRelayArgs = []string{
	"-c", `otel.trace_exporter={otlp-http={endpoint="` + testRelay + `/v1/traces",protocol="binary"}}`,
	"-c", `otel.metrics_exporter={otlp-http={endpoint="` + testRelay + `/v1/metrics",protocol="binary"}}`,
	"-c", `otel.exporter="none"`,
	"-c", "otel.log_user_prompt=false",
	"-c", "otel.log_agent_responses=false",
	"-c", "otel.log_guardian_assessments=false",
}

func TestCodexTelemetryArgs(t *testing.T) {
	if got := codexTelemetryArgs(testRelay); !slices.Equal(got, wantCodexRelayArgs) {
		t.Errorf("args:\n got %q\nwant %q", got, wantCodexRelayArgs)
	}
}

// Every turn, fresh or resumed, carries the relay's overrides before the
// stdin marker; with no relay, none.
func TestBuildCodexArgs_TelemetryRelay(t *testing.T) {
	for _, thread := range []string{"", "thread-1"} {
		args := buildCodexArgs(StartOptions{TelemetryEndpoint: testRelay}, thread)
		if args[len(args)-1] != "-" {
			t.Fatalf("thread %q: last arg %q, want -", thread, args[len(args)-1])
		}
		tail := args[len(args)-1-len(wantCodexRelayArgs) : len(args)-1]
		if !slices.Equal(tail, wantCodexRelayArgs) {
			t.Errorf("thread %q: args %q lack the relay's overrides before -", thread, args)
		}
	}
	for _, a := range buildCodexArgs(StartOptions{}, "") {
		if a == "-c" {
			t.Errorf("no relay, no options: got a -c override in %q", buildCodexArgs(StartOptions{}, ""))
		}
	}
}

// A harness's passed-through OTLP exporter settings are dropped for Codex;
// everything else, TRACEPARENT and OTEL_RESOURCE_ATTRIBUTES included, is kept
// in order.
func TestWithBackendRelayEnv_Codex(t *testing.T) {
	in := []string{
		"YNH_AGENT_SESSION=s",
		"OTEL_EXPORTER_OTLP_ENDPOINT=https://elsewhere.example",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=https://elsewhere.example/v1/traces",
		"OTEL_EXPORTER_OTLP_HEADERS=Authorization=Bearer x",
		"OTEL_RESOURCE_ATTRIBUTES=team=a",
		"TRACEPARENT=00-run",
		"OPENAI_API_KEY=k",
	}
	got := withBackendRelayEnv("codex", in, testRelay)
	want := []string{"YNH_AGENT_SESSION=s", "OTEL_RESOURCE_ATTRIBUTES=team=a", "TRACEPARENT=00-run", "OPENAI_API_KEY=k"}
	if !slices.Equal(got, want) {
		t.Errorf("env:\n got %q\nwant %q", got, want)
	}
	if got := withBackendRelayEnv("cursor", in, testRelay); !slices.Equal(got, in) {
		t.Errorf("cursor: env changed to %q", got)
	}
}
