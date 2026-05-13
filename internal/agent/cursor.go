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
	"os"
	"os/exec"
)

// CursorBackend implements WorkerBackend for Cursor's headless agent CLI.
// Cursor spawns a new subprocess per turn, resuming conversation state via
// --resume <chatId>. The session manages the chatId across turns.
type CursorBackend struct{}

func (b *CursorBackend) Name() string { return "cursor" }

// Start allocates a new session with a fresh chat ID.
// The first subprocess is not spawned until Send+Next are called.
func (b *CursorBackend) Start(_ context.Context, opts StartOptions) (WorkerSession, error) {
	cursorBin, err := exec.LookPath("cursor")
	if err != nil {
		return nil, fmt.Errorf("cursor not found on PATH: %w", err)
	}

	// Generate a unique chat ID for this session.
	b8 := make([]byte, 8)
	_, _ = rand.Read(b8)
	chatID := hex.EncodeToString(b8)

	return &cursorSession{
		cursorBin: cursorBin,
		chatID:    chatID,
		opts:      opts,
		firstTurn: true,
	}, nil
}

// cursorSession holds the resumable chat state across per-turn subprocesses.
type cursorSession struct {
	cursorBin string
	chatID    string
	opts      StartOptions
	firstTurn bool
	pending   string // message queued for the next Next() call
}

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

	args := []string{
		"agent",
		"--print",
		"--output-format", "stream-json",
		"--trust",
	}
	if s.opts.WorktreeDir != "" {
		args = append(args, "--workspace", s.opts.WorktreeDir)
	}
	if s.opts.Model != "" {
		args = append(args, "--model", s.opts.Model)
	}
	if !s.firstTurn {
		args = append(args, "--resume", s.chatID)
	}
	// Pass the user message as the final positional argument.
	args = append(args, msg)
	s.firstTurn = false

	cmd := exec.Command(s.cursorBin, args...)
	if s.opts.WorktreeDir != "" {
		cmd.Dir = s.opts.WorktreeDir
	}
	cmd.Env = append(os.Environ(), s.opts.Env...)
	if s.opts.Stderr != nil {
		cmd.Stderr = s.opts.Stderr
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return Turn{}, fmt.Errorf("cursor stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return Turn{}, fmt.Errorf("starting cursor: %w", err)
	}

	turn, parseErr := parseCursorOutput(stdoutPipe)

	if waitErr := cmd.Wait(); waitErr != nil {
		if parseErr == nil {
			// Subprocess exit error takes priority only if we have no content.
			if turn.Content == "" {
				return Turn{}, fmt.Errorf("cursor exited: %w", waitErr)
			}
		}
	}

	if parseErr != nil && parseErr != io.EOF {
		return Turn{}, parseErr
	}
	if turn.Content == "" {
		return Turn{}, io.EOF
	}
	return turn, nil
}

// Close is a no-op for Cursor — subprocesses are short-lived per-turn.
func (s *cursorSession) Close() error { return nil }

// cursor stream-json output shapes (same wire format as Claude Code).
type cursorOutputEvent struct {
	Type    string           `json:"type"`
	Message *cursorOutputMsg `json:"message,omitempty"`
	Usage   *cursorUsage     `json:"usage,omitempty"`
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
		case "assistant":
			if ev.Message != nil {
				for _, block := range ev.Message.Content {
					if block.Type == "text" {
						contentBuf.WriteString(block.Text)
					}
				}
				if ev.Message.Usage != nil {
					turn.Usage.InputTokens += ev.Message.Usage.InputTokens
					turn.Usage.OutputTokens += ev.Message.Usage.OutputTokens
					turn.Usage.CacheTokens += ev.Message.Usage.CacheTokens
				}
			}

		case "result":
			if ev.Usage != nil {
				turn.Usage.InputTokens += ev.Usage.InputTokens
				turn.Usage.OutputTokens += ev.Usage.OutputTokens
				turn.Usage.CacheTokens += ev.Usage.CacheTokens
			}
			turn.Content = contentBuf.String()
			return turn, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return Turn{}, fmt.Errorf("reading cursor output: %w", err)
	}
	turn.Content = contentBuf.String()
	return turn, io.EOF
}
