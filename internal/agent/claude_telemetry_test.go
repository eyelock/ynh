package agent

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

const testRelay = "http://127.0.0.1:4318"

// wantClaudeRelayEnv is the relay configuration Claude Code gets: exactly
// what ynr ADR-004 records as verified, then every content switch off.
var wantClaudeRelayEnv = []string{
	"CLAUDE_CODE_ENABLE_TELEMETRY=1",
	"OTEL_TRACES_EXPORTER=otlp",
	"OTEL_METRICS_EXPORTER=otlp",
	"OTEL_LOGS_EXPORTER=otlp",
	"OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf",
	"OTEL_EXPORTER_OTLP_ENDPOINT=" + testRelay,
	"CLAUDE_CODE_ENHANCED_TELEMETRY_BETA=1",
	"OTEL_LOG_USER_PROMPTS=0",
	"OTEL_LOG_ASSISTANT_RESPONSES=0",
	"OTEL_LOG_TOOL_DETAILS=0",
	"OTEL_LOG_TOOL_CONTENT=0",
	"OTEL_LOG_RAW_API_BODIES=0",
	"OTEL_LOG_MANAGED_SETTINGS=0",
	"ENABLE_BETA_TRACING_DETAILED=0",
}

// Whatever a harness passes through, the relay's settings replace it: no
// content switch, endpoint or exporter from the operator survives, and
// what the relay does not own is kept, in order.
func TestWithRelayEnv(t *testing.T) {
	in := []string{
		"YNH_AGENT_SESSION=s",
		"OTEL_LOG_USER_PROMPTS=1",
		"OTEL_LOG_RAW_API_BODIES=file:/tmp/bodies",
		"OTEL_LOG_SOMETHING_NEW=1",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=https://elsewhere.example/v1/traces",
		"OTEL_EXPORTER_OTLP_HEADERS=Authorization=Bearer x",
		"OTEL_TRACES_EXPORTER=console",
		"ENABLE_BETA_TRACING_DETAILED=1",
		"BETA_TRACING_ENDPOINT=https://elsewhere.example",
		"ENABLE_ENHANCED_TELEMETRY_BETA=1",
		"OTEL_RESOURCE_ATTRIBUTES=team=a",
		"TRACEPARENT=00-run",
		"ANTHROPIC_API_KEY=k",
	}
	got := withRelayEnv(in, testRelay)
	want := append([]string{"YNH_AGENT_SESSION=s", "OTEL_RESOURCE_ATTRIBUTES=team=a", "TRACEPARENT=00-run", "ANTHROPIC_API_KEY=k"}, wantClaudeRelayEnv...)
	if !slices.Equal(got, want) {
		t.Errorf("env:\n got %q\nwant %q", got, want)
	}
}

// The --settings JSON carries the same settings as an env block.
func TestClaudeSettingsArg(t *testing.T) {
	var got struct {
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal([]byte(claudeSettingsArg(testRelay)), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Env) != len(wantClaudeRelayEnv) {
		t.Errorf("settings env has %d entries, want %d: %v", len(got.Env), len(wantClaudeRelayEnv), got.Env)
	}
	for _, kv := range wantClaudeRelayEnv {
		name, value, _ := strings.Cut(kv, "=")
		if got.Env[name] != value {
			t.Errorf("settings env %s = %q, want %q", name, got.Env[name], value)
		}
	}
}

func TestBuildClaudeStreamArgs_TelemetrySettings(t *testing.T) {
	if args := buildClaudeStreamArgs(StartOptions{}); slices.Contains(args, "--settings") {
		t.Errorf("no relay, yet --settings: %v", args)
	}
	args := buildClaudeStreamArgs(StartOptions{TelemetryEndpoint: testRelay})
	i := slices.Index(args, "--settings")
	if i < 0 || i+1 >= len(args) || args[i+1] != claudeSettingsArg(testRelay) {
		t.Errorf("args lack --settings with the relay's settings: %v", args)
	}
}

func TestSupportsTelemetryRelay(t *testing.T) {
	for backend, want := range map[string]bool{"claude": true, "codex": false, "cursor": false, "": false} {
		if got := SupportsTelemetryRelay(backend); got != want {
			t.Errorf("SupportsTelemetryRelay(%q) = %v", backend, got)
		}
	}
}

// The loop asks for the relay once, for the backend it is about to start,
// and only a backend ynh can configure gets its settings; the trajectory
// records their names.
func TestRunLoop_RelayConfiguresOnlyClaude(t *testing.T) {
	tests := []struct {
		backend   string
		relay     string
		configure bool
	}{
		{backend: "claude", relay: testRelay, configure: true},
		{backend: "claude", relay: ""},
		{backend: "codex", relay: testRelay},
		{backend: "cursor", relay: testRelay},
	}
	for _, tt := range tests {
		t.Run(tt.backend+"/"+tt.relay, func(t *testing.T) {
			mb := &mockBackend{name: tt.backend, turns: []Turn{{Content: "done"}}}
			var stdout, stderr bytes.Buffer
			opts := baseOpts(mb, &stdout, &stderr, strings.NewReader(""))
			tel := &fakeTelemetry{relay: tt.relay}
			opts.Telemetry = tel
			if _, err := RunLoop(opts); err != nil {
				t.Fatalf("RunLoop: %v", err)
			}
			if !slices.Equal(tel.asked, []string{tt.backend}) {
				t.Errorf("relay asked for %v, want once for %s", tel.asked, tt.backend)
			}
			so := mb.startOpts[0]
			hasAll := true
			for _, kv := range wantClaudeRelayEnv {
				if !slices.Contains(so.Env, kv) {
					hasAll = false
				}
			}
			if tt.configure {
				if !hasAll || so.TelemetryEndpoint != testRelay {
					t.Errorf("claude with a relay: endpoint %q, env %v", so.TelemetryEndpoint, so.Env)
				}
				return
			}
			if so.TelemetryEndpoint != "" {
				t.Errorf("TelemetryEndpoint = %q, want none", so.TelemetryEndpoint)
			}
			for _, kv := range so.Env {
				if strings.HasPrefix(kv, "OTEL_") || strings.HasPrefix(kv, "CLAUDE_CODE_") {
					t.Errorf("%s got %s with no relay configuration for it", tt.backend, kv)
				}
			}
		})
	}
}
