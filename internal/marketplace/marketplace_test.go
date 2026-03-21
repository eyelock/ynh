package marketplace

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func testdataDir() string {
	return filepath.Join("..", "..", "testdata")
}

// setupMarketplace creates a marketplace config and source dirs in a temp directory.
func setupMarketplace(t *testing.T) (configPath string, configDir string) {
	t.Helper()
	dir := t.TempDir()

	// Create a persona source (reuse export-persona testdata)
	// We'll symlink to avoid duplication
	personaDir := filepath.Join(dir, "personas", "david")
	if err := os.MkdirAll(filepath.Dir(personaDir), 0o755); err != nil {
		t.Fatal(err)
	}
	srcPersona := filepath.Join(testdataDir(), "export-persona")
	abs, _ := filepath.Abs(srcPersona)
	if err := os.Symlink(abs, personaDir); err != nil {
		t.Fatal(err)
	}

	// Create a self-contained plugin source
	pluginDir := filepath.Join(dir, "plugins", "my-tool")
	claudePlugin := filepath.Join(pluginDir, ".claude-plugin")
	if err := os.MkdirAll(claudePlugin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(claudePlugin, "plugin.json"), map[string]any{
		"name":        "my-tool",
		"version":     "0.2.0",
		"description": "A standalone tool plugin",
	})
	skillDir := filepath.Join(pluginDir, "skills", "format")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("Format skill"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write marketplace.json
	configPath = filepath.Join(dir, "marketplace.json")
	writeJSON(t, configPath, map[string]any{
		"name":        "test-marketplace",
		"owner":       map[string]string{"name": "tester"},
		"description": "Test marketplace",
		"entries": []map[string]string{
			{"type": "persona", "source": "./personas/david"},
			{"type": "plugin", "source": "./plugins/my-tool"},
		},
	})

	return configPath, dir
}

func TestMarketplaceConfigParse(t *testing.T) {
	configPath, _ := setupMarketplace(t)

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.Name != "test-marketplace" {
		t.Errorf("name = %q, want %q", cfg.Name, "test-marketplace")
	}
	if cfg.Owner.Name != "tester" {
		t.Errorf("owner.name = %q, want %q", cfg.Owner.Name, "tester")
	}
	if len(cfg.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(cfg.Entries))
	}
	if cfg.Entries[0].Type != "persona" {
		t.Errorf("entry 0 type = %q, want persona", cfg.Entries[0].Type)
	}
	if cfg.Entries[1].Type != "plugin" {
		t.Errorf("entry 1 type = %q, want plugin", cfg.Entries[1].Type)
	}
}

func TestMarketplaceConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantErr string
	}{
		{
			name:    "missing name",
			json:    `{"owner":{"name":"x"},"entries":[{"type":"plugin","source":"./x"}]}`,
			wantErr: "name is required",
		},
		{
			name:    "missing owner",
			json:    `{"name":"x","owner":{},"entries":[{"type":"plugin","source":"./x"}]}`,
			wantErr: "owner.name is required",
		},
		{
			name:    "empty entries",
			json:    `{"name":"x","owner":{"name":"x"},"entries":[]}`,
			wantErr: "at least one entry",
		},
		{
			name:    "bad type",
			json:    `{"name":"x","owner":{"name":"x"},"entries":[{"type":"bad","source":"./x"}]}`,
			wantErr: "must be \"persona\" or \"plugin\"",
		},
		{
			name:    "missing source",
			json:    `{"name":"x","owner":{"name":"x"},"entries":[{"type":"plugin"}]}`,
			wantErr: "source is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "marketplace.json")
			if err := os.WriteFile(path, []byte(tt.json), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := LoadConfig(path)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want containing %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestMarketplacePersonaExport(t *testing.T) {
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

	// Persona should have dual manifests
	personaDir := filepath.Join(outputDir, "plugins", "export-test")
	assertFileExists(t, filepath.Join(personaDir, ".claude-plugin", "plugin.json"))
	assertFileExists(t, filepath.Join(personaDir, ".cursor-plugin", "plugin.json"))

	// Skills should be present
	assertFileExists(t, filepath.Join(personaDir, "skills", "dev-project", "SKILL.md"))
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

func TestMarketplaceIndexClaude(t *testing.T) {
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

	idxPath := filepath.Join(outputDir, ".claude-plugin", "marketplace.json")
	data, err := os.ReadFile(idxPath)
	if err != nil {
		t.Fatalf("reading marketplace.json: %v", err)
	}

	var idx marketplaceJSON
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatalf("parsing marketplace.json: %v", err)
	}

	if idx.Name != "test-marketplace" {
		t.Errorf("name = %q, want test-marketplace", idx.Name)
	}
	if idx.Owner.Name != "tester" {
		t.Errorf("owner = %q, want tester", idx.Owner.Name)
	}
	if len(idx.Plugins) != 2 {
		t.Fatalf("plugins = %d, want 2", len(idx.Plugins))
	}

	// Check relative source paths
	for _, p := range idx.Plugins {
		if !strings.HasPrefix(p.Source, "./plugins/") {
			t.Errorf("plugin %q source = %q, want ./plugins/ prefix", p.Name, p.Source)
		}
	}
}

func TestMarketplaceIndexCursor(t *testing.T) {
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

	// Cursor index should mirror Claude's
	assertFileExists(t, filepath.Join(outputDir, ".cursor-plugin", "marketplace.json"))

	data, err := os.ReadFile(filepath.Join(outputDir, ".cursor-plugin", "marketplace.json"))
	if err != nil {
		t.Fatal(err)
	}

	var idx marketplaceJSON
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatal(err)
	}
	if len(idx.Plugins) != 2 {
		t.Errorf("cursor index plugins = %d, want 2", len(idx.Plugins))
	}
}

func TestMarketplaceReadme(t *testing.T) {
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

	readmePath := filepath.Join(outputDir, "README.md")
	data, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("reading README.md: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "test-marketplace") {
		t.Error("README should contain marketplace name")
	}
	if !strings.Contains(content, "export-test") {
		t.Error("README should contain persona name")
	}
	if !strings.Contains(content, "my-tool") {
		t.Error("README should contain plugin name")
	}
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

	// Create plugin source
	pluginDir := filepath.Join(dir, "plugins", "widget")
	claudePlugin := filepath.Join(pluginDir, ".claude-plugin")
	if err := os.MkdirAll(claudePlugin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(claudePlugin, "plugin.json"), map[string]any{
		"name":        "widget",
		"version":     "1.0.0",
		"description": "Original description",
	})

	// Marketplace config with description override
	configPath := filepath.Join(dir, "marketplace.json")
	writeJSON(t, configPath, map[string]any{
		"name":  "override-test",
		"owner": map[string]string{"name": "tester"},
		"entries": []map[string]string{
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

func TestMarketplaceEmptyEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marketplace.json")
	writeJSON(t, path, map[string]any{
		"name":    "empty",
		"owner":   map[string]string{"name": "x"},
		"entries": []any{},
	})

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for empty entries")
	}
	if !strings.Contains(err.Error(), "at least one entry") {
		t.Errorf("error = %q, want 'at least one entry'", err.Error())
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
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
