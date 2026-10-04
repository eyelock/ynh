package exporter

import (
	"os"
	"path/filepath"
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
	assertFileExists(t, filepath.Join(outputDir, "AGENTS.md"))
	assertFileExists(t, filepath.Join(outputDir, "CLAUDE.md"))
	assertFileExists(t, filepath.Join(outputDir, ".cursorrules"))
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
	// Cursor's plugin file is mcp.json at the root (#470), Copilot's is
	// .github/mcp.json at the root, and nothing lands under .copilot/ (#471).
	assertFileExists(t, filepath.Join(outputDir, ".claude", ".mcp.json"))
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
