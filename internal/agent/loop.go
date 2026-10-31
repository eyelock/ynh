package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/baseline"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/gate"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/namespace"
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
	// IncludedProfiles maps a namespace to the profile applied to the
	// included harness that answers to it, from `--profile namespace:name`.
	// The root is unaffected. Mutually exclusive with Focus.
	IncludedProfiles map[string]string
	// Focus is an optional focus name. The focus's prompt becomes the task
	// and the focus's bound profile (if any) is applied. Mirrors
	// `ynh run --focus`. "namespace:name" names a focus of an included
	// harness. Mutually exclusive with Task and Profile.
	Focus string
	// Backend selects the worker backend ("claude", "codex", "cursor" or "opencode").
	// Defaults to "claude", or on a resume to the checkpoint's backend, which
	// it may repeat but not change.
	Backend string
	// Sandbox is "srt" or "none". Defaults to "none".
	Sandbox string
	// AutoApprove is "", "edits" or "all". Empty passes no permission flag to
	// the worker. Set per run only: it is not read from the harness manifest
	// and not restored on resume, so the operator re-grants it every time.
	AutoApprove string
	// Model overrides the worker's default model. Empty means backend default.
	Model string
	// Effort is the reasoning effort to ask the worker for: "low", "medium"
	// or "high". Empty falls back to the harness's agent.effort, and then to
	// asking for none. Not restored on resume, like Model.
	Effort string

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
	// WorkspaceGrant decides the harness's requested workspace folders and
	// returns the ones to grant the worker. It is handed the harness as
	// loaded, before any focus or profile, the installed id ("" for a
	// harness given by path), the backend and the worktree. It reports what
	// it granted and refused itself. Nil grants nothing; the decision needs
	// the operator's allow-list and trust record, which this package does not
	// read.
	WorkspaceGrant func(h *harness.Harness, id, backend, worktree string) []string

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

	// Telemetry carries the run's trace context and spool folder to every
	// process the loop starts. Nil, or a Telemetry that returns no
	// environment, starts every process exactly as it would without one.
	Telemetry Telemetry

	// backendOverride is the resolved WorkerBackend; set by tests or left nil to auto-select.
	backendOverride WorkerBackend

	// testSensorNames overrides sensor collection from the harness.
	// Set by tests that need sensor-loop behaviour without a real installed harness.
	testSensorNames []string
	// testPreRun runs once the harness is loaded and assembled, before the
	// worker starts, so a test can interrupt the pre-run phase.
	testPreRun func(ctx context.Context)
}

// RunLoop executes the agent loop. It returns an *ExitError on non-zero
// termination so the CLI handler can map it to os.Exit.
// RunLoop drives one agent session and returns its machine-readable result
// alongside the error.
//
// The result is returned on every path, including failures: a pipeline needs to
// know what a run consumed and what it touched precisely when it did not
// converge. It is populated by a deferred finaliser so the twenty-odd exit
// points do not each have to remember to fill it in — one of them forgetting
// would produce a plausible-looking result that quietly lied.
// sensorMatchers compiles each sensor's output.match once. A pattern that
// does not compile is treated as absent here: validation reports it, and the
// loop should not stop because of it.
func sensorMatchers(h *harness.Harness) map[string]*regexp.Regexp {
	if h == nil {
		return nil
	}
	out := make(map[string]*regexp.Regexp, len(h.Sensors))
	for name, s := range h.Sensors {
		if re, err := s.OutputMatcher(); err == nil && re != nil {
			out[name] = re
		}
	}
	return out
}

func RunLoop(opts RunOptions) (result *RunResult, err error) {
	result = &RunResult{
		Capabilities: config.CapabilitiesVersion,
		YnhVersion:   config.Version,
		ChangedFiles: []string{},
	}
	defer func() { result.finalise(err) }()

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
	// ── Cancellable context + stop signals ────────────────────────────────────
	// SIGINT/SIGTERM cancel the worker context so an in-flight Next() unblocks;
	// the loop then exits with the last completed turn already checkpointed, so
	// a later --resume continues from there. (A structured consumer sends an interrupt control
	// message first, then SIGTERM after a grace period.)
	//
	// Installed before anything slow runs (loading and assembling the harness
	// included), so an interrupt at any point of a run is caught and exits
	// ExitInterrupted rather than killing the process by its default action.
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

	// ── Resume state ──────────────────────────────────────────────────────────
	var resumeCP *Checkpoint
	resuming := opts.Resume != ""
	// A resume always expected verification — the original invocation had a
	// harness or it would not have been checkpointing sensor state — so a
	// resume that cannot restore one must not be able to claim convergence.
	verificationExpected := opts.HarnessName != "" || resuming
	// taskGiven records whether this resume named its own task or focus, which
	// must then be the session's (checked once a focus has resolved).
	taskGiven := opts.Task != "" || opts.Focus != ""
	if resuming {
		var rerr error
		resumeCP, rerr = readCheckpoint(opts.Resume)
		if rerr != nil {
			return result, &ExitError{Code: ExitResumeError, Message: rerr.Error()}
		}
		// The trajectory lives in the session directory; default it so callers
		// can pass just --resume <dir>.
		if opts.EmitJSONL == "" {
			opts.EmitJSONL = filepath.Join(opts.Resume, "trajectory.jsonl")
		}
		// Restore the run's identity. A resume that omits --harness previously
		// continued with no harness, therefore no sensors, and reported
		// converged — the safety verdict was forgeable by leaving out a flag.
		restoreIdentity(&opts, resumeCP)
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

	// On a resume the backend comes from the checkpoint: the resume token is
	// that backend's own and means nothing to another.
	var backend string
	if resuming {
		backend, err = resumeBackend(opts.Backend, resumeCP.Backend)
		if err != nil {
			return result, &ExitError{Code: ExitResumeError, Message: err.Error()}
		}
	} else if backend, err = validateBackend(opts.Backend); err != nil {
		return result, err
	}
	opts.Backend = backend
	if err := validateSandbox(opts.Sandbox, opts.Backend); err != nil {
		return result, err
	}
	if err := validateAutoApprove(opts.AutoApprove, opts.Backend); err != nil {
		return result, err
	}
	if err := validateEffort(opts.Effort, opts.Backend); err != nil {
		return result, err
	}
	if opts.WorktreeDir == "" {
		var err error
		opts.WorktreeDir, err = os.Getwd()
		if err != nil {
			return result, fmt.Errorf("resolving working directory: %w", err)
		}
	}
	// The project's own permission choice wins over a run-time grant.
	if err := checkProjectPermissions(opts.AutoApprove, opts.Backend, opts.WorktreeDir); err != nil {
		return result, err
	}

	result.Worktree = opts.WorktreeDir
	result.BaseCommit = baseCommit(opts.WorktreeDir)

	ynh, err := resolveYNHBinary(opts.YNHBinary)
	if err != nil {
		return result, err
	}

	// ── Trajectory writer (append on resume, truncate on a fresh run) ─────────
	traj := newNullTrajectory()
	if opts.EmitJSONL != "" {
		tw, cleanup, err := openTrajectory(opts.EmitJSONL, opts.Stdout, resuming)
		if err != nil {
			return result, err
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
	// reportedName is the harness as the trajectory and result name it: the
	// id for an installed harness, the manifest name for one given by path,
	// which keeps a filesystem path out of every report and telemetry.
	reportedName := opts.HarnessName
	// gateProfile is the profile the run resolved, from --profile or from the
	// focus's. The sensor gate (`ynh check`) is handed it so it judges with
	// the same severities the run was assembled under; without it a sensor a
	// profile softened would block the loop it was softened for.
	var gateProfile string
	// workspaceDirs are the extra folders granted to the worker.
	var workspaceDirs []string

	if opts.HarnessName != "" {
		// An installed id or a local harness directory, resolved exactly as
		// `ynh run` and `ynh check` resolve theirs (#560).
		harnessObj, err = harness.LoadIDOrPath(opts.HarnessName)
		if err != nil {
			return result, fmt.Errorf("loading harness %q: %w", opts.HarnessName, err)
		}
		// From here the run names a path by its absolute form: the checkpoint
		// stores it so --resume finds the same harness from any directory, and
		// the sensor gate (`ynh check`, run per turn) is handed it too.
		if namespace.Classify(opts.HarnessName) == namespace.RefPath {
			opts.HarnessName = harnessObj.Dir
			reportedName = harnessObj.Name
		}

		if opts.WorkspaceGrant != nil && len(harnessObj.Workspace) > 0 {
			id := ""
			if namespace.Classify(opts.HarnessName) == namespace.RefID {
				id = opts.HarnessName
			}
			workspaceDirs = opts.WorkspaceGrant(harnessObj, id, opts.Backend, opts.WorktreeDir)
		}

		// Resolve focus → prompt + bound profile. Mirrors `ynh run --focus`.
		sel := harness.Selection{Profile: opts.Profile, Included: opts.IncludedProfiles}
		if opts.Focus != "" {
			var selErr error
			if sel.FocusNS, sel.FocusName, selErr = harness.SplitQualified(opts.Focus); selErr != nil {
				return result, selErr
			}
		}
		profileName := opts.Profile
		if opts.Focus != "" && sel.FocusNS == "" {
			focus, ok := harnessObj.Focuses[opts.Focus]
			if !ok {
				return result, fmt.Errorf("focus %q not defined in harness", opts.Focus)
			}
			if focus.Profile != "" {
				profileName = focus.Profile
			}
			opts.Task = focus.Prompt
		}

		gateProfile = profileName

		// Apply profile overlay before assembly so includes/hooks/MCP layered
		// in by the profile are baked into the assembled harness.
		if profileName != "" {
			harnessObj, err = harness.ResolveProfile(harnessObj, profileName)
			if err != nil {
				return result, fmt.Errorf("resolving profile %q: %w", profileName, err)
			}
		}

		// A run is where the network is allowed, and the sensor merge below
		// reads includes from the cache only, so fetch the ones the cache
		// lacks first (ynf #130). Done after the profile, which can add
		// includes of its own. A failure stops the run before any worker.
		if err := fetchIncludes(harnessObj); err != nil {
			return result, err
		}

		// The sensors an include declares count, as they do for `ynh check`.
		if harnessObj, err = resolver.WithIncludedSensors(harnessObj); err != nil {
			return result, fmt.Errorf("resolving included sensors: %w", err)
		}

		// A convergence verifier named by --convergence-sensor has no role in
		// the manifest, so the resolver cannot tell; the same rule applies.
		if _, ok := harnessObj.ProfileSensorOverrides[opts.ConvergenceSensor]; ok && opts.ConvergenceSensor != "" {
			return result, fmt.Errorf("profile %q: sensor_overrides names %q, a convergence verifier: it decides when a run is done, so a profile may not change its tolerance", gateProfile, opts.ConvergenceSensor)
		}

		// Before the worker starts: a verifier that can never pass would
		// spend the whole budget and end at the turn cap (#447).
		if err := refuseConvergenceVerifier(harnessObj.Sensors, opts.ConvergenceSensor); err != nil {
			return result, err
		}

		var nsFocus *plugin.Focus
		configPath, nsFocus, err = assembleHarness(harnessObj, opts.Backend, sel)
		if err != nil {
			return result, fmt.Errorf("assembling harness: %w", err)
		}
		if nsFocus != nil {
			opts.Task = nsFocus.Prompt
		}
		defer func() { _ = os.RemoveAll(configPath) }()
	} else if opts.Focus != "" || opts.Profile != "" || len(opts.IncludedProfiles) > 0 {
		return result, fmt.Errorf("--focus and --profile require --harness")
	}

	if opts.testPreRun != nil {
		opts.testPreRun(ctx)
	}
	// An interrupt during loading and assembly: nothing has run yet, so there
	// is no checkpoint and nothing to resume, but it is still an interrupt.
	if ctx.Err() != nil {
		return result, &ExitError{Code: ExitInterrupted, Message: "interrupted before the run started"}
	}

	if resuming {
		if taskGiven {
			if conflict := resumeTaskConflict(opts.Focus, opts.Task, resumeCP); conflict != "" {
				return result, &ExitError{Code: ExitResumeError, Message: conflict}
			}
		}
		// Only a run that reached the act phase resumes from a pending message;
		// any other starts again from the task.
		if resumeCP.Phase != PhaseAct && opts.Task == "" {
			return result, &ExitError{Code: ExitResumeError, Message: fmt.Sprintf(
				"checkpoint %q records no task, and a run interrupted before acting starts again from it: pass --task",
				checkpointPath(opts.Resume))}
		}
	}

	// The harness's effort applies when the flag gave none. It is checked
	// here, once the harness has loaded, so a level the backend cannot honour
	// still stops the run before a worker starts.
	if opts.Effort == "" && harnessObj != nil && harnessObj.Agent != nil && harnessObj.Agent.Effort != "" {
		if err := validateEffort(harnessObj.Agent.Effort, opts.Backend); err != nil {
			return result, fmt.Errorf("harness agent.effort: %w", err)
		}
		opts.Effort = harnessObj.Agent.Effort
	}

	// ── Select backend ────────────────────────────────────────────────────────
	wb := opts.backendOverride
	if wb == nil {
		wb, err = selectBackend(opts.Backend)
		if err != nil {
			return result, err
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
				return result, fmt.Errorf("harness agent.max_wall %q: %w", harnessObj.Agent.MaxWall, dErr)
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
	result.budget = budget
	result.Budgets = BudgetLimits{
		MaxTurns:  opts.MaxTurns,
		MaxTokens: opts.MaxTokens,
		MaxWallMS: opts.MaxWall.Milliseconds(),
	}
	result.BudgetSources = budgetSource
	if resuming {
		budget.Resume(resumeCP.Budget)
		result.Effort = resumeCP.Effort
		result.Model = resumeCP.Model
	} else {
		budget.Start()
	}

	// Resume past an already-exceeded budget: honor the cap immediately and
	// exit before spawning a worker or starting a new turn.
	if resuming {
		if reason, budgetKind, code := budget.Exceeded(); reason != "" {
			result.BoundBy = string(budgetKind)
			_ = traj.Emit(KindBudgetExceeded, budget.Turns(), BudgetExceededData{Budget: budgetKind, Reason: reason})
			_ = traj.Emit(KindSessionEnd, budget.Turns(), SessionEndData{ExitCode: code, Reason: reason, TotalTurns: budget.Turns(), TotalTokens: budget.Tokens()})
			return result, &ExitError{Code: code, Message: reason}
		}
	}

	// ── Session start / resumed ───────────────────────────────────────────────
	harnessName := reportedName
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
			AutoApprove:     opts.AutoApprove,
			ModelRequested:  opts.Model,
			EffortRequested: opts.Effort,
		}); emitErr != nil {
			return result, fmt.Errorf("writing trajectory: %w", emitErr)
		}
	} else {
		start := SessionStartData{
			SessionID:       sessionID,
			Harness:         harnessName,
			Backend:         wb.Name(),
			Task:            opts.Task,
			ModelRequested:  opts.Model,
			EffortRequested: opts.Effort,
			AutoApprove:     opts.AutoApprove,
			YnhVersion:      config.Version,
			BaseCommit:      baseCommit(opts.WorktreeDir),
			Budgets: &BudgetLimits{
				MaxTurns:  opts.MaxTurns,
				MaxTokens: opts.MaxTokens,
				MaxWallMS: opts.MaxWall.Milliseconds(),
			},
			BudgetSources: &budgetSource,
		}
		if harnessObj != nil {
			start.HarnessVersion = harnessObj.Version
			if harnessObj.InstalledFrom != nil {
				start.HarnessSHA = harnessObj.InstalledFrom.SHA
			}
		}
		start.ImageDigest = imageDigest()
		if emitErr := traj.Emit(KindSessionStart, 0, start); emitErr != nil {
			return result, fmt.Errorf("writing trajectory: %w", emitErr)
		}
	}

	result.SessionID = sessionID
	result.SessionDir = sessionDir
	result.Backend = wb.Name()
	result.ModelRequested = opts.Model
	result.EffortRequested = opts.Effort
	result.AutoApprove = opts.AutoApprove
	// reportedName, not harnessName: the latter is "(none)" for display in
	// the trajectory when no harness was given, and a structured consumer
	// reading a harness literally named "(none)" would be worse served than by
	// the field being absent, which is what "this run verified nothing" means.
	result.Harness = harnessProvenance(reportedName, harnessObj)
	result.ImageDigest = imageDigest()

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
	// After the passthrough, so the run's own span is the vendor's parent
	// even when a harness passes the caller's TRACEPARENT through.
	var relayEndpoint string
	if opts.Telemetry != nil {
		workerEnv = append(workerEnv, opts.Telemetry.WorkerEnv()...)
		// The vendor's own telemetry, pointed at the run's relay when there
		// is one. Last, so its settings win over anything passed through.
		relayEndpoint = opts.Telemetry.RelayEndpoint(wb.Name())
		if relayEndpoint != "" && SupportsTelemetryRelay(wb.Name()) {
			workerEnv = withBackendRelayEnv(wb.Name(), workerEnv, relayEndpoint)
		} else {
			relayEndpoint = ""
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

	// What the resumed conversation already consumed, for a backend that
	// reports running totals. Only a complete record serves: codex's totals
	// carry cache reads, so a checkpoint without them cannot be its base.
	var usageBase *Usage
	if resumeToken != "" && resumeCP.Budget.UsageReported && resumeCP.Budget.CacheReported {
		usageBase = &Usage{
			InputTokens:  resumeCP.Budget.InputTokens,
			OutputTokens: resumeCP.Budget.OutputTokens,
			CacheTokens:  resumeCP.Budget.CacheReadTokens,
			// Zero when the backend never reported cache writes, which is
			// what codex's totals then hold too.
			CacheCreationTokens: resumeCP.Budget.CacheCreationTokens,
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

	isolatedMCP := harnessObj != nil && harnessObj.MCPIsolation
	if isolatedMCP && wb.Name() != "claude" {
		_, _ = fmt.Fprintf(opts.Stderr, "warning: MCP isolation is only applied to the claude backend: %s will load your own MCP servers\n", wb.Name())
	}
	sess, err := wb.Start(ctx, StartOptions{
		WorktreeDir: opts.WorktreeDir,
		Workspace:   workspaceDirs,
		ConfigPath:  configPath,
		Sandbox:     opts.Sandbox,
		SessionDir:  sessionDir,
		AutoApprove: opts.AutoApprove,
		Model:       opts.Model,
		Effort:      opts.Effort,
		IsolatedMCP: isolatedMCP,
		ResumeToken: resumeToken,
		UsageBase:   usageBase,
		Env:         workerEnv,
		Stderr:      opts.Stderr,

		TelemetryEndpoint: relayEndpoint,
	})
	if err != nil {
		if ctx.Err() != nil {
			return result, interruptExit(0)
		}
		_ = traj.Emit(KindSessionEnd, 0, SessionEndData{ExitCode: ExitWorkerError, Reason: err.Error()})
		return result, &ExitError{Code: ExitWorkerError, Message: fmt.Sprintf("starting worker: %v", err)}
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
		Backend:           opts.Backend,
		Task:              opts.Task,
		Focus:             opts.Focus,
		HarnessName:       opts.HarnessName,
		Profile:           opts.Profile,
		IncludedProfiles:  opts.IncludedProfiles,
		ConvergenceSensor: opts.ConvergenceSensor,
		MaxTurns:          opts.MaxTurns,
		MaxTokens:         opts.MaxTokens,
	}
	planIterations := 0
	result.planIterations = &planIterations
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
	}
	saveCheckpoint := func() {
		if sessionDir == "" {
			return
		}
		cp.ResumeToken = sess.ResumeToken()
		cp.Budget = budget.checkpoint(planIterations)
		cp.Effort = result.Effort
		cp.Model = result.Model
		if err := writeCheckpoint(sessionDir, cp); err != nil {
			_, _ = fmt.Fprintf(opts.Stderr, "checkpoint write failed: %v\n", err)
		}
	}
	// noteModel takes the model a turn reports into the result, and records
	// it in the trajectory the first time this process's worker reports it
	// and whenever it reports another. A turn that reports none leaves the
	// last one seen in place.
	var workerModel string
	noteModel := func(atTurn int, t Turn) {
		if t.Model == "" {
			return
		}
		result.Model = t.Model
		if t.Model != workerModel {
			workerModel = t.Model
			_ = traj.Emit(KindWorkerModel, atTurn, WorkerModelData{Model: t.Model})
		}
	}
	// ── Collect sensors ───────────────────────────────────────────────────────
	var sensorNames []string
	var convergenceVerifiers []string

	if opts.testSensorNames != nil {
		// Test injection: use the provided names, skip harness-based collection.
		sensorNames = opts.testSensorNames
	} else if harnessObj != nil {
		for name, s := range harnessObj.Sensors {
			if s.Role == "convergence-verifier" || name == opts.ConvergenceSensor {
				convergenceVerifiers = append(convergenceVerifiers, name)
				continue
			}
			if s.Role != "stuck-recovery" {
				sensorNames = append(sensorNames, name)
			}
		}
		// Sorted only so --only and the trajectory are deterministic. Execution
		// order is `ynh check`'s to decide now that it runs them.
		sort.Strings(sensorNames)
		sort.Strings(convergenceVerifiers)
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
					return result, fmt.Errorf("writing trajectory: %w", emitErr)
				}
			}
			if err := sess.Send(planMsg); err != nil {
				// An interrupt closed the worker under this send: that is the
				// interrupt, not a worker fault.
				if ctx.Err() != nil {
					return result, interruptExit(0)
				}
				_ = traj.Emit(KindSessionEnd, 0, SessionEndData{ExitCode: ExitWorkerError, Reason: err.Error()})
				return result, workerTurnExit(err, fmt.Sprintf("sending plan request: %v", err))
			}
			planTurn, err := sess.Next()
			if ctx.Err() != nil {
				return result, interruptExit(0)
			}
			if err == nil {
				err = unmeteredTurn(wb.Name(), planTurn)
			}
			if err == io.EOF {
				_ = traj.Emit(KindSessionEnd, 0, SessionEndData{ExitCode: ExitWorkerError, Reason: "worker exited during plan phase"})
				return result, &ExitError{Code: ExitWorkerError, Message: "worker exited during plan phase"}
			}
			if err != nil {
				_ = traj.Emit(KindSessionEnd, 0, SessionEndData{ExitCode: ExitWorkerError, Reason: err.Error()})
				return result, workerTurnExit(err, fmt.Sprintf("plan turn: %v", err))
			}
			_ = traj.Emit(KindAssistantMessage, 0, planTurn.Content)
			budget.RecordUsage(planTurn)
			result.noteEffort(planTurn)
			noteModel(0, planTurn)
			_ = traj.Emit(KindBudgetSnapshot, 0, BudgetSnapshotData{
				Turns:  budget.Turns(),
				Tokens: budget.Tokens(),
			})

			// Plan iterations consume tokens and wall-clock but not the act-phase
			// turn cap. budget.Exceeded checks turns first; turns is still 0 here
			// (RecordTurn is act-phase only) so the turns branch is dormant.
			if reason, budgetKind, code := budget.Exceeded(); reason != "" {
				result.BoundBy = string(budgetKind)
				_ = traj.Emit(KindBudgetExceeded, 0, BudgetExceededData{Budget: budgetKind, Reason: reason})
				_ = traj.Emit(KindSessionEnd, 0, SessionEndData{ExitCode: code, Reason: reason, TotalTokens: budget.Tokens()})
				return result, &ExitError{Code: code, Message: reason}
			}

			if !opts.Interactive {
				approvedPlan = planTurn.Content
				break planLoop
			}

			if emitErr := traj.Emit(KindPlanApprovalRequired, 0, PlanApprovalData{
				Plan:      planTurn.Content,
				Iteration: planIter,
			}); emitErr != nil {
				return result, fmt.Errorf("writing trajectory: %w", emitErr)
			}
			action, replyFeedback, aborted := waitForApproval(ctrl, ActionApprovePlan, ActionRejectPlan)
			if aborted {
				// Interrupt/SIGTERM during plan approval: the plan-phase
				// checkpoint already exists, so --resume re-runs the plan.
				return result, interruptExit(0)
			}
			if action == ActionRejectPlan {
				reason := "plan rejected by user"
				if replyFeedback != "" {
					reason = "plan rejected by user: " + replyFeedback
				}
				_ = traj.Emit(KindSessionEnd, 0, SessionEndData{ExitCode: ExitUserAborted, Reason: reason})
				return result, &ExitError{Code: ExitUserAborted, Message: reason}
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
				return result, &ExitError{Code: ExitPlanIterationCap, Message: reason}
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
		return result, &ExitError{Code: ExitGateError, Message: fmt.Sprintf("reading baseline: %v", fpErr)}
	}

	if err := sess.Send(firstMsg); err != nil {
		// An interrupt closed the worker under this send: that is the
		// interrupt, not a worker fault.
		if ctx.Err() != nil {
			return result, interruptExit(0)
		}
		_ = traj.Emit(KindSessionEnd, 0, SessionEndData{ExitCode: ExitWorkerError, Reason: err.Error()})
		return result, workerTurnExit(err, fmt.Sprintf("sending first message: %v", err))
	}

	// stopRun reports why the run must end before another worker turn, or nil
	// when one is allowed: an interrupt that arrived between turns (the last
	// completed turn is already checkpointed, so --resume continues from it),
	// or a spent turn, token or wall-clock budget. It is the one place those
	// are decided, asked at the top of an iteration and again before any
	// message is sent, because a message sent is a turn taken: the worker acts
	// on it as soon as it reads it, whatever the loop does next.
	stopRun := func() error {
		if ctx.Err() != nil {
			return interruptExit(budget.Turns())
		}
		if reason, budgetKind, code := budget.Exceeded(); reason != "" {
			result.BoundBy = string(budgetKind)
			_ = traj.Emit(KindBudgetExceeded, budget.Turns(), BudgetExceededData{Budget: budgetKind, Reason: reason})
			_ = traj.Emit(KindSessionEnd, budget.Turns(), SessionEndData{ExitCode: code, Reason: reason, TotalTurns: budget.Turns(), TotalTokens: budget.Tokens()})
			return &ExitError{Code: code, Message: reason}
		}
		return nil
	}

	// stopForWall is stopRun narrowed to the wall clock, asked before the
	// convergence verifiers run. They are the one slow call a converging turn
	// still has to make, so a spent wall clock ends the run before them. A
	// spent turn or token cap does not: the final turn the caps allow is
	// still entitled to converge.
	stopForWall := func() error {
		if _, kind, _ := budget.Exceeded(); kind == BudgetWallClock {
			return stopRun()
		}
		return nil
	}

	// stopBeforeSend is stopRun for a turn's feedback that has not been sent.
	// When the run ends there the feedback is kept in the checkpoint as the
	// pending message, so a --resume with room left sends it, and nothing is
	// written to the worker.
	stopBeforeSend := func(turnN int, feedback string) error {
		err := stopRun()
		if err != nil {
			cp.Phase = PhaseAct
			cp.LastCompletedTurn = turnN
			cp.PendingMessage = feedback
			saveCheckpoint()
		}
		return err
	}

	for {
		if err := stopRun(); err != nil {
			return result, err
		}

		turnN := budget.Turns() + 1
		_ = traj.Emit(KindTurnStart, turnN, nil)

		// ── Wait for assistant turn ───────────────────────────────────────────
		turn, err := sess.Next()
		// A cancelled context means an interrupt/SIGTERM killed the worker
		// mid-turn; the partial turn is discarded and resume redoes it.
		if ctx.Err() != nil {
			return result, interruptExit(turnN)
		}
		// A response the model did not write is not agent output. Fed back as
		// if it were, it repeats every turn until the watchdog calls it stuck.
		if err == nil {
			err = unmeteredTurn(wb.Name(), turn)
		}
		if err == io.EOF {
			_ = traj.Emit(KindSessionEnd, turnN, SessionEndData{ExitCode: ExitWorkerError, Reason: "worker exited unexpectedly"})
			return result, &ExitError{Code: ExitWorkerError, Message: "worker exited before convergence"}
		}
		if err != nil {
			_ = traj.Emit(KindSessionEnd, turnN, SessionEndData{ExitCode: ExitWorkerError, Reason: err.Error()})
			return result, workerTurnExit(err, fmt.Sprintf("worker turn %d: %v", turnN, err))
		}
		_ = traj.Emit(KindAssistantMessage, turnN, turn.Content)

		budget.RecordTurn()
		budget.RecordUsage(turn)
		result.noteEffort(turn)
		noteModel(turnN, turn)
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
			return result, &ExitError{Code: ExitTamper, Message: reason +
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
			callEnv, endCall := startCall(opts.Telemetry, CallCheck, turnN)
			env, checkErr := RunCheck(ynh, opts.HarnessName, opts.WorktreeDir, gateProfile, sensorNames, opts.SensorOverlay, callEnv)
			if checkErr != nil {
				endCall("error", false)
			} else {
				endCall(env.Verdict, true)
			}
			if checkErr != nil {
				// A gate that cannot run is an operator fault, not agent work.
				// Degrading to "no sensor results" would keep sending the worker
				// turns nothing could verify until the budget ran out, and then
				// report that exhaustion as the agent's failure.
				_ = traj.Emit(KindSessionEnd, turnN, SessionEndData{ExitCode: ExitGateError, Reason: checkErr.Error()})
				return result, &ExitError{Code: ExitGateError, Message: fmt.Sprintf("gate turn %d: %v", turnN, checkErr)}
			}
			checkEnv = env
			result.Sensors = env.Sensors
			for _, r := range env.Sensors {
				if !r.Ran() {
					continue
				}
				_ = traj.Emit(KindSensorResult, turnN, SensorResultData{
					Name:        r.Name,
					Kind:        r.Kind,
					Status:      r.Status,
					ExitCode:    r.ExitCode,
					DurationMS:  r.DurationMS,
					Tolerance:   r.Tolerance,
					ToolVersion: r.ToolVersion,
					KnownCount:  r.KnownCount,
					NewCount:    r.NewCount,
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
		converged, feedback, convergence, stopErr := checkConvergence(checkEnv, verifierRun{
			names:   convergenceVerifiers,
			ynh:     ynh,
			harness: opts.HarnessName,
			cwd:     opts.WorktreeDir,
			profile: gateProfile,
			overlay: opts.SensorOverlay,
			tel:     opts.Telemetry,
			stop:    stopForWall,
		}, traj, turnN, verificationExpected)
		if convergence != nil {
			result.Convergence = convergence
		}
		if stopErr != nil {
			return result, stopErr
		}
		if converged {
			_ = traj.Emit(KindConverged, turnN, nil)
			_ = traj.Emit(KindSessionEnd, turnN, SessionEndData{ExitCode: ExitConverged, TotalTurns: budget.Turns(), TotalTokens: budget.Tokens()})
			return result, nil
		} else if feedback == "" {
			// All sensors passed but no convergence sensor; treat as converged.
			_ = traj.Emit(KindConverged, turnN, nil)
			_ = traj.Emit(KindSessionEnd, turnN, SessionEndData{ExitCode: ExitConverged, TotalTurns: budget.Turns(), TotalTokens: budget.Tokens()})
			return result, nil
		} else {
			// ── Stuckness watchdog ─────────────────────────────────────────────
			sensorHash := SensorHash(checkEnv, sensorMatchers(harnessObj))
			if reason := watchdog.RecordTurn(turn.Content, sensorHash); reason != "" {
				_ = traj.Emit(KindStuckDetected, turnN, StuckDetectedData{Reason: reason, TurnCount: budget.Turns()})
				_ = traj.Emit(KindSessionEnd, turnN, SessionEndData{ExitCode: ExitStuck, Reason: reason})
				return result, &ExitError{Code: ExitStuck, Message: "stuck: " + reason}
			}

			// ── Is another turn allowed? ───────────────────────────────────────
			// Before the operator is asked to approve a turn, and before the
			// worker is sent one: a run that ends here must not have asked for
			// work it will not take.
			if err := stopBeforeSend(turnN, feedback); err != nil {
				return result, err
			}

			// ── Interactive approval ───────────────────────────────────────────
			if opts.Interactive {
				if emitErr := traj.Emit(KindTurnApprovalRequired, turnN, TurnApprovalData{SynthesizedFeedback: feedback}); emitErr != nil {
					return result, fmt.Errorf("writing trajectory: %w", emitErr)
				}
				_, replacement, aborted := waitForApproval(ctrl, ActionApproveTurn, ActionRejectPlan)
				if aborted {
					// Interrupt at the turn gate: the checkpoint reflects the
					// last completed turn, so --resume redoes this turn.
					return result, interruptExit(turnN)
				}
				if replacement != "" {
					feedback = replacement
				}
			}

			// The approval wait counts against the wall clock, so ask again.
			if opts.Interactive {
				if err := stopBeforeSend(turnN, feedback); err != nil {
					return result, err
				}
			}

			_ = traj.Emit(KindFeedbackSent, turnN, feedback)
			if err := sess.Send(feedback); err != nil {
				// An interrupt closed the worker under this send: that is the
				// interrupt, not a worker fault.
				if ctx.Err() != nil {
					return result, interruptExit(turnN)
				}
				_ = traj.Emit(KindSessionEnd, turnN, SessionEndData{ExitCode: ExitWorkerError, Reason: err.Error()})
				return result, workerTurnExit(err, fmt.Sprintf("sending feedback turn %d: %v", turnN, err))
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

// refuseConvergenceVerifier rejects a run whose convergence verifier could
// never return pass. Every sensor the collection treats as a verifier is
// checked, in name order.
func refuseConvergenceVerifier(sensors map[string]plugin.Sensor, flagName string) error {
	names := make([]string, 0, len(sensors))
	for name := range sensors {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		s := sensors[name]
		if s.Role != "convergence-verifier" && name != flagName {
			continue
		}
		if why := plugin.ConvergenceVerifierRefusal(s.Source.Kind()); why != "" {
			return fmt.Errorf("sensor %q cannot be the convergence verifier: %s", name, why)
		}
	}
	return nil
}

// verifierRun is what checkConvergence needs to run the convergence verifiers
// once the regular gate is green: which sensors, and how to call `ynh check`
// for them.
type verifierRun struct {
	names   []string // sorted; empty when the harness declares none
	ynh     string
	harness string
	cwd     string
	profile string // passed to `ynh check --profile`, so verifiers see the run's severities
	overlay map[string]json.RawMessage
	tel     Telemetry
	// stop is asked before the verifiers run and ends the run when it returns
	// an error. Nil asks nothing.
	stop func() error
}

// checkConvergence decides whether the turn's gate result means done.
//
// The verdict is `ynh check`'s, not the loop's. What blocks: tolerance,
// baseline forgiveness, the pass/fail rule for each sensor kind, is settled
// there, so a run and a human at a terminal cannot reach opposite conclusions
// about the same manifest. That holds for the convergence verifiers too: they
// run through `ynh check`, so a verifier with pre-existing failures in the
// baseline can still pass.
//
// Returns (converged=false, feedback=<synthesized feedback>) when work remains.
// verificationExpected is true when the run was configured to verify itself:
// a harness was named, or this is a resume that should have restored one.
// The error is non-nil only when the run must end here: v.stop ended it before
// the verifiers, or their check could not run (ExitGateError).
func checkConvergence(
	env *gate.Envelope,
	v verifierRun,
	traj *TrajectoryWriter,
	turnN int,
	verificationExpected bool,
) (bool, string, *RunConvergence, error) {
	// Convergence needs evidence when verification was asked for. An empty
	// result set made allPassed vacuously true, so a run whose harness went
	// missing, which is what --resume without --harness produced, reported
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
		return false, "verification was expected but no sensors ran, so convergence cannot be confirmed", nil, nil
	}
	if verificationExpected && ran > 0 && canGate == 0 {
		return false, "no blocking command sensor ran, so nothing gates convergence", nil, nil
	}

	if env != nil && env.Verdict == gate.VerdictBlocked {
		return false, synthesizeFeedback(env), nil, nil
	}

	if len(v.names) == 0 || v.ynh == "" || v.harness == "" {
		return true, "", nil, nil
	}
	return runVerifiers(v, traj, turnN)
}

// runVerifiers runs every convergence verifier in one `ynh check` call, after
// the regular gate went green, and judges them by that call's envelope.
//
// They are not folded into the per-turn gate call because they are the slow,
// expensive final say, run only for a turn that would otherwise converge.
//
// Converged means the check ran, its verdict is not blocked, and every
// verifier reached a decision. A verifier that was skipped, or that only
// reported or deferred (a files or focus sensor, which `ynh check` cannot
// judge), is not a pass, so the run does not converge on its say-so.
func runVerifiers(v verifierRun, traj *TrajectoryWriter, turnN int) (bool, string, *RunConvergence, error) {
	if v.stop != nil {
		if err := v.stop(); err != nil {
			return false, "", nil, err
		}
	}
	for _, name := range v.names {
		_ = traj.Emit(KindSensorRun, turnN, name)
	}
	callEnv, endCall := startCall(v.tel, CallCheck, turnN)
	venv, err := RunCheck(v.ynh, v.harness, v.cwd, v.profile, v.names, v.overlay, callEnv)
	if err != nil {
		// As for the per-turn gate, a check that cannot run is an operator
		// fault, not agent work: a broken verifier command would otherwise
		// spend turns until a budget cap and be reported as the agent's
		// failure. The result still shows what failed.
		endCall("error", false)
		summary := err.Error()
		msg := fmt.Sprintf("convergence verifiers (%s) on turn %d: %v", strings.Join(v.names, ", "), turnN, err)
		_ = traj.Emit(KindSessionEnd, turnN, SessionEndData{ExitCode: ExitGateError, Reason: msg})
		return false, "", &RunConvergence{
			Sensor: v.names[0], Passed: false, Summary: summary,
			Verifiers: failedVerifiers(v.names, summary),
		}, &ExitError{Code: ExitGateError, Message: msg}
	}
	endCall(venv.Verdict, true)

	byName := make(map[string]gate.Result, len(venv.Sensors))
	for _, r := range venv.Sensors {
		byName[r.Name] = r
	}

	verifiers := make([]VerifierResult, 0, len(v.names))
	allOK := true
	first := -1 // index of the first verifier that did not pass
	for i, name := range v.names {
		r, found := byName[name]
		vr := VerifierResult{Sensor: name, Status: gate.StatusSkipped, Summary: "did not run"}
		if found && r.Ran() {
			vr.Kind = r.Kind
			vr.Status = r.Status
			vr.Summary = resultSummary(r)
			_ = traj.Emit(KindSensorResult, turnN, SensorResultData{
				Name:        r.Name,
				Kind:        r.Kind,
				Status:      r.Status,
				ExitCode:    r.ExitCode,
				DurationMS:  r.DurationMS,
				Tolerance:   r.Tolerance,
				ToolVersion: r.ToolVersion,
				KnownCount:  r.KnownCount,
				NewCount:    r.NewCount,
				Role:        "convergence-verifier",
				Passed:      r.Status != gate.StatusFail,
				Summary:     vr.Summary,
			})
		}
		vr.Passed = found && verifierDecided(r) && !r.Gating()
		if !vr.Passed {
			allOK = false
			if first < 0 {
				first = i
			}
		}
		verifiers = append(verifiers, vr)
	}

	if venv.Verdict != gate.VerdictBlocked && allOK {
		return true, "", &RunConvergence{
			Sensor: v.names[0], Passed: true, Verifiers: verifiers,
		}, nil
	}

	failing := verifiers[0]
	if first >= 0 {
		failing = verifiers[first]
	}
	feedback := "The regular sensors passed, but the convergence verifiers did not.\n\n" + synthesizeFeedback(venv)
	return false, feedback, &RunConvergence{
		Sensor: failing.Sensor, Passed: false, Summary: failing.Summary, Verifiers: verifiers,
	}, nil
}

// verifierDecided reports whether a verifier's result is a verdict at all. A
// files sensor reports and a focus sensor defers: neither says pass, so
// neither may converge the run.
func verifierDecided(r gate.Result) bool {
	switch r.Status {
	case gate.StatusSkipped, gate.StatusReported, gate.StatusDeferred:
		return false
	}
	return true
}

// failedVerifiers is the verifier list for a check that could not run at all.
func failedVerifiers(names []string, summary string) []VerifierResult {
	out := make([]VerifierResult, 0, len(names))
	for _, name := range names {
		out = append(out, VerifierResult{Sensor: name, Status: gate.StatusSkipped, Summary: summary})
	}
	return out
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

// fetchIncludes makes sure every include of h is in the include cache,
// fetching the ones that are not, honouring allowed_remote_sources and the
// refs the includes pin. It reaches only the hosts the includes name.
//
// It exists because the sensor merge that follows is cache-only by design
// (`ynh check` must not reach the network), so a run on a cold cache, a path
// harness never installed or an installed one whose cache was cleared, would
// otherwise fail on its first include. Like every pre-run refusal, a failure
// exits with ExitRefused before any worker starts.
func fetchIncludes(h *harness.Harness) error {
	if len(h.Includes) == 0 {
		return nil
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if _, err := resolver.ResolveFromCache(h, cfg); err != nil {
		return fmt.Errorf("fetching includes: %w", err)
	}
	return nil
}

// assembleHarness assembles the harness for the named vendor backend into
// a temporary directory. The caller is responsible for os.RemoveAll on the
// returned path.
//
// sel carries the profiles and focus chosen for included harnesses. The focus
// of an included harness comes back when sel names one, since only resolving
// the includes finds it.
func assembleHarness(h *harness.Harness, backendName string, sel harness.Selection) (string, *plugin.Focus, error) {
	cfg, cfgErr := config.Load()
	if cfgErr != nil {
		return "", nil, fmt.Errorf("loading config: %w", cfgErr)
	}
	adapter, err := vendor.Get(backendName)
	if err != nil {
		return "", nil, fmt.Errorf("vendor %q: %w", backendName, err)
	}

	dir, err := os.MkdirTemp("", "ynh-agent-")
	if err != nil {
		return "", nil, fmt.Errorf("creating temp dir: %w", err)
	}

	// Resolve includes exactly as `ynh run` does. Assembling from h.Dir alone
	// silently dropped every base and profile include, so a harness composed
	// from other repositories ran the loop with none of that content — and
	// profile-level artifact swapping was a no-op.
	//
	// From the cache, as `ynh run` does: fetchIncludes has just filled it, and
	// a baked image carries its includes there with an origin that may be
	// unreachable (private, or a relative path), where a refresh would fail
	// and discard the entry.
	resolved, nsFocus, resErr := resolver.ResolveSelectedFromCache(h, cfg, sel)
	if resErr != nil {
		_ = os.RemoveAll(dir)
		return "", nil, fmt.Errorf("resolving includes: %w", resErr)
	}
	var content []resolver.ResolvedContent
	for _, r := range resolved {
		content = append(content, r.Content)
	}
	content = append(content, resolver.ResolvedContent{BasePath: h.Dir})

	if err := assembler.AssembleTo(dir, adapter, content); err != nil {
		_ = os.RemoveAll(dir)
		return "", nil, fmt.Errorf("assembling harness: %w", err)
	}

	// Assemble the delegates as agent files, as `ynh run` does, each with
	// its own includes' skills and MCP servers.
	for _, del := range h.DelegatesTo {
		if chkErr := cfg.CheckSource(del.Git, h.Dir); chkErr != nil {
			_ = os.RemoveAll(dir)
			return "", nil, fmt.Errorf("delegate %q: %w", del.Git, chkErr)
		}
	}
	if _, delErr := assembler.AssembleDelegates(dir, adapter, h.DelegatesTo, h.Dir, assembler.DelegateOptions{Config: cfg, Launch: true}); delErr != nil {
		_ = os.RemoveAll(dir)
		return "", nil, fmt.Errorf("assembling delegates: %w", delErr)
	}

	// Generate vendor-native hook config, and copy in the scripts those hooks
	// run from the harness, and from the included harnesses that consented to
	// their hooks.
	hookWarnings, hookErr := assembler.WriteComposedSessionHooks(dir, adapter, h, resolved)
	if hookErr != nil {
		_ = os.RemoveAll(dir)
		return "", nil, hookErr
	}
	for _, w := range hookWarnings {
		fmt.Fprintf(os.Stderr, "  warning: %s\n", w)
	}

	// Generate vendor-native MCP config: the harness's own servers and those
	// of its included harnesses, composed.
	dataDir := harness.PluginDataDir(h)
	servers, _, expErr := harness.ComposeMCPServers(h, resolver.IncludedHarnesses(resolved), dataDir, os.LookupEnv)
	if expErr != nil {
		_ = os.RemoveAll(dir)
		return "", nil, expErr
	}
	if len(servers) > 0 {
		if mkdirErr := os.MkdirAll(dataDir, 0o755); mkdirErr != nil {
			_ = os.RemoveAll(dir)
			return "", nil, mkdirErr
		}
		mcpFiles, err := adapter.GenerateMCPConfig(servers)
		if err != nil {
			_ = os.RemoveAll(dir)
			return "", nil, fmt.Errorf("generating MCP config: %w", err)
		}
		for relPath, data := range mcpFiles {
			absPath := fmt.Sprintf("%s/%s", dir, relPath)
			if mkdirErr := os.MkdirAll(dirOf(absPath), 0o755); mkdirErr != nil {
				_ = os.RemoveAll(dir)
				return "", nil, mkdirErr
			}
			if writeErr := os.WriteFile(absPath, data, 0o644); writeErr != nil {
				_ = os.RemoveAll(dir)
				return "", nil, writeErr
			}
		}
	}

	// After hooks and MCP, so the manifest's pointers to them are accurate.
	if err := writeSessionPluginManifest(dir, adapter, h); err != nil {
		_ = os.RemoveAll(dir)
		return "", nil, err
	}

	return dir, nsFocus, nil
}

// writeSessionPluginManifest writes the manifest of the plugin the worker
// loads, as `ynh run` does. Claude Code names a plugin without a manifest
// after its directory, so without it a Claude worker's MCP tools come out as
// plugin:.claude:<server> instead of plugin:<harness>:<server>. Vendors that
// do not load the run as a named plugin write nothing.
func writeSessionPluginManifest(dir string, adapter vendor.Adapter, h *harness.Harness) error {
	namer, ok := adapter.(vendor.SessionPluginNamer)
	if !ok {
		return nil
	}
	files, err := namer.GenerateSessionPluginManifest(&plugin.HarnessJSON{
		Name:        h.Name,
		Version:     h.Version,
		Description: h.Description,
		Author:      h.Author,
		Keywords:    h.Keywords,
	}, dir)
	if err != nil {
		return fmt.Errorf("writing plugin manifest: %w", err)
	}
	for relPath, data := range files {
		absPath := filepath.Join(dir, relPath)
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			return fmt.Errorf("creating manifest dir: %w", err)
		}
		if err := os.WriteFile(absPath, data, 0o644); err != nil {
			return fmt.Errorf("writing manifest %s: %w", relPath, err)
		}
	}
	return nil
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

// resumeBackend picks the backend a resume drives: the checkpoint's, which a
// --backend flag may repeat but not change. The resume token belongs to that
// backend (a codex thread id, a claude, cursor or opencode session id) and is meaningless
// to any other. A checkpoint without a backend predates recording it and was
// claude's, the only backend then.
func resumeBackend(flag, recorded string) (string, error) {
	want, err := validateBackend(recorded)
	if err != nil {
		return "", fmt.Errorf("checkpoint records unknown backend %q", recorded)
	}
	if flag == "" {
		return want, nil
	}
	got, err := validateBackend(flag)
	if err != nil {
		return "", err
	}
	if got != want {
		return "", fmt.Errorf(
			"this session was started on the %q backend, so it cannot resume on %q: its resume token is %s's; "+
				"omit --backend or pass --backend %s", want, got, want, want)
	}
	return got, nil
}

// resumeTaskConflict reports why the focus and task a resume was given cannot
// continue the checkpoint's session, or "" when they can. task is the resolved
// task, a focus's prompt once the focus has loaded. A resumed conversation
// carrying on with a different task is not a resume, so this refuses rather
// than override.
func resumeTaskConflict(focus, task string, cp *Checkpoint) string {
	if cp.Focus != "" && focus != cp.Focus {
		return fmt.Sprintf(
			"this session ran focus %q; resume it with --focus %s or with neither --task nor --focus", cp.Focus, cp.Focus)
	}
	if cp.Task != "" && task != cp.Task {
		return "the task given on this resume differs from the task this session was started with; " +
			"resume without --task or --focus to continue it, or start a new run for the new task"
	}
	return ""
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
	case "claude", "codex", "cursor", "opencode":
		return name, nil
	case "claude-code":
		return "", fmt.Errorf("unknown backend %q (did you mean \"claude\"?)", name)
	default:
		return "", fmt.Errorf("unknown backend %q (supported: claude, codex, cursor, opencode)", name)
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
	case "opencode":
		return &OpenCodeBackend{}, nil
	default:
		return nil, fmt.Errorf("unknown backend %q (supported: claude, codex, cursor, opencode)", name)
	}
}

// workerTurnExit is the exit for a turn the worker could not complete. A
// *WorkerError is the vendor reporting its own failure, so its words are the
// reason; any other error keeps the caller's description of where it happened.
func workerTurnExit(err error, fallback string) *ExitError {
	var we *WorkerError
	if errors.As(err, &we) {
		return &ExitError{Code: ExitWorkerError, Message: "worker error: " + we.Error()}
	}
	return &ExitError{Code: ExitWorkerError, Message: fallback}
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
