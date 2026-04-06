package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupMarketplaceTest creates a marketplace config and sources in a temp dir.
func setupMarketplaceTest(t *testing.T) (configFile string) {
	t.Helper()
	dir := t.TempDir()

	// Create harness source (symlink to export-harness testdata)
	harnessDir := filepath.Join(dir, "harnesses", "david")
	if err := os.MkdirAll(filepath.Dir(harnessDir), 0o755); err != nil {
		t.Fatal(err)
	}
	srcHarness, _ := filepath.Abs(filepath.Join("..", "..", "testdata", "export-harness"))
	if err := os.Symlink(srcHarness, harnessDir); err != nil {
		t.Fatal(err)
	}

	// Create plugin source
	pluginDir := filepath.Join(dir, "plugins", "my-tool")
	claudePlugin := filepath.Join(pluginDir, ".claude-plugin")
	if err := os.MkdirAll(claudePlugin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestJSON(t, filepath.Join(claudePlugin, "plugin.json"), map[string]any{
		"name":        "my-tool",
		"version":     "0.2.0",
		"description": "A standalone tool",
	})

	// Write marketplace.json
	configFile = filepath.Join(dir, "marketplace.json")
	writeTestJSON(t, configFile, map[string]any{
		"name":        "cli-test-marketplace",
		"owner":       map[string]string{"name": "tester"},
		"description": "CLI test",
		"entries": []map[string]string{
			{"type": "harness", "source": "./harnesses/david"},
			{"type": "plugin", "source": "./plugins/my-tool"},
		},
	})

	return configFile
}

func TestCmdMarketplaceBuild(t *testing.T) {
	configFile := setupMarketplaceTest(t)
	outputDir := t.TempDir()

	err := cmdMarketplace([]string{"build", configFile, "-o", outputDir})
	if err != nil {
		t.Fatalf("cmdMarketplace build: %v", err)
	}

	// Verify marketplace index
	assertExists(t, filepath.Join(outputDir, ".claude-plugin", "marketplace.json"))
	assertExists(t, filepath.Join(outputDir, ".cursor-plugin", "marketplace.json"))

	// Verify plugins
	assertExists(t, filepath.Join(outputDir, "plugins", "export-test"))
	assertExists(t, filepath.Join(outputDir, "plugins", "my-tool"))

	// Verify README
	assertExists(t, filepath.Join(outputDir, "README.md"))
}

func TestCmdMarketplaceBuildClean(t *testing.T) {
	configFile := setupMarketplaceTest(t)
	outputDir := t.TempDir()

	// Create stale file
	staleFile := filepath.Join(outputDir, "stale.txt")
	if err := os.WriteFile(staleFile, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := cmdMarketplace([]string{"build", configFile, "-o", outputDir, "--clean"})
	if err != nil {
		t.Fatalf("cmdMarketplace build: %v", err)
	}

	// Stale file should be gone
	if _, err := os.Stat(staleFile); err == nil {
		t.Error("stale file should have been removed by --clean")
	}

	// Fresh content should exist
	assertExists(t, filepath.Join(outputDir, ".claude-plugin", "marketplace.json"))
}

func TestCmdMarketplaceBuildVendorFilter(t *testing.T) {
	configFile := setupMarketplaceTest(t)
	outputDir := t.TempDir()

	err := cmdMarketplace([]string{"build", configFile, "-o", outputDir, "-v", "claude"})
	if err != nil {
		t.Fatalf("cmdMarketplace build: %v", err)
	}

	assertExists(t, filepath.Join(outputDir, ".claude-plugin", "marketplace.json"))

	// Cursor should not be generated
	cursorPath := filepath.Join(outputDir, ".cursor-plugin", "marketplace.json")
	if _, err := os.Stat(cursorPath); err == nil {
		t.Error(".cursor-plugin should not exist for claude-only build")
	}
}

func TestCmdMarketplaceIndexContent(t *testing.T) {
	configFile := setupMarketplaceTest(t)
	outputDir := t.TempDir()

	err := cmdMarketplace([]string{"build", configFile, "-o", outputDir})
	if err != nil {
		t.Fatalf("cmdMarketplace build: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outputDir, ".claude-plugin", "marketplace.json"))
	if err != nil {
		t.Fatal(err)
	}

	var idx struct {
		Name    string `json:"name"`
		Plugins []struct {
			Name   string `json:"name"`
			Source string `json:"source"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatal(err)
	}

	if idx.Name != "cli-test-marketplace" {
		t.Errorf("name = %q, want cli-test-marketplace", idx.Name)
	}
	if len(idx.Plugins) != 2 {
		t.Fatalf("plugins = %d, want 2", len(idx.Plugins))
	}
	for _, p := range idx.Plugins {
		if !strings.HasPrefix(p.Source, "./plugins/") {
			t.Errorf("source %q should start with ./plugins/", p.Source)
		}
	}
}

func TestCmdMarketplaceMissingSubcommand(t *testing.T) {
	err := cmdMarketplace([]string{})
	if err == nil {
		t.Fatal("expected error for missing subcommand")
	}
}

func TestCmdMarketplaceUnknownSubcommand(t *testing.T) {
	err := cmdMarketplace([]string{"destroy"})
	if err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
	if !strings.Contains(err.Error(), "unknown marketplace subcommand") {
		t.Errorf("error = %q", err.Error())
	}
}

func writeTestJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
