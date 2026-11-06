package exporter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

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
	// Claude Code and Cursor plugins cannot carry instructions
	assertFileNotExists(t, filepath.Join(outputDir, "AGENTS.md"))
	assertFileNotExists(t, filepath.Join(outputDir, "CLAUDE.md"))
	assertFileNotExists(t, filepath.Join(outputDir, ".cursorrules"))
}

func TestExportMergedWithMCPServers(t *testing.T) {
	// Create a harness with MCP servers
	srcDir := t.TempDir()
	writeJSON(t, filepath.Join(srcDir, plugin.PluginDir, plugin.PluginFile), map[string]any{
		"name":    "mcp-merged",
		"version": "0.1.0",
		"mcp_servers": map[string]any{
			"github": map[string]any{
				"command": "npx",
				"args":    []string{"-y", "server"},
			},
		},
	})

	outputDir := filepath.Join(t.TempDir(), "merged")
	_, err := Export(ExportOptions{
		SourceDir: srcDir,
		OutputDir: outputDir,
		Vendors:   []string{"claude", "copilot", "cursor"},
		Mode:      ModeMerged,
	})
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	// Each vendor's plugin MCP file exists, and no project-session file does:
	// Claude's is mcp/claude.json, not .claude/.mcp.json (#481),
	// Cursor's plugin file is mcp.json at the root (#470), Copilot's is
	// .github/mcp.json at the root, and nothing lands under .copilot/ (#471).
	assertFileExists(t, filepath.Join(outputDir, "mcp", "claude.json"))
	assertFileNotExists(t, filepath.Join(outputDir, ".claude"))
	assertFileExists(t, filepath.Join(outputDir, "mcp.json"))
	assertFileNotExists(t, filepath.Join(outputDir, ".cursor", "mcp.json"))
	assertFileExists(t, filepath.Join(outputDir, ".github", "mcp.json"))
	assertFileNotExists(t, filepath.Join(outputDir, ".copilot"))
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

func mergedInstructionsWarning(r ExportResult) bool {
	for _, w := range r.Warnings {
		if strings.Contains(w, "codex: plugins cannot carry instructions") {
			return true
		}
	}
	return false
}

// A merged export keeps the shared AGENTS.md for the vendors that read it and
// says that Codex does not (ynw-uubn).
func TestExportMergedCodexInstructions(t *testing.T) {
	srcDir := filepath.Join(testdataDir(), "export-harness")

	t.Run("with opencode", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "merged")
		results, err := Export(ExportOptions{SourceDir: srcDir, OutputDir: out, Vendors: []string{"opencode", "codex"}, Mode: ModeMerged})
		if err != nil {
			t.Fatal(err)
		}
		assertFileExists(t, filepath.Join(out, "AGENTS.md"))
		if !mergedInstructionsWarning(results[0]) {
			t.Errorf("no instructions warning: %v", results[0].Warnings)
		}
	})

	t.Run("codex alone", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "merged")
		results, err := Export(ExportOptions{SourceDir: srcDir, OutputDir: out, Vendors: []string{"codex"}, Mode: ModeMerged})
		if err != nil {
			t.Fatal(err)
		}
		assertFileNotExists(t, filepath.Join(out, "AGENTS.md"))
		if !mergedInstructionsWarning(results[0]) {
			t.Errorf("no instructions warning: %v", results[0].Warnings)
		}
	})

	t.Run("without codex", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "merged")
		results, err := Export(ExportOptions{SourceDir: srcDir, OutputDir: out, Vendors: []string{"claude", "opencode"}, Mode: ModeMerged})
		if err != nil {
			t.Fatal(err)
		}
		assertFileExists(t, filepath.Join(out, "AGENTS.md"))
		if mergedInstructionsWarning(results[0]) {
			t.Errorf("unexpected warning: %v", results[0].Warnings)
		}
	})
}
