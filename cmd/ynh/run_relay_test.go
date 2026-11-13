package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStartRunRelay(t *testing.T) {
	saved := [2]time.Duration{relayReadyTimeout, relayStopTimeout}
	t.Cleanup(func() { relayReadyTimeout, relayStopTimeout = saved[0], saved[1] })
	relayReadyTimeout, relayStopTimeout = 500*time.Millisecond, 500*time.Millisecond

	tests := []struct {
		name    string
		backend string
		flag    bool
		env     string // YNH_TELEMETRY_RELAY
		spool   string // "spool", "otlp"
		started bool
		note    string
	}{
		{name: "copilot with the flag", backend: "copilot", flag: true, spool: "spool", started: true},
		{name: "copilot with the variable", backend: "copilot", env: "1", spool: "spool", started: true},
		{name: "off", backend: "copilot", spool: "spool"},
		{name: "codex with the flag", backend: "codex", flag: true, spool: "spool", started: true},
		{name: "claude is not configured under ynh run", backend: "claude", flag: true, spool: "spool", note: "does not configure claude's telemetry"},
		{name: "operator's OTLP", backend: "copilot", flag: true, spool: "otlp", note: "OTEL_EXPORTER_OTLP_* is set"},
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
			}

			var stderr bytes.Buffer
			stop, args := startRunRelay(tt.backend, tt.flag, &stderr)
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
