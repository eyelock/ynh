package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func createDiffHarness(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Create .harness.json with hooks and MCP
	hj := map[string]any{
		"name":           "diff-test",
		"version":        "1.0.0",
		"description":    "Test harness for diff",
		"default_vendor": "claude",
		"hooks": map[string]any{
			"after_tool": []map[string]any{
				{"command": "echo done"},
			},
		},
		"mcp_servers": map[string]any{
			"diff-server": map[string]any{
				"command": "python",
				"args":    []string{"-m", "server"},
			},
		},
	}
	data, _ := json.MarshalIndent(hj, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, ".harness.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a skill
	skillDir := filepath.Join(dir, "skills", "diff-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: diff-skill\n---\nDiff skill.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create instructions
	if err := os.WriteFile(filepath.Join(dir, "instructions.md"), []byte("# Diff Test\nInstructions.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestCmdDiffTwoVendors(t *testing.T) {
	srcDir := createDiffHarness(t)

	err := cmdDiff([]string{srcDir, "claude", "cursor"})
	if err != nil {
		t.Fatalf("cmdDiff failed: %v", err)
	}
}

func TestCmdDiffAllVendors(t *testing.T) {
	srcDir := createDiffHarness(t)

	err := cmdDiff([]string{srcDir})
	if err != nil {
		t.Fatalf("cmdDiff failed: %v", err)
	}
}

func TestCmdDiffClaudeCodex(t *testing.T) {
	srcDir := createDiffHarness(t)

	err := cmdDiff([]string{srcDir, "claude", "codex"})
	if err != nil {
		t.Fatalf("cmdDiff failed: %v", err)
	}
}

func TestCmdDiffVendorFlag(t *testing.T) {
	srcDir := createDiffHarness(t)

	err := cmdDiff([]string{srcDir, "-v", "claude,cursor"})
	if err != nil {
		t.Fatalf("cmdDiff with -v failed: %v", err)
	}
}

func TestCmdDiffHarnessFlag(t *testing.T) {
	srcDir := createDiffHarness(t)

	err := cmdDiff([]string{"--harness", srcDir})
	if err != nil {
		t.Fatalf("cmdDiff with --harness failed: %v", err)
	}
}

func TestCmdDiffHarnessEnvVar(t *testing.T) {
	srcDir := createDiffHarness(t)

	t.Setenv("YNH_HARNESS", srcDir)

	err := cmdDiff(nil)
	if err != nil {
		t.Fatalf("cmdDiff with YNH_HARNESS failed: %v", err)
	}
}

func TestCmdDiffMissingSource(t *testing.T) {
	err := cmdDiff([]string{})
	if err == nil {
		t.Fatal("expected error for missing source")
	}
}

func TestCmdDiffBadSource(t *testing.T) {
	err := cmdDiff([]string{"./nonexistent-dir"})
	if err == nil {
		t.Fatal("expected error for nonexistent source")
	}
}

func TestCmdDiffBadVendor(t *testing.T) {
	srcDir := createDiffHarness(t)
	err := cmdDiff([]string{srcDir, "bogus", "claude"})
	if err == nil {
		t.Fatal("expected error for unknown vendor")
	}
	if !strings.Contains(err.Error(), "unknown vendor") {
		t.Errorf("expected 'unknown vendor' error, got: %v", err)
	}
}

func TestCmdDiffSingleVendor(t *testing.T) {
	srcDir := createDiffHarness(t)
	err := cmdDiff([]string{srcDir, "claude"})
	if err == nil {
		t.Fatal("expected error for single vendor")
	}
	if !strings.Contains(err.Error(), "at least 2 vendors") {
		t.Errorf("expected '2 vendors' error, got: %v", err)
	}
}
