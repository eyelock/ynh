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

	// Create plugin source (vendor-native format with .claude-plugin/plugin.json)
	pluginDir := filepath.Join(dir, "plugins", "my-tool")
	manifestDir := filepath.Join(pluginDir, ".claude-plugin")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestJSON(t, filepath.Join(manifestDir, "plugin.json"), map[string]any{
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
		"harnesses": []map[string]string{
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

	// --clean now asks before deleting a non-empty directory. Answer it
	// explicitly: a test that relied on the old unconditional delete would
	// otherwise pass for the wrong reason.
	restorePrompt := promptActionFunc
	t.Cleanup(func() { promptActionFunc = restorePrompt })
	asked := false
	promptActionFunc = func(_ string, _ ...string) string { asked = true; return "y" }

	err := cmdMarketplace([]string{"build", configFile, "-o", outputDir, "--clean"})
	if err != nil {
		t.Fatalf("cmdMarketplace build: %v", err)
	}

	// Stale file should be gone
	if _, err := os.Stat(staleFile); err == nil {
		t.Error("stale file should have been removed by --clean")
	}
	if !asked {
		t.Error("--clean deleted a non-empty directory without asking")
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

// The guard at marketplace.go skipped only vendor.Get validation — which would
// have passed, since codex is a registered adapter — while codex stayed in
// vendorList and reached Build regardless. Codex's manifest dir is
// .agents/plugins, so it genuinely got an index all along.
//
// The code said one thing, did another, and docs/ynd.md said a third. This
// asserts what actually happens.
func TestCmdMarketplaceBuild_CodexGetsAnIndex(t *testing.T) {
	configFile := setupMarketplaceTest(t)
	outputDir := t.TempDir()

	if err := cmdMarketplace([]string{"build", configFile, "-o", outputDir, "-v", "codex"}); err != nil {
		t.Fatalf("cmdMarketplace build -v codex: %v", err)
	}
	assertExists(t, filepath.Join(outputDir, ".agents", "plugins", "marketplace.json"))
}

// An unknown vendor must still be rejected — deleting the codex branch must not
// weaken validation.
func TestCmdMarketplaceBuild_UnknownVendorStillRejected(t *testing.T) {
	configFile := setupMarketplaceTest(t)
	outputDir := t.TempDir()

	err := cmdMarketplace([]string{"build", configFile, "-o", outputDir, "-v", "not-a-vendor"})
	if err == nil {
		t.Fatal("an unknown vendor must be rejected")
	}
}

// config.Load returns an empty config for an absent file, so an error there
// means the file exists and is malformed. Swallowing it produced a confusing
// resolution failure several steps later instead of the parse error.
func TestCmdMarketplaceBuild_MalformedGlobalConfigFailsImmediately(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	configFile := setupMarketplaceTest(t)
	err := cmdMarketplace([]string{"build", configFile, "-o", t.TempDir()})
	if err == nil {
		t.Fatal("a malformed global config must fail the build, not be replaced with an empty one")
	}
	if !strings.Contains(err.Error(), "global config") {
		t.Errorf("the error should name what failed, got: %v", err)
	}
}

// A genuinely absent global config must keep working — the fix must not turn
// "no config yet" into an error.
func TestCmdMarketplaceBuild_AbsentGlobalConfigStillWorks(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	configFile := setupMarketplaceTest(t)
	if err := cmdMarketplace([]string{"build", configFile, "-o", t.TempDir()}); err != nil {
		t.Fatalf("an absent global config must not fail the build: %v", err)
	}
}
