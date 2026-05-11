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

// Exit codes for loop termination.
const (
	ExitConverged    = 0
	ExitIterationCap = 10
	ExitTokenBudget  = 11
	ExitWallClock    = 12
	ExitStuck        = 13
	ExitTamper       = 14
	ExitWorkerError  = 20
	ExitUserAborted  = 30
)
