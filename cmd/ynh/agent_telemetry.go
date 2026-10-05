package main

import (
	"go.opentelemetry.io/otel/attribute"

	"github.com/eyelock/ynh/internal/agent"
	"github.com/eyelock/ynh/internal/namespace"
	"github.com/eyelock/ynh/internal/telemetry"
)

// telemetryOptions tunes the spool. Production uses the format's defaults;
// tests shrink the caps to reach them.
var telemetryOptions telemetry.Options

// runOutcomes is ynh's outcome vocabulary for a run, one word per exit code
// (docs/agent.md, "Exit codes").
var runOutcomes = map[int]string{
	agent.ExitConverged:        "converged",
	agent.ExitRefused:          "refused",
	agent.ExitIterationCap:     "turn_cap",
	agent.ExitTokenBudget:      "token_budget",
	agent.ExitWallClock:        "wall_clock",
	agent.ExitStuck:            "stuck",
	agent.ExitTamper:           "tamper",
	agent.ExitPlanIterationCap: "plan_iteration_cap",
	agent.ExitWorkerError:      "worker_error",
	agent.ExitResumeError:      "resume_error",
	agent.ExitGateError:        "gate_error",
	agent.ExitUserAborted:      "user_aborted",
	agent.ExitInterrupted:      "interrupted",
}

// runOutcome names a run's exit code.
func runOutcome(code int) string {
	if o, ok := runOutcomes[code]; ok {
		return o
	}
	return "unknown"
}

// Every attribute below is an id, a name, an enum or a count (contract rule
// 10). The task, the prompt, the run's error text and every path (worktree,
// session directory, --resume, --emit-jsonl) are deliberately absent.

// runStartAttributes describes the run as it was asked for, before it starts.
func runStartAttributes(opts agent.RunOptions) []attribute.KeyValue {
	var attrs []attribute.KeyValue
	add := func(k attribute.Key, v string) {
		if v != "" {
			attrs = append(attrs, k.String(v))
		}
	}
	// Only a canonical id: anything else is refused before it loads, and a
	// value that is not an id could be a path.
	if namespace.Classify(opts.HarnessName) == namespace.RefID {
		add(telemetry.AttrHarnessName, opts.HarnessName)
	}
	add(telemetry.AttrFocus, opts.Focus)
	add(telemetry.AttrProfile, opts.Profile)
	add(telemetry.AttrVendor, opts.Backend)
	add(telemetry.AttrModelRequested, opts.Model)
	add(telemetry.AttrEffortRequested, opts.Effort)
	if opts.Resume != "" {
		attrs = append(attrs, telemetry.AttrResumed.Bool(true))
	}
	return attrs
}

// runEndAttributes describes the run as it ended, from its result.
func runEndAttributes(r *agent.RunResult) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		telemetry.AttrExitCode.Int(r.ExitCode),
		telemetry.AttrTurns.Int(r.Consumed.Turns),
	}
	add := func(k attribute.Key, v string) {
		if v != "" {
			attrs = append(attrs, k.String(v))
		}
	}
	add(telemetry.AttrSessionID, r.SessionID)
	add(telemetry.AttrVendor, r.Backend)
	add(telemetry.AttrModel, r.Model)
	add(telemetry.AttrModelRequested, r.ModelRequested)
	add(telemetry.AttrEffort, r.Effort)
	add(telemetry.AttrEffortRequested, r.EffortRequested)
	add(telemetry.AttrBoundBy, r.BoundBy)
	if h := r.Harness; h != nil {
		add(telemetry.AttrHarnessName, h.Name)
		add(telemetry.AttrHarnessVersion, h.Version)
		add(telemetry.AttrHarnessCommit, h.SHA)
	}
	// Token counts only as the backend reported them, never a zero nobody
	// measured. gen_ai.usage.input_tokens includes cached tokens, which ynh
	// keeps beside its input count rather than in it.
	c := r.Consumed
	if c.InputTokens != nil {
		input := *c.InputTokens
		if c.CacheReadTokens != nil {
			input += *c.CacheReadTokens
			attrs = append(attrs, telemetry.AttrCacheReadTokens.Int64(*c.CacheReadTokens))
		}
		if c.CacheCreationTokens != nil {
			input += *c.CacheCreationTokens
			attrs = append(attrs, telemetry.AttrCacheCreationTokens.Int64(*c.CacheCreationTokens))
		}
		attrs = append(attrs, telemetry.AttrInputTokens.Int64(input))
	}
	if c.OutputTokens != nil {
		attrs = append(attrs, telemetry.AttrOutputTokens.Int64(*c.OutputTokens))
	}
	if c.CostUSD != nil {
		attrs = append(attrs, telemetry.AttrCostUSD.Float64(*c.CostUSD))
	}
	return attrs
}
