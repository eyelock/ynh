package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/gate"
)

// env builds a gate envelope for the loop to judge. Verdict is derived the
// way `ynh check` derives it — a blocking command sensor at StatusFail — so a
// test cannot accidentally assert on a verdict the real gate would never
// produce.
func env(sensors ...gate.Result) *gate.Envelope {
	e := &gate.Envelope{Harness: "demo", Verdict: gate.VerdictPass, Sensors: sensors}
	for _, s := range sensors {
		if s.Gating() {
			e.Verdict = gate.VerdictBlocked
			e.Summary.Blocking++
		}
	}
	e.Summary.Total = len(sensors)
	return e
}

// A resume that lost its harness ran zero sensors, and an empty result set made
// the all-passed check vacuously true — so the loop reported converged and
// exited 0 having verified nothing. The single most safety-critical value in
// the contract was forgeable by omitting a flag.
func TestCheckConvergence_NoEvidenceCannotConverge(t *testing.T) {
	converged, reason := checkConvergence(nil, "", "", "", "", newNullTrajectory(), 1, true)
	if converged {
		t.Fatal("converged with no sensor results — a verdict with no evidence behind it")
	}
	if !strings.Contains(reason, "no sensors ran") {
		t.Errorf("reason = %q, want it to name the missing evidence", reason)
	}
}

// A run started without a harness never asked to be verified; the worker
// declaring itself done is the only signal available, and requiring evidence
// there would break the plain agent-runner mode.
func TestCheckConvergence_UnverifiedRunStillConverges(t *testing.T) {
	converged, _ := checkConvergence(nil, "", "", "", "", newNullTrajectory(), 1, false)
	if !converged {
		t.Error("a run with no harness configured should still converge on worker completion")
	}
}

// `report` is documented as pure observation and `advisory` as non-gating, but
// the loop treated every result alike — so a report sensor that never passes
// held convergence open forever, and the loop contradicted `ynh check` on the
// same declaration. The loop now takes the gate's verdict rather than
// re-deriving one, which is what makes the contradiction impossible.
func TestCheckConvergence_NonGatingSensorsDoNotBlock(t *testing.T) {
	e := env(
		gate.Result{Name: "build", Kind: "command", Tolerance: "blocking", Status: gate.StatusPass},
		gate.Result{Name: "deps", Kind: "command", Tolerance: "report", Status: gate.StatusFail},
		gate.Result{Name: "typos", Kind: "command", Tolerance: "advisory", Status: gate.StatusFail},
	)
	converged, reason := checkConvergence(e, "", "", "", "", newNullTrajectory(), 1, true)
	if !converged {
		t.Errorf("advisory and report failures must not gate; got reason %q", reason)
	}
}

func TestCheckConvergence_BlockingFailureStillGates(t *testing.T) {
	e := env(
		gate.Result{Name: "lint", Kind: "command", Tolerance: "blocking", Status: gate.StatusFail},
		gate.Result{Name: "typos", Kind: "command", Tolerance: "advisory", Status: gate.StatusFail},
	)
	if converged, _ := checkConvergence(e, "", "", "", "", newNullTrajectory(), 1, true); converged {
		t.Error("a failing blocking sensor must hold convergence open")
	}
}

// Every sensor being non-gating means nothing verifies anything, which is not
// the same as everything passing.
func TestCheckConvergence_AllNonGatingIsNotEvidence(t *testing.T) {
	e := env(gate.Result{Name: "deps", Kind: "command", Tolerance: "report", Status: gate.StatusPass})
	if converged, _ := checkConvergence(e, "", "", "", "", newNullTrajectory(), 1, true); converged {
		t.Error("a run whose sensors are all non-gating has verified nothing")
	}
}

// The "something gates this run" guard must count only what can actually
// produce a blocked verdict. A files sensor reports and never gates whatever
// its tolerance says, so counting its declared tolerance would let a harness
// that gates on nothing pass the guard — a check that finds nothing is not a
// check that found nothing wrong.
func TestCheckConvergence_BlockingFilesSensorIsNotEvidence(t *testing.T) {
	e := env(gate.Result{Name: "coverage", Kind: "files", Tolerance: "blocking", Status: gate.StatusReported})
	converged, reason := checkConvergence(e, "", "", "", "", newNullTrajectory(), 1, true)
	if converged {
		t.Error("a files sensor cannot gate, so a harness declaring only one verifies nothing")
	}
	if !strings.Contains(reason, "nothing gates") {
		t.Errorf("reason = %q, want it to say nothing gates the run", reason)
	}
}

// The point of the loop consuming the gate: a sensor still failing, but
// failing only in ways the baseline already records, is inherited debt. It
// must not hold convergence open, or the agent is required to fix a
// repository it was never asked to fix before it can finish its own task.
func TestCheckConvergence_BaselinedFailureDoesNotBlock(t *testing.T) {
	e := env(gate.Result{
		Name: "lint", Kind: "command", Tolerance: "blocking",
		Status: gate.StatusKnown, ExitCode: 1, KnownCount: 12,
	})
	converged, reason := checkConvergence(e, "", "", "", "", newNullTrajectory(), 1, true)
	if !converged {
		t.Errorf("failures already in the baseline must not gate; got reason %q", reason)
	}
}

// A new failure alongside recorded debt still gates, and the feedback must
// separate the two. An agent shown one undifferentiated failure list will try
// to fix all of it.
func TestSynthesizeFeedback_DistinguishesKnownFromNew(t *testing.T) {
	e := env(
		gate.Result{
			Name: "lint", Kind: "command", Tolerance: "blocking", Status: gate.StatusFail,
			NewCount: 1, KnownCount: 12, NewOutput: "internal/x.go:4: undefined: y",
		},
		gate.Result{
			Name: "vet", Kind: "command", Tolerance: "blocking", Status: gate.StatusKnown,
			KnownCount: 3,
		},
	)
	e.Baseline = &gate.BaselineInfo{RecordedAt: "2026-08-29T00:00:00Z", Known: 15}

	fb := synthesizeFeedback(e)
	if !strings.Contains(fb, `status="fail"`) || !strings.Contains(fb, `status="known"`) {
		t.Fatalf("feedback must distinguish known from fail:\n%s", fb)
	}
	if !strings.Contains(fb, "internal/x.go:4: undefined: y") {
		t.Errorf("feedback must carry the new failure the agent has to fix:\n%s", fb)
	}
	if !strings.Contains(fb, "not blocking") {
		t.Errorf("feedback must say the recorded failures are not the agent's to fix:\n%s", fb)
	}
}

// Only the new lines reach the worker. Handing it the twelve findings it did
// not introduce alongside the one it did is how a turn gets spent fixing
// someone else's debt.
func TestResultSummary_ShowsOnlyNewFailures(t *testing.T) {
	s := resultSummary(gate.Result{
		Name: "lint", Kind: "command", Status: gate.StatusFail,
		Stdout:    "old.go:1: pre-existing\nnew.go:2: mine",
		NewOutput: "new.go:2: mine",
	})
	if strings.Contains(s, "pre-existing") {
		t.Errorf("pre-existing failures must not reach the worker: %q", s)
	}
	if !strings.Contains(s, "new.go:2: mine") {
		t.Errorf("the new failure must reach the worker: %q", s)
	}
}

// Hashing name and exit code alone meant fixing three of fifty findings
// produced an identical hash: real progress read as no progress, and the
// watchdog killed the run for doing what it was asked.
func TestSensorHash_SeesProgressWithinAFailingSensor(t *testing.T) {
	before := env(gate.Result{
		Name: "lint", Kind: "command", Status: gate.StatusFail,
		Stderr: "a.go:1: one\nb.go:2: two\nc.go:3: three\n",
	})
	after := env(gate.Result{
		Name: "lint", Kind: "command", Status: gate.StatusFail,
		Stderr: "a.go:1: one\n",
	})
	if SensorHash(before) == SensorHash(after) {
		t.Error("fixing findings while the sensor still fails must change the hash")
	}
}

// Positions moving is not progress, and must not reset the watchdog either.
func TestSensorHash_IgnoresPositionDrift(t *testing.T) {
	a := env(gate.Result{Name: "lint", Kind: "command", Status: gate.StatusFail, Stderr: "a.go:1:2: one\n"})
	b := env(gate.Result{Name: "lint", Kind: "command", Status: gate.StatusFail, Stderr: "a.go:99:2: one\n"})
	if SensorHash(a) != SensorHash(b) {
		t.Error("a finding moving down the file is not progress")
	}
}

// Churn among failures the baseline already forgives is not progress either.
// The watchdog must track the surface the gate blocks on, or a loop that
// rearranges pre-existing findings looks busy forever.
func TestSensorHash_TracksNewFailuresWhenBaselined(t *testing.T) {
	a := env(gate.Result{
		Name: "lint", Kind: "command", Status: gate.StatusFail,
		Stdout: "old-a.go:1: debt", NewOutput: "mine.go:9: real",
	})
	b := env(gate.Result{
		Name: "lint", Kind: "command", Status: gate.StatusFail,
		Stdout: "old-b.go:7: different debt", NewOutput: "mine.go:9: real",
	})
	if SensorHash(a) != SensorHash(b) {
		t.Error("baselined findings churning is not progress the watchdog should credit")
	}
}

// stubCheck replaces the gate with a synthesised envelope so a loop test can
// say "build fails, then passes" without constructing wire JSON. result is
// called once per sensor per gate invocation; call counts from 1.
func stubCheck(t *testing.T, result func(call int, name string) gate.Result) {
	t.Helper()
	orig := runCheckFn
	t.Cleanup(func() { runCheckFn = orig })
	call := 0
	runCheckFn = func(_, _, _ string, only []string, _ map[string]json.RawMessage) (*gate.Envelope, error) {
		call++
		rs := make([]gate.Result, 0, len(only))
		for _, n := range only {
			rs = append(rs, result(call, n))
		}
		return env(rs...), nil
	}
}

// A gate that cannot run is an operator fault. Reporting it as agent failure
// — or worse, feeding the worker an empty sensor block turn after turn until
// the budget ran out — hides a broken harness behind a plausible-looking
// unproductive run.
func TestRunLoop_GateErrorIsNotAgentFailure(t *testing.T) {
	orig := runCheckFn
	t.Cleanup(func() { runCheckFn = orig })
	runCheckFn = func(_, _, _ string, _ []string, _ map[string]json.RawMessage) (*gate.Envelope, error) {
		return nil, errors.New("harness \"demo\" not installed")
	}

	mb := &mockBackend{name: "mock", turns: []Turn{{Content: "work"}, {Content: "more"}}}
	var stdout, stderr bytes.Buffer
	opts := baseOpts(mb, &stdout, &stderr, strings.NewReader(""))
	opts.testSensorNames = []string{"build"}

	err := RunLoop(opts)
	var exitErr *ExitError
	if !asExitError(err, &exitErr) || exitErr.Code != ExitGateError {
		t.Fatalf("want ExitGateError (%d), got %v", ExitGateError, err)
	}
	if !strings.Contains(exitErr.Message, "not installed") {
		t.Errorf("the operator must be told what broke, got %q", exitErr.Message)
	}
}
