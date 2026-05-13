package agent

import (
	"encoding/json"
	"io"
	"time"
)

// EventKind identifies the type of a trajectory event.
type EventKind string

const (
	KindSessionStart         EventKind = "session_start"
	KindPlan                 EventKind = "plan"
	KindTurnStart            EventKind = "turn_start"
	KindAssistantMessage     EventKind = "assistant_message"
	KindSensorRun            EventKind = "sensor_run"
	KindSensorResult         EventKind = "sensor_result"
	KindFeedbackSent         EventKind = "feedback_sent"
	KindTurnApprovalRequired EventKind = "turn_approval_required"
	KindStuckDetected        EventKind = "stuck_detected"
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

// SensorResultData is the payload for KindSensorResult events.
type SensorResultData struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Role       string `json:"role,omitempty"`
	ExitCode   int    `json:"exit_code,omitempty"`
	DurationMS int64  `json:"duration_ms"`
	Passed     bool   `json:"passed"`
	Summary    string `json:"summary,omitempty"`
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
