package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// CodexBackend implements WorkerBackend for OpenAI Codex CLI.
//
// `codex exec` runs exactly one turn per process: it reads the prompt from
// stdin to EOF, emits the turn as JSONL events on stdout (--json), and exits
// once the turn completes or fails. So, like cursor, the session spawns one
// codex process per turn and carries the conversation across them by thread
// id, with `codex exec resume <thread_id>` from the second turn on.
//
// The event shapes are codex's ThreadEvent (codex-rs/exec/src/exec_events.rs
// in openai/codex): thread.started, turn.started, item.started/updated/
// completed, turn.completed, turn.failed and error.
type CodexBackend struct{}

func (b *CodexBackend) Name() string { return "codex" }

// Start prepares a codex session. No process runs until Send and Next.
func (b *CodexBackend) Start(ctx context.Context, opts StartOptions) (WorkerSession, error) {
	codexBin, err := lookWorkerCLI(b.Name())
	if err != nil {
		return nil, err
	}
	s := &codexSession{
		ctx:      ctx,
		codexBin: codexBin,
		opts:     opts,
		threadID: opts.ResumeToken,
	}
	// codex reports the thread's running token totals, so each turn's usage
	// is the difference from the totals already counted. A fresh thread starts
	// at zero. A resumed one continues from the totals its rollout saved,
	// which are what the checkpoint recorded as consumed; without that record
	// the first turn's usage cannot be told apart from the earlier turns'.
	switch {
	case opts.ResumeToken == "":
		s.totalsKnown = true
	case opts.UsageBase != nil:
		b := opts.UsageBase
		s.totals = codexTotals{
			input:   b.InputTokens + b.CacheTokens + b.CacheCreationTokens,
			cached:  b.CacheTokens,
			written: b.CacheCreationTokens,
			output:  b.OutputTokens,
		}
		s.totalsKnown = true
	}
	return s, nil
}

// buildCodexArgs constructs the arguments for one codex exec turn. threadID
// is the thread to continue, empty for the first turn of a fresh thread. The
// prompt goes on stdin ("-"), so its length and leading characters never
// meet argument parsing.
func buildCodexArgs(opts StartOptions, threadID string) []string {
	args := []string{"exec"}
	if threadID != "" {
		args = append(args, "resume", threadID)
	}
	args = append(args, "--json")
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	// codex exec has no effort flag; the config override sets it for this
	// process. The value is TOML, so it is quoted as a string. Every turn is
	// its own codex exec, so every turn passes it.
	if opts.Effort != "" {
		args = append(args, "-c", `model_reasoning_effort="`+opts.Effort+`"`)
	}
	// codex has no edits-only mode (its sandbox modes govern commands), so
	// validateAutoApprove admits only "all" here. Without --auto-approve no
	// approval or sandbox flag is passed.
	if opts.AutoApprove == AutoApproveAll {
		args = append(args, "--dangerously-bypass-approvals-and-sandbox")
	}
	return append(args, "-")
}

// codexOutputEvent is the part of a codex exec --json event ynh reads.
// Unknown event and item types decode to their type alone and are skipped.
// No exec event names the model the turn ran on, thread.started included, so
// a codex turn reports none: passing --model asks for one, it does not
// witness which ran.
type codexOutputEvent struct {
	Type string `json:"type"`
	// ThreadID is on thread.started, the first event of every run, fresh or
	// resumed. It is the id `codex exec resume` takes.
	ThreadID string `json:"thread_id,omitempty"`
	// Item is on item.* events; only completed agent messages are read.
	Item *codexItem `json:"item,omitempty"`
	// Usage is on turn.completed.
	Usage *codexUsage `json:"usage,omitempty"`
	// Message is on an error event.
	Message string `json:"message,omitempty"`
	// Error is on turn.failed, as {"message": "..."}. Held raw so an
	// unexpected shape cannot drop the event.
	Error json.RawMessage `json:"error,omitempty"`
}

type codexItem struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// codexUsage is turn.completed's usage: the thread's running totals, not the
// turn's own (codex reports ThreadTokenUsage.total, and a resumed thread
// continues from the totals its rollout saved).
//
// The counts nest the way the OpenAI Responses API's do: input_tokens
// includes cached_input_tokens and cache_write_input_tokens, and
// output_tokens includes reasoning_output_tokens. The nested counts are
// breakdowns, so ynh reads only what it needs to keep cache reads and cache
// writes apart from input. cache_write_input_tokens arrived in codex in
// July 2026 (openai/codex#33454); an older codex omits it.
type codexUsage struct {
	InputTokens           int64  `json:"input_tokens"`
	CachedInputTokens     int64  `json:"cached_input_tokens"`
	CacheWriteInputTokens *int64 `json:"cache_write_input_tokens"`
	OutputTokens          int64  `json:"output_tokens"`
}

// codexTotals are a thread's running totals in codex's own nesting, with a
// cache-write count codex did not report held as zero.
type codexTotals struct {
	input, cached, written, output int64
}

// codexErrorMessage returns the message of a turn.failed event's error.
func codexErrorMessage(raw json.RawMessage) string {
	var e struct {
		Message string `json:"message"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &e) != nil {
		return ""
	}
	return e.Message
}

// codexWorkerError is a turn codex could not complete. A 401 from the model
// endpoint is codex without usable credentials.
func codexWorkerError(msg string) *WorkerError {
	return &WorkerError{
		Backend: "codex",
		Message: msg,
		Auth:    strings.Contains(msg, "401 Unauthorized"),
	}
}

// codexRun is what one codex exec process reported.
type codexRun struct {
	threadID string
	content  string
	// completed marks turn.completed; usage is its totals, when it had any.
	completed bool
	usage     *codexUsage
	// failure is turn.failed's message; failed marks that the event arrived.
	failed  bool
	failure string
	// lastError is the last error event. codex also emits error events for
	// attempts it retries ("Reconnecting... 2/5"), so one alone does not end
	// the turn; it is the reason when the turn ends without completing.
	lastError string
}

// parseCodexOutput reads the events of one codex exec process until the turn
// completes or fails, or the stream ends.
func parseCodexOutput(r io.Reader) (codexRun, error) {
	var run codexRun
	var messages []string

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 2<<20), 2<<20)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev codexOutputEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}

		switch ev.Type {
		case "thread.started":
			run.threadID = ev.ThreadID

		case "item.completed":
			if ev.Item != nil && ev.Item.Type == "agent_message" {
				messages = append(messages, ev.Item.Text)
			}

		case "error":
			if m := strings.TrimSpace(ev.Message); m != "" {
				run.lastError = m
			}

		case "turn.failed":
			run.failed = true
			run.failure = codexErrorMessage(ev.Error)
			return run, nil

		case "turn.completed":
			run.completed = true
			run.content = strings.Join(messages, "\n\n")
			if ev.Usage != nil {
				u := *ev.Usage
				run.usage = &u
			}
			return run, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return run, fmt.Errorf("reading codex output: %w", err)
	}
	return run, nil
}

// codexSession carries a codex thread across per-turn processes.
type codexSession struct {
	ctx      context.Context
	codexBin string
	opts     StartOptions
	threadID string
	pending  string
	hasMsg   bool
	// totals are the thread's running token totals already counted, when
	// known (see Start).
	totals      codexTotals
	totalsKnown bool
}

// ResumeToken returns the codex thread id once known: the one resumed, or
// the one the first turn's thread.started reported.
func (s *codexSession) ResumeToken() string { return s.threadID }

// Send queues the user message for the next codex process.
func (s *codexSession) Send(msg string) error {
	s.pending, s.hasMsg = msg, true
	return nil
}

// Next runs one codex exec process for the pending message and returns the
// completed turn.
func (s *codexSession) Next() (Turn, error) {
	if !s.hasMsg {
		return Turn{}, fmt.Errorf("codex: Next called without a prior Send")
	}
	msg := s.pending
	s.pending, s.hasMsg = "", false

	cmd := exec.CommandContext(s.ctx, s.codexBin, buildCodexArgs(s.opts, s.threadID)...)
	confineWorker(cmd)
	if s.opts.WorktreeDir != "" {
		cmd.Dir = s.opts.WorktreeDir
	}
	cmd.Env = workerEnvFor(s.opts.Env)
	cmd.Stdin = strings.NewReader(msg)
	tail := &stderrTail{}
	cmd.Stderr = stderrSink(s.opts.Stderr, tail)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return Turn{}, fmt.Errorf("codex stdout pipe: %w", err)
	}
	if err := startWorker(cmd); err != nil {
		return Turn{}, fmt.Errorf("starting codex: %w", err)
	}
	run, parseErr := parseCodexOutput(stdoutPipe)
	// Drain what is left so codex never blocks writing after the turn ended.
	_, _ = io.Copy(io.Discard, stdoutPipe)
	waitErr := cmd.Wait()

	return s.turnFrom(run, parseErr, waitErr, tail)
}

// turnFrom turns one process's report into the turn, or the reason there is
// none.
func (s *codexSession) turnFrom(run codexRun, parseErr, waitErr error, tail *stderrTail) (Turn, error) {
	if run.threadID != "" {
		s.threadID = run.threadID
	}
	if parseErr != nil {
		return Turn{}, parseErr
	}
	switch {
	case run.failed:
		// codex could not complete the turn (it could not reach or
		// authenticate with its model, for one). The failure is the
		// worker's, not agent output.
		return Turn{}, codexWorkerError(firstNonBlank(run.failure, run.lastError, "turn failed"))
	case !run.completed && run.lastError != "":
		// The stream ended without the turn completing, after an error.
		return Turn{}, codexWorkerError(run.lastError)
	case !run.completed:
		// No turn. A non-zero exit is codex refusing or failing, and its
		// stderr says why; a clean one is io.EOF.
		return Turn{}, exitedWorkerError("codex", waitErr, tail)
	}
	turn := Turn{Content: run.content}
	if run.usage != nil {
		s.recordUsage(*run.usage, &turn)
	}
	return turn, nil
}

// recordUsage turns the thread's running totals into this turn's usage, in
// the meaning the claude backend gives the fields: InputTokens excludes cache
// reads and cache writes, CacheTokens is the cache reads,
// CacheCreationTokens the cache writes, and OutputTokens includes reasoning.
func (s *codexSession) recordUsage(u codexUsage, turn *Turn) {
	cur := codexTotals{input: u.InputTokens, cached: u.CachedInputTokens, output: u.OutputTokens}
	if u.CacheWriteInputTokens != nil {
		cur.written = *u.CacheWriteInputTokens
	}
	prev, known := s.totals, s.totalsKnown
	s.totals, s.totalsKnown = cur, true
	if !known {
		// Resumed without a record of what the thread had consumed: the
		// totals include earlier turns, so this turn's share is unknown.
		// Reported as no usage rather than counting those turns twice.
		return
	}
	d := codexTotals{
		input:   cur.input - prev.input,
		cached:  cur.cached - prev.cached,
		written: cur.written - prev.written,
		output:  cur.output - prev.output,
	}
	if d.input < 0 || d.cached < 0 || d.written < 0 || d.output < 0 {
		// A running total never falls on one thread, so the counted totals
		// were not this thread's. This turn's share is unknown.
		return
	}
	turn.UsageReported = true
	turn.CacheReported = true
	turn.CacheCreationReported = u.CacheWriteInputTokens != nil
	turn.Usage = Usage{
		InputTokens:         max(d.input-d.cached-d.written, 0),
		OutputTokens:        d.output,
		CacheTokens:         d.cached,
		CacheCreationTokens: d.written,
	}
}

// Close is a no-op: each turn's codex process has exited by the time Next
// returns.
func (s *codexSession) Close() error { return nil }
