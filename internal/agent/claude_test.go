package agent

import (
	"testing"
)

func TestClaudeBackend_Name(t *testing.T) {
	b := &ClaudeBackend{}
	if b.Name() != "claude" {
		t.Errorf("expected 'claude', got %q", b.Name())
	}
}

func TestBuildClaudeStreamArgs_Minimal(t *testing.T) {
	args := buildClaudeStreamArgs(StartOptions{})
	required := []string{"--input-format", "stream-json", "--output-format", "stream-json", "--print"}
	for _, r := range required {
		found := false
		for _, a := range args {
			if a == r {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("required arg %q not found in %v", r, args)
		}
	}
}

func TestBuildClaudeStreamArgs_Model(t *testing.T) {
	args := buildClaudeStreamArgs(StartOptions{Model: "claude-opus-4-7"})
	found := false
	for i, a := range args {
		if a == "--model" && i+1 < len(args) && args[i+1] == "claude-opus-4-7" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("--model flag not found in %v", args)
	}
}

func TestBuildClaudeStreamArgs_NoModelWhenEmpty(t *testing.T) {
	args := buildClaudeStreamArgs(StartOptions{})
	for _, a := range args {
		if a == "--model" {
			t.Error("--model should not appear when Model is empty")
		}
	}
}

func TestBuildClaudeStreamArgs_ConfigPath(t *testing.T) {
	// ConfigPath with no readable CLAUDE.md — should still add --plugin-dir and --add-dir.
	args := buildClaudeStreamArgs(StartOptions{ConfigPath: "/nonexistent/path"})
	hasPluginDir := false
	hasAddDir := false
	for i, a := range args {
		if a == "--plugin-dir" && i+1 < len(args) {
			hasPluginDir = true
		}
		if a == "--add-dir" && i+1 < len(args) {
			hasAddDir = true
		}
	}
	if !hasPluginDir {
		t.Error("expected --plugin-dir with ConfigPath set")
	}
	if !hasAddDir {
		t.Error("expected --add-dir with ConfigPath set")
	}
}
