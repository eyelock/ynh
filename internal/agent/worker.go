package agent

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// WorkerBackend abstracts over different vendor agent CLIs.
// All wire-format details (NDJSON protocol, message shapes) live inside
// each implementation — the loop driver never sees them.
type WorkerBackend interface {
	// Name returns the backend identifier ("claude", "codex", etc.).
	Name() string
	// Start spawns the worker subprocess and returns a live session.
	Start(ctx context.Context, opts StartOptions) (WorkerSession, error)
}

// WorkerSession is a running agent subprocess.
type WorkerSession interface {
	// Send delivers a user-turn message to the worker.
	Send(msg string) error
	// Next blocks until the worker completes the current assistant turn.
	// Returns io.EOF when the worker has exited cleanly.
	Next() (Turn, error)
	// ResumeToken returns the backend-native handle (claude session id,
	// cursor chatId, codex session id) that a later relaunch supplies via
	// StartOptions.ResumeToken to reconstruct this conversation. Returns an
	// empty string before the token is known (e.g. a backend that only learns
	// its session id after the first turn) or for backends that cannot resume.
	ResumeToken() string
	// Close terminates the worker process.
	Close() error
}

// StartOptions carries per-session configuration for the worker subprocess.
type StartOptions struct {
	// WorktreeDir is the git worktree for this session; the worker runs here.
	WorktreeDir string
	// ConfigPath is the assembled harness directory (contains .claude/, CLAUDE.md, etc.).
	ConfigPath string
	// Sandbox is "srt" or "none".
	Sandbox string
	// Model overrides the default model. Empty means backend default.
	Model string
	// ResumeToken, when non-empty, starts the worker in resume mode against a
	// prior conversation rather than a fresh one. The value is a backend-native
	// handle previously obtained from WorkerSession.ResumeToken (claude session
	// id, cursor chatId, codex session id).
	ResumeToken string
	// Env holds additional environment variables to pass to the subprocess.
	Env []string
	// Stderr captures subprocess stderr if non-nil.
	Stderr io.Writer
}

// Turn represents a completed assistant response.
type Turn struct {
	// Content is the assistant's response text.
	Content string
	// Usage tracks token consumption for budget enforcement.
	Usage Usage
	// UsageReported is true when the vendor output carried a usage record for
	// this turn, even one of all zeros. A zero Usage alone cannot tell "the
	// model consumed nothing" from "this backend does not report usage".
	UsageReported bool
}

// Usage tracks token consumption for a turn.
type Usage struct {
	InputTokens  int64
	OutputTokens int64
	CacheTokens  int64
}

// WorkerError is a turn the worker could not complete: the vendor CLI said so
// in its own output (an error result, a failed turn, an authentication
// failure). It is a fault of the worker's environment, not agent output, so
// the loop ends the run as a worker error on that turn rather than feeding the
// message back and later calling the repetition stuck.
type WorkerError struct {
	// Backend names the worker backend ("claude", "codex", "cursor").
	Backend string
	// Message is the vendor's own words for the failure.
	Message string
	// Auth marks a failure to authenticate with the model. The usual cause is
	// a harness whose env_passthrough does not pass the vendor's credentials,
	// which workerEnvFor then strips by design.
	Auth bool
}

func (e *WorkerError) Error() string {
	msg := e.Backend + ": " + e.Message
	if e.Auth {
		msg += " (authentication failed: does the harness env_passthrough pass the vendor's credentials?)"
	}
	return msg
}

// firstNonBlank returns the first of ss that is not blank, trimmed.
func firstNonBlank(ss ...string) string {
	for _, s := range ss {
		if t := strings.TrimSpace(s); t != "" {
			return t
		}
	}
	return ""
}

// unmeteredTurn reports a turn that has a response but consumed no tokens.
// A model cannot answer without consuming tokens, so such a response was
// written by the vendor CLI itself (a login prompt, an API error banner), not
// by the model. It applies only when the backend reported usage for the turn:
// for a backend that reports none, zero is the absence of a measurement, not
// a measurement of zero.
func unmeteredTurn(backend string, t Turn) error {
	if !t.UsageReported || t.Usage != (Usage{}) {
		return nil
	}
	content := strings.TrimSpace(t.Content)
	if content == "" {
		return nil
	}
	if line, _, cut := strings.Cut(content, "\n"); cut {
		content = line
	}
	const maxLen = 200
	if r := []rune(content); len(r) > maxLen {
		content = string(r[:maxLen]) + "..."
	}
	return &WorkerError{
		Backend: backend,
		Message: fmt.Sprintf("the response consumed no tokens, so the model did not write it: %q", content),
	}
}
