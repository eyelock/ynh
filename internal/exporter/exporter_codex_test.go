package exporter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportSingleVendorCodex(t *testing.T) {
	srcDir := filepath.Join(testdataDir(), "export-harness")
	outputDir := t.TempDir()

	results, err := Export(ExportOptions{
		SourceDir: srcDir,
		OutputDir: outputDir,
		Vendors:   []string{"codex"},
		Mode:      ModePerVendor,
	})
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	r := results[0]
	codexDir := filepath.Join(outputDir, "codex")

	// Check skills/ layout (at plugin root, not .agents/skills/)
	assertFileExists(t, filepath.Join(codexDir, "skills", "dev-project", "SKILL.md"))
	assertFileExists(t, filepath.Join(codexDir, "skills", "dev-quality", "SKILL.md"))

	// Check .codex-plugin/plugin.json manifest
	assertFileExists(t, filepath.Join(codexDir, ".codex-plugin", "plugin.json"))

	// Check AGENTS.md present
	assertFileExists(t, filepath.Join(codexDir, "AGENTS.md"))

	// No agents/rules/commands directories at top level
	assertFileNotExists(t, filepath.Join(codexDir, "agents"))
	assertFileNotExists(t, filepath.Join(codexDir, "rules"))
	assertFileNotExists(t, filepath.Join(codexDir, "commands"))

	// No plugin manifest
	assertFileNotExists(t, filepath.Join(codexDir, ".codex"))

	// Should have warnings about skipped artifacts
	if len(r.Warnings) == 0 {
		t.Error("expected warnings about skipped artifacts")
	}
	foundSkipWarning := false
	for _, w := range r.Warnings {
		if strings.Contains(w, "codex: skipping") {
			foundSkipWarning = true
		}
	}
	if !foundSkipWarning {
		t.Error("expected Codex skip warning")
	}
}

func TestExportCodexLayout(t *testing.T) {
	srcDir := filepath.Join(testdataDir(), "export-harness")
	outputDir := t.TempDir()

	_, err := Export(ExportOptions{
		SourceDir: srcDir,
		OutputDir: outputDir,
		Vendors:   []string{"codex"},
	})
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	codexDir := filepath.Join(outputDir, "codex")

	// Skills must be under skills/ (plugin root format)
	entries, err := os.ReadDir(filepath.Join(codexDir, "skills"))
	if err != nil {
		t.Fatalf("reading skills: %v", err)
	}

	skillNames := make(map[string]bool)
	for _, e := range entries {
		skillNames[e.Name()] = true
	}

	if !skillNames["dev-project"] {
		t.Error("missing dev-project skill")
	}
	if !skillNames["dev-quality"] {
		t.Error("missing dev-quality skill")
	}
}

func TestExportCodexSkipsAgentsRulesCommands(t *testing.T) {
	srcDir := filepath.Join(testdataDir(), "export-harness")
	outputDir := t.TempDir()

	results, err := Export(ExportOptions{
		SourceDir: srcDir,
		OutputDir: outputDir,
		Vendors:   []string{"codex"},
	})
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	r := results[0]
	codexDir := filepath.Join(outputDir, "codex")

	// These should NOT exist
	for _, name := range []string{"agents", "rules", "commands"} {
		path := filepath.Join(codexDir, name)
		if _, err := os.Stat(path); err == nil {
			t.Errorf("%s directory should not exist in Codex export", name)
		}
	}

	// Should have warnings
	if len(r.Warnings) == 0 {
		t.Error("expected warnings about skipped artifacts")
	}
}
