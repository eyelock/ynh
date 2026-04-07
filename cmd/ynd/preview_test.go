package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func createPreviewHarness(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Create harness.json with hooks and MCP servers
	hj := map[string]any{
		"name":           "preview-test",
		"version":        "1.0.0",
		"description":    "Test harness for preview",
		"default_vendor": "claude",
		"hooks": map[string]any{
			"before_tool": []map[string]any{
				{"command": "echo before"},
			},
		},
		"mcp_servers": map[string]any{
			"test-server": map[string]any{
				"command": "node",
				"args":    []string{"server.js"},
			},
		},
	}
	data, _ := json.MarshalIndent(hj, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "harness.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a skill
	skillDir := filepath.Join(dir, "skills", "test-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: test-skill\n---\nTest skill content.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create instructions
	if err := os.WriteFile(filepath.Join(dir, "instructions.md"), []byte("# Preview Test\nInstructions here.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestCmdPreviewDefault(t *testing.T) {
	srcDir := createPreviewHarness(t)

	// Preview to stdout (capture by just running without error)
	err := cmdPreview([]string{srcDir})
	if err != nil {
		t.Fatalf("cmdPreview failed: %v", err)
	}
}

func TestCmdPreviewClaude(t *testing.T) {
	srcDir := createPreviewHarness(t)

	err := cmdPreview([]string{srcDir, "-v", "claude"})
	if err != nil {
		t.Fatalf("cmdPreview failed: %v", err)
	}
}

func TestCmdPreviewCursor(t *testing.T) {
	srcDir := createPreviewHarness(t)

	err := cmdPreview([]string{srcDir, "-v", "cursor"})
	if err != nil {
		t.Fatalf("cmdPreview failed: %v", err)
	}
}

func TestCmdPreviewCodex(t *testing.T) {
	srcDir := createPreviewHarness(t)

	err := cmdPreview([]string{srcDir, "-v", "codex"})
	if err != nil {
		t.Fatalf("cmdPreview failed: %v", err)
	}
}

func TestCmdPreviewWithOutput(t *testing.T) {
	srcDir := createPreviewHarness(t)
	outputDir := filepath.Join(t.TempDir(), "preview-out")

	err := cmdPreview([]string{srcDir, "-v", "claude", "-o", outputDir})
	if err != nil {
		t.Fatalf("cmdPreview failed: %v", err)
	}

	// Verify output contains assembled files
	assertExists(t, filepath.Join(outputDir, "CLAUDE.md"))

	// Should have skill
	entries, err := os.ReadDir(filepath.Join(outputDir, ".claude", "skills"))
	if err != nil {
		t.Fatalf("reading skills dir: %v", err)
	}
	if len(entries) == 0 {
		t.Error("expected skills in output")
	}
}

func TestCmdPreviewWithHooks(t *testing.T) {
	srcDir := createPreviewHarness(t)
	outputDir := filepath.Join(t.TempDir(), "preview-hooks")

	err := cmdPreview([]string{srcDir, "-v", "claude", "-o", outputDir})
	if err != nil {
		t.Fatalf("cmdPreview failed: %v", err)
	}

	// Claude hooks go in .claude/settings.json
	settingsPath := filepath.Join(outputDir, ".claude", "settings.json")
	assertExists(t, settingsPath)

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "hooks") {
		t.Error("expected settings.json to contain hooks config")
	}
}

func TestCmdPreviewWithMCP(t *testing.T) {
	srcDir := createPreviewHarness(t)
	outputDir := filepath.Join(t.TempDir(), "preview-mcp")

	err := cmdPreview([]string{srcDir, "-v", "claude", "-o", outputDir})
	if err != nil {
		t.Fatalf("cmdPreview failed: %v", err)
	}

	// Claude MCP goes in .mcp.json
	mcpPath := filepath.Join(outputDir, ".mcp.json")
	assertExists(t, mcpPath)

	data, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "test-server") {
		t.Error("expected .mcp.json to contain test-server")
	}
}

func TestCmdPreviewBareAGENTS(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"),
		[]byte("# My Project\n\nDo stuff.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	outputDir := filepath.Join(t.TempDir(), "preview-bare")
	err := cmdPreview([]string{dir, "-v", "claude", "-o", outputDir})
	if err != nil {
		t.Fatalf("cmdPreview failed: %v", err)
	}

	// Should have CLAUDE.md with the AGENTS.md content
	assertExists(t, filepath.Join(outputDir, "CLAUDE.md"))

	// Source directory should NOT have been mutated
	if _, err := os.Stat(filepath.Join(dir, ".claude-plugin")); !os.IsNotExist(err) {
		t.Error("source directory should not have .claude-plugin after preview")
	}
}

func TestCmdPreviewMissingSource(t *testing.T) {
	err := cmdPreview([]string{})
	if err == nil {
		t.Fatal("expected error for missing source")
	}
}

func TestCmdPreviewBadSource(t *testing.T) {
	err := cmdPreview([]string{"./nonexistent-dir"})
	if err == nil {
		t.Fatal("expected error for nonexistent source")
	}
}

func TestCmdPreviewBadVendor(t *testing.T) {
	srcDir := createPreviewHarness(t)
	err := cmdPreview([]string{srcDir, "-v", "bogus"})
	if err == nil {
		t.Fatal("expected error for unknown vendor")
	}
	if !strings.Contains(err.Error(), "unknown vendor") {
		t.Errorf("expected 'unknown vendor' error, got: %v", err)
	}
}

func TestCmdPreviewNoHarnessOrInstructions(t *testing.T) {
	dir := t.TempDir()
	err := cmdPreview([]string{dir})
	if err == nil {
		t.Fatal("expected error for dir with no harness or AGENTS.md")
	}
}

func TestCmdPreviewSkillsOnly(t *testing.T) {
	// Harness with skills but no hooks or MCP
	dir := t.TempDir()
	hj := map[string]any{"name": "skills-only", "version": "1.0.0"}
	data, _ := json.MarshalIndent(hj, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "harness.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	skillDir := filepath.Join(dir, "skills", "simple")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: simple\n---\nSimple skill.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	outputDir := filepath.Join(t.TempDir(), "out")
	err := cmdPreview([]string{dir, "-v", "claude", "-o", outputDir})
	if err != nil {
		t.Fatalf("cmdPreview failed: %v", err)
	}

	assertExists(t, filepath.Join(outputDir, ".claude", "skills", "simple", "SKILL.md"))
}
