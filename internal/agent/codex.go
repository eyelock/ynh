package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
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

	args := []string{"exec", "--json"}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}

	cmd := exec.CommandContext(ctx, codexBin, args...)
	if opts.WorktreeDir != "" {
		cmd.Dir = opts.WorktreeDir
	}
	cmd.Env = append(os.Environ(), opts.Env...)
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
		cmd:     cmd,
		stdin:   stdinPipe,
		scanner: scanner,
	}, nil
}

// codex NDJSON input shape.
type codexUserMsg struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

// codex NDJSON output event shapes.
// Codex emits typed events; we only need "message" and "result" to drive the loop.
type codexOutputEvent struct {
	Type    string      `json:"type"`
	Message *codexMsg   `json:"message,omitempty"`
	Usage   *codexUsage `json:"usage,omitempty"`
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
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
}

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

		switch ev.Type {
		case "message":
			if ev.Message != nil && ev.Message.Role == "assistant" {
				for _, block := range ev.Message.Content {
					if block.Type == "text" {
						contentBuf.WriteString(block.Text)
					}
				}
			}

		case "result":
			if ev.Usage != nil {
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
