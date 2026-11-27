package main

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"

	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eyelock/ynh/internal/agent"
	"github.com/eyelock/ynh/internal/config"
)

func TestStartRunRelay(t *testing.T) {
	saved := [2]time.Duration{relayReadyTimeout, relayStopTimeout}
	t.Cleanup(func() { relayReadyTimeout, relayStopTimeout = saved[0], saved[1] })
	// The stub prints its endpoint at once; the bounds only keep a hung run
	// from hanging the suite, so they are generous for a loaded machine.
	relayReadyTimeout, relayStopTimeout = 30*time.Second, 30*time.Second

	tests := []struct {
		name    string
		backend string
		flag    bool
		env     string // YNH_TELEMETRY_RELAY
		spool   string // "spool", "otlp", "otlp-protobuf"
		started bool
		note    string
	}{
		{name: "copilot with the flag", backend: "copilot", flag: true, spool: "spool", started: true},
		{name: "copilot with the variable", backend: "copilot", env: "1", spool: "spool", started: true},
		{name: "off", backend: "copilot", spool: "spool"},
		{name: "codex with the flag", backend: "codex", flag: true, spool: "spool", started: true},
		{name: "cursor is not configured under ynh run", backend: "cursor", flag: true, spool: "spool", note: "does not configure cursor's telemetry"},
		{name: "operator's OTLP", backend: "copilot", flag: true, spool: "otlp", note: "OTEL_EXPORTER_OTLP_* is set"},
		{name: "operator's OTLP over http/protobuf", backend: "copilot", flag: true, spool: "otlp-protobuf", note: "OTEL_EXPORTER_OTLP_* is set"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("YNH_HOME", t.TempDir())
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			t.Setenv("YNH_TELEMETRY_RELAY", tt.env)
			// Set in the operator's environment: the relay's settings win.
			t.Setenv("COPILOT_OTEL_FILE_EXPORTER_PATH", "/tmp/elsewhere.jsonl")
			t.Setenv("COPILOT_OTEL_ENABLED", "")
			t.Setenv("OTEL_INSTRUMENTATION_GENAI_CAPTURE_CONTENT", "SPAN_ONLY")
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
			stub := installStubYnr(t, "serve")
			switch tt.spool {
			case "spool":
				t.Setenv("YNR_SPOOL", filepath.Join(t.TempDir(), "spool"))
			case "otlp":
				t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector.example:4318")
			case "otlp-protobuf":
				t.Setenv("YNR_SPOOL", filepath.Join(t.TempDir(), "spool"))
				t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector.example:4318")
				t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
			}

			var stderr bytes.Buffer
			stop, args := startRunRelay(tt.backend, tt.flag, "", nil, &stderr)
			if got := stub.started(); got != tt.started {
				t.Fatalf("relay started = %v, want %v (stderr %q)", got, tt.started, stderr.String())
			}
			if tt.started && tt.backend == "codex" {
				// Codex takes its settings as -c overrides and nothing in the environment.
				joined := strings.Join(args, " ")
				for _, w := range []string{relayStubEndpoint + "/v1/traces", relayStubEndpoint + "/v1/metrics", `otel.exporter="none"`, "otel.log_user_prompt=false"} {
					if !strings.Contains(joined, w) {
						t.Errorf("codex args %q lack %q", joined, w)
					}
				}
				if got := os.Getenv("OTEL_INSTRUMENTATION_GENAI_CAPTURE_CONTENT"); got != "SPAN_ONLY" {
					t.Errorf("environment changed for codex: %q", got)
				}
				pid := stub.pidValue(t)
				stop()
				if !gone(pid, 3*time.Second) {
					t.Error("the relay outlived stop")
				}
			} else if tt.started {
				if len(args) != 0 {
					t.Errorf("copilot got arguments %q", args)
				}
				want := map[string]string{
					"COPILOT_OTEL_ENABLED":                       "true",
					"OTEL_EXPORTER_OTLP_ENDPOINT":                relayStubEndpoint,
					"OTEL_INSTRUMENTATION_GENAI_CAPTURE_CONTENT": "NO_CONTENT",
				}
				for k, v := range want {
					if got := os.Getenv(k); got != v {
						t.Errorf("%s = %q, want %q", k, got, v)
					}
				}
				if _, set := os.LookupEnv("COPILOT_OTEL_FILE_EXPORTER_PATH"); set {
					t.Error("COPILOT_OTEL_FILE_EXPORTER_PATH survived")
				}
				pid := stub.pidValue(t)
				stop()
				if !gone(pid, 3*time.Second) {
					t.Error("the relay outlived stop")
				}
			} else {
				stop()
				if len(args) != 0 {
					t.Errorf("arguments without a relay: %q", args)
				}
				if got := os.Getenv("OTEL_INSTRUMENTATION_GENAI_CAPTURE_CONTENT"); got != "SPAN_ONLY" {
					t.Errorf("environment changed without a relay: %q", got)
				}
			}
			if tt.note != "" && !strings.Contains(stderr.String(), tt.note) {
				t.Errorf("stderr %q lacks %q", stderr.String(), tt.note)
			}
			if tt.note == "" && stderr.Len() != 0 {
				t.Errorf("unexpected stderr %q", stderr.String())
			}
		})
	}
}

func TestParseRunArgs_TelemetryRelay(t *testing.T) {
	ra := parseRunArgs([]string{"h", "--telemetry-relay", "-v", "copilot", "--", "go"})
	if !ra.Relay {
		t.Error("--telemetry-relay not parsed")
	}
	for _, a := range ra.VendorArgs {
		if a == "--telemetry-relay" {
			t.Error("--telemetry-relay was forwarded to the vendor")
		}
	}
}

// claudeSettingsOf parses the --settings JSON that `ynh run` hands Claude Code.
func claudeSettingsOf(t *testing.T, args []string) map[string]string {
	t.Helper()
	if len(args) != 2 || args[0] != "--settings" {
		t.Fatalf("claude args = %q, want --settings <json>", args)
	}
	var doc struct {
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal([]byte(args[1]), &doc); err != nil {
		t.Fatalf("--settings is not JSON: %v", err)
	}
	return doc.Env
}

func TestStartRunRelay_Claude(t *testing.T) {
	saved := [2]time.Duration{relayReadyTimeout, relayStopTimeout}
	t.Cleanup(func() { relayReadyTimeout, relayStopTimeout = saved[0], saved[1] })
	relayReadyTimeout, relayStopTimeout = 30*time.Second, 30*time.Second

	setup := func(t *testing.T) stubRelay {
		t.Helper()
		t.Setenv("YNH_HOME", t.TempDir())
		t.Setenv("XDG_STATE_HOME", t.TempDir())
		t.Setenv("YNH_TELEMETRY_RELAY", "")
		t.Setenv("YNH_TELEMETRY_RELAY_ENDPOINT", "")
		// startRunRelay sets variables with os.Setenv; registering each one
		// here has t restore it, so one case does not leak into the next.
		for _, kv := range agent.RunRelayEnv("claude", "") {
			t.Setenv(kv[0], "")
		}
		t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
		t.Setenv("OTEL_LOG_USER_PROMPTS", "1") // inherited: content on
		t.Setenv("CLAUDE_CODE_ENABLE_TELEMETRY", "")
		return installStubYnr(t, "serve")
	}

	t.Run("per-launch relay: exact environment and settings, stopped on exit", func(t *testing.T) {
		stub := setup(t)
		t.Setenv("YNR_SPOOL", filepath.Join(t.TempDir(), "spool"))
		var stderr bytes.Buffer
		stop, args := startRunRelay("claude", true, "", nil, &stderr)
		if !stub.started() {
			t.Fatalf("no relay started (stderr %q)", stderr.String())
		}
		want := map[string]string{}
		for _, kv := range agent.RunRelayEnv("claude", relayStubEndpoint) {
			want[kv[0]] = kv[1]
		}
		for k, v := range want {
			if got := os.Getenv(k); got != v {
				t.Errorf("%s = %q, want %q", k, got, v)
			}
		}
		if got := os.Getenv("OTEL_LOG_USER_PROMPTS"); got != "0" {
			t.Errorf("inherited OTEL_LOG_USER_PROMPTS survived: %q", got)
		}
		env := claudeSettingsOf(t, args)
		if !reflect.DeepEqual(env, want) {
			t.Errorf("--settings env = %v, want %v", env, want)
		}
		if env["OTEL_EXPORTER_OTLP_ENDPOINT"] != relayStubEndpoint {
			t.Errorf("endpoint = %q", env["OTEL_EXPORTER_OTLP_ENDPOINT"])
		}
		pid := stub.pidValue(t)
		stop()
		if !gone(pid, 3*time.Second) {
			t.Error("the relay outlived stop")
		}
		if stderr.Len() != 0 {
			t.Errorf("unexpected stderr %q", stderr.String())
		}
	})

	t.Run("standing endpoint: pointed at it, no relay starts", func(t *testing.T) {
		stub := setup(t)
		t.Setenv("YNR_SPOOL", filepath.Join(t.TempDir(), "spool"))
		var stderr bytes.Buffer
		stop, args := startRunRelay("claude", false, "http://127.0.0.1:4318", nil, &stderr)
		defer stop()
		if stub.started() {
			t.Fatal("a relay started with a standing endpoint")
		}
		if got := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); got != "http://127.0.0.1:4318" {
			t.Errorf("OTEL_EXPORTER_OTLP_ENDPOINT = %q", got)
		}
		if got := os.Getenv("OTEL_LOG_USER_PROMPTS"); got != "0" {
			t.Errorf("OTEL_LOG_USER_PROMPTS = %q", got)
		}
		if env := claudeSettingsOf(t, args); env["OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"] != "http://127.0.0.1:4318/v1/logs" {
			t.Errorf("logs endpoint = %q", env["OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"])
		}
		if stderr.Len() != 0 {
			t.Errorf("unexpected stderr %q", stderr.String())
		}
	})

	t.Run("standing endpoint from the variable and from config", func(t *testing.T) {
		setup(t)
		t.Setenv(telemetryRelayEndpointEnv, "http://127.0.0.1:5000")
		if got := standingRelayEndpoint("", io.Discard); got != "http://127.0.0.1:5000" {
			t.Errorf("from the variable: %q", got)
		}
		if got := standingRelayEndpoint("http://127.0.0.1:6000", io.Discard); got != "http://127.0.0.1:6000" {
			t.Errorf("the flag must win: %q", got)
		}
		t.Setenv(telemetryRelayEndpointEnv, "")
		cfg := `{"telemetry_relay_endpoint": "http://[::1]:7000"}`
		if err := os.WriteFile(config.ConfigPath(), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := standingRelayEndpoint("", io.Discard); got != "http://[::1]:7000" {
			t.Errorf("from config: %q", got)
		}
	})

	t.Run("a standing endpoint off the loopback is refused", func(t *testing.T) {
		stub := setup(t)
		var stderr bytes.Buffer
		stop, args := startRunRelay("claude", false, "http://collector.example:4318", nil, &stderr)
		defer stop()
		if stub.started() || len(args) != 0 {
			t.Errorf("started %v, args %q", stub.started(), args)
		}
		if os.Getenv("OTEL_LOG_USER_PROMPTS") != "1" {
			t.Error("environment changed")
		}
		if !strings.Contains(stderr.String(), "not on a loopback address") {
			t.Errorf("stderr %q", stderr.String())
		}
	})

	t.Run("operator's OTEL_EXPORTER_OTLP_* wins over both routes", func(t *testing.T) {
		for _, tt := range []struct {
			name, endpoint string
			flag           bool
		}{{"relay", "", true}, {"standing", "http://127.0.0.1:4318", false}} {
			t.Run(tt.name, func(t *testing.T) {
				stub := setup(t)
				t.Setenv("YNR_SPOOL", filepath.Join(t.TempDir(), "spool"))
				t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector.example:4318")
				var stderr bytes.Buffer
				stop, args := startRunRelay("claude", tt.flag, tt.endpoint, nil, &stderr)
				defer stop()
				if stub.started() || len(args) != 0 {
					t.Errorf("started %v, args %q", stub.started(), args)
				}
				if got := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); got != "http://collector.example:4318" {
					t.Errorf("the operator's endpoint changed: %q", got)
				}
				if os.Getenv("CLAUDE_CODE_ENABLE_TELEMETRY") != "" || os.Getenv("OTEL_LOG_USER_PROMPTS") != "1" {
					t.Error("environment changed")
				}
				if !strings.Contains(stderr.String(), "OTEL_EXPORTER_OTLP_* is set") {
					t.Errorf("stderr %q", stderr.String())
				}
			})
		}
	})

	t.Run("neither: the environment is unchanged and nothing starts", func(t *testing.T) {
		stub := setup(t)
		t.Setenv("YNR_SPOOL", filepath.Join(t.TempDir(), "spool"))
		before := os.Environ()
		var stderr bytes.Buffer
		stop, args := startRunRelay("claude", false, "", nil, &stderr)
		stop()
		if stub.started() || len(args) != 0 || stderr.Len() != 0 {
			t.Errorf("started %v, args %q, stderr %q", stub.started(), args, stderr.String())
		}
		if !reflect.DeepEqual(before, os.Environ()) {
			t.Error("the environment changed")
		}
	})

	t.Run("unsupported vendor with a standing endpoint: a note, no change", func(t *testing.T) {
		setup(t)
		var stderr bytes.Buffer
		stop, args := startRunRelay("opencode", false, "http://127.0.0.1:4318", nil, &stderr)
		defer stop()
		if len(args) != 0 || !strings.Contains(stderr.String(), "does not configure opencode's telemetry") {
			t.Errorf("args %q stderr %q", args, stderr.String())
		}
	})
}

func TestParseRunArgs_TelemetryRelayEndpoint(t *testing.T) {
	for _, args := range [][]string{
		{"h", "--telemetry-relay-endpoint", "http://127.0.0.1:4318", "-v", "claude"},
		{"h", "--telemetry-relay-endpoint=http://127.0.0.1:4318", "-v", "claude"},
	} {
		ra := parseRunArgs(args)
		if ra.RelayEndpoint != "http://127.0.0.1:4318" {
			t.Errorf("%q: endpoint = %q", args, ra.RelayEndpoint)
		}
		for _, a := range ra.VendorArgs {
			if strings.Contains(a, "telemetry-relay-endpoint") || a == "http://127.0.0.1:4318" {
				t.Errorf("%q: forwarded %q to the vendor", args, a)
			}
		}
	}
}
