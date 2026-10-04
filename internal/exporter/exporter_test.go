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

// manifestJSON is used to unmarshal the vendor-specific plugin.json output.
type manifestJSON struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description,omitempty"`
	Keywords    []string `json:"keywords,omitempty"`
}

// testdataDir returns the path to the testdata directory.
func testdataDir() string {
	// Tests run from the package directory; testdata is at repo root
	return filepath.Join("..", "..", "testdata")
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

	// Rules exported as .mdc with frontmatter, not plain .md
	if _, err := os.Stat(filepath.Join(cursorDir, "rules", "be-concise.md")); !os.IsNotExist(err) {
		t.Error("expected plain .md rule to be absent from Cursor export")
	}
	data, err := os.ReadFile(filepath.Join(cursorDir, "rules", "be-concise.mdc"))
	if err != nil {
		t.Fatalf("expected be-concise.mdc: %v", err)
	}
	if !strings.HasPrefix(string(data), "---\ndescription:") {
		t.Errorf("expected .mdc frontmatter, got %q", string(data))
	}
	if !strings.Contains(string(data), "alwaysApply: true") {
		t.Errorf("expected alwaysApply: true in frontmatter, got %q", string(data))
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

	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}

	// Each vendor should have its own directory
	for _, r := range results {
		if _, err := os.Stat(r.OutputDir); os.IsNotExist(err) {
			t.Errorf("output dir for %s does not exist: %s", r.Vendor, r.OutputDir)
		}
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

	var pj manifestJSON
	if err := json.Unmarshal(data, &pj); err != nil {
		t.Fatalf("parsing manifest: %v", err)
	}

	if pj.Name != "export-test" {
		t.Errorf("expected name export-test, got %s", pj.Name)
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

	// Claude: AGENTS.md + CLAUDE.md (with @-import)
	claudeAgents := filepath.Join(outputDir, "claude", "AGENTS.md")
	assertFileExists(t, claudeAgents)
	assertFileExists(t, filepath.Join(outputDir, "claude", "CLAUDE.md"))

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
	writeJSON(t, filepath.Join(srcDir, plugin.PluginDir, plugin.PluginFile), map[string]any{
		"name":           "no-instructions",
		"version":        "0.1.0",
		"default_vendor": "claude",
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

func TestExportWithHooks(t *testing.T) {
	// Create a harness with hooks
	srcDir := t.TempDir()
	writeJSON(t, filepath.Join(srcDir, plugin.PluginDir, plugin.PluginFile), map[string]any{
		"name":           "hooks-test",
		"version":        "0.1.0",
		"default_vendor": "claude",
		"hooks": map[string]any{
			"before_tool": []any{
				map[string]string{"matcher": "Bash", "command": "echo before bash"},
			},
			"on_stop": []any{
				map[string]string{"command": "echo done"},
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

	// Claude should have .claude/hooks/hooks.json (plugin format)
	assertFileExists(t, filepath.Join(outputDir, "claude", ".claude", "hooks", "hooks.json"))

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

	// A Cursor plugin reads hooks/hooks.json at its root and nothing else
	// (cursor.com/docs/reference/plugins). .cursor/hooks.json is the project
	// file a `ynh run` session reads; inside a plugin it is inert (#454).
	assertFileExists(t, filepath.Join(outputDir2, "cursor", "hooks", "hooks.json"))
	assertFileNotExists(t, filepath.Join(outputDir2, "cursor", ".cursor", "hooks.json"))

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

func TestExportWithMCPServers(t *testing.T) {
	// Create a harness with MCP servers
	srcDir := t.TempDir()
	writeJSON(t, filepath.Join(srcDir, plugin.PluginDir, plugin.PluginFile), map[string]any{
		"name":           "mcp-test",
		"version":        "0.1.0",
		"default_vendor": "claude",
		"mcp_servers": map[string]any{
			"github": map[string]any{
				"command": "npx",
				"args":    []string{"-y", "@modelcontextprotocol/server-github"},
				"env":     map[string]string{"GITHUB_TOKEN": "${GITHUB_TOKEN}"},
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

	// Claude should have .claude/.mcp.json (plugin format)
	assertFileExists(t, filepath.Join(outputDir, "claude", ".claude", ".mcp.json"))

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

	// A Cursor plugin reads mcp.json at its root and nothing else
	// (cursor.com/docs/reference/plugins). .cursor/mcp.json is the project
	// file a `ynh run` session reads; inside a plugin it is inert (#470).
	assertFileExists(t, filepath.Join(outputDir2, "cursor", "mcp.json"))
	assertFileNotExists(t, filepath.Join(outputDir2, "cursor", ".cursor", "mcp.json"))

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

	// Codex should have .mcp.json (JSON format at plugin root)
	assertFileExists(t, filepath.Join(outputDir3, "codex", ".mcp.json"))
}

// TestExportCopilotWithMCPUsesExportLayout locks #471. A copilot export is a
// plugin whose skills sit at its root, so the manifest must sit there too:
// Copilot loads nothing from a plugin whose .claude-plugin/plugin.json is not
// at the directory it is pointed at. The export used to infer a `ynh run`
// layout from the MCP file it had just written under .copilot/, and nested the
// manifest there. The MCP file goes where a Copilot plugin reads it,
// .github/mcp.json at the plugin root (docs.github.com, CLI plugin reference).
func TestExportCopilotWithMCPUsesExportLayout(t *testing.T) {
	srcDir := t.TempDir()
	writeJSON(t, filepath.Join(srcDir, plugin.PluginDir, plugin.PluginFile), map[string]any{
		"name":    "copilot-mcp",
		"version": "0.1.0",
		"mcp_servers": map[string]any{
			"github": map[string]any{"command": "npx", "args": []string{"-y", "server"}},
		},
	})
	skill := filepath.Join(srcDir, "skills", "greet", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skill), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skill, []byte("---\nname: greet\ndescription: Greets.\n---\n\nSay hello.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	outputDir := t.TempDir()
	if _, err := Export(ExportOptions{
		SourceDir: srcDir,
		OutputDir: outputDir,
		Vendors:   []string{"copilot"},
		Mode:      ModePerVendor,
	}); err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	root := filepath.Join(outputDir, "copilot")
	assertFileExists(t, filepath.Join(root, "skills", "greet", "SKILL.md"))
	assertFileExists(t, filepath.Join(root, ".claude-plugin", "plugin.json"))
	assertFileNotExists(t, filepath.Join(root, ".copilot"))

	data, err := os.ReadFile(filepath.Join(root, ".github", "mcp.json"))
	if err != nil {
		t.Fatalf("copilot plugin MCP file: %v", err)
	}
	if !strings.Contains(string(data), `"type": "local"`) {
		t.Errorf("copilot MCP file should be in Copilot's format:\n%s", data)
	}
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
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// BeforeWrite runs once the source has loaded and before anything is written,
// so a refused source never reaches it and a failing hook writes nothing
// (#451).
func TestExportBeforeWrite(t *testing.T) {
	good := filepath.Join(testdataDir(), "export-harness")
	refused := t.TempDir() // no manifest and no AGENTS.md: LoadDir refuses it

	tests := []struct {
		name       string
		src        string
		hookErr    error
		wantCalled bool
	}{
		{"refused source never calls the hook", refused, nil, false},
		{"failing hook stops the export", good, os.ErrPermission, true},
		{"passing hook lets the export run", good, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out")
			called := false
			_, err := Export(ExportOptions{
				SourceDir: tt.src,
				OutputDir: out,
				Vendors:   []string{"claude"},
				BeforeWrite: func() error {
					if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
						t.Errorf("output existed before BeforeWrite ran (stat err: %v)", statErr)
					}
					called = true
					return tt.hookErr
				},
			})
			if called != tt.wantCalled {
				t.Errorf("BeforeWrite called = %v, want %v", called, tt.wantCalled)
			}
			wantOK := tt.src == good && tt.hookErr == nil
			if (err == nil) != wantOK {
				t.Fatalf("err = %v, want success %v", err, wantOK)
			}
			_, statErr := os.Stat(out)
			if wantOK == os.IsNotExist(statErr) {
				t.Errorf("output exists = %v, want %v", !os.IsNotExist(statErr), wantOK)
			}
		})
	}
}
