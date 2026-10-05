//go:build e2e

package e2e

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A refused `ynh agent run` exits with the code its --format json result
// reports (#509): 1 for a run refused before any worker started, 21 for a
// refused resume. It used to exit 1 while the result said 20.
func TestAgentRun_RefusalExitCodeMatchesResult(t *testing.T) {
	s := newSandbox(t)
	resumeDir := t.TempDir()
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"effort on cursor", []string{"--task", "t", "--backend", "cursor", "--effort", "high"}, 1},
		{"edits on cursor", []string{"--task", "t", "--backend", "cursor", "--auto-approve", "edits"}, 1},
		{"harness not installed", []string{"--task", "t", "--harness", "local/absent"}, 1},
		{"resume with no checkpoint", []string{"--resume", resumeDir}, 21},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"agent", "run", "--format", "json", "--worktree", t.TempDir()}, tt.args...)
			stdout, stderr, err := s.runYnh(t, args...)
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				t.Fatalf("want a non-zero exit, got %v\nstderr: %s", err, stderr)
			}
			var result struct {
				ExitCode int `json:"exit_code"`
			}
			if jerr := json.Unmarshal([]byte(stdout), &result); jerr != nil {
				t.Fatalf("no run result on stdout: %v\nstdout: %s\nstderr: %s", jerr, stdout, stderr)
			}
			if got := exitErr.ExitCode(); got != result.ExitCode || got != tt.want {
				t.Errorf("process exit %d, result exit_code %d, want %d for both", got, result.ExitCode, tt.want)
			}
			if stderr != "" {
				t.Errorf("--format json wrote to stderr: %q", stderr)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(resumeDir, "checkpoint.json")); err == nil {
		t.Error("a refused resume wrote a checkpoint")
	}
}

// An argument error with --format json exits 1 with the structured error
// envelope on stderr and nothing on stdout, whether --format json comes
// before or after the bad argument (#513).
func TestAgentRun_ArgumentErrorEnvelope(t *testing.T) {
	s := newSandbox(t)
	tests := []struct {
		name string
		args []string
	}{
		{"unknown flag, json first", []string{"--format", "json", "--task", "t", "--bogus"}},
		{"unknown flag, json last", []string{"--task", "t", "--bogus", "--format", "json"}},
		{"task with focus, json last", []string{"--task", "t", "--focus", "f", "--format", "json"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, err := s.runYnh(t, append([]string{"agent", "run"}, tt.args...)...)
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
				t.Fatalf("want exit 1, got %v\nstderr: %s", err, stderr)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}
			var env struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if jerr := json.Unmarshal([]byte(stderr), &env); jerr != nil {
				t.Fatalf("stderr is not one envelope: %v\nstderr: %s", jerr, stderr)
			}
			if env.Error.Code != "invalid_input" || env.Error.Message == "" {
				t.Errorf("envelope = %+v, want invalid_input with a message", env.Error)
			}
		})
	}
}
