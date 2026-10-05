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

	// The split behind tokens, and the cost the vendor reported. Each is
	// known only once a turn has reported it: a backend that reports none
	// leaves the field absent from the result, never zero.
	inputTokens           int64
	outputTokens          int64
	cacheReadTokens       int64
	usageReported         bool
	cacheReported         bool
	cacheCreationTokens   int64
	cacheCreationReported bool
	costUSD               float64
	costReported          bool
}

// Start records the session start time. Must be called before the loop begins.
func (b *Budget) Start() {
	b.startTime = time.Now()
}

// Resume restores counters from a checkpoint so caps carry across a relaunch.
// The wall-clock already spent in prior sessions back-dates the start time,
// so Exceeded() measures cumulative elapsed time, not just this process's
// lifetime. Use instead of Start on resume. A checkpoint written before the
// split and cost were recorded restores them as not yet reported.
func (b *Budget) Resume(cb CheckpointBudget) {
	b.turns = cb.Turns
	b.tokens = cb.Tokens
	b.inputTokens = cb.InputTokens
	b.outputTokens = cb.OutputTokens
	b.cacheReadTokens = cb.CacheReadTokens
	b.usageReported = cb.UsageReported
	b.cacheReported = cb.CacheReported
	b.cacheCreationTokens = cb.CacheCreationTokens
	b.cacheCreationReported = cb.CacheCreationReported
	b.costUSD = cb.CostUSD
	b.costReported = cb.CostReported
	b.startTime = time.Now().Add(-time.Duration(cb.WallConsumedMS) * time.Millisecond)
}

// checkpoint returns the budget's accounting in its persisted form.
func (b *Budget) checkpoint(planIterations int) CheckpointBudget {
	return CheckpointBudget{
		Turns:                 b.turns,
		Tokens:                b.tokens,
		WallConsumedMS:        b.WallConsumed().Milliseconds(),
		PlanIterations:        planIterations,
		InputTokens:           b.inputTokens,
		OutputTokens:          b.outputTokens,
		CacheReadTokens:       b.cacheReadTokens,
		UsageReported:         b.usageReported,
		CacheReported:         b.cacheReported,
		CacheCreationTokens:   b.cacheCreationTokens,
		CacheCreationReported: b.cacheCreationReported,
		CostUSD:               b.costUSD,
		CostReported:          b.costReported,
	}
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

// RecordUsage adds what a completed turn consumed. The token total counts
// input and output only, as it always has; the split, the cache reads and
// writes and the cost are carried beside it and never change it.
func (b *Budget) RecordUsage(t Turn) {
	b.tokens += t.Usage.InputTokens + t.Usage.OutputTokens
	if t.UsageReported {
		b.usageReported = true
		b.inputTokens += t.Usage.InputTokens
		b.outputTokens += t.Usage.OutputTokens
		if t.CacheReported {
			b.cacheReported = true
			b.cacheReadTokens += t.Usage.CacheTokens
		}
		if t.CacheCreationReported {
			b.cacheCreationReported = true
			b.cacheCreationTokens += t.Usage.CacheCreationTokens
		}
	}
	if t.CostReported {
		b.costReported = true
		b.costUSD += t.CostUSD
	}
}

// Turns returns the number of completed turns so far.
func (b *Budget) Turns() int { return b.turns }

// Tokens returns the total tokens consumed so far.
func (b *Budget) Tokens() int64 { return b.tokens }

// fillConsumed sets the split and cost fields a backend reported, leaving
// the rest nil so they are absent from the result.
func (b *Budget) fillConsumed(c *RunConsumed) {
	if b.usageReported {
		c.InputTokens = ptr(b.inputTokens)
		c.OutputTokens = ptr(b.outputTokens)
		if b.cacheReported {
			c.CacheReadTokens = ptr(b.cacheReadTokens)
		}
		if b.cacheCreationReported {
			c.CacheCreationTokens = ptr(b.cacheCreationTokens)
		}
	}
	if b.costReported {
		c.CostUSD = ptr(b.costUSD)
	}
}

func ptr[T any](v T) *T { return &v }

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
	ExitConverged = 0
	// ExitRefused is the 1 every ynh command exits with on a user or
	// configuration error. Here it almost always means the run was refused
	// before any worker started: a flag or setting the backend cannot honour,
	// a harness that cannot load, a project that chooses its own permission
	// mode, a convergence verifier that can never pass. It also covers ynh
	// failing to write its own trajectory. Distinct from ExitWorkerError, which
	// is kept for a worker that was started and then failed.
	ExitRefused          = 1
	ExitIterationCap     = 10
	ExitTokenBudget      = 11
	ExitWallClock        = 12
	ExitStuck            = 13
	ExitTamper           = 14
	ExitPlanIterationCap = 15
	ExitWorkerError      = 20
	ExitResumeError      = 21
	// ExitGateError mirrors `ynh check`'s own exit 2: the gate could not run
	// at all. Distinct from ExitWorkerError so a batch of runs can tell "the
	// agent failed" from "the harness is broken" without reading logs — the
	// second is an operator fault and every run in the batch will hit it.
	ExitGateError   = 22
	ExitUserAborted = 30
	ExitInterrupted = 31
)
