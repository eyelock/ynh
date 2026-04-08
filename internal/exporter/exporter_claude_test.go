package exporter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

func TestExportSingleVendorClaude(t *testing.T) {
	srcDir := filepath.Join(testdataDir(), "export-harness")
	outputDir := t.TempDir()

	results, err := Export(ExportOptions{
		SourceDir: srcDir,
		OutputDir: outputDir,
		Vendors:   []string{"claude"},
		Mode:      ModePerVendor,
	})
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Vendor != "claude" {
		t.Errorf("expected vendor claude, got %s", r.Vendor)
	}

	claudeDir := filepath.Join(outputDir, "claude")

	// Check .claude-plugin/plugin.json exists
	assertFileExists(t, filepath.Join(claudeDir, ".claude-plugin", "plugin.json"))

	// Check skills copied
	assertFileExists(t, filepath.Join(claudeDir, "skills", "dev-project", "SKILL.md"))
	assertFileExists(t, filepath.Join(claudeDir, "skills", "dev-quality", "SKILL.md"))

	// Check agents copied
	assertFileExists(t, filepath.Join(claudeDir, "agents", "planner.md"))

	// Check rules copied
	assertFileExists(t, filepath.Join(claudeDir, "rules", "be-concise.md"))

	// Check commands copied
	assertFileExists(t, filepath.Join(claudeDir, "commands", "check.md"))

	// Check AGENTS.md (from instructions.md)
	assertFileExists(t, filepath.Join(claudeDir, "AGENTS.md"))

	// CLAUDE.md with @AGENTS.md import (Claude doesn't read AGENTS.md natively)
	claudeMDPath := filepath.Join(claudeDir, "CLAUDE.md")
	assertFileExists(t, claudeMDPath)
	claudeMD, _ := os.ReadFile(claudeMDPath)
	if string(claudeMD) != "@AGENTS.md\n" {
		t.Errorf("CLAUDE.md = %q, want %q", string(claudeMD), "@AGENTS.md\n")
	}

	// No .cursorrules in Claude export
	assertFileNotExists(t, filepath.Join(claudeDir, ".cursorrules"))

	if r.Skills != 2 {
		t.Errorf("expected 2 skills, got %d", r.Skills)
	}
}

func TestExportManifestClaude(t *testing.T) {
	srcDir := filepath.Join(testdataDir(), "export-harness")
	outputDir := t.TempDir()

	_, err := Export(ExportOptions{
		SourceDir: srcDir,
		OutputDir: outputDir,
		Vendors:   []string{"claude"},
	})
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	manifestPath := filepath.Join(outputDir, "claude", ".claude-plugin", "plugin.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("reading manifest: %v", err)
	}

	var pj manifestJSON
	if err := json.Unmarshal(data, &pj); err != nil {
		t.Fatalf("parsing manifest: %v", err)
	}

	if pj.Name != "export-test" {
		t.Errorf("expected name export-test, got %s", pj.Name)
	}
	if pj.Version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", pj.Version)
	}
}

// TestExportManifestGeneration verifies manifest JSON format.
func TestExportManifestGeneration(t *testing.T) {
	hj := &plugin.HarnessJSON{
		Name:        "test-plugin",
		Version:     "2.0.0",
		Description: "A test plugin",
	}

	dir := t.TempDir()

	if err := GenerateClaudeManifest(hj, dir); err != nil {
		t.Fatalf("GenerateClaudeManifest: %v", err)
	}
	if err := GenerateCursorManifest(hj, dir); err != nil {
		t.Fatalf("GenerateCursorManifest: %v", err)
	}

	// Verify Claude manifest
	claudeData, err := os.ReadFile(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var claudePJ manifestJSON
	if err := json.Unmarshal(claudeData, &claudePJ); err != nil {
		t.Fatal(err)
	}
	if claudePJ.Name != "test-plugin" || claudePJ.Version != "2.0.0" {
		t.Error("Claude manifest content mismatch")
	}

	// Verify Cursor manifest
	cursorData, err := os.ReadFile(filepath.Join(dir, ".cursor-plugin", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cursorPJ manifestJSON
	if err := json.Unmarshal(cursorData, &cursorPJ); err != nil {
		t.Fatal(err)
	}
	if cursorPJ.Name != "test-plugin" || cursorPJ.Version != "2.0.0" {
		t.Error("Cursor manifest content mismatch")
	}
}
