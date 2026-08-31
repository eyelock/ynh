package agent

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	schema "github.com/eyelock/ynh/docs/schema"
	"github.com/eyelock/ynh/internal/jsonschema"
)

// The published trajectory schema is validated against what the emitter really
// produces, not against a hand-written fixture.
//
// A fixture would only prove the schema matches the fixture. Emitting through
// the real TrajectoryWriter means the schema is checked against the actual wire
// bytes, so a field renamed in Go breaks this test rather than silently
// diverging from the published contract.
func TestTrajectorySchemaMatchesTheEmitter(t *testing.T) {
	compiled := loadTrajectorySchema(t)

	// One emit per event kind, with every optional field populated — an
	// omitted field cannot violate a schema, so the strict case is the one
	// worth checking.
	cases := []struct {
		kind EventKind
		turn int
		data any
	}{
		{KindSessionStart, 0, SessionStartData{
			SessionID: "s1", Harness: "local/h", Backend: "claude", Task: "do it",
			Model: "opus", YnhVersion: "0.6.0", HarnessVersion: "0.6.0",
			HarnessSHA: "abc123", ImageDigest: "sha256:deadbeef", BaseCommit: "def456",
			Budgets:       &BudgetLimits{MaxTurns: 10, MaxTokens: 1000, MaxWallMS: 60000},
			BudgetSources: &BudgetSource{Turns: "flag", Tokens: "manifest", Wall: "default"},
		}},
		{KindSessionResumed, 3, SessionResumedData{
			SessionID: "s1", Backend: "claude", ResumedAtTurn: 3,
			RestoredTurns: 3, RestoredTokens: 900, PendingApproval: "plan",
		}},
		{KindPlan, 0, nil},
		{KindPlanRevised, 0, PlanRevisedData{Iteration: 2, Notes: "narrow it"}},
		{KindPlanApprovalRequired, 0, PlanApprovalData{Plan: "the plan", Iteration: 1}},
		{KindTurnStart, 1, nil},
		{KindAssistantMessage, 1, "what the model said"},
		{KindSensorRun, 1, "lint"},
		{KindSensorResult, 1, SensorResultData{
			Name: "lint", Kind: "command", Role: "regular", ExitCode: 1,
			DurationMS: 12, Tolerance: "blocking", Status: "fail",
			ToolVersion: "go version go1.25.0", KnownCount: 2, NewCount: 1,
			Passed: false, Summary: "1 new",
		}},
		{KindFeedbackSent, 1, "fix the lint failure"},
		{KindWorkerEnv, 0, WorkerEnvData{
			Passed: []string{"HOME"}, Declared: []string{"HOME", "TOKEN"}, Missing: []string{"TOKEN"},
		}},
		{KindTurnApprovalRequired, 1, TurnApprovalData{SynthesizedFeedback: "continue?"}},
		{KindStuckDetected, 4, StuckDetectedData{Reason: "no progress", TurnCount: 3}},
		{KindTamperDetected, 2, TamperData{What: "baseline", Before: "a", After: "b"}},
		{KindBudgetSnapshot, 2, BudgetSnapshotData{Turns: 2, Tokens: 400}},
		{KindBudgetExceeded, 9, BudgetExceededData{Budget: "turns", Reason: "limit reached"}},
		{KindConverged, 5, nil},
		{KindSessionEnd, 5, SessionEndData{
			ExitCode: 0, Reason: "converged", TotalTurns: 5, TotalTokens: 1200,
		}},
	}

	// Every declared kind must be exercised. Without this the test passes
	// while a newly added event goes unvalidated — which is how the schema
	// would drift from the emitter in the first place.
	if len(cases) != len(allEventKinds()) {
		t.Fatalf("%d cases for %d declared event kinds — add the missing one",
			len(cases), len(allEventKinds()))
	}

	var buf bytes.Buffer
	w := NewTrajectoryWriter(&buf)
	for _, c := range cases {
		if err := w.Emit(c.kind, c.turn, c.data); err != nil {
			t.Fatalf("emitting %s: %v", c.kind, err)
		}
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != len(cases) {
		t.Fatalf("emitted %d lines for %d events", len(lines), len(cases))
	}
	for i, line := range lines {
		var v any
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			t.Errorf("%s: not valid JSON: %v", cases[i].kind, err)
			continue
		}
		if err := compiled.Validate(v); err != nil {
			t.Errorf("%s does not validate against the published schema:\n  %v\n  %s",
				cases[i].kind, err, line)
		}
	}
}

// An event type nobody has seen must still parse as a trajectory line, because
// kinds are added additively and docs/cli-structured.md requires consumers to
// tolerate unknown members. If this ever fails, the schema has become a
// compatibility break rather than a description.
func TestTrajectorySchemaRejectsMalformedButNotUnknown(t *testing.T) {
	compiled := loadTrajectorySchema(t)

	var known any
	_ = json.Unmarshal([]byte(`{"timestamp":"2026-08-31T12:00:00Z","type":"turn_start","turn":1}`), &known)
	if err := compiled.Validate(known); err != nil {
		t.Fatalf("a well-formed known event should validate: %v", err)
	}

	// Malformed: session_end without its required exit_code.
	var bad any
	_ = json.Unmarshal([]byte(`{"timestamp":"2026-08-31T12:00:00Z","type":"session_end","data":{}}`), &bad)
	if err := compiled.Validate(bad); err == nil {
		t.Error("session_end without exit_code should not validate")
	}

	// Missing the discriminator entirely.
	var noType any
	_ = json.Unmarshal([]byte(`{"timestamp":"2026-08-31T12:00:00Z"}`), &noType)
	if err := compiled.Validate(noType); err == nil {
		t.Error("an event with no type should not validate")
	}
}

func loadTrajectorySchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	data, err := schema.FS.ReadFile("agent/trajectory.schema.json")
	if err != nil {
		t.Fatalf("reading the published schema: %v", err)
	}
	c := jsonschema.NewCompiler()
	const id = "https://eyelock.github.io/ynh/schema/agent/trajectory.schema.json"
	if err := c.Add(id, data); err != nil {
		t.Fatalf("adding schema: %v", err)
	}
	s, err := c.Compile(id)
	if err != nil {
		t.Fatalf("compiling schema: %v", err)
	}
	return s
}

// allEventKinds lists every kind the package declares, so the test above can
// assert it covers them all.
func allEventKinds() []EventKind {
	return []EventKind{
		KindSessionStart, KindSessionResumed, KindPlan, KindPlanRevised,
		KindPlanApprovalRequired, KindTurnStart, KindAssistantMessage,
		KindSensorRun, KindSensorResult, KindFeedbackSent, KindWorkerEnv,
		KindTurnApprovalRequired, KindStuckDetected, KindTamperDetected,
		KindBudgetSnapshot, KindBudgetExceeded, KindConverged, KindSessionEnd,
	}
}
