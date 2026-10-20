package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eyelock/ynh/internal/gate"
)

// checkCall is one recorded runCheckFn call.
type checkCall struct {
	only    []string
	overlay map[string]json.RawMessage
}

// stubVerifierCheck replaces runCheckFn with a fake that answers every call with the
// given results, or the given error, and records the calls.
func stubVerifierCheck(t *testing.T, results []gate.Result, err error) *[]checkCall {
	t.Helper()
	orig := runCheckFn
	t.Cleanup(func() { runCheckFn = orig })
	var calls []checkCall
	runCheckFn = func(_, _, _ string, only []string, overlay map[string]json.RawMessage, _ []string) (*gate.Envelope, error) {
		calls = append(calls, checkCall{only: only, overlay: overlay})
		if err != nil {
			return nil, err
		}
		return env(results...), nil
	}
	return &calls
}

func cmdResult(name, status string) gate.Result {
	r := gate.Result{Name: name, Kind: "command", Tolerance: "blocking", Status: status}
	switch status {
	case gate.StatusFail:
		r.ExitCode, r.NewCount, r.NewOutput = 1, 1, name+": it is wrong"
	case gate.StatusKnown:
		r.ExitCode, r.KnownCount = 1, 4
	}
	return r
}

func TestCheckConvergence_Verifiers(t *testing.T) {
	green := &gate.Envelope{Verdict: gate.VerdictPass}
	tests := []struct {
		name         string
		results      []gate.Result
		checkErr     error
		wantConv     bool
		wantSensor   string // RunConvergence.Sensor
		wantInReason string
	}{
		{
			name:       "all pass",
			results:    []gate.Result{cmdResult("a", gate.StatusPass), cmdResult("b", gate.StatusPass)},
			wantConv:   true,
			wantSensor: "a",
		},
		{
			name:         "one failing blocks and its output reaches the worker",
			results:      []gate.Result{cmdResult("a", gate.StatusPass), cmdResult("b", gate.StatusFail)},
			wantSensor:   "b",
			wantInReason: "b: it is wrong",
		},
		{
			name:       "baseline-forgiven failures converge",
			results:    []gate.Result{cmdResult("a", gate.StatusKnown), cmdResult("b", gate.StatusPass)},
			wantConv:   true,
			wantSensor: "a",
		},
		{
			name:         "a verifier absent from the report did not run",
			results:      []gate.Result{cmdResult("a", gate.StatusPass)},
			wantSensor:   "b",
			wantInReason: "regular sensors passed",
		},
		{
			name:         "a verifier that was skipped did not run",
			results:      []gate.Result{cmdResult("a", gate.StatusPass), cmdResult("b", gate.StatusSkipped)},
			wantSensor:   "b",
			wantInReason: "convergence verifiers did not",
		},
		{
			name: "a deferred verifier is no verdict",
			results: []gate.Result{cmdResult("a", gate.StatusPass),
				{Name: "b", Kind: "focus", Tolerance: "blocking", Status: gate.StatusDeferred}},
			wantSensor: "b",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := stubVerifierCheck(t, tt.results, tt.checkErr)
			v := verifierRun{
				names: []string{"a", "b"}, ynh: "ynh", harness: "local/demo", cwd: t.TempDir(),
				overlay: map[string]json.RawMessage{"a": json.RawMessage(`{}`)},
			}
			converged, feedback, conv, err := checkConvergence(green, v, newNullTrajectory(), 2, false)
			if err != nil {
				t.Fatal(err)
			}
			if converged != tt.wantConv {
				t.Errorf("converged = %v, want %v (feedback %q)", converged, tt.wantConv, feedback)
			}
			if len(*calls) != 1 || !reflect.DeepEqual((*calls)[0].only, []string{"a", "b"}) {
				t.Errorf("check calls = %+v, want one call with --only a,b", *calls)
			}
			if len(*calls) == 1 && len((*calls)[0].overlay) != 1 {
				t.Errorf("the sensor overlay did not reach the verifiers' check")
			}
			if conv == nil || conv.Sensor != tt.wantSensor || conv.Passed != tt.wantConv {
				t.Errorf("convergence = %+v, want sensor %q passed %v", conv, tt.wantSensor, tt.wantConv)
			}
			if conv != nil && len(conv.Verifiers) != 2 {
				t.Errorf("verifiers = %+v, want both listed", conv.Verifiers)
			}
			if !converged && feedback == "" {
				t.Error("a refusal must say why")
			}
			if !strings.Contains(feedback, tt.wantInReason) {
				t.Errorf("feedback %q does not contain %q", feedback, tt.wantInReason)
			}
		})
	}
}

// Both verifiers are reported, with their real kind and status, and the
// trajectory carries one sensor_run and one sensor_result for each, in name
// order, naming the role rather than a made-up kind.
func TestCheckConvergence_VerifiersTrajectoryAndList(t *testing.T) {
	stubVerifierCheck(t, []gate.Result{
		{Name: "a", Kind: "command", Tolerance: "blocking", Status: gate.StatusKnown, ExitCode: 1, KnownCount: 4, DurationMS: 7, ToolVersion: "1.2"},
		cmdResult("b", gate.StatusFail),
	}, nil)
	var buf bytes.Buffer
	v := verifierRun{names: []string{"a", "b"}, ynh: "ynh", harness: "local/demo"}
	_, _, conv, err := checkConvergence(&gate.Envelope{Verdict: gate.VerdictPass}, v, NewTrajectoryWriter(&buf), 2, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []VerifierResult{
		{Sensor: "a", Kind: "command", Status: gate.StatusKnown, Passed: true, Summary: resultSummary(cmdResultKnown())},
		{Sensor: "b", Kind: "command", Status: gate.StatusFail, Passed: false, Summary: "b: it is wrong"},
	}
	if !reflect.DeepEqual(conv.Verifiers, want) {
		t.Errorf("verifiers = %+v\nwant       %+v", conv.Verifiers, want)
	}

	events := parseTrajectory(t, &buf)
	var runs []string
	var results []map[string]any
	for _, e := range events {
		switch e.Kind {
		case KindSensorRun:
			runs = append(runs, e.Data.(string))
		case KindSensorResult:
			results = append(results, e.Data.(map[string]any))
		}
	}
	if !reflect.DeepEqual(runs, []string{"a", "b"}) {
		t.Errorf("sensor_run events = %v", runs)
	}
	if len(results) != 2 {
		t.Fatalf("sensor_result events = %v", results)
	}
	for i, r := range results {
		if r["kind"] != "command" || r["role"] != "convergence-verifier" {
			t.Errorf("result %d = %v, want the real kind and the verifier role", i, r)
		}
	}
	if results[0]["status"] != gate.StatusKnown || results[0]["passed"] != true ||
		results[0]["tool_version"] != "1.2" || results[0]["known_count"] != float64(4) {
		t.Errorf("result a = %v", results[0])
	}
	if results[1]["status"] != gate.StatusFail || results[1]["passed"] != false || results[1]["new_count"] != float64(1) {
		t.Errorf("result b = %v", results[1])
	}
}

func cmdResultKnown() gate.Result {
	return gate.Result{Name: "a", Kind: "command", Tolerance: "blocking", Status: gate.StatusKnown, KnownCount: 4}
}

// A spent budget ends the run before the verifiers are run at all.
func TestCheckConvergence_StopBeforeVerifiers(t *testing.T) {
	calls := stubVerifierCheck(t, nil, nil)
	stop := errors.New("out of wall clock")
	v := verifierRun{names: []string{"a"}, ynh: "ynh", harness: "local/demo", stop: func() error { return stop }}
	converged, _, _, err := checkConvergence(&gate.Envelope{Verdict: gate.VerdictPass}, v, newNullTrajectory(), 1, false)
	if converged || !errors.Is(err, stop) {
		t.Errorf("converged %v err %v, want the stop error", converged, err)
	}
	if len(*calls) != 0 {
		t.Errorf("verifiers ran %d times after the stop", len(*calls))
	}
}

// A blocked regular gate never reaches the verifiers.
func TestCheckConvergence_BlockedGateSkipsVerifiers(t *testing.T) {
	calls := stubVerifierCheck(t, nil, nil)
	e := env(cmdResult("lint", gate.StatusFail))
	v := verifierRun{names: []string{"a"}, ynh: "ynh", harness: "local/demo"}
	if converged, _, conv, _ := checkConvergence(e, v, newNullTrajectory(), 1, false); converged || conv != nil {
		t.Errorf("converged %v conv %+v on a blocked gate", converged, conv)
	}
	if len(*calls) != 0 {
		t.Errorf("verifiers ran after a blocked gate")
	}
}

// No verifier declared: convergence is the gate's alone, as it always was.
func TestCheckConvergence_NoVerifiersNoConvergenceField(t *testing.T) {
	calls := stubVerifierCheck(t, nil, nil)
	v := verifierRun{ynh: "ynh", harness: "local/demo"}
	converged, _, conv, _ := checkConvergence(&gate.Envelope{Verdict: gate.VerdictPass}, v, newNullTrajectory(), 1, false)
	if !converged || conv != nil || len(*calls) != 0 {
		t.Errorf("converged %v conv %+v calls %d", converged, conv, len(*calls))
	}
}

func commandSensor(role string) map[string]any {
	s := map[string]any{"source": map[string]any{"command": "true"}, "output": map[string]any{"format": "text"}}
	if role != "" {
		s["role"] = role
	}
	return s
}

// verifierLoop runs the loop over a harness with a regular sensor, two
// verifiers declared by role, and a stub check that answers the gate and the
// verifiers' call separately.
func verifierLoop(t *testing.T, mb *mockBackend, verifierResults []gate.Result, tweak func(*RunOptions)) (*RunResult, *[]checkCall, *bytes.Buffer, error) {
	t.Helper()
	id := installTestHarness(t, map[string]any{
		"build": commandSensor(""),
		"zeta":  commandSensor("convergence-verifier"),
		"alpha": commandSensor("convergence-verifier"),
	})
	orig := runCheckFn
	t.Cleanup(func() { runCheckFn = orig })
	var calls []checkCall
	runCheckFn = func(_, _, _ string, only []string, overlay map[string]json.RawMessage, _ []string) (*gate.Envelope, error) {
		calls = append(calls, checkCall{only: only, overlay: overlay})
		if len(only) == 1 && only[0] == "build" {
			return env(cmdResult("build", gate.StatusPass)), nil
		}
		return env(verifierResults...), nil
	}
	var stdout, stderr bytes.Buffer
	opts := baseOpts(mb, &stdout, &stderr, strings.NewReader(""))
	opts.HarnessName = id
	opts.WorktreeDir = t.TempDir()
	opts.EmitJSONL = "-"
	if tweak != nil {
		tweak(&opts)
	}
	res, err := RunLoop(opts)
	return res, &calls, &stdout, err
}

func TestRunLoop_RunsEveryConvergenceVerifierInNameOrder(t *testing.T) {
	mb := &mockBackend{name: "mock", turns: []Turn{{Content: "done"}}}
	res, calls, _, err := verifierLoop(t, mb,
		[]gate.Result{cmdResult("alpha", gate.StatusPass), cmdResult("zeta", gate.StatusPass)}, nil)
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if len(*calls) != 2 ||
		!reflect.DeepEqual((*calls)[0].only, []string{"build"}) ||
		!reflect.DeepEqual((*calls)[1].only, []string{"alpha", "zeta"}) {
		t.Errorf("check calls = %+v, want the gate, then one call for both verifiers", *calls)
	}
	if res.Convergence == nil || !res.Convergence.Passed || len(res.Convergence.Verifiers) != 2 {
		t.Errorf("convergence = %+v", res.Convergence)
	}
}

// A verifier named by --convergence-sensor and also carrying the role runs
// once, and a sensor named only by the flag joins the role's.
func TestRunLoop_ConvergenceSensorFlagJoinsAndDeduplicates(t *testing.T) {
	mb := &mockBackend{name: "mock", turns: []Turn{{Content: "done"}}}
	_, calls, _, err := verifierLoop(t, mb,
		[]gate.Result{cmdResult("alpha", gate.StatusPass), cmdResult("zeta", gate.StatusPass)},
		func(o *RunOptions) { o.ConvergenceSensor = "zeta" })
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if got := (*calls)[len(*calls)-1].only; !reflect.DeepEqual(got, []string{"alpha", "zeta"}) {
		t.Errorf("verifiers' --only = %v, want each once", got)
	}
}

// A failing verifier's output goes to the worker as the next turn's feedback.
func TestRunLoop_FailingVerifierFeedbackReachesWorker(t *testing.T) {
	mb := &mockBackend{name: "mock", turns: []Turn{{Content: "one"}, {Content: "two"}}}
	calls := 0
	res, _, _, err := verifierLoop(t, mb, nil, func(o *RunOptions) {
		o.MaxTurns = 2
		// The first verifier call fails, the second passes.
		runCheckFn = func(_, _, _ string, only []string, _ map[string]json.RawMessage, _ []string) (*gate.Envelope, error) {
			if len(only) == 1 && only[0] == "build" {
				return env(cmdResult("build", gate.StatusPass)), nil
			}
			calls++
			if calls == 1 {
				return env(cmdResult("alpha", gate.StatusPass), cmdResult("zeta", gate.StatusFail)), nil
			}
			return env(cmdResult("alpha", gate.StatusPass), cmdResult("zeta", gate.StatusPass)), nil
		}
	})
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if len(mb.sends) < 2 || !strings.Contains(mb.sends[1], "zeta: it is wrong") {
		t.Errorf("worker sends = %q, want the failing verifier's output in the feedback", mb.sends)
	}
	if res.Convergence == nil || !res.Convergence.Passed {
		t.Errorf("convergence = %+v, want a pass on the second turn", res.Convergence)
	}
}

// The wall clock is spent by the time the gate is green: the run ends as it
// does for a wall cap at a turn boundary, and the verifiers never run.
func TestRunLoop_WallClockSpentBeforeVerifiers(t *testing.T) {
	mb := &mockBackend{name: "mock", turns: []Turn{{Content: "done"}}, delays: []time.Duration{300 * time.Millisecond}}
	res, calls, out, err := verifierLoop(t, mb,
		[]gate.Result{cmdResult("alpha", gate.StatusPass), cmdResult("zeta", gate.StatusPass)},
		func(o *RunOptions) { o.MaxWall = 150 * time.Millisecond })
	var ee *ExitError
	if !asExitError(err, &ee) || ee.Code != ExitWallClock {
		t.Fatalf("err = %v, want ExitWallClock (%d)", err, ExitWallClock)
	}
	if res.BoundBy != string(BudgetWallClock) {
		t.Errorf("BoundBy = %q", res.BoundBy)
	}
	if len(*calls) != 1 {
		t.Errorf("check calls = %+v, want only the gate", *calls)
	}
	events := parseTrajectory(t, out)
	if kindCount(events, KindBudgetExceeded) != 1 || kindCount(events, KindSessionEnd) != 1 || kindCount(events, KindConverged) != 0 {
		t.Errorf("trajectory kinds are wrong for a wall-clock end")
	}
}

// A turn cap the final turn just reached does not stop that turn converging.
func TestRunLoop_TurnCapDoesNotPreemptVerifiers(t *testing.T) {
	mb := &mockBackend{name: "mock", turns: []Turn{{Content: "done"}}}
	_, calls, _, err := verifierLoop(t, mb,
		[]gate.Result{cmdResult("alpha", gate.StatusPass), cmdResult("zeta", gate.StatusPass)},
		func(o *RunOptions) { o.MaxTurns = 1 })
	if err != nil || len(*calls) != 2 {
		t.Errorf("err = %v, calls = %d, want convergence on the last allowed turn", err, len(*calls))
	}
}

// A verifiers' check that cannot run is an operator fault: the run ends with
// the gate error, not another turn, and the result shows what failed.
func TestCheckConvergence_VerifierCheckErrorEndsRun(t *testing.T) {
	stubVerifierCheck(t, nil, errors.New("ynh check: boom"))
	var buf bytes.Buffer
	v := verifierRun{names: []string{"a", "b"}, ynh: "ynh", harness: "local/demo"}
	converged, feedback, conv, err := checkConvergence(&gate.Envelope{Verdict: gate.VerdictPass}, v, NewTrajectoryWriter(&buf), 4, false)
	var ee *ExitError
	if converged || feedback != "" || !asExitError(err, &ee) || ee.Code != ExitGateError {
		t.Fatalf("converged %v feedback %q err %v, want ExitGateError", converged, feedback, err)
	}
	if want := "convergence verifiers (a, b) on turn 4: ynh check: boom"; ee.Message != want {
		t.Errorf("message = %q, want %q", ee.Message, want)
	}
	if conv == nil || conv.Passed || len(conv.Verifiers) != 2 {
		t.Errorf("convergence = %+v", conv)
	}
	if kindCount(parseTrajectory(t, &buf), KindSessionEnd) != 1 {
		t.Error("want one session_end")
	}
}

func TestRunLoop_VerifierCheckErrorEndsWithGateError(t *testing.T) {
	mb := &mockBackend{name: "mock", turns: []Turn{{Content: "one"}, {Content: "two"}}}
	res, _, out, err := verifierLoop(t, mb, nil, func(o *RunOptions) {
		runCheckFn = func(_, _, _ string, only []string, _ map[string]json.RawMessage, _ []string) (*gate.Envelope, error) {
			if len(only) == 1 && only[0] == "build" {
				return env(cmdResult("build", gate.StatusPass)), nil
			}
			return nil, errors.New("ynh check: no such tool")
		}
	})
	var ee *ExitError
	if !asExitError(err, &ee) || ee.Code != ExitGateError || !strings.Contains(ee.Message, "convergence verifiers (alpha, zeta) on turn 1") {
		t.Fatalf("err = %v, want ExitGateError naming the verifiers", err)
	}
	if len(mb.sends) != 1 {
		t.Errorf("worker sends = %q, want no feedback turn", mb.sends)
	}
	if res.Convergence == nil || res.Convergence.Passed || len(res.Convergence.Verifiers) != 2 {
		t.Errorf("convergence = %+v, want the failed verifiers recorded", res.Convergence)
	}
	if kindCount(parseTrajectory(t, out), KindSessionEnd) != 1 {
		t.Error("want one session_end")
	}
}
