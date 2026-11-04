package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/gate"
	"github.com/eyelock/ynh/internal/labels"
)

func labelEntries(env []string) []string {
	var out []string
	for _, kv := range env {
		if strings.HasPrefix(kv, labels.Env+"=") {
			out = append(out, kv)
		}
	}
	return out
}

// The run's labels reach the worker and every `ynh check` the loop starts as
// YNH_LABELS, with telemetry on or off; with none, nothing is added.
func TestRunLoop_LabelsReachEveryProcess(t *testing.T) {
	for _, tt := range []struct {
		name string
		set  labels.Set
		tel  *fakeTelemetry
		want []string
	}{
		{name: "none", want: nil},
		{name: "labels, telemetry off", set: labels.Set{"wheelhouse": "ynw", "batch": "7"}, want: []string{"YNH_LABELS=batch:7,wheelhouse:ynw"}},
		{name: "labels, telemetry on", set: labels.Set{"wheelhouse": "ynw"}, tel: &fakeTelemetry{}, want: []string{"YNH_LABELS=wheelhouse:ynw"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			orig := runCheckFn
			t.Cleanup(func() { runCheckFn = orig })
			var gotCheck [][]string
			runCheckFn = func(_, _, _, _ string, only []string, _ map[string]json.RawMessage, extra []string) (*gate.Envelope, error) {
				gotCheck = append(gotCheck, extra)
				return env(gate.Result{Name: only[0], Kind: "command", Status: gate.StatusPass}), nil
			}
			mb := &mockBackend{name: "mock", turns: []Turn{{Content: "done"}}}
			var stdout, stderr bytes.Buffer
			opts := baseOpts(mb, &stdout, &stderr, strings.NewReader(""))
			opts.testSensorNames = []string{"build"}
			opts.Labels = tt.set
			if tt.tel != nil {
				opts.Telemetry = tt.tel
			}
			if _, err := RunLoop(opts); err != nil {
				t.Fatalf("RunLoop: %v", err)
			}
			if got := labelEntries(mb.startOpts[0].Env); fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Errorf("worker labels = %v, want %v", got, tt.want)
			}
			if len(gotCheck) != 1 {
				t.Fatalf("%d gate calls", len(gotCheck))
			}
			if got := labelEntries(gotCheck[0]); fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Errorf("check labels = %v, want %v", got, tt.want)
			}
		})
	}
}

// The convergence verifiers' check carries the labels too.
func TestCheckConvergence_VerifierCallGetsLabels(t *testing.T) {
	restore := runCheckFn
	t.Cleanup(func() { runCheckFn = restore })
	var got []string
	runCheckFn = func(_, _, _, _ string, _ []string, _ map[string]json.RawMessage, extra []string) (*gate.Envelope, error) {
		got = extra
		return env(gate.Result{Name: "verifier", Kind: "command", Tolerance: "blocking", Status: gate.StatusPass}), nil
	}
	e := &gate.Envelope{Verdict: gate.VerdictPass}
	v := verifierRun{names: []string{"verifier"}, ynh: "ynh", harness: "local/demo", labels: labels.Set{"wheelhouse": "ynw"}}
	if converged, _, _, _ := checkConvergence(e, v, newNullTrajectory(), 1, false); !converged {
		t.Fatal("a clean verifier must converge")
	}
	if fmt.Sprint(got) != "[YNH_LABELS=wheelhouse:ynw]" {
		t.Errorf("verifier check env = %v", got)
	}
}
