package agent

import (
	"context"
	"io"
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
}

// Usage tracks token consumption for a turn.
type Usage struct {
	InputTokens  int64
	OutputTokens int64
	CacheTokens  int64
}
