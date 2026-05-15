package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// planOpts builds RunOptions for a plan-iteration test. Stdin carries
// scripted control messages; trajectory NDJSON is captured to traj.
func planOpts(backend WorkerBackend, traj io.Writer, stdin io.Reader) RunOptions {
	return RunOptions{
		Task:              "test task",
		Interactive:       true,
		MaxPlanIterations: 5,
		Stdout:            traj,
		Stderr:            io.Discard,
		Stdin:             stdin,
		YNHBinary:         "ynh",
		EmitJSONL:         "-",
		backendOverride:   backend,
	}
}

func parseTrajectory(t *testing.T, buf *bytes.Buffer) []Event {
	t.Helper()
	var events []Event
	for _, line := range bytes.Split(bytes.TrimRight(buf.Bytes(), "\n"), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var e Event
		if err := json.Unmarshal(line, &e); err != nil {
			t.Fatalf("trajectory line %q: %v", line, err)
		}
		events = append(events, e)
	}
	return events
}

func kindCount(events []Event, kind EventKind) int {
	n := 0
	for _, e := range events {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

// Approve on the first iteration: the trajectory must contain exactly
// one KindPlanApprovalRequired and zero KindPlanRevised. Regression
// guard for the no-refine path that consumers depend on today.
func TestRunLoop_PlanApproveOnFirstIteration(t *testing.T) {
	mb := &mockBackend{
		name: "mock",
		turns: []Turn{
			{Content: "the plan"},
			{Content: "act done"},
		},
	}
	var traj bytes.Buffer
	opts := planOpts(mb, &traj, strings.NewReader(`{"action":"approve_plan"}`+"\n"))

	if err := RunLoop(opts); err != nil {
		t.Fatalf("RunLoop: %v", err)
	}

	events := parseTrajectory(t, &traj)
	if got := kindCount(events, KindPlanApprovalRequired); got != 1 {
		t.Errorf("KindPlanApprovalRequired = %d, want 1", got)
	}
	if got := kindCount(events, KindPlanRevised); got != 0 {
		t.Errorf("KindPlanRevised = %d, want 0", got)
	}
	if got := kindCount(events, KindPlan); got != 1 {
		t.Errorf("KindPlan = %d, want 1", got)
	}
}

// Refine once → approve. Trajectory must show a KindPlanRevised between
// the two approval gates, and the worker must have been sent the
// refinement notes verbatim.
func TestRunLoop_PlanRefineOnceThenApprove(t *testing.T) {
	mb := &mockBackend{
		name: "mock",
		turns: []Turn{
			{Content: "plan v1"},
			{Content: "plan v2"},
			{Content: "act done"},
		},
	}
	var traj bytes.Buffer
	ctrl := `{"action":"replace_feedback","feedback":"add error handling"}` + "\n" +
		`{"action":"approve_plan"}` + "\n"
	opts := planOpts(mb, &traj, strings.NewReader(ctrl))

	if err := RunLoop(opts); err != nil {
		t.Fatalf("RunLoop: %v", err)
	}

	events := parseTrajectory(t, &traj)
	if got := kindCount(events, KindPlanRevised); got != 1 {
		t.Errorf("KindPlanRevised = %d, want 1", got)
	}
	if got := kindCount(events, KindPlanApprovalRequired); got != 2 {
		t.Errorf("KindPlanApprovalRequired = %d, want 2", got)
	}
	if len(mb.sends) != 3 {
		t.Fatalf("sends = %d, want 3 (plan + revise + act); got %v", len(mb.sends), mb.sends)
	}
	if !strings.Contains(mb.sends[1], "add error handling") {
		t.Errorf("revise prompt missing user notes: %q", mb.sends[1])
	}

	// Plan prompts must not demand a file write — claude in plan mode is
	// read-only and would stall at its own permission gate.
	for _, want := range []string{"plan.md", "current directory"} {
		if strings.Contains(mb.sends[0], want) || strings.Contains(mb.sends[1], want) {
			t.Errorf("plan prompts must not reference %q (plan mode is read-only); got initial=%q revise=%q",
				want, mb.sends[0], mb.sends[1])
		}
	}

	// Act-phase first message must forward the final approved plan, not
	// just the original task. Otherwise refinement work evaporates at the
	// phase boundary.
	act := mb.sends[2]
	if !strings.Contains(act, "plan v2") {
		t.Errorf("act-phase first message must contain the approved plan content (%q); got %q", "plan v2", act)
	}
	if !strings.Contains(act, "test task") {
		t.Errorf("act-phase first message must contain the original task; got %q", act)
	}

	// Verify KindPlanRevised payload carries iteration=2 and notes.
	for _, e := range events {
		if e.Kind != KindPlanRevised {
			continue
		}
		raw, _ := json.Marshal(e.Data)
		var rev PlanRevisedData
		if err := json.Unmarshal(raw, &rev); err != nil {
			t.Fatalf("decoding plan_revised data: %v", err)
		}
		if rev.Iteration != 2 {
			t.Errorf("plan_revised iteration = %d, want 2", rev.Iteration)
		}
		if rev.Notes != "add error handling" {
			t.Errorf("plan_revised notes = %q, want %q", rev.Notes, "add error handling")
		}
	}
}

// MaxPlanIterations bounds the refine loop; an over-cap refine attempt
// must return ExitPlanIterationCap with a clean session_end event.
func TestRunLoop_PlanRefineHitsCap(t *testing.T) {
	mb := &mockBackend{
		name: "mock",
		turns: []Turn{
			{Content: "plan v1"}, {Content: "plan v2"},
		},
	}
	var traj bytes.Buffer
	ctrl := `{"action":"replace_feedback","feedback":"again"}` + "\n" +
		`{"action":"replace_feedback","feedback":"again"}` + "\n"
	opts := planOpts(mb, &traj, strings.NewReader(ctrl))
	opts.MaxPlanIterations = 2

	err := RunLoop(opts)
	var ee *ExitError
	if !asExitError(err, &ee) || ee.Code != ExitPlanIterationCap {
		t.Fatalf("expected ExitPlanIterationCap (%d), got %v", ExitPlanIterationCap, err)
	}

	events := parseTrajectory(t, &traj)
	last := events[len(events)-1]
	if last.Kind != KindSessionEnd {
		t.Errorf("last event = %q, want session_end", last.Kind)
	}
}

// Reject with notes: the user's "why" payload surfaces both in the
// ExitError message and in KindSessionEnd.Reason with the documented
// stable prefix consumers can pattern-match.
func TestRunLoop_PlanRejectWithNotes(t *testing.T) {
	mb := &mockBackend{
		name:  "mock",
		turns: []Turn{{Content: "plan v1"}},
	}
	var traj bytes.Buffer
	ctrl := `{"action":"reject_plan","feedback":"completely wrong direction"}` + "\n"
	opts := planOpts(mb, &traj, strings.NewReader(ctrl))

	err := RunLoop(opts)
	var ee *ExitError
	if !asExitError(err, &ee) || ee.Code != ExitUserAborted {
		t.Fatalf("expected ExitUserAborted, got %v", err)
	}
	want := "plan rejected by user: completely wrong direction"
	if ee.Message != want {
		t.Errorf("error message = %q, want %q", ee.Message, want)
	}

	events := parseTrajectory(t, &traj)
	last := events[len(events)-1]
	if last.Kind != KindSessionEnd {
		t.Fatalf("last event = %q, want session_end", last.Kind)
	}
	raw, _ := json.Marshal(last.Data)
	var end SessionEndData
	if err := json.Unmarshal(raw, &end); err != nil {
		t.Fatalf("decoding session_end: %v", err)
	}
	if end.Reason != want {
		t.Errorf("session_end reason = %q, want %q", end.Reason, want)
	}
}

// Token budget exhausted by the second plan iteration's response. Must
// exit cleanly with ExitTokenBudget rather than continuing into the act
// phase or returning a worker error.
func TestRunLoop_PlanRefineTokenBudget(t *testing.T) {
	mb := &mockBackend{
		name: "mock",
		turns: []Turn{
			{Content: "plan v1", Usage: Usage{InputTokens: 600, OutputTokens: 600}},
			{Content: "plan v2", Usage: Usage{InputTokens: 600, OutputTokens: 600}},
		},
	}
	var traj bytes.Buffer
	ctrl := `{"action":"replace_feedback","feedback":"again"}` + "\n" +
		`{"action":"approve_plan"}` + "\n"
	opts := planOpts(mb, &traj, strings.NewReader(ctrl))
	opts.MaxTokens = 1500

	err := RunLoop(opts)
	var ee *ExitError
	if !asExitError(err, &ee) || ee.Code != ExitTokenBudget {
		t.Fatalf("expected ExitTokenBudget (%d), got %v", ExitTokenBudget, err)
	}
}

// Wall-clock budget covers plan iterations as well as act iterations.
// Phase-agnostic behaviour matters because consumers rely on max-wall
// as the single hard ceiling on session duration.
func TestRunLoop_PlanRefineWallBudget(t *testing.T) {
	mb := &mockBackend{
		name: "mock",
		turns: []Turn{
			{Content: "plan v1"},
			{Content: "plan v2"},
		},
		// Iteration 2's response takes long enough to push past MaxWall.
		delays: []time.Duration{0, 30 * time.Millisecond},
	}
	var traj bytes.Buffer
	ctrl := `{"action":"replace_feedback","feedback":"again"}` + "\n" +
		`{"action":"approve_plan"}` + "\n"
	opts := planOpts(mb, &traj, strings.NewReader(ctrl))
	opts.MaxWall = 5 * time.Millisecond

	err := RunLoop(opts)
	var ee *ExitError
	if !asExitError(err, &ee) || ee.Code != ExitWallClock {
		t.Fatalf("expected ExitWallClock (%d), got %v", ExitWallClock, err)
	}
}

// Worker error mid-refine: KindPlanRevised has been emitted, then the
// next worker turn errors. The trajectory must still terminate cleanly
// with a KindSessionEnd event so consumers don't see an orphan
// KindPlanRevised stuck in flight.
func TestRunLoop_PlanRefineWorkerError(t *testing.T) {
	mb := &mockBackend{
		name: "mock",
		turns: []Turn{
			{Content: "plan v1"},
			{}, // placeholder; errs[1] supplies the error
		},
		errs: []error{nil, errors.New("worker crashed mid-refine")},
	}
	var traj bytes.Buffer
	ctrl := `{"action":"replace_feedback","feedback":"again"}` + "\n"
	opts := planOpts(mb, &traj, strings.NewReader(ctrl))

	err := RunLoop(opts)
	var ee *ExitError
	if !asExitError(err, &ee) || ee.Code != ExitWorkerError {
		t.Fatalf("expected ExitWorkerError, got %v", err)
	}

	events := parseTrajectory(t, &traj)
	if got := kindCount(events, KindPlanRevised); got != 1 {
		t.Errorf("KindPlanRevised = %d, want 1 (must be emitted before the failed turn)", got)
	}
	last := events[len(events)-1]
	if last.Kind != KindSessionEnd {
		t.Fatalf("trajectory must end with session_end after worker error, got %q", last.Kind)
	}
}
