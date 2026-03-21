package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

func testdataExportDir() string {
	return filepath.Join("..", "..", "testdata", "export-persona")
}

func TestCmdExportLocalSource(t *testing.T) {
	outputDir := t.TempDir()
	srcDir := testdataExportDir()

	err := cmdExport([]string{srcDir, "-o", outputDir, "-v", "claude"})
	if err != nil {
		t.Fatalf("cmdExport failed: %v", err)
	}

	// Verify output
	manifestPath := filepath.Join(outputDir, "claude", ".claude-plugin", "plugin.json")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		t.Error("expected .claude-plugin/plugin.json")
	}
}

func TestCmdExportAllVendors(t *testing.T) {
	outputDir := t.TempDir()
	srcDir := testdataExportDir()

	err := cmdExport([]string{srcDir, "-o", outputDir})
	if err != nil {
		t.Fatalf("cmdExport failed: %v", err)
	}

	// All three vendor dirs should exist
	for _, v := range []string{"claude", "cursor", "codex"} {
		dir := filepath.Join(outputDir, v)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			t.Errorf("expected vendor dir: %s", dir)
		}
	}
}

func TestCmdExportMergedFlag(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "merged-out")
	srcDir := testdataExportDir()

	err := cmdExport([]string{srcDir, "-o", outputDir, "--merged", "-v", "claude,cursor"})
	if err != nil {
		t.Fatalf("cmdExport failed: %v", err)
	}

	// Both manifests should be in same directory
	assertExists(t, filepath.Join(outputDir, ".claude-plugin", "plugin.json"))
	assertExists(t, filepath.Join(outputDir, ".cursor-plugin", "plugin.json"))
}

func TestCmdExportCleanFlag(t *testing.T) {
	outputDir := t.TempDir()
	srcDir := testdataExportDir()

	// Create stale content
	staleFile := filepath.Join(outputDir, "stale.txt")
	if err := os.WriteFile(staleFile, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := cmdExport([]string{srcDir, "-o", outputDir, "-v", "claude", "--clean"})
	if err != nil {
		t.Fatalf("cmdExport failed: %v", err)
	}

	// Stale file should be gone (--clean removes entire output dir)
	if _, err := os.Stat(staleFile); err == nil {
		t.Error("stale file should have been removed by --clean")
	}

	// Fresh content should exist
	assertExists(t, filepath.Join(outputDir, "claude", ".claude-plugin", "plugin.json"))
}

func TestCmdExportDefaultOutput(t *testing.T) {
	// Get absolute path to testdata before changing dirs
	srcDir, err := filepath.Abs(testdataExportDir())
	if err != nil {
		t.Fatal(err)
	}

	// Run in a temp directory so the default ./dist/ doesn't pollute
	origDir, _ := os.Getwd()
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	err = cmdExport([]string{srcDir, "-v", "claude"})
	if err != nil {
		t.Fatalf("cmdExport failed: %v", err)
	}

	// Default output should be ./dist/<persona-name>/
	assertExists(t, filepath.Join(tmpDir, "dist", "export-test", "claude", ".claude-plugin", "plugin.json"))
}

func TestCmdExportUnknownVendor(t *testing.T) {
	srcDir := testdataExportDir()
	outputDir := t.TempDir()

	err := cmdExport([]string{srcDir, "-o", outputDir, "-v", "bogus"})
	if err == nil {
		t.Fatal("expected error for unknown vendor")
	}
	if !strings.Contains(err.Error(), "unknown vendor") {
		t.Errorf("expected 'unknown vendor' error, got: %v", err)
	}
}

func TestCmdExportMissingSource(t *testing.T) {
	err := cmdExport([]string{})
	if err == nil {
		t.Fatal("expected error for missing source")
	}
}

func TestCmdExportBadPath(t *testing.T) {
	err := cmdExport([]string{"./nonexistent-dir", "-o", t.TempDir()})
	if err == nil {
		t.Fatal("expected error for nonexistent source")
	}
}

func TestCmdExportManifestContent(t *testing.T) {
	outputDir := t.TempDir()
	srcDir := testdataExportDir()

	err := cmdExport([]string{srcDir, "-o", outputDir, "-v", "claude"})
	if err != nil {
		t.Fatalf("cmdExport failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outputDir, "claude", ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}

	var pj plugin.PluginJSON
	if err := json.Unmarshal(data, &pj); err != nil {
		t.Fatalf("invalid manifest JSON: %v", err)
	}

	if pj.Name != "export-test" {
		t.Errorf("manifest name = %q, want %q", pj.Name, "export-test")
	}
	if pj.Version != "1.0.0" {
		t.Errorf("manifest version = %q, want %q", pj.Version, "1.0.0")
	}
}

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("expected to exist: %s", path)
	}
}
