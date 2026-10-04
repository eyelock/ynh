package marketplace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestMarketplaceIndexCodex(t *testing.T) {
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

	// Codex index at .agents/plugins/marketplace.json
	codexIdxPath := filepath.Join(outputDir, ".agents", "plugins", "marketplace.json")
	assertFileExists(t, codexIdxPath)

	data, err := os.ReadFile(codexIdxPath)
	if err != nil {
		t.Fatal(err)
	}

	var idx codexMarketplaceJSON
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatalf("parsing codex marketplace.json: %v", err)
	}

	if idx.Name != "test-marketplace" {
		t.Errorf("name = %q, want test-marketplace", idx.Name)
	}
	if idx.Interface.DisplayName != "test-marketplace" {
		t.Errorf("displayName = %q, want test-marketplace", idx.Interface.DisplayName)
	}
	if len(idx.Plugins) != 2 {
		t.Fatalf("plugins = %d, want 2", len(idx.Plugins))
	}
	for _, p := range idx.Plugins {
		if p.Source.Source != "local" {
			t.Errorf("plugin %q source.source = %q, want local", p.Name, p.Source.Source)
		}
		if !strings.HasPrefix(p.Source.Path, "./plugins/") {
			t.Errorf("plugin %q source.path = %q, want ./plugins/ prefix", p.Name, p.Source.Path)
		}
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
		t.Error("README should contain harness name")
	}
	if !strings.Contains(content, "my-tool") {
		t.Error("README should contain plugin name")
	}
}

// BeforeWrite runs once every entry has been checked and before anything is
// written, so a refused entry never reaches it and a failing hook writes
// nothing (#451).
func TestBuildBeforeWrite(t *testing.T) {
	configPath, configDir := setupMarketplace(t)
	goodCfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	refusedCfg := &MarketplaceConfig{
		Name:      goodCfg.Name,
		Owner:     goodCfg.Owner,
		Harnesses: append(append([]MarketplaceEntry{}, goodCfg.Harnesses...), MarketplaceEntry{Type: "harness", Source: "./does-not-exist"}),
	}

	tests := []struct {
		name       string
		cfg        *MarketplaceConfig
		hookErr    error
		wantCalled bool
	}{
		{"refused entry never calls the hook", refusedCfg, nil, false},
		{"failing hook stops the build", goodCfg, os.ErrPermission, true},
		{"passing hook lets the build run", goodCfg, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out")
			called := false
			err := Build(tt.cfg, BuildOptions{
				ConfigDir: configDir,
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
			wantOK := tt.cfg == goodCfg && tt.hookErr == nil
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
