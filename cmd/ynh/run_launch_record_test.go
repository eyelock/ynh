//go:build unix

package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/eyelock/ynh/internal/telemetry"
	"github.com/eyelock/ynh/internal/vendor"
)

// launchTelemetryEnv makes the process's telemetry environment empty, then
// gives the test a spool folder when spool is true.
func launchTelemetryEnv(t *testing.T, spool bool) string {
	t.Helper()
	isolateRun(t)
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "OTEL_") {
			t.Setenv(name, "")
			_ = os.Unsetenv(name)
		}
	}
	for _, name := range []string{"YNR_SPOOL", "TRACEPARENT", "TRACESTATE", "YNH_LABELS"} {
		t.Setenv(name, "")
		_ = os.Unsetenv(name)
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if !spool {
		return ""
	}
	dir := filepath.Join(t.TempDir(), "spool")
	t.Setenv("YNR_SPOOL", dir)
	return dir
}

// launchWith runs `ynh run` on a throwaway harness with the vendor launch
// replaced by fn, and returns what cmdRun returned.
func launchWith(t *testing.T, vendorName string, fn func() error, extra ...string) error {
	t.Helper()
	work := t.TempDir()
	harnessDir := filepath.Join(work, "h")
	writeSkillHarness(t, harnessDir, "h")
	t.Chdir(work)
	// A vendor that needs project symlinks asks before planting them; give the
	// consent once, as a user would. It is not a launch and records nothing.
	if vendorName == "codex" || vendorName == "cursor" {
		if err := cmdRun([]string{harnessDir, "-v", vendorName, "--install"}); err != nil {
			t.Fatalf("--install: %v", err)
		}
	}
	stubLaunch(t, func(string, runArgs) error { return fn() })
	return cmdRun(append([]string{harnessDir, "-v", vendorName}, extra...))
}

func TestLaunchRecord_Outcomes(t *testing.T) {
	cases := []struct {
		name    string
		launch  error
		outcome string
		code    int64
		signal  string
		ok      bool
	}{
		{"exits 0", nil, telemetry.SessionOutcomeExited, 0, "", true},
		{"exits non-zero", &vendor.ExitError{Code: 3}, telemetry.SessionOutcomeFailed, 3, "", false},
		{"exits 130 by itself", &vendor.ExitError{Code: 130}, telemetry.SessionOutcomeFailed, 130, "", false},
		{"killed by a signal", &vendor.ExitError{Code: 128 + int(syscall.SIGTERM), Signal: syscall.SIGTERM}, telemetry.SessionOutcomeSignalled, 143, "SIGTERM", false},
		{"refused before the vendor starts", errors.New("resume: not supported"), telemetry.SessionOutcomeRefused, 1, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := launchTelemetryEnv(t, true)
			_ = launchWith(t, "claude", func() error { return tc.launch })
			recs := readSpoolRecords(t, dir)
			var started, finished, span *spoolRecord
			for i := range recs {
				switch {
				case recs[i].name == telemetry.EventSessionStarted:
					started = &recs[i]
				case recs[i].name == telemetry.EventSessionFinished:
					finished = &recs[i]
				case recs[i].name == telemetry.SpanSession:
					span = &recs[i]
				case strings.HasPrefix(recs[i].name, "ynh.run"):
					t.Errorf("ynh run wrote %s, which is agent run's alone", recs[i].name)
				}
			}
			if started == nil || finished == nil || span == nil {
				t.Fatalf("started %v, finished %v, span %v in %+v", started != nil, finished != nil, span != nil, recs)
			}
			id, _ := started.attrs[string(telemetry.AttrSessionID)].(string)
			if len(id) != 32 {
				t.Errorf("ynh.session.id = %q", id)
			}
			for _, r := range []*spoolRecord{finished, span} {
				if r.attrs[string(telemetry.AttrSessionID)] != id {
					t.Errorf("%s: session id %v, want %s", r.name, r.attrs[string(telemetry.AttrSessionID)], id)
				}
				if r.attrs[string(telemetry.AttrSessionOutcome)] != tc.outcome {
					t.Errorf("%s: outcome %v, want %s", r.name, r.attrs[string(telemetry.AttrSessionOutcome)], tc.outcome)
				}
				if got, _ := r.attrs[string(telemetry.AttrProcessExitCode)].(string); got != strconv.FormatInt(tc.code, 10) {
					t.Errorf("%s: exit code %v, want %d", r.name, r.attrs[string(telemetry.AttrProcessExitCode)], tc.code)
				}
				sig, _ := r.attrs[string(telemetry.AttrSessionSignal)].(string)
				if sig != tc.signal {
					t.Errorf("%s: signal %q, want %q", r.name, sig, tc.signal)
				}
				if r.attrs[string(telemetry.AttrSessionVendor)] != "claude" {
					t.Errorf("%s: vendor %v", r.name, r.attrs[string(telemetry.AttrSessionVendor)])
				}
			}
			if started.attrs[string(telemetry.AttrSessionOutcome)] != nil {
				t.Error("the started event carries an outcome")
			}
			for _, r := range recs {
				for k := range r.attrs {
					if strings.Contains(k, "token") || strings.Contains(k, "cost") || strings.Contains(k, "path") {
						t.Errorf("%s carries %s: no tokens, cost or paths on a launch", r.name, k)
					}
				}
			}
		})
	}
}

func TestLaunchRecord_VendorEnvironment(t *testing.T) {
	dir := launchTelemetryEnv(t, true)
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "deployment.environment.name=dev")
	var traceparent, resource, tracestate string
	if err := launchWith(t, "claude", func() error {
		traceparent, resource, tracestate = os.Getenv("TRACEPARENT"), os.Getenv("OTEL_RESOURCE_ATTRIBUTES"), os.Getenv("TRACESTATE")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	recs := readSpoolRecords(t, dir)
	var span *spoolRecord
	for i := range recs {
		if recs[i].name == telemetry.SpanSession {
			span = &recs[i]
		}
	}
	if span == nil {
		t.Fatal("no ynh.session span")
	}
	id, _ := span.attrs[string(telemetry.AttrSessionID)].(string)
	// 00-<trace id>-<span id>-<flags>: the span id is the session span's.
	parts := strings.Split(traceparent, "-")
	if len(parts) != 4 || parts[1] != span.traceID || parts[2] != span.spanID {
		t.Errorf("TRACEPARENT = %q, want trace %s span %s", traceparent, span.traceID, span.spanID)
	}
	if tracestate != "" {
		t.Errorf("TRACESTATE = %q, want none", tracestate)
	}
	if want := "deployment.environment.name=dev,ynh.session.id=" + id; resource != want {
		t.Errorf("OTEL_RESOURCE_ATTRIBUTES = %q, want %q", resource, want)
	}
}

func TestLaunchRecord_JoinsTheTraceItIsGiven(t *testing.T) {
	dir := launchTelemetryEnv(t, true)
	t.Setenv("TRACEPARENT", testTraceParent)
	if err := launchWith(t, "claude", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	for _, r := range readSpoolRecords(t, dir) {
		if r.name == telemetry.SpanSession {
			if r.traceID != "4bf92f3577b34da6a3ce929d0e0e4736" || r.parentID != "00f067aa0ba902b7" {
				t.Errorf("session span trace %s parent %s", r.traceID, r.parentID)
			}
			return
		}
	}
	t.Fatal("no ynh.session span")
}

func TestLaunchRecord_EveryVendorIsRecorded(t *testing.T) {
	for _, v := range []string{"claude", "codex", "cursor", "copilot", "opencode"} {
		t.Run(v, func(t *testing.T) {
			dir := launchTelemetryEnv(t, true)
			if err := launchWith(t, v, func() error { return nil }); err != nil {
				t.Fatal(err)
			}
			for _, r := range readSpoolRecords(t, dir) {
				if r.name == telemetry.SpanSession {
					if r.attrs[string(telemetry.AttrSessionVendor)] != v {
						t.Errorf("vendor %v, want %s", r.attrs[string(telemetry.AttrSessionVendor)], v)
					}
					return
				}
			}
			t.Fatal("no ynh.session span")
		})
	}
}

// With no destination, ynh run writes nothing and the vendor's environment is
// exactly what it was, an operator's ynh.label.* in OTEL_RESOURCE_ATTRIBUTES
// included.
func TestLaunchRecord_NoDestinationLeavesTheEnvironmentAlone(t *testing.T) {
	launchTelemetryEnv(t, false)
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "ynh.label.site=lab,deployment.environment.name=dev")
	before := os.Environ()
	var during []string
	if err := launchWith(t, "claude", func() error {
		during = os.Environ()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	slices.Sort(before)
	slices.Sort(during)
	// The run's own variables are not telemetry's; compare without them.
	drop := func(env []string) []string {
		var out []string
		for _, kv := range env {
			if strings.HasPrefix(kv, "YNH_") || strings.HasPrefix(kv, "PWD=") {
				continue
			}
			out = append(out, kv)
		}
		return out
	}
	if !slices.Equal(drop(before), drop(during)) {
		t.Errorf("the vendor's environment changed:\nbefore %v\nduring %v", drop(before), drop(during))
	}
}

func TestLaunchOutcome(t *testing.T) {
	if o, c, _ := launchOutcome(errors.New("x"), true); o != telemetry.SessionOutcomeFailed || c != 1 {
		t.Errorf("an error after the vendor started = %s %d", o, c)
	}
	if o, c, _ := launchOutcome(errors.New("x"), false); o != telemetry.SessionOutcomeRefused || c != 1 {
		t.Errorf("an error before it started = %s %d", o, c)
	}
}
