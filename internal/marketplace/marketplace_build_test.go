package marketplace

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMarketplaceHarnessExport(t *testing.T) {
	configPath, configDir := setupMarketplace(t)
	outputDir := t.TempDir()

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}

	err = Build(cfg, BuildOptions{
		ConfigDir: configDir,
		OutputDir: outputDir,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Harness should have dual manifests
	harnessDir := filepath.Join(outputDir, "plugins", "export-test")
	assertFileExists(t, filepath.Join(harnessDir, ".claude-plugin", "plugin.json"))
	assertFileExists(t, filepath.Join(harnessDir, ".cursor-plugin", "plugin.json"))

	// Skills should be present
	assertFileExists(t, filepath.Join(harnessDir, "skills", "dev-project", "SKILL.md"))
}

func TestMarketplacePluginCopy(t *testing.T) {
	configPath, configDir := setupMarketplace(t)
	outputDir := t.TempDir()

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}

	err = Build(cfg, BuildOptions{
		ConfigDir: configDir,
		OutputDir: outputDir,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Plugin should be copied as-is
	pluginDir := filepath.Join(outputDir, "plugins", "my-tool")
	assertFileExists(t, filepath.Join(pluginDir, ".claude-plugin", "plugin.json"))
	assertFileExists(t, filepath.Join(pluginDir, "skills", "format", "SKILL.md"))
}

func TestMarketplacePluginMissingManifest(t *testing.T) {
	configPath, configDir := setupMarketplace(t)
	outputDir := t.TempDir()

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}

	err = Build(cfg, BuildOptions{
		ConfigDir: configDir,
		OutputDir: outputDir,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Plugin only had .claude-plugin/ — .cursor-plugin/ should be generated
	pluginDir := filepath.Join(outputDir, "plugins", "my-tool")
	assertFileExists(t, filepath.Join(pluginDir, ".cursor-plugin", "plugin.json"))
}

func TestMarketplaceCleanFlag(t *testing.T) {
	configPath, configDir := setupMarketplace(t)
	outputDir := t.TempDir()

	// Create stale content
	staleFile := filepath.Join(outputDir, "stale.txt")
	if err := os.WriteFile(staleFile, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}

	// Build (without clean — stale file should remain since we don't clean at package level)
	err = Build(cfg, BuildOptions{
		ConfigDir: configDir,
		OutputDir: outputDir,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Marketplace content should exist
	assertFileExists(t, filepath.Join(outputDir, ".claude-plugin", "marketplace.json"))
}

func TestMarketplaceDescriptionOverride(t *testing.T) {
	dir := t.TempDir()

	// Create plugin source (vendor-native format)
	writePluginManifest(t, filepath.Join(dir, "plugins", "widget"), "widget", "1.0.0", "Original description")

	// Marketplace config with description override
	configPath := filepath.Join(dir, "marketplace.json")
	writeJSON(t, configPath, map[string]any{
		"name":  "override-test",
		"owner": map[string]string{"name": "tester"},
		"harnesses": []map[string]string{
			{
				"type":        "plugin",
				"source":      "./plugins/widget",
				"description": "Overridden description",
			},
		},
	})

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}

	outputDir := t.TempDir()
	err = Build(cfg, BuildOptions{
		ConfigDir: dir,
		OutputDir: outputDir,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Check the index uses the overridden description
	data, err := os.ReadFile(filepath.Join(outputDir, ".claude-plugin", "marketplace.json"))
	if err != nil {
		t.Fatal(err)
	}

	var idx marketplaceJSON
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatal(err)
	}

	if idx.Plugins[0].Description != "Overridden description" {
		t.Errorf("description = %q, want %q", idx.Plugins[0].Description, "Overridden description")
	}
}

func TestMarketplaceBuildInitGitRepo(t *testing.T) {
	configPath, configDir := setupMarketplace(t)
	outputDir := t.TempDir()

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}

	err = Build(cfg, BuildOptions{
		ConfigDir: configDir,
		OutputDir: outputDir,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Output dir should now be a Git repo
	assertFileExists(t, filepath.Join(outputDir, ".git"))

	// Verify there's at least one commit
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = outputDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git rev-parse HEAD failed: %v\n%s", err, out)
	}
}

func TestMarketplaceBuildSkipsExistingGitRepo(t *testing.T) {
	configPath, configDir := setupMarketplace(t)
	outputDir := t.TempDir()

	// Pre-initialize a git repo with a known commit
	for _, args := range [][]string{
		{"init"},
		{"-c", "user.name=test", "-c", "user.email=test@test", "commit", "--allow-empty", "-m", "pre-existing"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = outputDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", args[0], err, out)
		}
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}

	err = Build(cfg, BuildOptions{
		ConfigDir: configDir,
		OutputDir: outputDir,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Should still have the original commit (Build should not re-init)
	cmd := exec.Command("git", "log", "--oneline")
	cmd.Dir = outputDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "pre-existing") {
		t.Errorf("expected pre-existing commit in log, got:\n%s", out)
	}
}

func TestMarketplaceVendorFiltering(t *testing.T) {
	configPath, configDir := setupMarketplace(t)
	outputDir := t.TempDir()

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}

	// Build for claude only
	err = Build(cfg, BuildOptions{
		ConfigDir: configDir,
		OutputDir: outputDir,
		Vendors:   []string{"claude"},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	assertFileExists(t, filepath.Join(outputDir, ".claude-plugin", "marketplace.json"))
	assertFileNotExists(t, filepath.Join(outputDir, ".cursor-plugin", "marketplace.json"))
}

func TestMarketplaceBuild_EntryPathTraversalBlocked(t *testing.T) {
	dir := t.TempDir()
	for _, badPath := range []string{"../../etc", "../secret", "/etc/passwd", "a/../../etc"} {
		cfg := &MarketplaceConfig{
			Name:  "test",
			Owner: MarketplaceOwner{Name: "tester"},
			Harnesses: []MarketplaceEntry{
				{Type: "plugin", Source: "./plugins/foo", Path: badPath},
			},
		}
		err := Build(cfg, BuildOptions{ConfigDir: dir, OutputDir: t.TempDir()})
		if err == nil {
			t.Errorf("path %q: expected error, got nil", badPath)
			continue
		}
		if !strings.Contains(err.Error(), "invalid path") {
			t.Errorf("path %q: unexpected error: %v", badPath, err)
		}
	}
}
