package marketplace

import (
	"encoding/json"
	"os"
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

	// Create a harness source (reuse export-harness testdata)
	// We'll symlink to avoid duplication
	harnessDir := filepath.Join(dir, "harnesses", "david")
	if err := os.MkdirAll(filepath.Dir(harnessDir), 0o755); err != nil {
		t.Fatal(err)
	}
	srcHarness := filepath.Join(testdataDir(), "export-harness")
	abs, _ := filepath.Abs(srcHarness)
	if err := os.Symlink(abs, harnessDir); err != nil {
		t.Fatal(err)
	}

	// Create a self-contained plugin source (vendor-native format)
	pluginDir := filepath.Join(dir, "plugins", "my-tool")
	writePluginManifest(t, pluginDir, "my-tool", "0.2.0", "A standalone tool plugin")
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
		"harnesses": []map[string]string{
			{"type": "harness", "source": "./harnesses/david"},
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
	if len(cfg.Harnesses) != 2 {
		t.Fatalf("entries = %d, want 2", len(cfg.Harnesses))
	}
	if cfg.Harnesses[0].Type != "harness" {
		t.Errorf("entry 0 type = %q, want harness", cfg.Harnesses[0].Type)
	}
	if cfg.Harnesses[1].Type != "plugin" {
		t.Errorf("entry 1 type = %q, want plugin", cfg.Harnesses[1].Type)
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
			json:    `{"owner":{"name":"x"},"harnesses":[{"type":"plugin","source":"./x"}]}`,
			wantErr: "name is required",
		},
		{
			name:    "missing owner",
			json:    `{"name":"x","owner":{},"harnesses":[{"type":"plugin","source":"./x"}]}`,
			wantErr: "owner.name is required",
		},
		{
			name:    "empty entries",
			json:    `{"name":"x","owner":{"name":"x"},"harnesses":[]}`,
			wantErr: "at least one harness",
		},
		{
			name:    "bad type",
			json:    `{"name":"x","owner":{"name":"x"},"harnesses":[{"type":"bad","source":"./x"}]}`,
			wantErr: "must be \"harness\" or \"plugin\"",
		},
		{
			name:    "missing source",
			json:    `{"name":"x","owner":{"name":"x"},"harnesses":[{"type":"plugin"}]}`,
			wantErr: "source is required",
		},
		{
			name:    "github shorthand missing repo",
			json:    `{"name":"x","owner":{"name":"x"},"harnesses":[{"type":"plugin","source":"github.com/user"}]}`,
			wantErr: "remote source",
		},
		{
			name:    "bare user/repo without host",
			json:    `{"name":"x","owner":{"name":"x"},"harnesses":[{"type":"plugin","source":"user/repo"}]}`,
			wantErr: "remote source",
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

func TestMarketplaceRemoteSourceValidation(t *testing.T) {
	valid := []string{
		"github.com/user/repo",
		"gitlab.com/org/repo",
		"https://github.com/user/repo",
		"https://github.com/user/repo.git",
		"git@github.com:user/repo.git",
	}
	for _, src := range valid {
		t.Run(src, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "marketplace.json")
			data := `{"name":"x","owner":{"name":"x"},"harnesses":[{"type":"plugin","source":"` + src + `"}]}`
			if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadConfig(path); err != nil {
				t.Errorf("LoadConfig(%q) unexpected error: %v", src, err)
			}
		})
	}
}

func TestMarketplaceEmptyEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marketplace.json")
	writeJSON(t, path, map[string]any{
		"name":      "empty",
		"owner":     map[string]string{"name": "x"},
		"harnesses": []any{},
	})

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for empty entries")
	}
	if !strings.Contains(err.Error(), "at least one harness") {
		t.Errorf("error = %q, want 'at least one harness'", err.Error())
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

// writePluginManifest creates a vendor-native plugin directory with .claude-plugin/plugin.json.
func writePluginManifest(t *testing.T, pluginDir, name, version, description string) {
	t.Helper()
	manifestDir := filepath.Join(pluginDir, ".claude-plugin")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(manifestDir, "plugin.json"), map[string]any{
		"name":        name,
		"version":     version,
		"description": description,
	})
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
