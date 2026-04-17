package vendor

import (
	"errors"
	"testing"
)

func TestClaudeAdapter(t *testing.T) {
	adapter, err := Get("claude")
	if err != nil {
		t.Fatalf("Get(claude) failed: %v", err)
	}

	if adapter.Name() != "claude" {
		t.Errorf("Name = %q, want %q", adapter.Name(), "claude")
	}
	if adapter.DisplayName() != "Claude Code" {
		t.Errorf("DisplayName = %q, want %q", adapter.DisplayName(), "Claude Code")
	}
	if adapter.CLIName() != "claude" {
		t.Errorf("CLIName = %q, want %q", adapter.CLIName(), "claude")
	}
	if adapter.ConfigDir() != ".claude" {
		t.Errorf("ConfigDir = %q, want %q", adapter.ConfigDir(), ".claude")
	}

	dirs := adapter.ArtifactDirs()
	assertArtifactDirs(t, dirs)

	if adapter.InstructionsFile() != "CLAUDE.md" {
		t.Errorf("InstructionsFile = %q, want %q", adapter.InstructionsFile(), "CLAUDE.md")
	}
}

func TestCodexAdapter(t *testing.T) {
	adapter, err := Get("codex")
	if err != nil {
		t.Fatalf("Get(codex) failed: %v", err)
	}

	if adapter.Name() != "codex" {
		t.Errorf("Name = %q, want %q", adapter.Name(), "codex")
	}
	if adapter.DisplayName() != "OpenAI Codex" {
		t.Errorf("DisplayName = %q, want %q", adapter.DisplayName(), "OpenAI Codex")
	}
	if adapter.CLIName() != "codex" {
		t.Errorf("CLIName = %q, want %q", adapter.CLIName(), "codex")
	}
	if adapter.ConfigDir() != ".codex" {
		t.Errorf("ConfigDir = %q, want %q", adapter.ConfigDir(), ".codex")
	}

	dirs := adapter.ArtifactDirs()
	assertArtifactDirs(t, dirs)

	if adapter.InstructionsFile() != "codex.md" {
		t.Errorf("InstructionsFile = %q, want %q", adapter.InstructionsFile(), "codex.md")
	}
}

func TestCursorAdapter(t *testing.T) {
	adapter, err := Get("cursor")
	if err != nil {
		t.Fatalf("Get(cursor) failed: %v", err)
	}

	if adapter.Name() != "cursor" {
		t.Errorf("Name = %q, want %q", adapter.Name(), "cursor")
	}
	if adapter.DisplayName() != "Cursor" {
		t.Errorf("DisplayName = %q, want %q", adapter.DisplayName(), "Cursor")
	}
	if adapter.CLIName() != "agent" {
		t.Errorf("CLIName = %q, want %q", adapter.CLIName(), "agent")
	}
	if adapter.ConfigDir() != ".cursor" {
		t.Errorf("ConfigDir = %q, want %q", adapter.ConfigDir(), ".cursor")
	}

	dirs := adapter.ArtifactDirs()
	assertArtifactDirs(t, dirs)

	if adapter.InstructionsFile() != ".cursorrules" {
		t.Errorf("InstructionsFile = %q, want %q", adapter.InstructionsFile(), ".cursorrules")
	}
}

func TestGetUnknownVendor(t *testing.T) {
	_, err := Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown vendor")
	}
	if !errors.Is(err, ErrUnknownVendor) {
		t.Errorf("expected ErrUnknownVendor, got: %v", err)
	}
}

func TestAvailable(t *testing.T) {
	names := Available()

	expected := map[string]bool{"claude": false, "codex": false, "cursor": false}
	for _, n := range names {
		if _, ok := expected[n]; ok {
			expected[n] = true
		}
	}

	for name, found := range expected {
		if !found {
			t.Errorf("Available() missing %q", name)
		}
	}
}

func TestRegisterDuplicate(t *testing.T) {
	// Re-registering should overwrite without panic
	Register(&Claude{})
	adapter, err := Get("claude")
	if err != nil {
		t.Fatalf("Get after re-register failed: %v", err)
	}
	if adapter.Name() != "claude" {
		t.Errorf("Name = %q after re-register", adapter.Name())
	}
}

func assertArtifactDirs(t *testing.T, dirs map[string]string) {
	t.Helper()
	expected := []string{"skills", "agents", "rules", "commands"}
	for _, key := range expected {
		if _, ok := dirs[key]; !ok {
			t.Errorf("ArtifactDirs missing key %q", key)
		}
	}
}
