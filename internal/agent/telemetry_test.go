package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/gate"
)

// fakeTelemetry hands out recognisable environments and records each call.
type fakeTelemetry struct {
	calls    []string
	outcomes []string
}

func (f *fakeTelemetry) WorkerEnv() []string {
	return []string{"TRACEPARENT=worker", "YNR_SPOOL=/spool"}
}

func (f *fakeTelemetry) StartCall(kind string, turn int, sensor string) ([]string, func(string, bool)) {
	id := fmt.Sprintf("%s-%d-%s", kind, turn, sensor)
	f.calls = append(f.calls, id)
	return []string{"TRACEPARENT=" + id, "YNR_SPOOL=/spool"}, func(outcome string, ok bool) {
		f.outcomes = append(f.outcomes, fmt.Sprintf("%s:%s:%v", id, outcome, ok))
	}
}

// The worker and every `ynh check` the loop runs get the telemetry's
// environment; with none, they get nothing added.
func TestRunLoop_TelemetryEnvReachesEveryProcess(t *testing.T) {
	tests := []struct {
		name       string
		tel        *fakeTelemetry
		wantWorker []string
		wantCheck  [][]string
	}{
		{name: "telemetry off", wantCheck: [][]string{nil}},
		{
			name:       "telemetry on",
			tel:        &fakeTelemetry{},
			wantWorker: []string{"TRACEPARENT=worker", "YNR_SPOOL=/spool"},
			wantCheck:  [][]string{{"TRACEPARENT=check-1-", "YNR_SPOOL=/spool"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := runCheckFn
			t.Cleanup(func() { runCheckFn = orig })
			var gotCheck [][]string
			runCheckFn = func(_, _, _ string, only []string, _ map[string]json.RawMessage, extra []string) (*gate.Envelope, error) {
				gotCheck = append(gotCheck, extra)
				return env(gate.Result{Name: only[0], Kind: "command", Status: gate.StatusPass}), nil
			}
			mb := &mockBackend{name: "mock", turns: []Turn{{Content: "done"}}}
			var stdout, stderr bytes.Buffer
			opts := baseOpts(mb, &stdout, &stderr, strings.NewReader(""))
			opts.testSensorNames = []string{"build"}
			if tt.tel != nil {
				opts.Telemetry = tt.tel
			}
			if _, err := RunLoop(opts); err != nil {
				t.Fatalf("RunLoop: %v", err)
			}

			var telemetryEnv []string
			for _, kv := range mb.startOpts[0].Env {
				if strings.HasPrefix(kv, "TRACE") || strings.HasPrefix(kv, "YNR_") {
					telemetryEnv = append(telemetryEnv, kv)
				}
			}
			if fmt.Sprint(telemetryEnv) != fmt.Sprint(tt.wantWorker) {
				t.Errorf("worker telemetry env = %v, want %v", telemetryEnv, tt.wantWorker)
			}
			if fmt.Sprint(gotCheck) != fmt.Sprint(tt.wantCheck) {
				t.Errorf("check env = %v, want %v", gotCheck, tt.wantCheck)
			}
			if tt.tel != nil && fmt.Sprint(tt.tel.outcomes) != "[check-1-:pass:true]" {
				t.Errorf("call outcomes = %v, want the gate's verdict", tt.tel.outcomes)
			}
		})
	}
}

// The convergence verifier's sensors run is a call of its own.
func TestCheckConvergence_SensorCallGetsTelemetryEnv(t *testing.T) {
	restore := runSensorFn
	t.Cleanup(func() { runSensorFn = restore })
	var got []string
	runSensorFn = func(_, _, _, _, _ string, extra []string) (*SensorResult, error) {
		got = extra
		return &SensorResult{Kind: "command", ExitCode: 0}, nil
	}
	tel := &fakeTelemetry{}
	e := &gate.Envelope{Verdict: gate.VerdictPass}
	if converged, _, _ := checkConvergence(e, "verifier", "ynh", "local/demo", t.TempDir(), newNullTrajectory(), tel, 3, false); !converged {
		t.Fatal("a clean verifier must converge")
	}
	if fmt.Sprint(got) != "[TRACEPARENT=sensor-3-verifier YNR_SPOOL=/spool]" {
		t.Errorf("sensors run env = %v", got)
	}
	if fmt.Sprint(tel.outcomes) != "[sensor-3-verifier:pass:true]" {
		t.Errorf("outcomes = %v", tel.outcomes)
	}
}

func TestChildEnv(t *testing.T) {
	t.Setenv("TRACEPARENT", "operator")
	if got := childEnv(nil); got != nil {
		t.Errorf("childEnv(nil) = %v, want nil so the child inherits as before", got)
	}
	got := childEnv([]string{"TRACEPARENT=run"})
	if got[len(got)-1] != "TRACEPARENT=run" || len(got) != len(os.Environ())+1 {
		t.Errorf("childEnv adds after the inherited environment, so it wins; got %d entries ending %q", len(got), got[len(got)-1])
	}
}

func TestResumedIdentity(t *testing.T) {
	dir := t.TempDir()
	if err := writeCheckpoint(dir, &Checkpoint{
		Version: 1, SessionID: "s", Backend: "claude",
		HarnessName: "local/h", Profile: "ci", Focus: "review",
	}); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		opts RunOptions
		want string // harness/profile/focus
	}{
		{name: "not a resume", opts: RunOptions{Focus: "f"}, want: "//f"},
		{name: "restored", opts: RunOptions{Resume: dir}, want: "local/h/ci/review"},
		{name: "flags win", opts: RunOptions{Resume: dir, HarnessName: "local/x", Profile: "p"}, want: "local/x/p/review"},
		{name: "a task given restores no focus", opts: RunOptions{Resume: dir, Task: "t"}, want: "local/h/ci/"},
		{name: "unreadable checkpoint", opts: RunOptions{Resume: t.TempDir()}, want: "//"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResumedIdentity(tt.opts)
			if s := got.HarnessName + "/" + got.Profile + "/" + got.Focus; s != tt.want {
				t.Errorf("got %s, want %s", s, tt.want)
			}
		})
	}
}
