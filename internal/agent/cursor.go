package agent

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// CursorBackend implements WorkerBackend for Cursor's headless agent CLI.
// Cursor spawns a new subprocess per turn, resuming conversation state via
// --resume <chatId>. The session manages the chatId across turns.
type CursorBackend struct{}

func (b *CursorBackend) Name() string { return "cursor" }

// Start allocates a new session with a fresh chat ID.
// The first subprocess is not spawned until Send+Next are called.
func (b *CursorBackend) Start(ctx context.Context, opts StartOptions) (WorkerSession, error) {
	cursorBin, err := lookWorkerCLI(b.Name())
	if err != nil {
		return nil, err
	}

	// Resume token = cursor chatId. cursor already persists chats on disk and
	// resumes them with --resume <chatId> (it spawns a fresh subprocess per
	// turn), so the chatId is all we need to carry across a relaunch. A resume
	// adopts the prior chatId and uses --resume from the very first turn; a
	// fresh run generates a new chatId and omits --resume on turn one.
	chatID := opts.ResumeToken
	firstTurn := true
	if chatID != "" {
		firstTurn = false
	} else {
		b8 := make([]byte, 8)
		_, _ = rand.Read(b8)
		chatID = hex.EncodeToString(b8)
	}

	return &cursorSession{
		ctx:       ctx,
		cursorBin: cursorBin,
		chatID:    chatID,
		opts:      opts,
		firstTurn: firstTurn,
	}, nil
}

// cursorSession holds the resumable chat state across per-turn subprocesses.
type cursorSession struct {
	// ctx ends the run: cancelling it stops the turn in flight.
	ctx       context.Context
	cursorBin string
	chatID    string
	opts      StartOptions
	firstTurn bool
	pending   string // message queued for the next Next() call
}

// ResumeToken returns the cursor chatId driving this conversation.
func (s *cursorSession) ResumeToken() string { return s.chatID }

// Send queues the user message for the next subprocess invocation.
func (s *cursorSession) Send(msg string) error {
	s.pending = msg
	return nil
}

// Next spawns a cursor subprocess for the pending message, collects its output,
// and returns the completed assistant turn. Returns io.EOF if cursor exits with
// no output (should not happen in normal flow).
func (s *cursorSession) Next() (Turn, error) {
	if s.pending == "" {
		return Turn{}, fmt.Errorf("cursor: Next called without a prior Send")
	}
	msg := s.pending
	s.pending = ""

	args := buildCursorArgs(s.opts, s.chatID, s.firstTurn, msg)
	s.firstTurn = false

	cmd := exec.CommandContext(s.ctx, s.cursorBin, args...)
	confineWorker(cmd)
	if s.opts.WorktreeDir != "" {
		cmd.Dir = s.opts.WorktreeDir
	}
	cmd.Env = workerEnvFor(s.opts.Env)
	tail := &stderrTail{}
	cmd.Stderr = stderrSink(s.opts.Stderr, tail)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return Turn{}, fmt.Errorf("cursor stdout pipe: %w", err)
	}
	if err := startWorker(cmd); err != nil {
		return Turn{}, fmt.Errorf("starting cursor: %w", err)
	}

	turn, parseErr := parseCursorOutput(stdoutPipe)
	waitErr := cmd.Wait()

	if parseErr != nil && parseErr != io.EOF {
		return Turn{}, parseErr
	}
	if turn.Content == "" {
		// No answer. A non-zero exit is cursor refusing or failing, and its
		// stderr says why; a clean one is io.EOF.
		return Turn{}, exitedWorkerError("cursor", waitErr, tail)
	}
	return turn, nil
}

// buildCursorArgs constructs the arguments for one turn of Cursor's CLI, in
// its documented headless form (cursor.com/docs/cli/headless and
// cursor.com/docs/cli/reference/parameters). The binary is the CLI itself, so
// there is no leading "agent" subcommand: that is the editor launcher's
// "cursor agent" form.
func buildCursorArgs(opts StartOptions, chatID string, firstTurn bool, msg string) []string {
	args := []string{
		"--print",
		"--output-format", "stream-json",
		"--trust",
	}
	if opts.WorktreeDir != "" {
		args = append(args, "--workspace", opts.WorktreeDir)
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	// cursor has no edits-only mode, so validateAutoApprove admits only "all"
	// here. --force allows commands "unless explicitly denied", so a deny list
	// in the project's .cursor/cli.json still applies.
	if opts.AutoApprove == AutoApproveAll {
		args = append(args, "--force")
	}
	if !firstTurn {
		args = append(args, "--resume", chatID)
	}
	// Pass the user message as the final positional argument.
	return append(args, msg)
}

// Close is a no-op for Cursor — subprocesses are short-lived per-turn.
func (s *cursorSession) Close() error { return nil }

// cursor stream-json output shapes (same wire format as Claude Code).
// IsError and Result mark a turn cursor itself reports as failed; Result is
// held raw so a field of an unexpected type cannot drop the result event.
//
// Model is on the system init event, the only one that names the model:
// cursor's documented stream-json output gives a display name there, such as
// "Claude 4 Sonnet", and neither assistant messages nor the result carry one.
type cursorOutputEvent struct {
	Type    string           `json:"type"`
	Subtype string           `json:"subtype,omitempty"`
	Model   string           `json:"model,omitempty"`
	Message *cursorOutputMsg `json:"message,omitempty"`
	Usage   *cursorUsage     `json:"usage,omitempty"`
	IsError bool             `json:"is_error,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
}

type cursorOutputMsg struct {
	Role    string          `json:"role"`
	Content []cursorContent `json:"content"`
	Usage   *cursorUsage    `json:"usage,omitempty"`
}

type cursorContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type cursorUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	CacheTokens  int64 `json:"cache_read_input_tokens,omitempty"`
	// Claude's cache-write total. A pointer, so its absence is not a zero;
	// the cache_creation breakdown beside it is not decoded.
	CacheCreationTokens *int64 `json:"cache_creation_input_tokens,omitempty"`
}

// add sums the record into turn's usage.
func (u *cursorUsage) add(turn *Turn) {
	turn.UsageReported = true
	turn.Usage.InputTokens += u.InputTokens
	turn.Usage.OutputTokens += u.OutputTokens
	turn.Usage.CacheTokens += u.CacheTokens
	if u.CacheCreationTokens != nil {
		turn.CacheCreationReported = true
		turn.Usage.CacheCreationTokens += *u.CacheCreationTokens
	}
}

// parseCursorOutput reads stream-json events from a single Cursor subprocess run.
func parseCursorOutput(r io.Reader) (Turn, error) {
	var turn Turn
	var contentBuf bytes.Buffer

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 2<<20), 2<<20)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var ev cursorOutputEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}

		switch ev.Type {
		case "system":
			// One process per turn, so the model is this turn's process's.
			if m := strings.TrimSpace(ev.Model); ev.Subtype == "init" && m != "" {
				turn.Model = m
			}

		case "assistant":
			if ev.Message != nil {
				for _, block := range ev.Message.Content {
					if block.Type == "text" {
						contentBuf.WriteString(block.Text)
					}
				}
				if ev.Message.Usage != nil {
					ev.Message.Usage.add(&turn)
				}
			}

		case "result":
			if ev.Usage != nil {
				ev.Usage.add(&turn)
			}
			turn.Content = contentBuf.String()
			if ev.IsError {
				return Turn{}, &WorkerError{
					Backend: "cursor",
					Message: firstNonBlank(rawString(ev.Result), turn.Content, "turn failed"),
				}
			}
			// cursor's usage, when it reports any, is Claude Code's shape,
			// which carries cache_read_input_tokens. It reports no cost.
			turn.CacheReported = turn.UsageReported
			return turn, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return Turn{}, fmt.Errorf("reading cursor output: %w", err)
	}
	turn.Content = contentBuf.String()
	return turn, io.EOF
}
