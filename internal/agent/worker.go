package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
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
	// SessionDir is the run's session directory (beside its trajectory),
	// or "" when the run has none. Under srt the sandbox's settings file is
	// written there.
	SessionDir string
	// AutoApprove is "", "edits" or "all": the --auto-approve level, already
	// validated for this backend. Empty passes no permission flag at all.
	AutoApprove string
	// Model overrides the default model. Empty means backend default.
	Model string
	// Effort is "", "low", "medium" or "high": the reasoning effort to ask
	// for, already validated for this backend. Empty passes no effort setting.
	Effort string
	// ResumeToken, when non-empty, starts the worker in resume mode against a
	// prior conversation rather than a fresh one. The value is a backend-native
	// handle previously obtained from WorkerSession.ResumeToken (claude session
	// id, cursor chatId, codex session id).
	ResumeToken string
	// UsageBase is what the resumed conversation had already consumed, as
	// the checkpoint recorded it, or nil when that is unknown or nothing is
	// resumed. A backend whose vendor reports running totals per
	// conversation (codex) subtracts it so earlier turns are not counted
	// again; the others ignore it.
	UsageBase *Usage
	// Env holds additional environment variables to pass to the subprocess.
	Env []string
	// TelemetryEndpoint is the run's telemetry relay, or "" when there is
	// none. A backend ynh can configure (SupportsTelemetryRelay) points its
	// vendor CLI's telemetry there; its environment already carries the
	// settings, and this is for what has to go on the command line.
	TelemetryEndpoint string
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
	// CacheReported is true when that usage record carries a cache-read
	// count, so Usage.CacheTokens is a measurement rather than a default.
	CacheReported bool
	// CacheCreationReported is true when that usage record carries a
	// cache-write count, so Usage.CacheCreationTokens is a measurement.
	CacheCreationReported bool
	// CostUSD is what the vendor reported this turn cost, in US dollars.
	// It is meaningful only when CostReported: ynh never prices tokens itself,
	// and a zero for a backend that reports no cost would read as "free".
	CostUSD      float64
	CostReported bool
	// Effort is the reasoning effort the worker reported it runs with, once
	// the backend has said. Empty means the backend has not reported one,
	// not that the worker runs without one.
	Effort string
	// Model is the model the worker reported running, in the backend's own
	// words, once it has said. Empty means the backend has not reported one;
	// it is never the model that was asked for.
	Model string
}

// Usage tracks token consumption for a turn.
type Usage struct {
	InputTokens  int64
	OutputTokens int64
	CacheTokens  int64
	// CacheCreationTokens is what the turn wrote to the prompt cache. Like
	// CacheTokens it is carried beside the input and output, never in them.
	CacheCreationTokens int64
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

// stderrTail keeps the last part of a worker's stderr while it is also passed
// through to the operator. A vendor CLI that refuses to start (bypass
// permissions as root, a mode disabled by managed policy) says why on stderr
// and exits; without the tail the run would end as "worker exited" with the
// reason scrolled past.
type stderrTail struct {
	mu  sync.Mutex
	buf []byte
}

const stderrTailMax = 4096

func (t *stderrTail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - stderrTailMax; over > 0 {
		t.buf = append(t.buf[:0], t.buf[over:]...)
	}
	return len(p), nil
}

// String returns the retained stderr, trimmed, starting at a line boundary
// when the start was cut off.
func (t *stderrTail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := string(t.buf)
	if len(t.buf) == stderrTailMax {
		if _, rest, ok := strings.Cut(s, "\n"); ok {
			s = rest
		}
	}
	return strings.TrimSpace(s)
}

// stderrSink returns the writer a worker subprocess's stderr goes to: the
// operator's stream, if any, and the tail.
func stderrSink(operator io.Writer, tail *stderrTail) io.Writer {
	if operator == nil {
		return tail
	}
	return io.MultiWriter(operator, tail)
}

// exitedWorkerError classifies a worker process that ended. A clean exit is
// io.EOF, as before. A non-zero exit is the vendor refusing or failing, and
// its own words on stderr are the reason.
func exitedWorkerError(backend string, waitErr error, tail *stderrTail) error {
	if waitErr == nil {
		return io.EOF
	}
	var exitErr *exec.ExitError
	if !errors.As(waitErr, &exitErr) {
		return fmt.Errorf("%s: waiting for worker: %w", backend, waitErr)
	}
	var stderr string
	if tail != nil {
		stderr = tail.String()
	}
	return &WorkerError{
		Backend: backend,
		Message: firstNonBlank(stderr, "exited with "+exitErr.String()+" and no message"),
	}
}
