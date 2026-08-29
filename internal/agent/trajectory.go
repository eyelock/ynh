package agent

import (
	"encoding/json"
	"io"
	"time"
)

// EventKind identifies the type of a trajectory event.
type EventKind string

const (
	KindSessionStart EventKind = "session_start"
	// KindSessionResumed is emitted once at the head of a resumed trajectory
	// (a --resume relaunch) before the act loop continues. It delimits the
	// boundary between the prior session's appended events and the new ones.
	KindSessionResumed       EventKind = "session_resumed"
	KindPlan                 EventKind = "plan"
	KindPlanRevised          EventKind = "plan_revised"
	KindPlanApprovalRequired EventKind = "plan_approval_required"
	KindTurnStart            EventKind = "turn_start"
	KindAssistantMessage     EventKind = "assistant_message"
	KindSensorRun            EventKind = "sensor_run"
	KindSensorResult         EventKind = "sensor_result"
	KindFeedbackSent         EventKind = "feedback_sent"
	// KindTurnApprovalRequired is emitted only during the act phase (turn ≥ 1).
	// Plan-phase approval gates use KindPlanApprovalRequired.
	KindTurnApprovalRequired EventKind = "turn_approval_required"
	KindStuckDetected        EventKind = "stuck_detected"
	KindBudgetSnapshot       EventKind = "budget_snapshot"
	KindBudgetExceeded       EventKind = "budget_exceeded"
	KindConverged            EventKind = "converged"
	KindSessionEnd           EventKind = "session_end"
)

// Event is a single trajectory event emitted by the loop driver.
// Wire field names match what TermQ and other consumers expect:
//   - "type" (not "kind") for the event discriminator
//   - "timestamp" (not "time") for the wall-clock time
type Event struct {
	Timestamp time.Time `json:"timestamp"`
	Kind      EventKind `json:"type"`
	Turn      int       `json:"turn,omitempty"`
	Data      any       `json:"data,omitempty"`
}

// TrajectoryWriter writes trajectory events as NDJSON.
type TrajectoryWriter struct {
	enc *json.Encoder
}

// NewTrajectoryWriter returns a writer that emits one JSON object per line.
func NewTrajectoryWriter(w io.Writer) *TrajectoryWriter {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return &TrajectoryWriter{enc: enc}
}

// Emit writes a single event to the trajectory stream.
func (t *TrajectoryWriter) Emit(kind EventKind, turn int, data any) error {
	return t.enc.Encode(Event{
		Timestamp: time.Now().UTC(),
		Kind:      kind,
		Turn:      turn,
		Data:      data,
	})
}

// Typed data payloads for specific event kinds.

// SessionStartData is the payload for KindSessionStart events.
type SessionStartData struct {
	SessionID string `json:"session_id"`
	Harness   string `json:"harness"`
	Backend   string `json:"backend"`
	Task      string `json:"task"`
}

// SessionResumedData is the payload for KindSessionResumed events. Emitted
// once at the start of a resumed trajectory before the loop continues.
// ResumedAtTurn is the act-phase turn number the loop will produce next
// (the checkpoint's last_completed_turn + 1). RestoredTurns/RestoredTokens
// echo the budget counters carried over from the checkpoint so consumers can
// re-seed live progress without replaying the prior trajectory. PendingApproval
// is "plan" or "turn" when the prior session stopped at an approval gate that
// the resume will re-prompt, otherwise empty.
type SessionResumedData struct {
	SessionID       string `json:"session_id"`
	Backend         string `json:"backend"`
	ResumedAtTurn   int    `json:"resumed_at_turn"`
	RestoredTurns   int    `json:"restored_turns"`
	RestoredTokens  int64  `json:"restored_tokens"`
	PendingApproval string `json:"pending_approval,omitempty"`
}

// SensorResultData is the payload for KindSensorResult events.
type SensorResultData struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Role       string `json:"role,omitempty"`
	ExitCode   int    `json:"exit_code,omitempty"`
	DurationMS int64  `json:"duration_ms"`
	// Tolerance is why a failing sensor may not have gated: advisory and
	// report sensors are non-gating by declaration.
	Tolerance string `json:"tolerance,omitempty"`
	Passed    bool   `json:"passed"`
	Summary   string `json:"summary,omitempty"`
}

// BudgetType identifies which budget limit was hit.
type BudgetType string

const (
	BudgetTurns     BudgetType = "turns"
	BudgetTokens    BudgetType = "tokens"
	BudgetWallClock BudgetType = "wall_clock"
)

// BudgetExceededData is the payload for KindBudgetExceeded events.
// Budget holds the machine-readable limit type; Reason holds a human
// string for logs and UIs that don't switch on Budget.
type BudgetExceededData struct {
	Budget BudgetType `json:"budget"`
	Reason string     `json:"reason"`
}

// StuckDetectedData is the payload for KindStuckDetected events.
type StuckDetectedData struct {
	Reason    string `json:"reason"`
	TurnCount int    `json:"turn_count"`
}

// SessionEndData is the payload for KindSessionEnd events.
type SessionEndData struct {
	ExitCode    int    `json:"exit_code"`
	Reason      string `json:"reason,omitempty"`
	TotalTurns  int    `json:"total_turns,omitempty"`
	TotalTokens int64  `json:"total_tokens,omitempty"`
}

// TurnApprovalData is the payload for KindTurnApprovalRequired events.
// SynthesizedFeedback matches the TermQ wire name.
type TurnApprovalData struct {
	SynthesizedFeedback string `json:"synthesized_feedback"`
}

// PlanApprovalData is the payload for KindPlanApprovalRequired events.
// Plan carries the full plan text as produced by the worker for this
// iteration; consumers render it directly without walking the trajectory
// for the preceding KindAssistantMessage. Iteration is 1-indexed; the
// initial plan is iteration 1, refinements are 2+.
type PlanApprovalData struct {
	Plan      string `json:"plan"`
	Iteration int    `json:"iteration"`
}

// BudgetSnapshotData is the payload for KindBudgetSnapshot events,
// emitted once per turn (in both plan and act phases) immediately after
// the worker's reply is recorded against the budget. Carries running
// totals consumers can read for live progress UI without shadowing the
// driver's accounting.
//
// Turns reflects the act-phase turn count and is 0 throughout the plan
// phase — RecordTurn fires only in the act loop. Plan-iteration count
// is a separate concept (see PlanApprovalData.Iteration); the two are
// deliberately not folded together to keep the act-turn budget and the
// plan-iteration budget as distinct surfaces.
//
// Tokens is the running total across both phases (plan iterations
// consume tokens; the count carries forward into the act phase).
type BudgetSnapshotData struct {
	Turns  int   `json:"turns"`
	Tokens int64 `json:"tokens"`
}

// PlanRevisedData is the payload for KindPlanRevised events. Emitted at
// the start of plan iterations 2+; the initial plan iteration is bounded
// by KindPlan instead. Iteration is the iteration we're about to produce
// (matches the Iteration value of the next KindPlanApprovalRequired).
// Notes is the user's free-form refinement payload from
// ActionReplaceFeedback.
type PlanRevisedData struct {
	Iteration int    `json:"iteration"`
	Notes     string `json:"notes"`
}
