package vendor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildClaudeArgs_WithInstructions(t *testing.T) {
	configPath := t.TempDir()

	claudeDir := filepath.Join(configPath, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	instructions := "You are a helpful persona."
	if err := os.WriteFile(filepath.Join(configPath, "CLAUDE.md"), []byte(instructions), 0o644); err != nil {
		t.Fatal(err)
	}

	args := buildClaudeArgs(configPath, []string{"--model", "opus"})

	expected := []string{
		"claude",
		"--plugin-dir", claudeDir,
		"--append-system-prompt", instructions,
		"--model", "opus",
	}

	if len(args) != len(expected) {
		t.Fatalf("args length = %d, want %d\ngot:  %v\nwant: %v", len(args), len(expected), args, expected)
	}
	for i := range expected {
		if args[i] != expected[i] {
			t.Errorf("args[%d] = %q, want %q", i, args[i], expected[i])
		}
	}
}

func TestBuildClaudeArgs_NoInstructions(t *testing.T) {
	configPath := t.TempDir()

	claudeDir := filepath.Join(configPath, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	args := buildClaudeArgs(configPath, nil)

	expected := []string{
		"claude",
		"--plugin-dir", claudeDir,
	}

	if len(args) != len(expected) {
		t.Fatalf("args length = %d, want %d\ngot:  %v\nwant: %v", len(args), len(expected), args, expected)
	}
	for i := range expected {
		if args[i] != expected[i] {
			t.Errorf("args[%d] = %q, want %q", i, args[i], expected[i])
		}
	}
}

func TestBuildClaudeArgs_EmptyInstructions(t *testing.T) {
	configPath := t.TempDir()

	if err := os.MkdirAll(filepath.Join(configPath, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(configPath, "CLAUDE.md"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	args := buildClaudeArgs(configPath, nil)

	for _, arg := range args {
		if arg == "--append-system-prompt" {
			t.Error("empty instructions should not produce --append-system-prompt")
		}
	}
}

func TestBuildClaudeArgs_ExtraArgsLast(t *testing.T) {
	configPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(configPath, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	extra := []string{"--verbose", "--model", "sonnet"}
	args := buildClaudeArgs(configPath, extra)

	tail := args[len(args)-3:]
	for i, want := range extra {
		if tail[i] != want {
			t.Errorf("tail[%d] = %q, want %q", i, tail[i], want)
		}
	}
}

func TestBuildClaudeArgs_NonInteractive(t *testing.T) {
	configPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(configPath, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configPath, "CLAUDE.md"), []byte("instructions"), 0o644); err != nil {
		t.Fatal(err)
	}

	args := buildClaudeArgs(configPath, []string{"-p", "fix the bug"})

	foundPlugin := false
	foundPrompt := false
	for i, arg := range args {
		if arg == "--plugin-dir" {
			foundPlugin = true
		}
		if arg == "-p" && i+1 < len(args) && args[i+1] == "fix the bug" {
			foundPrompt = true
		}
	}
	if !foundPlugin {
		t.Error("missing --plugin-dir")
	}
	if !foundPrompt {
		t.Error("missing -p prompt")
	}
}
