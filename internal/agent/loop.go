package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/harness"
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

	// I/O streams. Defaults to os.Stdout / os.Stderr / os.Stdin.
	Stdout io.Writer
	Stderr io.Writer
	Stdin  io.Reader

	// YNHBinary is the path to the ynh executable for sensor invocation.
	// Defaults to os.Executable() if empty.
	YNHBinary string

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
	if opts.Backend == "" {
		opts.Backend = "claude"
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

	// ── Trajectory writer ─────────────────────────────────────────────────────
	traj := newNullTrajectory()
	if opts.EmitJSONL != "" {
		tw, cleanup, err := openTrajectory(opts.EmitJSONL, opts.Stdout)
		if err != nil {
			return err
		}
		defer cleanup()
		traj = tw
	}

	sessionID := newSessionID()

	// ── Load and assemble harness ─────────────────────────────────────────────
	var configPath string
	var harnessObj *harness.Harness

	if opts.HarnessName != "" {
		harnessObj, err = harness.LoadQualified(opts.HarnessName)
		if err != nil {
			return fmt.Errorf("loading harness %q: %w", opts.HarnessName, err)
		}
		configPath, err = assembleHarness(harnessObj, opts.Backend)
		if err != nil {
			return fmt.Errorf("assembling harness: %w", err)
		}
		defer func() { _ = os.RemoveAll(configPath) }()
	}

	// ── Select backend ────────────────────────────────────────────────────────
	wb := opts.backendOverride
	if wb == nil {
		wb, err = selectBackend(opts.Backend)
		if err != nil {
			return err
		}
	}

	// ── Session start ─────────────────────────────────────────────────────────
	ctx := context.Background()

	harnessName := opts.HarnessName
	if harnessName == "" {
		harnessName = "(none)"
	}
	if emitErr := traj.Emit(KindSessionStart, 0, SessionStartData{
		SessionID: sessionID,
		Harness:   harnessName,
		Backend:   wb.Name(),
		Task:      opts.Task,
	}); emitErr != nil {
		return fmt.Errorf("writing trajectory: %w", emitErr)
	}

	sess, err := wb.Start(ctx, StartOptions{
		WorktreeDir: opts.WorktreeDir,
		ConfigPath:  configPath,
		Sandbox:     opts.Sandbox,
		Model:       opts.Model,
		Stderr:      opts.Stderr,
	})
	if err != nil {
		_ = traj.Emit(KindSessionEnd, 0, SessionEndData{ExitCode: ExitWorkerError, Reason: err.Error()})
		return &ExitError{Code: ExitWorkerError, Message: fmt.Sprintf("starting worker: %v", err)}
	}
	defer func() { _ = sess.Close() }()

	// ── Budget + watchdog ─────────────────────────────────────────────────────
	budget := &Budget{
		MaxTurns:  opts.MaxTurns,
		MaxTokens: opts.MaxTokens,
		MaxWall:   opts.MaxWall,
	}
	budget.Start()
	watchdog := NewWatchdog()

	// ── Control channel ───────────────────────────────────────────────────────
	ctrl := NewControlReader(opts.Stdin)

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
		// Sort by tier (build → lint → test → other), then alphabetically within tier.
		sort.Slice(sensorNames, func(i, j int) bool {
			ti := sensorTier(harnessObj.Sensors[sensorNames[i]].Category)
			tj := sensorTier(harnessObj.Sensors[sensorNames[j]].Category)
			if ti != tj {
				return ti < tj
			}
			return sensorNames[i] < sensorNames[j]
		})
	} // end else if harnessObj != nil

	// ── Plan phase ────────────────────────────────────────────────────────────
	if !opts.NoPlan {
		planMsg := fmt.Sprintf(
			"Write a plan for the following task. Document it clearly in plan.md in the current directory.\n\nTask: %s",
			opts.Task,
		)
		if emitErr := traj.Emit(KindPlan, 0, nil); emitErr != nil {
			return fmt.Errorf("writing trajectory: %w", emitErr)
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

		if opts.Interactive {
			feedback := planTurn.Content
			if emitErr := traj.Emit(KindTurnApprovalRequired, 0, TurnApprovalData{SynthesizedFeedback: feedback}); emitErr != nil {
				return fmt.Errorf("writing trajectory: %w", emitErr)
			}
			action, replacement, aborted := waitForApproval(ctrl, ActionApprovePlan, ActionRejectPlan)
			if aborted {
				_ = traj.Emit(KindSessionEnd, 0, SessionEndData{ExitCode: ExitUserAborted, Reason: "user aborted"})
				return &ExitError{Code: ExitUserAborted, Message: "plan rejected by user"}
			}
			if action == ActionRejectPlan {
				_ = traj.Emit(KindSessionEnd, 0, SessionEndData{ExitCode: ExitUserAborted, Reason: "plan rejected"})
				return &ExitError{Code: ExitUserAborted, Message: "plan rejected by user"}
			}
			if replacement != "" {
				feedback = replacement
			}
			_ = feedback // plan approval carries no feedback to the worker
		}
	}

	// ── Act loop ──────────────────────────────────────────────────────────────
	// First message: task (or plan-approved continuation).
	firstMsg := opts.Task
	if !opts.NoPlan {
		firstMsg = "Plan approved. Proceed with implementation: " + opts.Task
	}
	if err := sess.Send(firstMsg); err != nil {
		_ = traj.Emit(KindSessionEnd, 0, SessionEndData{ExitCode: ExitWorkerError, Reason: err.Error()})
		return &ExitError{Code: ExitWorkerError, Message: fmt.Sprintf("sending first message: %v", err)}
	}

	for {
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

		// ── Auto-commit ───────────────────────────────────────────────────────
		if opts.AutoCommit {
			if err := gitAutoCommit(opts.WorktreeDir, turnN); err != nil {
				_, _ = fmt.Fprintf(opts.Stderr, "auto-commit turn %d: %v\n", turnN, err)
			}
		}

		// ── Run sensors ───────────────────────────────────────────────────────
		var sensorResults []*SensorResult
		for _, name := range sensorNames {
			_ = traj.Emit(KindSensorRun, turnN, name)
			result, err := RunSensor(ynh, opts.HarnessName, name, opts.WorktreeDir)
			if err != nil {
				_, _ = fmt.Fprintf(opts.Stderr, "sensor %q error: %v\n", name, err)
				result = &SensorResult{Name: name, ExitCode: -1}
			}
			sensorResults = append(sensorResults, result)
			_ = traj.Emit(KindSensorResult, turnN, SensorResultData{
				Name:       result.Name,
				Kind:       result.Kind,
				Role:       result.Role,
				ExitCode:   result.ExitCode,
				DurationMS: result.DurationMS,
				Passed:     result.Passed(),
				Summary:    result.Summary(),
			})
		}

		// ── Check convergence ─────────────────────────────────────────────────
		if converged, feedback := checkConvergence(sensorResults, convergenceSensor, ynh, opts.HarnessName, opts.WorktreeDir, traj, turnN); converged {
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
			sensorHash := SensorHash(sensorResults)
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
					_ = traj.Emit(KindSessionEnd, turnN, SessionEndData{ExitCode: ExitUserAborted, Reason: "user interrupted"})
					return &ExitError{Code: ExitUserAborted, Message: "session interrupted by user"}
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
		}
	}
}

// checkConvergence returns (converged=true, feedback="") when all sensors
// pass and the convergence sensor (if any) confirms done.
// Returns (converged=false, feedback=<synthesized feedback>) when work remains.
func checkConvergence(
	results []*SensorResult,
	convergenceSensor, ynh, harnessName, cwd string,
	traj *TrajectoryWriter,
	turnN int,
) (bool, string) {
	// Check all regular sensors.
	allPassed := true
	for _, r := range results {
		if !r.Passed() {
			allPassed = false
		}
	}

	if !allPassed {
		return false, synthesizeFeedback(results)
	}

	// All regular sensors green — consult convergence-verifier if declared.
	if convergenceSensor != "" && ynh != "" && harnessName != "" {
		_ = traj.Emit(KindSensorRun, turnN, convergenceSensor)
		cvResult, err := RunSensor(ynh, harnessName, convergenceSensor, cwd)
		if err != nil || !cvResult.Passed() {
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

// synthesizeFeedback produces the user-turn message injected after a
// non-converged turn. Format mirrors the plan §7.3 sensor-results block.
func synthesizeFeedback(results []*SensorResult) string {
	var sb strings.Builder
	sb.WriteString("<sensor-results>\n")
	for _, r := range results {
		status := "passed"
		if !r.Passed() {
			status = "failed"
		}
		durationSec := float64(r.DurationMS) / 1000
		_, _ = fmt.Fprintf(&sb, "  <%s status=%q duration=%.1fs", r.Name, status, durationSec)
		if summary := r.Summary(); !r.Passed() && summary != "" && summary != "passed" && summary != "failed" {
			_, _ = fmt.Fprintf(&sb, ">\n%s\n  </%s>", summary, r.Name)
		} else {
			sb.WriteString("/>")
		}
		sb.WriteString("\n")
	}
	sb.WriteString("</sensor-results>\n\nContinue work. Address the failing sensors first.")
	return sb.String()
}

// waitForApproval blocks until the control channel delivers one of the
// expected approval or abort actions. Returns (action, replacementFeedback, aborted).
// aborted is true if an interrupt was received.
func waitForApproval(ctrl *ControlReader, approveAction, rejectAction ControlAction) (ControlAction, string, bool) {
	for msg := range ctrl.C() {
		switch msg.Action {
		case approveAction:
			return approveAction, "", false
		case ActionReplaceFeedback:
			return approveAction, msg.Feedback, false
		case rejectAction, ActionRejectPlan:
			return rejectAction, "", false
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
	adapter, err := vendor.Get(backendName)
	if err != nil {
		return "", fmt.Errorf("vendor %q: %w", backendName, err)
	}

	dir, err := os.MkdirTemp("", "ynh-agent-")
	if err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}

	content := []resolver.ResolvedContent{{BasePath: h.Dir}}

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
		mcpFiles, err := adapter.GenerateMCPConfig(h.MCPServers)
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

// selectBackend returns the WorkerBackend for the given name.
func selectBackend(name string) (WorkerBackend, error) {
	switch name {
	case "claude", "":
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
// Returns the writer and a cleanup function.
func openTrajectory(path string, stdout io.Writer) (*TrajectoryWriter, func(), error) {
	if path == "-" {
		return NewTrajectoryWriter(stdout), func() {}, nil
	}
	f, err := os.Create(path)
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
