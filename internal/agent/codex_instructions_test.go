package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func codexInstructionsArg(args []string) (string, bool) {
	for i, a := range args {
		if i > 0 && args[i-1] == "-c" && strings.HasPrefix(a, "developer_instructions=") {
			return strings.TrimPrefix(a, "developer_instructions="), true
		}
	}
	return "", false
}

// The harness's instructions reach a codex worker as developer_instructions,
// on every turn, and nothing is written to the worktree (ynh #987).
func TestBuildCodexArgs_PassesHarnessInstructions(t *testing.T) {
	runDir := t.TempDir()
	worktree := t.TempDir()
	text := "CODEWORD-1234\n" + strings.Repeat("x", 64*1024)
	if err := os.WriteFile(filepath.Join(runDir, "AGENTS.md"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, thread := range []string{"", "thread-1"} {
		args := buildCodexArgs(StartOptions{ConfigPath: runDir, WorktreeDir: worktree}, thread)
		got, ok := codexInstructionsArg(args)
		if !ok || got != text {
			t.Errorf("thread %q: developer_instructions missing or altered (found %v, %d bytes)", thread, ok, len(got))
		}
	}
	entries, _ := os.ReadDir(worktree)
	if len(entries) != 0 {
		t.Errorf("worktree was written to: %v", entries)
	}
}

func TestBuildCodexArgs_NoInstructionsNoOverride(t *testing.T) {
	runDir := t.TempDir()
	if _, ok := codexInstructionsArg(buildCodexArgs(StartOptions{ConfigPath: runDir}, "")); ok {
		t.Error("developer_instructions passed with no AGENTS.md")
	}
	if err := os.WriteFile(filepath.Join(runDir, "AGENTS.md"), []byte("  \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := codexInstructionsArg(buildCodexArgs(StartOptions{ConfigPath: runDir}, "")); ok {
		t.Error("developer_instructions passed for a blank AGENTS.md")
	}
}
