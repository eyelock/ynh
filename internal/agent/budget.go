package agent

import (
	"fmt"
	"time"
)

// Budget tracks resource consumption for a loop session and enforces limits.
type Budget struct {
	MaxTurns  int
	MaxTokens int64
	MaxWall   time.Duration

	startTime time.Time
	turns     int
	tokens    int64
}

// Start records the session start time. Must be called before the loop begins.
func (b *Budget) Start() {
	b.startTime = time.Now()
}

// Resume restores counters from a checkpoint so caps carry across a relaunch.
// wallConsumed is the wall-clock already spent in prior sessions; the start
// time is back-dated by that amount so Exceeded() measures cumulative elapsed
// time, not just this process's lifetime. Use instead of Start on resume.
func (b *Budget) Resume(turns int, tokens int64, wallConsumed time.Duration) {
	b.turns = turns
	b.tokens = tokens
	b.startTime = time.Now().Add(-wallConsumed)
}

// WallConsumed returns the cumulative wall-clock elapsed since the (possibly
// back-dated) start time. Persisted into a checkpoint so a later Resume can
// continue the wall-clock budget from where this session left off.
func (b *Budget) WallConsumed() time.Duration {
	return time.Since(b.startTime)
}

// RecordTurn increments the completed-turn counter.
func (b *Budget) RecordTurn() {
	b.turns++
}

// RecordTokens adds token usage from a completed turn.
func (b *Budget) RecordTokens(u Usage) {
	b.tokens += u.InputTokens + u.OutputTokens
}

// Turns returns the number of completed turns so far.
func (b *Budget) Turns() int { return b.turns }

// Tokens returns the total tokens consumed so far.
func (b *Budget) Tokens() int64 { return b.tokens }

// Exceeded returns a non-empty reason string if any limit has been hit,
// or an empty string if still within budget. The BudgetType and exit code
// corresponding to the exceeded limit are also returned.
func (b *Budget) Exceeded() (reason string, budgetKind BudgetType, exitCode int) {
	if b.MaxTurns > 0 && b.turns >= b.MaxTurns {
		return fmt.Sprintf("turn cap reached (%d/%d)", b.turns, b.MaxTurns), BudgetTurns, ExitIterationCap
	}
	if b.MaxTokens > 0 && b.tokens >= b.MaxTokens {
		return fmt.Sprintf("token budget exceeded (%d/%d)", b.tokens, b.MaxTokens), BudgetTokens, ExitTokenBudget
	}
	if b.MaxWall > 0 && time.Since(b.startTime) >= b.MaxWall {
		elapsed := time.Since(b.startTime).Round(time.Second)
		return fmt.Sprintf("wall-clock limit reached (%s/%s)", elapsed, b.MaxWall), BudgetWallClock, ExitWallClock
	}
	return "", "", 0
}

// Default budget caps, applied when a run declares none.
//
// A loop with no caps is unbounded in turns, tokens and wall clock. That is not
// a control anyone chose — it is the absence of one, and token consumption
// between runs on the same task varies by more than an order of magnitude, so
// the tail is real. These are a starting point to be retuned against measured
// distributions, not tuned values.
const (
	DefaultMaxTurns  = 25
	DefaultMaxTokens = 2_000_000
	DefaultMaxWall   = 60 * time.Minute
)

// BudgetSource records whether a cap was chosen or defaulted. Analysing a
// batch of runs, a cap nobody chose that fires is noise in the result; a cap
// that was chosen and fires is a finding. They must be distinguishable.
type BudgetSource struct {
	Turns  string `json:"turns"`  // "flag" | "manifest" | "default"
	Tokens string `json:"tokens"` // "
	Wall   string `json:"wall"`   // "
}

// Exit codes for loop termination.
const (
	ExitConverged        = 0
	ExitIterationCap     = 10
	ExitTokenBudget      = 11
	ExitWallClock        = 12
	ExitStuck            = 13
	ExitTamper           = 14
	ExitPlanIterationCap = 15
	ExitWorkerError      = 20
	ExitResumeError      = 21
	ExitUserAborted      = 30
	ExitInterrupted      = 31
)
