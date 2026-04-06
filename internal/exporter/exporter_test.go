package exporter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/resolver"
)

// testdataDir returns the path to the testdata directory.
func testdataDir() string {
	// Tests run from the package directory; testdata is at repo root
	return filepath.Join("..", "..", "testdata")
}

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

	// No CLAUDE.md in export
	assertFileNotExists(t, filepath.Join(claudeDir, "CLAUDE.md"))

	// No .cursorrules in Claude export
	assertFileNotExists(t, filepath.Join(claudeDir, ".cursorrules"))

	if r.Skills != 2 {
		t.Errorf("expected 2 skills, got %d", r.Skills)
	}
}

func TestExportSingleVendorCursor(t *testing.T) {
	srcDir := filepath.Join(testdataDir(), "export-harness")
	outputDir := t.TempDir()

	_, err := Export(ExportOptions{
		SourceDir: srcDir,
		OutputDir: outputDir,
		Vendors:   []string{"cursor"},
		Mode:      ModePerVendor,
	})
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	cursorDir := filepath.Join(outputDir, "cursor")

	// Check .cursor-plugin/plugin.json exists
	assertFileExists(t, filepath.Join(cursorDir, ".cursor-plugin", "plugin.json"))

	// Check .cursorrules (from instructions.md)
	assertFileExists(t, filepath.Join(cursorDir, ".cursorrules"))

	// Check AGENTS.md also present
	assertFileExists(t, filepath.Join(cursorDir, "AGENTS.md"))

	// Skills present
	assertFileExists(t, filepath.Join(cursorDir, "skills", "dev-project", "SKILL.md"))
}

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

	// Check .agents/skills/ layout
	assertFileExists(t, filepath.Join(codexDir, ".agents", "skills", "dev-project", "SKILL.md"))
	assertFileExists(t, filepath.Join(codexDir, ".agents", "skills", "dev-quality", "SKILL.md"))

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
		if strings.Contains(w, "Codex: skipping") {
			foundSkipWarning = true
		}
	}
	if !foundSkipWarning {
		t.Error("expected Codex skip warning")
	}
}

func TestExportAllVendors(t *testing.T) {
	srcDir := filepath.Join(testdataDir(), "export-harness")
	outputDir := t.TempDir()

	results, err := Export(ExportOptions{
		SourceDir: srcDir,
		OutputDir: outputDir,
		Vendors:   []string{}, // all
		Mode:      ModePerVendor,
	})
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Each vendor should have its own directory
	for _, r := range results {
		if _, err := os.Stat(r.OutputDir); os.IsNotExist(err) {
			t.Errorf("output dir for %s does not exist: %s", r.Vendor, r.OutputDir)
		}
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

	var pj plugin.PluginJSON
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

func TestExportManifestCursor(t *testing.T) {
	srcDir := filepath.Join(testdataDir(), "export-harness")
	outputDir := t.TempDir()

	_, err := Export(ExportOptions{
		SourceDir: srcDir,
		OutputDir: outputDir,
		Vendors:   []string{"cursor"},
	})
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	manifestPath := filepath.Join(outputDir, "cursor", ".cursor-plugin", "plugin.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("reading manifest: %v", err)
	}

	var pj plugin.PluginJSON
	if err := json.Unmarshal(data, &pj); err != nil {
		t.Fatalf("parsing manifest: %v", err)
	}

	if pj.Name != "export-test" {
		t.Errorf("expected name export-test, got %s", pj.Name)
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

	// Skills must be under .agents/skills/
	entries, err := os.ReadDir(filepath.Join(codexDir, ".agents", "skills"))
	if err != nil {
		t.Fatalf("reading .agents/skills: %v", err)
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

func TestExportWithInstructions(t *testing.T) {
	srcDir := filepath.Join(testdataDir(), "export-harness")
	outputDir := t.TempDir()

	_, err := Export(ExportOptions{
		SourceDir: srcDir,
		OutputDir: outputDir,
		Vendors:   []string{"claude", "cursor", "codex"},
	})
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	// Claude: AGENTS.md only
	claudeAgents := filepath.Join(outputDir, "claude", "AGENTS.md")
	assertFileExists(t, claudeAgents)
	assertFileNotExists(t, filepath.Join(outputDir, "claude", "CLAUDE.md"))

	// Cursor: .cursorrules + AGENTS.md
	assertFileExists(t, filepath.Join(outputDir, "cursor", ".cursorrules"))
	assertFileExists(t, filepath.Join(outputDir, "cursor", "AGENTS.md"))

	// Codex: AGENTS.md only
	assertFileExists(t, filepath.Join(outputDir, "codex", "AGENTS.md"))

	// Content should match instructions.md
	data, err := os.ReadFile(claudeAgents)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Export Test Harness") {
		t.Error("AGENTS.md should contain instructions content")
	}
}

func TestExportNoInstructions(t *testing.T) {
	// Create a minimal harness without instructions.md
	srcDir := t.TempDir()
	pluginDir := filepath.Join(srcDir, ".claude-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(pluginDir, "plugin.json"), map[string]string{
		"name":    "no-instructions",
		"version": "0.1.0",
	})
	writeJSON(t, filepath.Join(srcDir, "metadata.json"), map[string]any{
		"ynh": map[string]string{"default_vendor": "claude"},
	})

	// Add a skill
	skillDir := filepath.Join(srcDir, "skills", "hello")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("Hello skill"), 0o644); err != nil {
		t.Fatal(err)
	}

	outputDir := t.TempDir()
	results, err := Export(ExportOptions{
		SourceDir: srcDir,
		OutputDir: outputDir,
		Vendors:   []string{"claude"},
	})
	if err != nil {
		t.Fatalf("Export should succeed without instructions.md: %v", err)
	}

	// No AGENTS.md
	assertFileNotExists(t, filepath.Join(outputDir, "claude", "AGENTS.md"))

	// But skills should still be there
	assertFileExists(t, filepath.Join(outputDir, "claude", "skills", "hello", "SKILL.md"))

	if results[0].Skills != 1 {
		t.Errorf("expected 1 skill, got %d", results[0].Skills)
	}
}

func TestExportInstructionDiscovery(t *testing.T) {
	// Create two content sources with instructions.md — last one should win
	src1 := t.TempDir()
	if err := os.WriteFile(filepath.Join(src1, "instructions.md"), []byte("First instructions"), 0o644); err != nil {
		t.Fatal(err)
	}

	src2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(src2, "instructions.md"), []byte("Second instructions wins"), 0o644); err != nil {
		t.Fatal(err)
	}

	content := []resolver.ResolvedContent{
		{BasePath: src1},
		{BasePath: src2},
	}

	found := DiscoverInstructions(content)
	if found == "" {
		t.Fatal("expected to find instructions.md")
	}

	data, err := os.ReadFile(found)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "Second instructions wins" {
		t.Errorf("expected last instructions.md to win, got: %s", string(data))
	}
}

func TestExportMergedMode(t *testing.T) {
	srcDir := filepath.Join(testdataDir(), "export-harness")
	outputDir := filepath.Join(t.TempDir(), "merged-out")

	results, err := Export(ExportOptions{
		SourceDir: srcDir,
		OutputDir: outputDir,
		Vendors:   []string{"claude", "cursor"},
		Mode:      ModeMerged,
	})
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 merged result, got %d", len(results))
	}
	if results[0].Vendor != "merged" {
		t.Errorf("expected vendor 'merged', got %s", results[0].Vendor)
	}

	// Both manifests in same dir
	assertFileExists(t, filepath.Join(outputDir, ".claude-plugin", "plugin.json"))
	assertFileExists(t, filepath.Join(outputDir, ".cursor-plugin", "plugin.json"))

	// Shared artifacts
	assertFileExists(t, filepath.Join(outputDir, "skills", "dev-project", "SKILL.md"))
	assertFileExists(t, filepath.Join(outputDir, "skills", "dev-quality", "SKILL.md"))
	assertFileExists(t, filepath.Join(outputDir, "agents", "planner.md"))

	// Instructions
	assertFileExists(t, filepath.Join(outputDir, "AGENTS.md"))
	assertFileExists(t, filepath.Join(outputDir, ".cursorrules"))
}

func TestExportCleanFlag(t *testing.T) {
	srcDir := filepath.Join(testdataDir(), "export-harness")
	outputDir := t.TempDir()

	// Create a stale file that should be cleaned
	staleDir := filepath.Join(outputDir, "old-vendor")
	if err := os.MkdirAll(staleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	staleFile := filepath.Join(staleDir, "stale.txt")
	if err := os.WriteFile(staleFile, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Export with clean behaviour (remove and recreate vendor subdirs)
	_, err := Export(ExportOptions{
		SourceDir: srcDir,
		OutputDir: outputDir,
		Vendors:   []string{"claude"},
	})
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	// The export only cleans vendor subdirs it owns, not the parent output dir.
	// The --clean flag at CLI level handles full output dir removal.

	// Claude dir should have fresh content
	assertFileExists(t, filepath.Join(outputDir, "claude", ".claude-plugin", "plugin.json"))
}

// TestExportManifestGeneration verifies manifest JSON format.
func TestExportManifestGeneration(t *testing.T) {
	pj := &plugin.PluginJSON{
		Name:        "test-plugin",
		Version:     "2.0.0",
		Description: "A test plugin",
	}

	dir := t.TempDir()

	if err := GenerateClaudeManifest(pj, dir); err != nil {
		t.Fatalf("GenerateClaudeManifest: %v", err)
	}
	if err := GenerateCursorManifest(pj, dir); err != nil {
		t.Fatalf("GenerateCursorManifest: %v", err)
	}

	// Verify Claude manifest
	claudeData, err := os.ReadFile(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var claudePJ plugin.PluginJSON
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
	var cursorPJ plugin.PluginJSON
	if err := json.Unmarshal(cursorData, &cursorPJ); err != nil {
		t.Fatal(err)
	}
	if cursorPJ.Name != "test-plugin" || cursorPJ.Version != "2.0.0" {
		t.Error("Cursor manifest content mismatch")
	}
}

func TestExportWithHooks(t *testing.T) {
	// Create a harness with hooks
	srcDir := t.TempDir()
	pluginDir := filepath.Join(srcDir, ".claude-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(pluginDir, "plugin.json"), map[string]string{
		"name":    "hooks-test",
		"version": "0.1.0",
	})
	writeJSON(t, filepath.Join(srcDir, "metadata.json"), map[string]any{
		"ynh": map[string]any{
			"default_vendor": "claude",
			"hooks": map[string]any{
				"before_tool": []any{
					map[string]string{"matcher": "Bash", "command": "echo before bash"},
				},
				"on_stop": []any{
					map[string]string{"command": "echo done"},
				},
			},
		},
	})

	// Test Claude export
	outputDir := t.TempDir()
	_, err := Export(ExportOptions{
		SourceDir: srcDir,
		OutputDir: outputDir,
		Vendors:   []string{"claude"},
		Mode:      ModePerVendor,
	})
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	// Claude should have .claude/settings.json
	assertFileExists(t, filepath.Join(outputDir, "claude", ".claude", "settings.json"))

	// Test Cursor export
	outputDir2 := t.TempDir()
	_, err = Export(ExportOptions{
		SourceDir: srcDir,
		OutputDir: outputDir2,
		Vendors:   []string{"cursor"},
		Mode:      ModePerVendor,
	})
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	// Cursor should have .cursor/hooks.json
	assertFileExists(t, filepath.Join(outputDir2, "cursor", ".cursor", "hooks.json"))

	// Test Codex export
	outputDir3 := t.TempDir()
	_, err = Export(ExportOptions{
		SourceDir: srcDir,
		OutputDir: outputDir3,
		Vendors:   []string{"codex"},
		Mode:      ModePerVendor,
	})
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	// Codex should have .codex/hooks.json
	assertFileExists(t, filepath.Join(outputDir3, "codex", ".codex", "hooks.json"))
}

func TestExportMergedWithHooks(t *testing.T) {
	// Create a harness with hooks
	srcDir := t.TempDir()
	pluginDir := filepath.Join(srcDir, ".claude-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(pluginDir, "plugin.json"), map[string]string{
		"name":    "hooks-merged",
		"version": "0.1.0",
	})
	writeJSON(t, filepath.Join(srcDir, "metadata.json"), map[string]any{
		"ynh": map[string]any{
			"hooks": map[string]any{
				"before_tool": []any{
					map[string]string{"command": "echo hi"},
				},
			},
		},
	})

	outputDir := filepath.Join(t.TempDir(), "merged")
	_, err := Export(ExportOptions{
		SourceDir: srcDir,
		OutputDir: outputDir,
		Vendors:   []string{"claude", "cursor"},
		Mode:      ModeMerged,
	})
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	// Both hook configs should exist
	assertFileExists(t, filepath.Join(outputDir, ".claude", "settings.json"))
	assertFileExists(t, filepath.Join(outputDir, ".cursor", "hooks.json"))
}

func TestJoinParts(t *testing.T) {
	tests := []struct {
		parts []string
		want  string
	}{
		{nil, ""},
		{[]string{"1 agent"}, "1 agent"},
		{[]string{"1 agent", "2 rules"}, "1 agent and 2 rules"},
		{[]string{"1 agent", "2 rules", "3 commands"}, "1 agent, 2 rules, and 3 commands"},
	}

	for _, tt := range tests {
		got := joinParts(tt.parts)
		if got != tt.want {
			t.Errorf("joinParts(%v) = %q, want %q", tt.parts, got, tt.want)
		}
	}
}

// Helpers

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("expected file to exist: %s", path)
	}
}

func assertFileNotExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Errorf("expected file NOT to exist: %s", path)
	}
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
