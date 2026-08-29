package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/baseline"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/gate"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/resolver"
	"github.com/eyelock/ynh/internal/vendor"
)

// ExitError carries a specific exit code for non-zero loop termination.
// The CLI handler checks for this type and calls os.Exit with the code.
type ExitError struct {
	Code    int
	Message string
}

func (e *ExitError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("agent loop exited with code %d", e.Code)
}

// RunOptions configures a single agent loop session.
type RunOptions struct {
	// HarnessName is the qualified name of the harness to load and assemble.
	HarnessName string
	// Task is the task text sent as the first user message.
	Task string
	// Profile is an optional profile name to apply to the harness before
	// assembly. Mirrors `ynh run --profile`. Mutually exclusive with Focus.
	Profile string
	// Focus is an optional focus name. The focus's prompt becomes the task
	// and the focus's bound profile (if any) is applied. Mirrors
	// `ynh run --focus`. Mutually exclusive with Task and Profile.
	Focus string
	// Backend selects the worker backend ("claude" or "codex"). Defaults to "claude".
	Backend string
	// Sandbox is "srt" or "none". Defaults to "none".
	Sandbox string
	// Model overrides the worker's default model. Empty means backend default.
	Model string

	// Budget limits — zero means unlimited.
	MaxTurns  int
	MaxTokens int64
	MaxWall   time.Duration

	// MaxPlanIterations bounds the plan-refine loop in interactive mode.
	// Each refine round (replace_feedback during plan approval) costs an
	// extra LLM round-trip; this guard prevents runaway. Zero applies the
	// default of 5. Plan iterations do NOT consume MaxTurns budget;
	// MaxTurns is the act-phase convergence cap. They DO consume tokens
	// and wall-clock.
	MaxPlanIterations int

	// ConvergenceSensor is the name of a sensor to consult as a final done-check.
	// All regular sensors must pass first; then this sensor is consulted.
	ConvergenceSensor string

	// AutoCommit creates a git commit in WorktreeDir after each assistant turn.
	AutoCommit bool
	// Interactive pauses after each turn for user approval via the control channel.
	Interactive bool
	// NoPlan skips the plan phase and sends the task directly into the act loop.
	NoPlan bool

	// WorktreeDir is where the worker subprocess runs. Defaults to cwd.
	WorktreeDir string

	// EmitJSONL is the path for trajectory output. "-" writes to Stdout.
	// If empty, trajectory events are discarded.
	EmitJSONL string

	// Resume, when non-empty, is the session directory of a prior run. The
	// loop reads <dir>/checkpoint.json and continues from the last completed
	// turn with budget counters and the worker conversation restored. The
	// trajectory is appended (not truncated); EmitJSONL defaults to
	// <dir>/trajectory.jsonl when not given explicitly.
	Resume string

	// I/O streams. Defaults to os.Stdout / os.Stderr / os.Stdin.
	Stdout io.Writer
	Stderr io.Writer
	Stdin  io.Reader

	// YNHBinary is the path to the ynh executable for sensor invocation.
	// Defaults to os.Executable() if empty.
	YNHBinary string

	// SensorOverlay is an optional per-sensor JSON patch applied before each
	// sensor run. Keys are sensor names; values are partial plugin.Sensor JSON
	// (e.g. `{"source":{"command":"make fast"}}`). Passed to ynh sensors run
	// via --sensor-overlay-json so the merge happens inside ynh.
	SensorOverlay map[string]json.RawMessage

	// backendOverride is the resolved WorkerBackend; set by tests or left nil to auto-select.
	backendOverride WorkerBackend

	// testSensorNames overrides sensor collection from the harness.
	// Set by tests that need sensor-loop behaviour without a real installed harness.
	testSensorNames []string
}

// RunLoop executes the agent loop. It returns an *ExitError on non-zero
// termination so the CLI handler can map it to os.Exit.
func RunLoop(opts RunOptions) error {
	// ── I/O defaults ─────────────────────────────────────────────────────────
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.Stdin == nil {
		opts.Stdin = os.Stdin
	}
	backend, err := validateBackend(opts.Backend)
	if err != nil {
		return err
	}
	opts.Backend = backend
	if err := validateSandbox(opts.Sandbox, opts.Backend); err != nil {
		return err
	}
	if opts.WorktreeDir == "" {
		var err error
		opts.WorktreeDir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("resolving working directory: %w", err)
		}
	}

	ynh, err := resolveYNHBinary(opts.YNHBinary)
	if err != nil {
		return err
	}

	// ── Resume state ──────────────────────────────────────────────────────────
	var resumeCP *Checkpoint
	resuming := opts.Resume != ""
	// A resume always expected verification — the original invocation had a
	// harness or it would not have been checkpointing sensor state — so a
	// resume that cannot restore one must not be able to claim convergence.
	verificationExpected := opts.HarnessName != "" || resuming
	if resuming {
		var rerr error
		resumeCP, rerr = readCheckpoint(opts.Resume)
		if rerr != nil {
			return &ExitError{Code: ExitResumeError, Message: rerr.Error()}
		}
		// The trajectory lives in the session directory; default it so callers
		// can pass just --resume <dir>.
		if opts.EmitJSONL == "" {
			opts.EmitJSONL = filepath.Join(opts.Resume, "trajectory.jsonl")
		}
		// Restore the run's identity. A resume that omits --harness previously
		// continued with no harness, therefore no sensors, and reported
		// converged — the safety verdict was forgeable by leaving out a flag.
		if opts.HarnessName == "" {
			opts.HarnessName = resumeCP.HarnessName
		}
		if opts.Profile == "" {
			opts.Profile = resumeCP.Profile
		}
		if opts.ConvergenceSensor == "" {
			opts.ConvergenceSensor = resumeCP.ConvergenceSensor
		}
		if opts.MaxTurns == 0 {
			opts.MaxTurns = resumeCP.MaxTurns
		}
		if opts.MaxTokens == 0 {
			opts.MaxTokens = resumeCP.MaxTokens
		}
		verificationExpected = true
		// A checkpoint written before these fields existed has none to restore.
		// Warn rather than refuse: failing here would break resumes that are
		// otherwise fine, and it is no longer load-bearing for safety —
		// checkConvergence declines to converge on an empty result set, so the
		// run can end un-converged but never falsely converged.
		if opts.HarnessName == "" {
			_, _ = fmt.Fprintln(opts.Stderr,
				"warning: this checkpoint records no harness, so no sensors will run. "+
					"This run cannot converge — pass --harness <name> to resume with verification.")
		}
	}

	// ── Trajectory writer (append on resume, truncate on a fresh run) ─────────
	traj := newNullTrajectory()
	if opts.EmitJSONL != "" {
		tw, cleanup, err := openTrajectory(opts.EmitJSONL, opts.Stdout, resuming)
		if err != nil {
			return err
		}
		defer cleanup()
		// Redact from the operator's whole environment, not merely what was
		// passed through. A variable the worker never received can still reach
		// the trajectory — a sensor subprocess inherits more than the worker
		// does, and a failing command prints what it was given. Redacting the
		// broader set costs nothing and covers the case the narrower one
		// misses.
		tw.SetRedactor(NewRedactor(os.Environ()))
		traj = tw
	}

	// Session directory holds checkpoint.json beside the trajectory. Durability
	// is enabled only when there is a real file path to anchor it.
	sessionDir := opts.Resume
	if sessionDir == "" {
		sessionDir = sessionDirFromEmit(opts.EmitJSONL)
	}

	sessionID := newSessionID()
	if resuming {
		sessionID = resumeCP.SessionID
	}

	// ── Load and assemble harness ─────────────────────────────────────────────
	var configPath string
	var harnessObj *harness.Harness

	if opts.HarnessName != "" {
		harnessObj, err = harness.LoadQualified(opts.HarnessName)
		if err != nil {
			return fmt.Errorf("loading harness %q: %w", opts.HarnessName, err)
		}

		// Resolve focus → prompt + bound profile. Mirrors `ynh run --focus`.
		profileName := opts.Profile
		if opts.Focus != "" {
			focus, ok := harnessObj.Focuses[opts.Focus]
			if !ok {
				return fmt.Errorf("focus %q not defined in harness", opts.Focus)
			}
			if focus.Profile != "" {
				profileName = focus.Profile
			}
			opts.Task = focus.Prompt
		}

		// Apply profile overlay before assembly so includes/hooks/MCP layered
		// in by the profile are baked into the assembled harness.
		if profileName != "" {
			harnessObj, err = harness.ResolveProfile(harnessObj, profileName)
			if err != nil {
				return fmt.Errorf("resolving profile %q: %w", profileName, err)
			}
		}

		configPath, err = assembleHarness(harnessObj, opts.Backend)
		if err != nil {
			return fmt.Errorf("assembling harness: %w", err)
		}
		defer func() { _ = os.RemoveAll(configPath) }()
	} else if opts.Focus != "" || opts.Profile != "" {
		return fmt.Errorf("--focus and --profile require --harness")
	}

	// ── Select backend ────────────────────────────────────────────────────────
	wb := opts.backendOverride
	if wb == nil {
		wb, err = selectBackend(opts.Backend)
		if err != nil {
			return err
		}
	}

	// ── Budget (restored on resume so caps carry across the relaunch) ─────────
	// Precedence: flag, then harness manifest, then built-in default. A run
	// with no caps at all is unbounded, which is the absence of a control
	// rather than a choice, so there is no "unlimited" state to fall into.
	budgetSource := BudgetSource{Turns: "flag", Tokens: "flag", Wall: "flag"}
	if opts.MaxTurns == 0 {
		budgetSource.Turns = "manifest"
		if harnessObj != nil && harnessObj.Agent != nil {
			opts.MaxTurns = harnessObj.Agent.MaxTurns
		}
	}
	if opts.MaxTurns == 0 {
		budgetSource.Turns = "default"
		opts.MaxTurns = DefaultMaxTurns
	}
	if opts.MaxTokens == 0 {
		budgetSource.Tokens = "manifest"
		if harnessObj != nil && harnessObj.Agent != nil {
			opts.MaxTokens = harnessObj.Agent.MaxTokens
		}
	}
	if opts.MaxTokens == 0 {
		budgetSource.Tokens = "default"
		opts.MaxTokens = DefaultMaxTokens
	}
	if opts.MaxWall == 0 {
		budgetSource.Wall = "manifest"
		if harnessObj != nil && harnessObj.Agent != nil && harnessObj.Agent.MaxWall != "" {
			d, dErr := time.ParseDuration(harnessObj.Agent.MaxWall)
			if dErr != nil {
				return fmt.Errorf("harness agent.max_wall %q: %w", harnessObj.Agent.MaxWall, dErr)
			}
			opts.MaxWall = d
		}
	}
	if opts.MaxWall == 0 {
		budgetSource.Wall = "default"
		opts.MaxWall = DefaultMaxWall
	}

	budget := &Budget{
		MaxTurns:  opts.MaxTurns,
		MaxTokens: opts.MaxTokens,
		MaxWall:   opts.MaxWall,
	}
	if resuming {
		budget.Resume(
			resumeCP.Budget.Turns,
			resumeCP.Budget.Tokens,
			time.Duration(resumeCP.Budget.WallConsumedMS)*time.Millisecond,
		)
	} else {
		budget.Start()
	}

	// Resume past an already-exceeded budget: honor the cap immediately and
	// exit before spawning a worker or starting a new turn.
	if resuming {
		if reason, budgetKind, code := budget.Exceeded(); reason != "" {
			_ = traj.Emit(KindBudgetExceeded, budget.Turns(), BudgetExceededData{Budget: budgetKind, Reason: reason})
			_ = traj.Emit(KindSessionEnd, budget.Turns(), SessionEndData{ExitCode: code, Reason: reason, TotalTurns: budget.Turns(), TotalTokens: budget.Tokens()})
			return &ExitError{Code: code, Message: reason}
		}
	}

	// ── Cancellable context + stop signals ────────────────────────────────────
	// SIGINT/SIGTERM cancel the worker context so an in-flight Next() unblocks;
	// the loop then exits with the last completed turn already checkpointed, so
	// a later --resume continues from there. (TermQ sends an interrupt control
	// message first, then SIGTERM after a grace period.)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		select {
		case <-sigCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	// ── Session start / resumed ───────────────────────────────────────────────
	harnessName := opts.HarnessName
	if harnessName == "" {
		harnessName = "(none)"
	}
	if resuming {
		resumedAtTurn := resumeCP.LastCompletedTurn + 1
		if emitErr := traj.Emit(KindSessionResumed, budget.Turns(), SessionResumedData{
			SessionID:       sessionID,
			Backend:         wb.Name(),
			ResumedAtTurn:   resumedAtTurn,
			RestoredTurns:   budget.Turns(),
			RestoredTokens:  budget.Tokens(),
			PendingApproval: resumeCP.PendingApproval,
		}); emitErr != nil {
			return fmt.Errorf("writing trajectory: %w", emitErr)
		}
	} else {
		start := SessionStartData{
			SessionID:  sessionID,
			Harness:    harnessName,
			Backend:    wb.Name(),
			Task:       opts.Task,
			Model:      opts.Model,
			YnhVersion: config.Version,
			BaseCommit: baseCommit(opts.WorktreeDir),
			Budgets: &BudgetLimits{
				MaxTurns:  opts.MaxTurns,
				MaxTokens: opts.MaxTokens,
				MaxWallMS: opts.MaxWall.Milliseconds(),
			},
			BudgetSources: &budgetSource,
		}
		if harnessObj != nil {
			start.HarnessVersion = harnessObj.Version
		}
		if emitErr := traj.Emit(KindSessionStart, 0, start); emitErr != nil {
			return fmt.Errorf("writing trajectory: %w", emitErr)
		}
	}

	// ── Start (or reconstruct) the worker ─────────────────────────────────────
	var resumeToken string
	if resuming {
		resumeToken = resumeCP.ResumeToken
		if resumeToken == "" {
			_, _ = fmt.Fprintf(opts.Stderr,
				"resume: no backend resume token for %s; continuing with fresh conversation context (loop accounting restored)\n",
				wb.Name())
		}
	}
	// The worker gets the variables the harness declared, and nothing else.
	// StartOptions.Env existed and was never populated, so the worker inherited
	// the parent environment wholesale — meaning the agent held every credential
	// the operator held. ynh declares the scope; the process boundary that makes
	// it meaningful is the container's (see "ynh does not own containment").
	// Mark the worker as an agent session. This is ynh's own variable, not a
	// passthrough of the operator's environment, and it is what lets a gate
	// recognise that the process asking it a question is the process it is
	// gating. See the baseline write refusal in cmd/ynh/check.go.
	workerEnv := []string{"YNH_AGENT_SESSION=" + sessionID}
	if sessionDir != "" {
		workerEnv = append(workerEnv, "YNH_AGENT_SESSION_DIR="+sessionDir)
	}
	if harnessObj != nil {
		for _, name := range harnessObj.EnvPassthrough {
			if v, ok := os.LookupEnv(name); ok {
				workerEnv = append(workerEnv, name+"="+v)
			}
		}
	}
	// Record what actually reached the worker, names only. An agent that
	// cannot authenticate because a variable was never declared is otherwise
	// indistinguishable from one that is simply failing.
	{
		var declared, missing []string
		if harnessObj != nil {
			declared = harnessObj.EnvPassthrough
			for _, name := range declared {
				if _, ok := os.LookupEnv(name); !ok {
					missing = append(missing, name)
				}
			}
		}
		_ = traj.Emit(KindWorkerEnv, 0, WorkerEnvData{
			Passed:   envNames(workerEnvFor(workerEnv)),
			Declared: declared,
			Missing:  missing,
		})
	}

	sess, err := wb.Start(ctx, StartOptions{
		WorktreeDir: opts.WorktreeDir,
		ConfigPath:  configPath,
		Sandbox:     opts.Sandbox,
		Model:       opts.Model,
		ResumeToken: resumeToken,
		Env:         workerEnv,
		Stderr:      opts.Stderr,
	})
	if err != nil {
		_ = traj.Emit(KindSessionEnd, 0, SessionEndData{ExitCode: ExitWorkerError, Reason: err.Error()})
		return &ExitError{Code: ExitWorkerError, Message: fmt.Sprintf("starting worker: %v", err)}
	}
	defer func() { _ = sess.Close() }()

	watchdog := NewWatchdog()

	// ── Control channel ───────────────────────────────────────────────────────
	ctrl := NewControlReader(opts.Stdin)

	// In non-interactive mode nothing else consumes the control channel, so a
	// dedicated reader turns {"action":"interrupt"} into a context cancel.
	// Interactive mode handles interrupt inside the approval gates instead.
	if !opts.Interactive {
		go func() {
			for msg := range ctrl.C() {
				if msg.Action == ActionInterrupt {
					cancel()
					return
				}
			}
		}()
	}

	// ── Checkpoint writer ─────────────────────────────────────────────────────
	// cp is mutated and re-saved at each turn boundary; ResumeToken/Budget are
	// refreshed from live state on every write.
	cp := &Checkpoint{
		SessionID:         sessionID,
		Backend:           wb.Name(),
		Task:              opts.Task,
		HarnessName:       opts.HarnessName,
		Profile:           opts.Profile,
		ConvergenceSensor: opts.ConvergenceSensor,
		MaxTurns:          opts.MaxTurns,
		MaxTokens:         opts.MaxTokens,
	}
	planIterations := 0
	if resuming {
		// Carry the prior checkpoint's state forward so per-turn saves preserve
		// plan/approval fields across a second interrupt-and-resume.
		planIterations = resumeCP.Budget.PlanIterations
		cp.Phase = resumeCP.Phase
		cp.PlanFinalized = resumeCP.PlanFinalized
		cp.ApprovedPlan = resumeCP.ApprovedPlan
		cp.LastCompletedTurn = resumeCP.LastCompletedTurn
		cp.PendingMessage = resumeCP.PendingMessage
		cp.PendingApproval = resumeCP.PendingApproval
		if opts.Task == "" {
			cp.Task = resumeCP.Task
		}
	}
	saveCheckpoint := func() {
		if sessionDir == "" {
			return
		}
		cp.ResumeToken = sess.ResumeToken()
		cp.Budget = CheckpointBudget{
			Turns:          budget.Turns(),
			Tokens:         budget.Tokens(),
			WallConsumedMS: budget.WallConsumed().Milliseconds(),
			PlanIterations: planIterations,
		}
		if err := writeCheckpoint(sessionDir, cp); err != nil {
			_, _ = fmt.Fprintf(opts.Stderr, "checkpoint write failed: %v\n", err)
		}
	}
	interruptExit := func(atTurn int) error {
		const reason = "interrupted (resumable)"
		_ = traj.Emit(KindSessionEnd, atTurn, SessionEndData{
			ExitCode:    ExitInterrupted,
			Reason:      reason,
			TotalTurns:  budget.Turns(),
			TotalTokens: budget.Tokens(),
		})
		return &ExitError{Code: ExitInterrupted, Message: reason}
	}

	// ── Collect sensors ───────────────────────────────────────────────────────
	var sensorNames []string
	var convergenceSensor string

	if opts.testSensorNames != nil {
		// Test injection: use the provided names, skip harness-based collection.
		sensorNames = opts.testSensorNames
	} else if harnessObj != nil {
		for name, s := range harnessObj.Sensors {
			if s.Role == "convergence-verifier" || name == opts.ConvergenceSensor {
				if convergenceSensor == "" {
					convergenceSensor = name
				}
				continue
			}
			if s.Role != "stuck-recovery" {
				sensorNames = append(sensorNames, name)
			}
		}
		// Sorted only so --only and the trajectory are deterministic. Execution
		// order is `ynh check`'s to decide now that it runs them.
		sort.Strings(sensorNames)
	} // end else if harnessObj != nil

	// ── Plan phase ────────────────────────────────────────────────────────────
	// Plan iteration loop: produces a plan, optionally awaits user approval,
	// loops back into "revise the plan" if the user replied with refinement
	// feedback. Non-interactive mode runs exactly one iteration and continues
	// straight into the act phase.
	//
	// Prompts ask only for an inline reply — no file write. claude in plan
	// mode is read-only, and demanding a plan.md write there caused the
	// worker to stall at its own permission gate instead of producing a
	// plan. The act phase has write access if a plan file is wanted later.
	//
	// approvedPlan is lifted out of the loop so the act phase can forward
	// the final plan content into its first message; otherwise the worker
	// would enter act mode with only the original task and lose every
	// refinement it just produced.
	var approvedPlan string
	runPlan := !opts.NoPlan
	if resuming && resumeCP.Phase == PhaseAct {
		// Plan was already finalized (or skipped via --no-plan) before the
		// checkpoint; continue straight into the act phase.
		runPlan = false
		approvedPlan = resumeCP.ApprovedPlan
	}
	if runPlan {
		// Persist a plan-phase checkpoint so a crash during planning is
		// resumable (the plan phase is simply re-run from scratch on resume).
		cp.Phase = PhasePlan
		cp.PlanFinalized = false
		cp.LastCompletedTurn = 0
		cp.PendingMessage = ""
		saveCheckpoint()

		maxPlanIters := opts.MaxPlanIterations
		if maxPlanIters <= 0 {
			maxPlanIters = 5
		}
		planMsg := fmt.Sprintf(
			"Write a clear, structured plan for the following task. Reply with the plan in your message — do not write any files yet.\n\nTask: %s",
			opts.Task,
		)
	planLoop:
		for planIter := 1; ; planIter++ {
			planIterations = planIter
			if planIter == 1 {
				if emitErr := traj.Emit(KindPlan, 0, nil); emitErr != nil {
					return fmt.Errorf("writing trajectory: %w", emitErr)
				}
			}
			if err := sess.Send(planMsg); err != nil {
				_ = traj.Emit(KindSessionEnd, 0, SessionEndData{ExitCode: ExitWorkerError, Reason: err.Error()})
				return &ExitError{Code: ExitWorkerError, Message: fmt.Sprintf("sending plan request: %v", err)}
			}
			planTurn, err := sess.Next()
			if err == io.EOF {
				_ = traj.Emit(KindSessionEnd, 0, SessionEndData{ExitCode: ExitWorkerError, Reason: "worker exited during plan phase"})
				return &ExitError{Code: ExitWorkerError, Message: "worker exited during plan phase"}
			}
			if err != nil {
				_ = traj.Emit(KindSessionEnd, 0, SessionEndData{ExitCode: ExitWorkerError, Reason: err.Error()})
				return &ExitError{Code: ExitWorkerError, Message: fmt.Sprintf("plan turn: %v", err)}
			}
			_ = traj.Emit(KindAssistantMessage, 0, planTurn.Content)
			budget.RecordTokens(planTurn.Usage)
			_ = traj.Emit(KindBudgetSnapshot, 0, BudgetSnapshotData{
				Turns:  budget.Turns(),
				Tokens: budget.Tokens(),
			})

			// Plan iterations consume tokens and wall-clock but not the act-phase
			// turn cap. budget.Exceeded checks turns first; turns is still 0 here
			// (RecordTurn is act-phase only) so the turns branch is dormant.
			if reason, budgetKind, code := budget.Exceeded(); reason != "" {
				_ = traj.Emit(KindBudgetExceeded, 0, BudgetExceededData{Budget: budgetKind, Reason: reason})
				_ = traj.Emit(KindSessionEnd, 0, SessionEndData{ExitCode: code, Reason: reason, TotalTokens: budget.Tokens()})
				return &ExitError{Code: code, Message: reason}
			}

			if !opts.Interactive {
				approvedPlan = planTurn.Content
				break planLoop
			}

			if emitErr := traj.Emit(KindPlanApprovalRequired, 0, PlanApprovalData{
				Plan:      planTurn.Content,
				Iteration: planIter,
			}); emitErr != nil {
				return fmt.Errorf("writing trajectory: %w", emitErr)
			}
			action, replyFeedback, aborted := waitForApproval(ctrl, ActionApprovePlan, ActionRejectPlan)
			if aborted {
				// Interrupt/SIGTERM during plan approval: the plan-phase
				// checkpoint already exists, so --resume re-runs the plan.
				return interruptExit(0)
			}
			if action == ActionRejectPlan {
				reason := "plan rejected by user"
				if replyFeedback != "" {
					reason = "plan rejected by user: " + replyFeedback
				}
				_ = traj.Emit(KindSessionEnd, 0, SessionEndData{ExitCode: ExitUserAborted, Reason: reason})
				return &ExitError{Code: ExitUserAborted, Message: reason}
			}
			// ActionApprovePlan: empty feedback means plain approve; non-empty
			// means refine — produce a revised plan addressing the feedback.
			if replyFeedback == "" {
				approvedPlan = planTurn.Content
				break planLoop
			}
			if planIter >= maxPlanIters {
				reason := fmt.Sprintf("plan iteration cap reached (%d/%d)", planIter, maxPlanIters)
				_ = traj.Emit(KindSessionEnd, 0, SessionEndData{ExitCode: ExitPlanIterationCap, Reason: reason})
				return &ExitError{Code: ExitPlanIterationCap, Message: reason}
			}
			nextIter := planIter + 1
			_ = traj.Emit(KindPlanRevised, 0, PlanRevisedData{Iteration: nextIter, Notes: replyFeedback})
			planMsg = fmt.Sprintf(
				"Revise the plan to address this feedback. Reply with the full revised plan in your message — do not write any files yet.\n\nFeedback:\n%s",
				replyFeedback,
			)
		}
	}

	// ── Act loop ──────────────────────────────────────────────────────────────
	// First message: forward the approved plan into the act phase so the
	// worker has the full text in context as it transitions to write mode.
	// Without this, refined plans evaporate at the phase boundary and the
	// worker re-derives intent from the original task alone. On resume the
	// pending message from the checkpoint is re-sent instead, which redoes at
	// most the interrupted turn.
	var firstMsg string
	switch {
	case resuming && resumeCP.Phase == PhaseAct:
		firstMsg = resumeCP.PendingMessage
	case runPlan:
		firstMsg = fmt.Sprintf(
			"Plan approved:\n\n%s\n\nProceed with implementation. Original task: %s",
			approvedPlan,
			opts.Task,
		)
	default: // --no-plan fresh run
		firstMsg = opts.Task
	}

	// Persist the act-entry checkpoint for fresh runs and resume-restarted
	// plans. A phase==act resume already has an authoritative checkpoint, so
	// it is left untouched (last_completed_turn and pending_message stand).
	// (!resuming short-circuits before the nil resumeCP deref.)
	if !resuming || resumeCP.Phase != PhaseAct {
		cp.Phase = PhaseAct
		cp.PlanFinalized = runPlan
		cp.ApprovedPlan = approvedPlan
		cp.LastCompletedTurn = budget.Turns()
		cp.PendingMessage = firstMsg
		cp.PendingApproval = ""
		saveCheckpoint()
	}

	// Snapshot the gate's own reference point.
	//
	// `ynh check --update-baseline` refuses inside an agent session, but that
	// only closes the front door: nothing stops a worker editing the baseline
	// files directly, and an agent that cannot converge has every incentive
	// to. Comparing this each turn is what makes "the agent may not rewrite
	// its own gate" enforced rather than merely refused.
	baselineFP, fpErr := baseline.Fingerprint(opts.WorktreeDir)
	if fpErr != nil {
		// A baseline that cannot be read before the run has even started is a
		// broken gate, not tampering — nothing has had the chance to touch it.
		_ = traj.Emit(KindSessionEnd, 0, SessionEndData{ExitCode: ExitGateError, Reason: fpErr.Error()})
		return &ExitError{Code: ExitGateError, Message: fmt.Sprintf("reading baseline: %v", fpErr)}
	}

	if err := sess.Send(firstMsg); err != nil {
		_ = traj.Emit(KindSessionEnd, 0, SessionEndData{ExitCode: ExitWorkerError, Reason: err.Error()})
		return &ExitError{Code: ExitWorkerError, Message: fmt.Sprintf("sending first message: %v", err)}
	}

	for {
		// ── Interrupt check ───────────────────────────────────────────────────
		// An interrupt/SIGTERM that arrived between turns (e.g. a cursor turn
		// that ran to completion before the cancel took effect). The last
		// completed turn is already checkpointed, so --resume continues from it.
		if ctx.Err() != nil {
			return interruptExit(budget.Turns())
		}

		// ── Budget check ──────────────────────────────────────────────────────
		if reason, budgetKind, code := budget.Exceeded(); reason != "" {
			_ = traj.Emit(KindBudgetExceeded, budget.Turns(), BudgetExceededData{Budget: budgetKind, Reason: reason})
			_ = traj.Emit(KindSessionEnd, budget.Turns(), SessionEndData{ExitCode: code, Reason: reason, TotalTurns: budget.Turns(), TotalTokens: budget.Tokens()})
			return &ExitError{Code: code, Message: reason}
		}

		turnN := budget.Turns() + 1
		_ = traj.Emit(KindTurnStart, turnN, nil)

		// ── Wait for assistant turn ───────────────────────────────────────────
		turn, err := sess.Next()
		// A cancelled context means an interrupt/SIGTERM killed the worker
		// mid-turn; the partial turn is discarded and resume redoes it.
		if ctx.Err() != nil {
			return interruptExit(turnN)
		}
		if err == io.EOF {
			_ = traj.Emit(KindSessionEnd, turnN, SessionEndData{ExitCode: ExitWorkerError, Reason: "worker exited unexpectedly"})
			return &ExitError{Code: ExitWorkerError, Message: "worker exited before convergence"}
		}
		if err != nil {
			_ = traj.Emit(KindSessionEnd, turnN, SessionEndData{ExitCode: ExitWorkerError, Reason: err.Error()})
			return &ExitError{Code: ExitWorkerError, Message: fmt.Sprintf("worker turn %d: %v", turnN, err)}
		}
		_ = traj.Emit(KindAssistantMessage, turnN, turn.Content)

		budget.RecordTurn()
		budget.RecordTokens(turn.Usage)
		_ = traj.Emit(KindBudgetSnapshot, turnN, BudgetSnapshotData{
			Turns:  budget.Turns(),
			Tokens: budget.Tokens(),
		})

		// ── Gate integrity ────────────────────────────────────────────────────
		// Before the gate is consulted, and before auto-commit: a baseline the
		// worker just widened would otherwise forgive the very failures this
		// turn introduced, and the run would converge on amnesty it granted
		// itself — with the tampering committed on the way past.
		if fp, err := baseline.Fingerprint(opts.WorktreeDir); err != nil || fp != baselineFP {
			after, reason := "unreadable", "the baseline can no longer be read"
			if err == nil {
				after, reason = fp, "the baseline changed during the run"
			}
			_ = traj.Emit(KindTamperDetected, turnN, TamperData{
				What: "baseline", Before: baselineFP, After: after,
			})
			_ = traj.Emit(KindSessionEnd, turnN, SessionEndData{ExitCode: ExitTamper, Reason: reason})
			return &ExitError{Code: ExitTamper, Message: reason +
				" — nothing being gated may rewrite the gate's reference point"}
		}

		// ── Auto-commit ───────────────────────────────────────────────────────
		if opts.AutoCommit {
			if err := gitAutoCommit(opts.WorktreeDir, turnN); err != nil {
				_, _ = fmt.Fprintf(opts.Stderr, "auto-commit turn %d: %v\n", turnN, err)
			}
		}

		// ── Run the gate ──────────────────────────────────────────────────────
		// One `ynh check` call rather than one `ynh sensors run` per sensor, so
		// the loop inherits the gate's policy instead of re-deriving a second,
		// contradictory one: the baseline ratchet, the tolerance rules, and the
		// verdict all now come from the same place a human running `ynh check`
		// would get them from.
		var checkEnv *gate.Envelope
		if len(sensorNames) > 0 {
			for _, name := range sensorNames {
				_ = traj.Emit(KindSensorRun, turnN, name)
			}
			env, checkErr := RunCheck(ynh, opts.HarnessName, opts.WorktreeDir, sensorNames, opts.SensorOverlay)
			if checkErr != nil {
				// A gate that cannot run is an operator fault, not agent work.
				// Degrading to "no sensor results" would keep sending the worker
				// turns nothing could verify until the budget ran out, and then
				// report that exhaustion as the agent's failure.
				_ = traj.Emit(KindSessionEnd, turnN, SessionEndData{ExitCode: ExitGateError, Reason: checkErr.Error()})
				return &ExitError{Code: ExitGateError, Message: fmt.Sprintf("gate turn %d: %v", turnN, checkErr)}
			}
			checkEnv = env
			for _, r := range env.Sensors {
				if !r.Ran() {
					continue
				}
				_ = traj.Emit(KindSensorResult, turnN, SensorResultData{
					Name:       r.Name,
					Kind:       r.Kind,
					Status:     r.Status,
					ExitCode:   r.ExitCode,
					DurationMS: r.DurationMS,
					Tolerance:  r.Tolerance,
					KnownCount: r.KnownCount,
					NewCount:   r.NewCount,
					// Passed stays "did this sensor pass", not "did it block".
					// Tolerance is what explains a failure that did not gate;
					// folding the two together would hide advisory failures from
					// anyone reading the trajectory afterwards.
					Passed:  r.Status != gate.StatusFail,
					Summary: resultSummary(r),
				})
			}
		}

		// ── Check convergence ─────────────────────────────────────────────────
		if converged, feedback := checkConvergence(checkEnv, convergenceSensor, ynh, opts.HarnessName, opts.WorktreeDir, traj, turnN, verificationExpected); converged {
			_ = traj.Emit(KindConverged, turnN, nil)
			_ = traj.Emit(KindSessionEnd, turnN, SessionEndData{ExitCode: ExitConverged, TotalTurns: budget.Turns(), TotalTokens: budget.Tokens()})
			return nil
		} else if feedback == "" {
			// All sensors passed but no convergence sensor; treat as converged.
			_ = traj.Emit(KindConverged, turnN, nil)
			_ = traj.Emit(KindSessionEnd, turnN, SessionEndData{ExitCode: ExitConverged, TotalTurns: budget.Turns(), TotalTokens: budget.Tokens()})
			return nil
		} else {
			// ── Stuckness watchdog ─────────────────────────────────────────────
			sensorHash := SensorHash(checkEnv)
			if reason := watchdog.RecordTurn(turn.Content, sensorHash); reason != "" {
				_ = traj.Emit(KindStuckDetected, turnN, StuckDetectedData{Reason: reason, TurnCount: budget.Turns()})
				_ = traj.Emit(KindSessionEnd, turnN, SessionEndData{ExitCode: ExitStuck, Reason: reason})
				return &ExitError{Code: ExitStuck, Message: "stuck: " + reason}
			}

			// ── Interactive approval ───────────────────────────────────────────
			if opts.Interactive {
				if emitErr := traj.Emit(KindTurnApprovalRequired, turnN, TurnApprovalData{SynthesizedFeedback: feedback}); emitErr != nil {
					return fmt.Errorf("writing trajectory: %w", emitErr)
				}
				_, replacement, aborted := waitForApproval(ctrl, ActionApproveTurn, ActionRejectPlan)
				if aborted {
					// Interrupt at the turn gate: the checkpoint reflects the
					// last completed turn, so --resume redoes this turn.
					return interruptExit(turnN)
				}
				if replacement != "" {
					feedback = replacement
				}
			}

			_ = traj.Emit(KindFeedbackSent, turnN, feedback)
			if err := sess.Send(feedback); err != nil {
				_ = traj.Emit(KindSessionEnd, turnN, SessionEndData{ExitCode: ExitWorkerError, Reason: err.Error()})
				return &ExitError{Code: ExitWorkerError, Message: fmt.Sprintf("sending feedback turn %d: %v", turnN, err)}
			}

			// ── Checkpoint the completed turn ──────────────────────────────────
			// Turn turnN is now fully observed and its feedback is queued as the
			// next message; persist so a crash redoes at most this next turn.
			cp.Phase = PhaseAct
			cp.LastCompletedTurn = turnN
			cp.PendingMessage = feedback
			saveCheckpoint()
		}
	}
}

// checkConvergence decides whether the turn's gate result means done.
//
// The verdict is `ynh check`'s, not the loop's. What blocks — tolerance,
// baseline forgiveness, the pass/fail rule for each sensor kind — is settled
// there, so a run and a human at a terminal cannot reach opposite conclusions
// about the same manifest.
//
// Returns (converged=false, feedback=<synthesized feedback>) when work remains.
// verificationExpected is true when the run was configured to verify itself —
// a harness was named, or this is a resume that should have restored one.
func checkConvergence(
	env *gate.Envelope,
	convergenceSensor, ynh, harnessName, cwd string,
	traj *TrajectoryWriter,
	turnN int,
	verificationExpected bool,
) (bool, string) {
	// Convergence needs evidence when verification was asked for. An empty
	// result set made allPassed vacuously true, so a run whose harness went
	// missing — which is what --resume without --harness produced — reported
	// converged and exited 0 after one turn, having verified nothing.
	//
	// The distinction matters: a run started with no harness never asked to be
	// verified and is just an agent runner, so the worker declaring itself done
	// is the only signal there is. A run that expected a harness and has no
	// results has lost something, and must not claim a verdict it cannot back.
	ran := 0
	canGate := 0
	if env != nil {
		for _, r := range env.Sensors {
			if !r.Ran() {
				continue
			}
			ran++
			// Only a blocking *command* sensor can ever produce a blocked
			// verdict. A files sensor has no derivable pass/fail and a focus
			// sensor needs a runtime ynh does not own, so counting either as
			// gating just because its tolerance says "blocking" would let a
			// harness that gates on nothing satisfy the guard below without
			// ever being able to fail.
			if r.Tolerance == "blocking" && r.Kind == "command" {
				canGate++
			}
		}
	}
	if verificationExpected && ran == 0 {
		return false, "verification was expected but no sensors ran, so convergence cannot be confirmed"
	}
	if verificationExpected && ran > 0 && canGate == 0 {
		return false, "no blocking command sensor ran, so nothing gates convergence"
	}

	if env != nil && env.Verdict == gate.VerdictBlocked {
		return false, synthesizeFeedback(env)
	}

	// Gate green — consult convergence-verifier if declared. It stays a
	// direct sensor run: resolving a focus sensor needs an agent runtime,
	// which is why `ynh check` reports it as deferred rather than judging it.
	if convergenceSensor != "" && ynh != "" && harnessName != "" {
		_ = traj.Emit(KindSensorRun, turnN, convergenceSensor)
		cvResult, err := RunSensor(ynh, harnessName, convergenceSensor, cwd, "")
		// Convergence is gate.StatusPass, not a locally invented verdict.
		// #214 routed the gate through `ynh check` but left this call site
		// deriving its own answer, and that answer said a files sensor had
		// converged because a path existed — contents never read, and the
		// path inside the agent's own write path. A files sensor now yields
		// StatusReported, which is not StatusPass, so it cannot converge:
		// the refusal falls out of existing doctrine rather than adding a rule.
		converged := err == nil &&
			gate.StatusForKind(cvResult.Kind, cvResult.ExitCode) == gate.StatusPass
		if !converged {
			var summary string
			if err != nil {
				summary = err.Error()
			} else {
				summary = cvResult.Summary()
			}
			_ = traj.Emit(KindSensorResult, turnN, SensorResultData{
				Name:    convergenceSensor,
				Kind:    "focus",
				Role:    "convergence-verifier",
				Passed:  false,
				Summary: summary,
			})
			return false, "All sensors passed but convergence verifier says: " + summary
		}
		_ = traj.Emit(KindSensorResult, turnN, SensorResultData{
			Name:   convergenceSensor,
			Kind:   "focus",
			Role:   "convergence-verifier",
			Passed: true,
		})
	}

	return true, ""
}

// maxFeedbackLines caps how much of one sensor's output reaches the worker.
//
// The old cap was three lines, which for a linter meant the agent was told
// there was a problem and had to re-run the tool itself to find out what —
// paying for the sensor twice. With a baseline in play the body is already
// narrowed to the lines this change introduced, so a larger cap costs little
// and usually carries the whole remediation.
const maxFeedbackLines = 20

// resultSummary renders the part of a sensor result the worker should act on.
//
// For a failing sensor with a baseline that is only the new lines. Showing an
// agent the twelve findings it did not introduce alongside the one it did is
// how a turn gets spent fixing someone else's debt — and, at the end of it,
// how a converged run turns out to have rewritten files nobody asked about.
func resultSummary(r gate.Result) string {
	switch r.Status {
	case gate.StatusPass:
		return "passed"
	case gate.StatusKnown:
		return fmt.Sprintf("failing, but all %d %s recorded in the baseline — pre-existing, not yours to fix",
			r.KnownCount, pluralise(r.KnownCount, "failure"))
	case gate.StatusReported:
		return "reported — observation only, no pass/fail verdict"
	case gate.StatusDeferred:
		return "deferred — needs an agent runtime"
	case gate.StatusSkipped:
		return "skipped"
	}

	body := strings.TrimSpace(r.NewOutput)
	if body == "" {
		body = strings.TrimSpace(r.Stdout)
	}
	if body == "" {
		body = strings.TrimSpace(r.Stderr)
	}
	if body == "" {
		return fmt.Sprintf("failed (exit %d)", r.ExitCode)
	}
	lines := strings.Split(body, "\n")
	if len(lines) > maxFeedbackLines {
		lines = append(lines[:maxFeedbackLines:maxFeedbackLines], "…")
	}
	return strings.Join(lines, "\n")
}

func pluralise(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// synthesizeFeedback produces the user-turn message injected after a
// non-converged turn.
//
// Every sensor that ran is listed with the gate's own status word, so
// "known" and "reported" are visible as distinct from "fail". An agent shown
// a bare failed/passed split has no way to tell recorded debt from a
// regression it just caused, and will try to fix both.
func synthesizeFeedback(env *gate.Envelope) string {
	var sb strings.Builder
	sb.WriteString("<sensor-results>\n")
	for _, r := range env.Sensors {
		if !r.Ran() {
			continue
		}
		durationSec := float64(r.DurationMS) / 1000
		_, _ = fmt.Fprintf(&sb, "  <%s status=%q duration=%.1fs", r.Name, r.Status, durationSec)
		if r.Status == gate.StatusFail && r.Tolerance != "blocking" {
			_, _ = fmt.Fprintf(&sb, " tolerance=%q", r.Tolerance)
		}
		summary := resultSummary(r)
		if r.Status == gate.StatusPass || summary == "" {
			sb.WriteString("/>")
		} else {
			_, _ = fmt.Fprintf(&sb, ">\n%s\n  </%s>", summary, r.Name)
		}
		sb.WriteString("\n")
	}
	sb.WriteString("</sensor-results>\n\n")
	if env.Baseline != nil && env.Baseline.Known > 0 {
		_, _ = fmt.Fprintf(&sb, "%d pre-existing failure(s) are recorded in the baseline and are not "+
			"blocking. Do not fix them unless the task asks you to.\n\n", env.Baseline.Known)
	}
	sb.WriteString("Continue work. Address the failing sensors first.")
	return sb.String()
}

// waitForApproval blocks until the control channel delivers one of the
// expected approval or abort actions. Returns (action, feedback, aborted).
//
// Feedback semantics by action:
//   - approveAction: feedback is empty (plain approval).
//   - ActionReplaceFeedback: returned as approveAction with the user's
//     replacement payload — caller decides whether to augment the next
//     prompt (act phase) or trigger a refine iteration (plan phase).
//   - rejectAction: feedback carries the optional "why" payload, surfaced
//     in KindSessionEnd.Reason for telemetry. Empty if the consumer
//     rejected without notes.
//
// aborted is true if an interrupt was received or stdin closed.
func waitForApproval(ctrl *ControlReader, approveAction, rejectAction ControlAction) (ControlAction, string, bool) {
	for msg := range ctrl.C() {
		switch msg.Action {
		case approveAction:
			return approveAction, "", false
		case ActionReplaceFeedback:
			return approveAction, msg.Feedback, false
		case rejectAction, ActionRejectPlan:
			return rejectAction, msg.Feedback, false
		case ActionInterrupt:
			return ActionInterrupt, "", true
		}
	}
	// Control channel closed (stdin EOF) — treat as interrupt.
	return ActionInterrupt, "", true
}

// assembleHarness assembles the harness for the named vendor backend into
// a temporary directory. The caller is responsible for os.RemoveAll on the
// returned path.
func assembleHarness(h *harness.Harness, backendName string) (string, error) {
	cfg, cfgErr := config.Load()
	if cfgErr != nil {
		return "", fmt.Errorf("loading config: %w", cfgErr)
	}
	adapter, err := vendor.Get(backendName)
	if err != nil {
		return "", fmt.Errorf("vendor %q: %w", backendName, err)
	}

	dir, err := os.MkdirTemp("", "ynh-agent-")
	if err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}

	// Resolve includes exactly as `ynh run` does. Assembling from h.Dir alone
	// silently dropped every base and profile include, so a harness composed
	// from other repositories ran the loop with none of that content — and
	// profile-level artifact swapping was a no-op.
	resolved, resErr := resolver.Resolve(h, cfg)
	if resErr != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("resolving includes: %w", resErr)
	}
	var content []resolver.ResolvedContent
	for _, r := range resolved {
		content = append(content, r.Content)
	}
	content = append(content, resolver.ResolvedContent{BasePath: h.Dir})

	if err := assembler.AssembleTo(dir, adapter, content); err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("assembling harness: %w", err)
	}

	// Generate vendor-native hook config.
	if len(h.Hooks) > 0 {
		hookFiles, err := adapter.GenerateHookConfig(h.Hooks)
		if err != nil {
			_ = os.RemoveAll(dir)
			return "", fmt.Errorf("generating hook config: %w", err)
		}
		for relPath, data := range hookFiles {
			absPath := fmt.Sprintf("%s/%s", dir, relPath)
			if mkdirErr := os.MkdirAll(dirOf(absPath), 0o755); mkdirErr != nil {
				_ = os.RemoveAll(dir)
				return "", mkdirErr
			}
			if writeErr := os.WriteFile(absPath, data, 0o644); writeErr != nil {
				_ = os.RemoveAll(dir)
				return "", writeErr
			}
		}
	}

	// Generate vendor-native MCP config.
	if len(h.MCPServers) > 0 {
		servers, expErr := plugin.ExpandMCPEnv(h.MCPServers, h.EnvPassthrough, os.LookupEnv)
		if expErr != nil {
			_ = os.RemoveAll(dir)
			return "", expErr
		}
		mcpFiles, err := adapter.GenerateMCPConfig(servers)
		if err != nil {
			_ = os.RemoveAll(dir)
			return "", fmt.Errorf("generating MCP config: %w", err)
		}
		for relPath, data := range mcpFiles {
			absPath := fmt.Sprintf("%s/%s", dir, relPath)
			if mkdirErr := os.MkdirAll(dirOf(absPath), 0o755); mkdirErr != nil {
				_ = os.RemoveAll(dir)
				return "", mkdirErr
			}
			if writeErr := os.WriteFile(absPath, data, 0o644); writeErr != nil {
				_ = os.RemoveAll(dir)
				return "", writeErr
			}
		}
	}

	return dir, nil
}

// gitAutoCommit runs `git add -A && git commit` in the given directory.
// A git commit with nothing to commit is silently ignored.
func gitAutoCommit(dir string, turnN int) error {
	addCmd := exec.Command("git", "-C", dir, "add", "-A")
	if err := addCmd.Run(); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	commitCmd := exec.Command("git", "-C", dir, "commit", "-m",
		fmt.Sprintf("agent: turn %d", turnN))
	out, err := commitCmd.CombinedOutput()
	if err != nil {
		// Exit 1 from git commit means "nothing to commit" — not an error.
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil
		}
		return fmt.Errorf("git commit: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// validateBackend defaults the backend name and rejects unknown values
// (including common near-misses like "claude-code") with a clear error
// pointing at the canonical names. Run before any vendor lookup so
// callers get a useful message instead of "unknown vendor" from deeper in.
// validateSandbox rejects a sandbox request the chosen backend cannot honour.
//
// `--sandbox srt` was read only by the Claude backend; codex and cursor
// contained no reference to it, so `--sandbox srt --backend codex` ran
// completely unsandboxed and said nothing. A declared containment control that
// silently does not apply is worse than an absent one, because it gets relied
// upon — see "ynh does not own containment" in docs/harness-engineering.md.
//
// This is an error, not a warning. A warning on stderr is not a control.
func validateSandbox(sandbox, backend string) error {
	switch sandbox {
	case "", "none":
		return nil
	case "srt":
		if backend != "claude" {
			return fmt.Errorf(
				"--sandbox srt is not supported by the %s backend (only claude implements it); "+
					"run inside a container you configured, or use --sandbox none to proceed deliberately unsandboxed",
				backend)
		}
		return nil
	default:
		return fmt.Errorf("unknown sandbox %q (supported: none, srt)", sandbox)
	}
}

func validateBackend(name string) (string, error) {
	switch name {
	case "":
		return "claude", nil
	case "claude", "codex", "cursor":
		return name, nil
	case "claude-code":
		return "", fmt.Errorf("unknown backend %q (did you mean \"claude\"?)", name)
	default:
		return "", fmt.Errorf("unknown backend %q (supported: claude, codex, cursor)", name)
	}
}

// selectBackend returns the WorkerBackend for the given canonical name.
// Names must be pre-validated via validateBackend.
func selectBackend(name string) (WorkerBackend, error) {
	switch name {
	case "claude":
		return &ClaudeBackend{}, nil
	case "codex":
		return &CodexBackend{}, nil
	case "cursor":
		return &CursorBackend{}, nil
	default:
		return nil, fmt.Errorf("unknown backend %q (supported: claude, codex, cursor)", name)
	}
}

// resolveYNHBinary returns the path to the ynh binary to use for sensor execution.
func resolveYNHBinary(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolving ynh binary: %w", err)
	}
	return exe, nil
}

// openTrajectory opens a trajectory output destination.
// If path is "-", it returns the provided stdout writer.
// A fresh run truncates the file; a resume run (appendMode) opens with
// O_APPEND so the prior trajectory survives and new events are appended.
// Returns the writer and a cleanup function.
func openTrajectory(path string, stdout io.Writer, appendMode bool) (*TrajectoryWriter, func(), error) {
	if path == "-" {
		return NewTrajectoryWriter(stdout), func() {}, nil
	}
	flags := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if appendMode {
		flags = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	}
	f, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("opening trajectory file %q: %w", path, err)
	}
	return NewTrajectoryWriter(f), func() { _ = f.Close() }, nil
}

// newNullTrajectory returns a TrajectoryWriter that discards all events.
func newNullTrajectory() *TrajectoryWriter {
	return NewTrajectoryWriter(io.Discard)
}

// newSessionID returns a random hex session identifier.
func newSessionID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// dirOf returns the directory component of a path.
func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return "."
}

// baseCommit records the commit the run started from, so a trajectory can be
// replayed against the tree it actually saw. Best effort: a worktree that is
// not a git repository is a legitimate target, and failing the run over
// missing provenance would be worse than recording none.
func baseCommit(dir string) string {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
