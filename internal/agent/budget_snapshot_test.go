package agent

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/gate"
)

// budgetSnapshotsFromTrajectory extracts decoded BudgetSnapshotData
// payloads from a trajectory buffer in emission order. Helper used by
// every test in this file.
func budgetSnapshotsFromTrajectory(t *testing.T, buf *bytes.Buffer) []BudgetSnapshotData {
	t.Helper()
	events := parseTrajectory(t, buf)
	var out []BudgetSnapshotData
	for _, e := range events {
		if e.Kind != KindBudgetSnapshot {
			continue
		}
		raw, _ := json.Marshal(e.Data)
		var d BudgetSnapshotData
		if err := json.Unmarshal(raw, &d); err != nil {
			t.Fatalf("decoding budget_snapshot: %v", err)
		}
		out = append(out, d)
	}
	return out
}

// Three act-phase turns produce three KindBudgetSnapshot events with
// monotonically non-decreasing token totals and a turn count that
// advances by exactly one per turn.
func TestRunLoop_BudgetSnapshotPerActTurn(t *testing.T) {
	mb := &mockBackend{
		name: "mock",
		turns: []Turn{
			{Content: "turn 1", Usage: Usage{InputTokens: 100, OutputTokens: 50}},
			{Content: "turn 2", Usage: Usage{InputTokens: 100, OutputTokens: 50}},
			{Content: "turn 3", Usage: Usage{InputTokens: 100, OutputTokens: 50}},
		},
	}
	// Failing sensor keeps the loop going for 3 turns.
	stubCheck(t, func(_ int, name string) gate.Result {
		return gate.Result{Name: name, Kind: "command", Tolerance: "blocking", Status: gate.StatusFail, ExitCode: 1}
	})

	var traj bytes.Buffer
	opts := baseOpts(mb, &traj, &bytes.Buffer{}, strings.NewReader(""))
	opts.EmitJSONL = "-"
	opts.Stdout = &traj
	opts.MaxTurns = 3
	opts.testSensorNames = []string{"build"}

	_, _ = RunLoop(opts)

	snaps := budgetSnapshotsFromTrajectory(t, &traj)
	if len(snaps) != 3 {
		t.Fatalf("budget_snapshot count = %d, want 3", len(snaps))
	}
	for i, s := range snaps {
		wantTurn := i + 1
		wantTokens := int64((i + 1) * 150)
		if s.Turns != wantTurn {
			t.Errorf("snap[%d] Turns = %d, want %d", i, s.Turns, wantTurn)
		}
		if s.Tokens != wantTokens {
			t.Errorf("snap[%d] Tokens = %d, want %d", i, s.Tokens, wantTokens)
		}
	}
}

// Plan-phase emission: a KindBudgetSnapshot event accompanies each plan
// iteration with Turns=0 (act-phase counter has not started) and the
// running token total from plan-iteration usage.
func TestRunLoop_BudgetSnapshotInPlanPhase(t *testing.T) {
	mb := &mockBackend{
		name: "mock",
		turns: []Turn{
			{Content: "plan v1", Usage: Usage{InputTokens: 200, OutputTokens: 100}},
			{Content: "act done"},
		},
	}
	var traj bytes.Buffer
	opts := planOpts(mb, &traj, strings.NewReader(`{"action":"approve_plan"}`+"\n"))

	if _, err := RunLoop(opts); err != nil {
		t.Fatalf("RunLoop: %v", err)
	}

	snaps := budgetSnapshotsFromTrajectory(t, &traj)
	if len(snaps) < 1 {
		t.Fatal("expected at least one budget_snapshot from the plan phase")
	}
	plan := snaps[0]
	if plan.Turns != 0 {
		t.Errorf("plan-phase snap Turns = %d, want 0 (act counter must not start in plan phase)", plan.Turns)
	}
	if plan.Tokens != 300 {
		t.Errorf("plan-phase snap Tokens = %d, want 300", plan.Tokens)
	}
}

// Final KindSessionEnd totals must match the last KindBudgetSnapshot.
// Guards against drift between the per-turn and terminal accounting paths.
func TestRunLoop_BudgetSnapshotMatchesSessionEnd(t *testing.T) {
	mb := &mockBackend{
		name: "mock",
		turns: []Turn{
			{Content: "turn 1", Usage: Usage{InputTokens: 200, OutputTokens: 75}},
			{Content: "turn 2", Usage: Usage{InputTokens: 200, OutputTokens: 75}},
		},
	}
	stubCheck(t, func(_ int, name string) gate.Result {
		return gate.Result{Name: name, Kind: "command", Tolerance: "blocking", Status: gate.StatusFail, ExitCode: 1}
	})

	var traj bytes.Buffer
	opts := baseOpts(mb, &traj, &bytes.Buffer{}, strings.NewReader(""))
	opts.EmitJSONL = "-"
	opts.Stdout = &traj
	opts.MaxTurns = 2
	opts.testSensorNames = []string{"build"}

	_, _ = RunLoop(opts)

	snaps := budgetSnapshotsFromTrajectory(t, &traj)
	if len(snaps) == 0 {
		t.Fatal("no budget_snapshot events emitted")
	}
	last := snaps[len(snaps)-1]

	events := parseTrajectory(t, &traj)
	end := events[len(events)-1]
	if end.Kind != KindSessionEnd {
		t.Fatalf("trailing event = %q, want session_end", end.Kind)
	}
	raw, _ := json.Marshal(end.Data)
	var endData SessionEndData
	if err := json.Unmarshal(raw, &endData); err != nil {
		t.Fatalf("decoding session_end: %v", err)
	}
	if endData.TotalTurns != last.Turns {
		t.Errorf("session_end TotalTurns = %d, last snapshot Turns = %d (drift)", endData.TotalTurns, last.Turns)
	}
	if endData.TotalTokens != last.Tokens {
		t.Errorf("session_end TotalTokens = %d, last snapshot Tokens = %d (drift)", endData.TotalTokens, last.Tokens)
	}
}

// Wire-shape regression: the event encodes with the documented field
// names and types so consumers can decode it without surprises.
func TestBudgetSnapshotData_WireShape(t *testing.T) {
	d := BudgetSnapshotData{Turns: 7, Tokens: 142378}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)
	want := `{"turns":7,"tokens":142378}`
	if got != want {
		t.Errorf("encoded = %q, want %q", got, want)
	}

	// Round-trip from the wire.
	var back BudgetSnapshotData
	if err := json.Unmarshal([]byte(want), &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back != d {
		t.Errorf("round-trip = %+v, want %+v", back, d)
	}
}
