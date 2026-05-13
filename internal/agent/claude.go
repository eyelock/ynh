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
	"path/filepath"
)

// ClaudeBackend implements WorkerBackend for Claude Code CLI.
// All stream-json wire-format details are encapsulated here — the loop
// driver never touches them.
type ClaudeBackend struct{}

func (b *ClaudeBackend) Name() string { return "claude" }

// Start spawns a claude subprocess in stream-json mode.
func (b *ClaudeBackend) Start(ctx context.Context, opts StartOptions) (WorkerSession, error) {
	claudeBin, err := exec.LookPath("claude")
	if err != nil {
		return nil, fmt.Errorf("claude not found on PATH: %w", err)
	}

	args := buildClaudeStreamArgs(opts)

	var cmd *exec.Cmd
	if opts.Sandbox == "srt" {
		srtBin, err := exec.LookPath("srt")
		if err != nil {
			return nil, fmt.Errorf("srt not found on PATH: %w", err)
		}
		srtArgs := append([]string{
			"--profile", "workspace",
			"--network-allow", ".anthropic.com,.openai.com",
			"--", claudeBin,
		}, args...)
		cmd = exec.CommandContext(ctx, srtBin, srtArgs...)
	} else {
		cmd = exec.CommandContext(ctx, claudeBin, args...)
	}

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
		return nil, fmt.Errorf("starting claude: %w", err)
	}

	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 2<<20), 2<<20) // 2 MB — handles large tool outputs

	return &claudeSession{
		cmd:     cmd,
		stdin:   stdinPipe,
		scanner: scanner,
	}, nil
}

// buildClaudeStreamArgs constructs arguments for stream-json mode.
func buildClaudeStreamArgs(opts StartOptions) []string {
	args := []string{
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--print",
	}

	if opts.ConfigPath != "" {
		pluginDir := filepath.Join(opts.ConfigPath, ".claude")
		args = append(args, "--plugin-dir", pluginDir, "--add-dir", opts.ConfigPath)

		instructionsPath := filepath.Join(opts.ConfigPath, "CLAUDE.md")
		if data, err := os.ReadFile(instructionsPath); err == nil && len(data) > 0 {
			args = append(args, "--append-system-prompt", string(data))
		}
	}

	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}

	return args
}

// claudeSession is a running Claude Code subprocess in stream-json mode.
type claudeSession struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
}

// stream-json input message shapes.
type claudeUserMsg struct {
	Type    string          `json:"type"`
	Message claudeUserInner `json:"message"`
}

type claudeUserInner struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// stream-json output event shapes.
// Unknown fields are silently ignored for graceful degradation on protocol
// changes — we use json.Decoder's default behaviour (skip unknown keys).
type claudeOutputEvent struct {
	Type    string           `json:"type"`
	Message *claudeOutputMsg `json:"message,omitempty"`
	IsError bool             `json:"is_error,omitempty"`
	Usage   *claudeUsage     `json:"usage,omitempty"`
}

type claudeOutputMsg struct {
	Role    string          `json:"role"`
	Content []claudeContent `json:"content"`
	Usage   *claudeUsage    `json:"usage,omitempty"`
}

type claudeContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type claudeUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	CacheTokens  int64 `json:"cache_read_input_tokens,omitempty"`
}

// Send delivers a user-turn message to the worker via NDJSON.
func (s *claudeSession) Send(msg string) error {
	payload := claudeUserMsg{
		Type: "user",
		Message: claudeUserInner{
			Role:    "user",
			Content: msg,
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = s.stdin.Write(data)
	return err
}

// Next reads output events until the worker completes the current turn.
// Returns io.EOF when the subprocess exits cleanly.
func (s *claudeSession) Next() (Turn, error) {
	var turn Turn
	var contentBuf bytes.Buffer

	for s.scanner.Scan() {
		line := s.scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var ev claudeOutputEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			// Unknown event shape — skip gracefully.
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
			// result signals end of this turn.
			if ev.Usage != nil {
				turn.Usage.InputTokens += ev.Usage.InputTokens
				turn.Usage.OutputTokens += ev.Usage.OutputTokens
				turn.Usage.CacheTokens += ev.Usage.CacheTokens
			}
			turn.Content = contentBuf.String()
			return turn, nil

			// All other types (system, tool_use, tool_result, stream_event, etc.)
			// are intentionally ignored — the loop driver doesn't need them.
		}
	}

	if err := s.scanner.Err(); err != nil {
		return Turn{}, fmt.Errorf("reading claude output: %w", err)
	}
	return Turn{}, io.EOF
}

// Close terminates the claude subprocess cleanly.
func (s *claudeSession) Close() error {
	// Closing stdin signals the subprocess to exit.
	if err := s.stdin.Close(); err != nil {
		_ = s.cmd.Process.Kill()
		_ = s.cmd.Wait()
		return err
	}
	return s.cmd.Wait()
}
