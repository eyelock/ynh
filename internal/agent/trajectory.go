package agent

import (
	"bytes"
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
	KindWorkerEnv            EventKind = "worker_env"
	KindTurnApprovalRequired EventKind = "turn_approval_required"
	KindStuckDetected        EventKind = "stuck_detected"
	// KindTamperDetected is emitted when the gate's own reference point moved
	// during a run. `ynh check --update-baseline` refuses inside an agent
	// session, but nothing stops a worker editing the baseline files directly,
	// and an agent that cannot converge has every incentive to.
	KindTamperDetected EventKind = "tamper_detected"
	KindBudgetSnapshot EventKind = "budget_snapshot"
	KindBudgetExceeded EventKind = "budget_exceeded"
	KindConverged      EventKind = "converged"
	KindSessionEnd     EventKind = "session_end"
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
	w   io.Writer
	red *Redactor
}

// NewTrajectoryWriter returns a writer that emits one JSON object per line.
func NewTrajectoryWriter(w io.Writer) *TrajectoryWriter {
	return &TrajectoryWriter{w: w}
}

// SetRedactor installs value-based redaction over everything this writer
// emits.
//
// Applied to the encoded line rather than to individual fields, because a
// secret reaches a trajectory through whatever a sensor or a worker happened
// to print — not through a field anyone thought to guard.
func (t *TrajectoryWriter) SetRedactor(r *Redactor) { t.red = r }

// Emit writes a single event to the trajectory stream.
func (t *TrajectoryWriter) Emit(kind EventKind, turn int, data any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(Event{
		Timestamp: time.Now().UTC(),
		Kind:      kind,
		Turn:      turn,
		Data:      data,
	}); err != nil {
		return err
	}
	line := t.red.Redact(buf.String())
	_, err := io.WriteString(t.w, line)
	return err
}

// Typed data payloads for specific event kinds.

// SessionStartData is the payload for KindSessionStart events.
type SessionStartData struct {
	SessionID string `json:"session_id"`
	Harness   string `json:"harness"`
	Backend   string `json:"backend"`
	Task      string `json:"task"`
	// The fields below make a run reproducible. Without the model, the harness
	// version and the commit the work started from, a trajectory records what
	// happened but not what it happened to, and cannot be replayed or audited
	// after the fact.
	Model          string `json:"model,omitempty"`
	YnhVersion     string `json:"ynh_version,omitempty"`
	HarnessVersion string `json:"harness_version,omitempty"`
	BaseCommit     string `json:"base_commit,omitempty"`
	// Budgets records the caps in force and where each came from. A cap nobody
	// chose that fires is noise in a batch result; a chosen cap that fires is a
	// finding. Aggregating a hundred runs needs them distinguishable.
	Budgets       *BudgetLimits `json:"budgets,omitempty"`
	BudgetSources *BudgetSource `json:"budget_sources,omitempty"`
}

// BudgetLimits is the resolved cap set for a run.
type BudgetLimits struct {
	MaxTurns  int   `json:"max_turns"`
	MaxTokens int64 `json:"max_tokens"`
	MaxWallMS int64 `json:"max_wall_ms"`
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
	// Status is `ynh check`'s verdict word — pass, fail, known, reported,
	// deferred. Passed alone cannot express "failing, but every failure is
	// already in the baseline", which is the difference between a regression
	// this run caused and debt it inherited. Empty for the convergence
	// verifier, which does not go through the gate.
	Status string `json:"status,omitempty"`
	// ToolVersion is the version of the tool that produced this result, when
	// the sensor declares a version_command.
	ToolVersion string `json:"tool_version,omitempty"`
	KnownCount  int    `json:"known_count,omitempty"`
	NewCount    int    `json:"new_count,omitempty"`
	Passed      bool   `json:"passed"`
	Summary     string `json:"summary,omitempty"`
}

// TamperData is the payload for KindTamperDetected events. The fingerprints
// are recorded so an operator can confirm the move rather than take the
// loop's word for it.
type TamperData struct {
	What   string `json:"what"`
	Before string `json:"before"`
	After  string `json:"after"`
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

// WorkerEnvData is the payload for KindWorkerEnv events.
//
// Names only, never values. A run that fails for want of a variable has to be
// diagnosable, but a trajectory that carried the values would be the leak the
// allowlist exists to prevent — and trajectories are written to disk and read
// by whatever grades the corpus.
type WorkerEnvData struct {
	// Passed names every variable the worker process received.
	Passed []string `json:"passed"`
	// Declared names what the harness asked for via env_passthrough, so a
	// variable that was declared but not set in the operator's environment is
	// visible as a gap rather than a mystery.
	Declared []string `json:"declared,omitempty"`
	// Missing names declared variables that were not set, which is the
	// single most likely cause of a worker that starts and cannot authenticate.
	Missing []string `json:"missing,omitempty"`
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
