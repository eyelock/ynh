package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
)

// CodexBackend implements WorkerBackend for OpenAI Codex CLI.
// Codex runs as a long-lived subprocess; `codex exec --json` emits NDJSON
// events on stdout and accepts NDJSON user turns on stdin.
type CodexBackend struct{}

func (b *CodexBackend) Name() string { return "codex" }

// Start spawns a codex subprocess in JSON streaming mode.
func (b *CodexBackend) Start(ctx context.Context, opts StartOptions) (WorkerSession, error) {
	codexBin, err := exec.LookPath("codex")
	if err != nil {
		return nil, fmt.Errorf("codex not found on PATH: %w", err)
	}

	// Resume token = codex session id. codex persists session rollouts and
	// resumes them via `codex exec resume <id>`. The id is not known up front
	// (codex assigns it), so it is captured from the --json stream on the first
	// turn (see codexSession.Next) and only then becomes available to persist.
	var args []string
	if opts.ResumeToken != "" {
		args = []string{"exec", "resume", opts.ResumeToken, "--json"}
	} else {
		args = []string{"exec", "--json"}
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}

	cmd := exec.CommandContext(ctx, codexBin, args...)
	if opts.WorktreeDir != "" {
		cmd.Dir = opts.WorktreeDir
	}
	cmd.Env = workerEnvFor(opts.Env)
	if opts.Stderr != nil {
		cmd.Stderr = opts.Stderr
	}

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting codex: %w", err)
	}

	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 2<<20), 2<<20)

	return &codexSession{
		cmd:       cmd,
		stdin:     stdinPipe,
		scanner:   scanner,
		sessionID: opts.ResumeToken,
	}, nil
}

// codex NDJSON input shape.
type codexUserMsg struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

// codex NDJSON output event shapes.
// Codex emits typed events; we only need "message" and "result" to drive the
// loop, plus a session/thread id (emitted on an init/session event) to capture
// the resume token. Field names cover the known codex variants; unknown shapes
// are tolerated because absent fields decode to the zero value.
type codexOutputEvent struct {
	Type      string      `json:"type"`
	Message   *codexMsg   `json:"message,omitempty"`
	Usage     *codexUsage `json:"usage,omitempty"`
	SessionID string      `json:"session_id,omitempty"`
	ThreadID  string      `json:"thread_id,omitempty"`
	// Error carries the failure on a "turn.failed" event, as
	// {"message": "..."}. Held raw so an unexpected shape cannot drop the event.
	Error json.RawMessage `json:"error,omitempty"`
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

type codexMsg struct {
	Role    string         `json:"role"`
	Content []codexContent `json:"content"`
}

type codexContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type codexUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

type codexSession struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	scanner   *bufio.Scanner
	sessionID string
}

// ResumeToken returns the codex session id once captured from the event
// stream. Empty until the first turn surfaces it (or if codex did not emit
// one, in which case resume falls back to loop accounting only).
func (s *codexSession) ResumeToken() string { return s.sessionID }

// Send delivers a user-turn message to the codex subprocess via NDJSON.
func (s *codexSession) Send(msg string) error {
	payload := codexUserMsg{Type: "user", Content: msg}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = s.stdin.Write(data)
	return err
}

// Next reads events until the codex subprocess signals end-of-turn.
// Returns io.EOF when the subprocess exits cleanly.
func (s *codexSession) Next() (Turn, error) {
	var turn Turn
	var contentBuf bytes.Buffer

	for s.scanner.Scan() {
		line := s.scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var ev codexOutputEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}

		// Capture the session/thread id the first time codex emits it so a
		// later relaunch can resume via `codex exec resume <id>`.
		if s.sessionID == "" {
			if ev.SessionID != "" {
				s.sessionID = ev.SessionID
			} else if ev.ThreadID != "" {
				s.sessionID = ev.ThreadID
			}
		}

		switch ev.Type {
		case "message":
			if ev.Message != nil && ev.Message.Role == "assistant" {
				for _, block := range ev.Message.Content {
					if block.Type == "text" {
						contentBuf.WriteString(block.Text)
					}
				}
			}

		case "turn.failed":
			// codex could not complete the turn (it could not reach or
			// authenticate with its model, for one). The failure is the
			// worker's, not agent output.
			return Turn{}, &WorkerError{
				Backend: "codex",
				Message: firstNonBlank(codexErrorMessage(ev.Error), "turn failed"),
			}

		case "result":
			if ev.Usage != nil {
				turn.UsageReported = true
				turn.Usage.InputTokens += ev.Usage.InputTokens
				turn.Usage.OutputTokens += ev.Usage.OutputTokens
			}
			turn.Content = contentBuf.String()
			return turn, nil
		}
	}

	if err := s.scanner.Err(); err != nil {
		return Turn{}, fmt.Errorf("reading codex output: %w", err)
	}
	return Turn{}, io.EOF
}

// Close terminates the codex subprocess cleanly.
func (s *codexSession) Close() error {
	if err := s.stdin.Close(); err != nil {
		_ = s.cmd.Process.Kill()
		_ = s.cmd.Wait()
		return err
	}
	return s.cmd.Wait()
}
