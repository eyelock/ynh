package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/eyelock/ynh/internal/vendor"
)

// CopilotBackend implements WorkerBackend for GitHub Copilot CLI's
// non-interactive mode (`copilot --prompt ... --output-format json`).
//
// Copilot spawns a new process per turn and carries the conversation across
// them by session id, with `--resume=<id>` from the second turn on. The id is
// learned from the first turn's final result event, so ResumeToken is empty
// until then, unless a resume seeded it.
//
// The output shapes were captured from Copilot CLI 1.0.107 and are the
// fixtures in testdata/copilot-*.jsonl. Copilot has no per-turn exit of its
// own: a "turn" in its events (assistant.turn_start / assistant.turn_end) is
// one model call, and a tool call makes a second one. The answer to a ynh turn
// is every assistant.message of the process, and the process ends with one
// result event.
type CopilotBackend struct{}

func (b *CopilotBackend) Name() string { return "copilot" }

// Start allocates a session. No process runs until Send and Next.
func (b *CopilotBackend) Start(ctx context.Context, opts StartOptions) (WorkerSession, error) {
	bin, err := lookWorkerCLI(b.Name())
	if err != nil {
		return nil, err
	}
	sess := &copilotSession{
		ctx:       ctx,
		bin:       bin,
		opts:      opts,
		sessionID: opts.ResumeToken,
	}
	if opts.IsolatedMCP {
		sess.isolation, _ = workerMCPIsolation(&vendor.Copilot{}, opts.ConfigPath, workerEnvFor(opts.Env), opts.WorktreeDir, opts.Stderr)
	}
	return sess, nil
}

// copilotSession holds the resumable session id across per-turn processes.
type copilotSession struct {
	ctx       context.Context
	bin       string
	opts      StartOptions
	sessionID string
	pending   string
	hasMsg    bool
	// isolation turns off the operator's own MCP servers (IsolatedMCP),
	// found once in Start and passed on every turn.
	isolation []string
}

// ResumeToken returns the Copilot session id once known: the one resumed, or
// the one the first turn's result reported.
func (s *copilotSession) ResumeToken() string { return s.sessionID }

// Send queues the user message for the next process.
func (s *copilotSession) Send(msg string) error {
	s.pending, s.hasMsg = msg, true
	return nil
}

// Close is a no-op: processes are short-lived and per-turn.
func (s *copilotSession) Close() error { return nil }

// Next runs one `copilot` process for the pending message and returns the
// completed turn.
func (s *copilotSession) Next() (Turn, error) {
	if !s.hasMsg {
		return Turn{}, fmt.Errorf("copilot: Next called without a prior Send")
	}
	msg := s.pending
	s.pending, s.hasMsg = "", false

	args, err := buildCopilotWorkerArgs(s.opts, s.sessionID, msg)
	if err != nil {
		return Turn{}, err
	}
	args = append(args, s.isolation...)

	cmd := exec.CommandContext(s.ctx, s.bin, args...)
	confineWorker(cmd)
	if s.opts.WorktreeDir != "" {
		cmd.Dir = s.opts.WorktreeDir
	}
	cmd.Env = workerEnvFor(s.opts.Env)
	tail := &stderrTail{}
	cmd.Stderr = stderrSink(s.opts.Stderr, tail)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Turn{}, fmt.Errorf("copilot stdout pipe: %w", err)
	}
	if err := startWorker(cmd); err != nil {
		return Turn{}, fmt.Errorf("starting copilot: %w", err)
	}

	run, parseErr := parseCopilotOutput(stdout)
	// Drain what is left so copilot never blocks writing after parsing
	// stopped early.
	_, _ = io.Copy(io.Discard, stdout)
	waitErr := cmd.Wait()

	if run.sessionID != "" {
		s.sessionID = run.sessionID
	}
	if parseErr != nil {
		return Turn{}, parseErr
	}
	// No result event, or one that carries a failing exit code, is a process
	// that died or failed mid-turn: whatever text came before is a fragment,
	// not an answer, and passing it on as a finished turn would hide it.
	failedExit := run.finished && run.exitCode != 0
	if (waitErr != nil && !run.finished) || failedExit {
		if waitErr == nil {
			return Turn{}, markCopilotAuth(&WorkerError{
				Backend: "copilot",
				Message: firstNonBlank(tail.String(), fmt.Sprintf("exited with code %d and no message", run.exitCode)),
			})
		}
		return Turn{}, markCopilotAuth(exitedWorkerError("copilot", waitErr, tail))
	}
	if run.turn.Content == "" {
		return Turn{}, markCopilotAuth(exitedWorkerError("copilot", waitErr, tail))
	}
	return run.turn, nil
}

// copilotAuthMarkers are the words Copilot's stderr opens with when it has no
// usable login. A signed-out `copilot -p` run (captured on 1.0.107, fixture
// testdata/copilot-signed-out.stderr) writes nothing to stdout, no events and
// no result, prints this to stderr and exits 1. Copilot gives no structured
// signal for it, so stderr is the only thing to read. A bad credential has
// not been captured: that needs a token to corrupt, and a wrong one may well
// word it differently, so only the signed-out case is recognised.
var copilotAuthMarkers = []string{"No authentication information found"}

// markCopilotAuth flags a WorkerError whose message is Copilot saying it is
// not signed in, so the failure reads as an authentication one. Any other
// error, or none, passes through unchanged.
func markCopilotAuth(err error) error {
	var we *WorkerError
	if !errors.As(err, &we) {
		return err
	}
	for _, m := range copilotAuthMarkers {
		if strings.Contains(we.Message, m) {
			we.Auth = true
			break
		}
	}
	return err
}

// buildCopilotWorkerArgs constructs the arguments for one turn.
//
//   - --output-format json gives newline-delimited events.
//   - The message is the value of --prompt=, so one starting with a dash is not
//     read as a flag.
//   - --no-ask-user stops the agent asking a question nobody is there to
//     answer; --no-auto-update keeps a mid-launch self-update from tearing
//     down the process (see vendor.buildCopilotArgs).
//   - Copilot has no mode that approves edits and withholds commands, so
//     validateAutoApprove admits only "all", which is --allow-all-tools. Paths
//     and URLs keep Copilot's own checks.
//   - The harness arrives as vendor.CopilotWorkerArgs: its plugin directory,
//     its MCP servers, and its instructions projected into the worktree.
func buildCopilotWorkerArgs(opts StartOptions, sessionID, msg string) ([]string, error) {
	args := []string{"--output-format", "json", "--no-ask-user", "--no-auto-update"}
	harness, err := vendor.CopilotWorkerArgs(opts.ConfigPath, opts.WorktreeDir)
	if err != nil {
		return nil, err
	}
	args = append(args, harness...)
	args = append(args, workspaceArgs(vendorCopilot, opts.Workspace)...)
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	if opts.Effort != "" {
		args = append(args, "--reasoning-effort", opts.Effort)
	}
	if opts.AutoApprove == AutoApproveAll {
		args = append(args, "--allow-all-tools")
	}
	if sessionID != "" {
		args = append(args, "--resume="+sessionID)
	}
	return append(args, "--prompt="+msg), nil
}

// copilotEvent is the part of a `copilot --output-format json` event ynh reads.
// Events are one JSON object per line; most are ephemeral progress and are
// skipped by type.
type copilotEvent struct {
	Type string `json:"type"`
	// Data is on the session.* and assistant.* events.
	Data json.RawMessage `json:"data,omitempty"`
	// The rest are on the final result event.
	SessionID string         `json:"sessionId,omitempty"`
	ExitCode  int            `json:"exitCode,omitempty"`
	Usage     *copilotResult `json:"usage,omitempty"`
}

// copilotResult is the result event's usage. It counts premium requests and
// durations, not tokens, so it is not the turn's token usage.
type copilotResult struct {
	PremiumRequests int64 `json:"premiumRequests"`
}

// copilotMessage is an assistant.message's data: one model response, which is
// either text or tool requests.
type copilotMessage struct {
	Content string `json:"content"`
	Model   string `json:"model"`
}

// copilotCheckpoint is a session.usage_checkpoint's data. Its accounting
// snapshot holds the session's running totals, so the last checkpoint of the
// process is the turn's usage.
type copilotCheckpoint struct {
	AccountingSnapshot struct {
		TokenDetails map[string]struct {
			TokenCount int64 `json:"tokenCount"`
		} `json:"tokenDetails"`
	} `json:"accountingSnapshot"`
}

// copilotRun is what one process's events add up to.
type copilotRun struct {
	sessionID string
	turn      Turn
	// finished is true once the result event was seen, the mark of a process
	// that ran to its end; exitCode is its exit code.
	finished bool
	exitCode int
}

// parseCopilotOutput reads the events of one copilot process to the end of its
// stream.
//
// Content is every non-empty assistant.message, joined with a blank line (a
// tool call makes one message with no text and the answer comes in the next),
// which is what the codex backend returns. Usage is the last usage
// checkpoint's token details: input is what was not cached, beside cache_read
// and cache_write, so none of them is counted inside another. Copilot reports
// no cost in money, only premium requests, so no cost is reported; and no
// token details means no usage is reported, never a zero. Reasoning tokens
// have no entry in the token details and are not counted.
func parseCopilotOutput(r io.Reader) (copilotRun, error) {
	var run copilotRun
	var texts []string
	var details map[string]struct {
		TokenCount int64 `json:"tokenCount"`
	}

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4<<20), 4<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev copilotEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "assistant.message":
			var m copilotMessage
			if json.Unmarshal(ev.Data, &m) != nil {
				continue
			}
			if m.Content != "" {
				texts = append(texts, m.Content)
			}
			if m.Model != "" {
				run.turn.Model = strings.TrimSpace(m.Model)
			}
		case "session.usage_checkpoint":
			var c copilotCheckpoint
			if json.Unmarshal(ev.Data, &c) == nil && c.AccountingSnapshot.TokenDetails != nil {
				details = c.AccountingSnapshot.TokenDetails
			}
		case "result":
			run.finished = true
			run.exitCode = ev.ExitCode
			if ev.SessionID != "" {
				run.sessionID = ev.SessionID
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return run, fmt.Errorf("reading copilot output: %w", err)
	}
	run.turn.Content = strings.Join(texts, "\n\n")
	if in, ok := details["input"]; ok {
		out := details["output"]
		run.turn.UsageReported = true
		run.turn.Usage.InputTokens = in.TokenCount
		run.turn.Usage.OutputTokens = out.TokenCount
		if c, ok := details["cache_read"]; ok {
			run.turn.CacheReported = true
			run.turn.Usage.CacheTokens = c.TokenCount
		}
		if c, ok := details["cache_write"]; ok {
			run.turn.CacheCreationReported = true
			run.turn.Usage.CacheCreationTokens = c.TokenCount
		}
	}
	return run, nil
}
