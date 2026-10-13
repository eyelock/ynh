package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/vendor"
)

// OpenCodeBackend implements WorkerBackend for OpenCode 2.x's `opencode run`.
//
// OpenCode spawns a new process per turn and carries the conversation across
// them by session id, with `-s <id>` from the second turn on, like Codex's
// thread id. The id is learned from the first turn's events (every event
// carries "sessionID"), so ResumeToken is empty until then, unless a resume
// seeded it. OpenCode keeps its sessions in a SQLite store of its own, so
// nothing but the id needs to cross a relaunch.
//
// Every fact here was hand-tested against v2.0.27 with the free
// opencode/nemotron-3-ultra-free model (no login), and is marked CONFIRMED
// where it drove a decision. See .claude/skills/vendor-adapters/references/opencode.md.
type OpenCodeBackend struct{}

func (b *OpenCodeBackend) Name() string { return "opencode" }

// Start allocates a session. No process runs until Send and Next.
func (b *OpenCodeBackend) Start(ctx context.Context, opts StartOptions) (WorkerSession, error) {
	bin, err := lookWorkerCLI(b.Name())
	if err != nil {
		return nil, err
	}
	// The same preparation a launch does: the 2.x gate, and the composed
	// .opencode/AGENTS.md that carries the instructions and rules. Once per
	// session, not per turn: nothing it reads changes between turns.
	if err := vendor.PrepareOpenCode(opts.ConfigPath, bin); err != nil {
		return nil, err
	}
	return &openCodeSession{
		ctx:       ctx,
		bin:       bin,
		opts:      opts,
		sessionID: opts.ResumeToken,
	}, nil
}

// openCodeSession holds the resumable session id across per-turn processes.
type openCodeSession struct {
	ctx       context.Context
	bin       string
	opts      StartOptions
	sessionID string
	pending   string
	hasMsg    bool
}

// ResumeToken returns the OpenCode session id once known: the one resumed, or
// the one the first turn's events reported.
func (s *openCodeSession) ResumeToken() string { return s.sessionID }

// Send queues the user message for the next process.
func (s *openCodeSession) Send(msg string) error {
	s.pending, s.hasMsg = msg, true
	return nil
}

// Close is a no-op: processes are short-lived and per-turn.
func (s *openCodeSession) Close() error { return nil }

// Next runs one `opencode run` process for the pending message and returns the
// completed turn.
func (s *openCodeSession) Next() (Turn, error) {
	if !s.hasMsg {
		return Turn{}, fmt.Errorf("opencode: Next called without a prior Send")
	}
	msg := s.pending
	s.pending, s.hasMsg = "", false

	cmd := exec.CommandContext(s.ctx, s.bin, buildOpenCodeArgs(s.opts, s.sessionID, msg)...)
	confineWorker(cmd)
	if s.opts.WorktreeDir != "" {
		cmd.Dir = s.opts.WorktreeDir
	}
	cmd.Env = workerEnvFor(s.opts.Env)
	if dir := openCodeConfigDir(s.opts.ConfigPath); dir != "" {
		cmd.Env = append(cmd.Env, "OPENCODE_CONFIG_DIR="+dir)
	}
	tail := &stderrTail{}
	cmd.Stderr = stderrSink(s.opts.Stderr, tail)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Turn{}, fmt.Errorf("opencode stdout pipe: %w", err)
	}
	if err := startWorker(cmd); err != nil {
		return Turn{}, fmt.Errorf("starting opencode: %w", err)
	}

	run, parseErr := parseOpenCodeOutput(stdout)
	// Drain what is left so opencode never blocks writing after parsing
	// stopped early.
	_, _ = io.Copy(io.Discard, stdout)
	waitErr := cmd.Wait()

	if run.sessionID != "" {
		s.sessionID = run.sessionID
	}
	if parseErr != nil {
		return Turn{}, parseErr
	}
	// An error event is OpenCode refusing or failing the turn (an unknown
	// model, for one). It also exits 1, but the event carries the reason, so
	// it wins over the exit status and the stderr tail.
	if run.failure != "" && (waitErr != nil || run.turn.Content == "") {
		return Turn{}, &WorkerError{Backend: "opencode", Message: run.failure}
	}
	// A failed exit with no step_finish is a process that died mid-turn: the
	// text so far is a fragment, not an answer, and passing it on as a
	// finished turn would hide the crash.
	if waitErr != nil && !run.finished {
		return Turn{}, exitedWorkerError("opencode", waitErr, tail)
	}
	if run.turn.Content == "" {
		return Turn{}, exitedWorkerError("opencode", waitErr, tail)
	}
	return run.turn, nil
}

// openCodeConfigDir is the OPENCODE_CONFIG_DIR for the assembled harness at
// configPath, or "" when there is none. The bundle is the vendor's ConfigDir
// inside it, and the adapter is the one place that is spelled.
func openCodeConfigDir(configPath string) string {
	if configPath == "" {
		return ""
	}
	adapter, err := vendor.Get("opencode")
	if err != nil {
		return ""
	}
	return filepath.Join(configPath, adapter.ConfigDir())
}

// buildOpenCodeArgs constructs the arguments for one turn.
//
//   - --standalone is mandatory. CONFIRMED: without it OpenCode 2.x routes the
//     command through a shared background service that ignores the client's
//     environment, so OPENCODE_CONFIG_DIR would have no effect and the
//     harness's agents would be "not found".
//   - --format json gives newline-delimited events, each with "sessionID".
//   - The message is an argument, after "--" so one starting with a dash is
//     not read as a flag (CONFIRMED). It is not piped on stdin even though
//     that works: CONFIRMED that a piped prompt makes the process exit before
//     its final step_finish, which is the only event carrying token usage, so
//     the budget would see nothing.
//   - --auto approves everything not explicitly denied, so a deny rule in the
//     project's config still applies. OpenCode has no edits-only mode, which
//     is why validateAutoApprove admits only "all" here.
//   - -m is provider/model. An effort would be a '#variant' on it, which ynh
//     does not set (see validateEffort), so a variant is the operator's to
//     write into --model.
func buildOpenCodeArgs(opts StartOptions, sessionID, msg string) []string {
	args := []string{"run", "--standalone", "--format", "json"}
	if opts.Model != "" {
		args = append(args, "-m", opts.Model)
	}
	if opts.AutoApprove == AutoApproveAll {
		args = append(args, "--auto")
	}
	if sessionID != "" {
		args = append(args, "-s", sessionID)
	}
	return append(args, "--", msg)
}

// opencode --format json event shapes (CONFIRMED against v2.0.27). Every event
// has type, timestamp, sessionID and part; the part holds the payload.
type openCodeEvent struct {
	Type      string        `json:"type"`
	SessionID string        `json:"sessionID"`
	Part      openCodePart  `json:"part"`
	Error     *openCodeFail `json:"error,omitempty"`
}

type openCodePart struct {
	Type   string          `json:"type"`
	Text   string          `json:"text,omitempty"`
	Cost   float64         `json:"cost,omitempty"`
	Tokens *openCodeTokens `json:"tokens,omitempty"`
}

// openCodeTokens is step_finish's usage for that one step. Reasoning is
// counted apart from output (CONFIRMED: output 2 beside reasoning 18), and
// both are billed as output.
type openCodeTokens struct {
	Input     int64 `json:"input"`
	Output    int64 `json:"output"`
	Reasoning int64 `json:"reasoning"`
	Cache     struct {
		Read  int64 `json:"read"`
		Write int64 `json:"write"`
	} `json:"cache"`
}

type openCodeFail struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// openCodeRun is what one process's events add up to.
type openCodeRun struct {
	sessionID string
	turn      Turn
	// failure is the last error event's message.
	failure string
	// finished is true once a step_finish was seen, the mark of a turn that
	// ran to its end.
	finished bool
}

// parseOpenCodeOutput reads the events of one `opencode run` process to the
// end of its stream. A turn spans several steps (a tool call ends a step and
// the answer comes in the next), so text parts are joined and step_finish
// usage is summed. Content is every text part, joined with a blank line, which
// is what the codex backend returns (claude concatenates its text blocks). Cost is the vendor's own per-step figure.
func parseOpenCodeOutput(r io.Reader) (openCodeRun, error) {
	var run openCodeRun
	var texts []string

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 2<<20), 2<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev openCodeEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		if run.sessionID == "" {
			run.sessionID = ev.SessionID
		}
		switch ev.Type {
		case "text":
			if ev.Part.Text != "" {
				texts = append(texts, ev.Part.Text)
			}
		case "step_finish":
			run.finished = true
			if t := ev.Part.Tokens; t != nil {
				run.turn.UsageReported = true
				run.turn.CacheReported = true
				run.turn.CacheCreationReported = true
				run.turn.Usage.InputTokens += t.Input
				run.turn.Usage.OutputTokens += t.Output + t.Reasoning
				run.turn.Usage.CacheTokens += t.Cache.Read
				run.turn.Usage.CacheCreationTokens += t.Cache.Write
				run.turn.CostReported = true
				run.turn.CostUSD += ev.Part.Cost
			}
		case "error":
			if ev.Error != nil {
				run.failure = firstNonBlank(ev.Error.Message, ev.Error.Type, "turn failed")
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return run, fmt.Errorf("reading opencode output: %w", err)
	}
	run.turn.Content = strings.Join(texts, "\n\n")
	return run, nil
}
