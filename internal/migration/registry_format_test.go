package migration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

var registryFormatM = RegistryFormatMigrator{}

func TestRegistryFormatMigrator_Applies(t *testing.T) {
	t.Run("true when only registry.json exists", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "registry.json"), `{"name":"r","entries":[]}`)
		if !registryFormatM.Applies(dir) {
			t.Error("expected Applies=true")
		}
	})

	t.Run("false when marketplace.json already exists", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "registry.json"), `{"name":"r","entries":[]}`)
		if err := os.MkdirAll(filepath.Join(dir, plugin.PluginDir), 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, plugin.PluginDir, plugin.MarketplaceFile), `{"name":"r","owner":{"name":"r"},"harnesses":[]}`)
		if registryFormatM.Applies(dir) {
			t.Error("expected Applies=false")
		}
	})
}

func TestRegistryFormatMigrator_Run(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "registry.json"), `{
		"name": "my-registry",
		"description": "A test registry",
		"entries": [
			{"name": "david", "repo": "eyelock/assistants", "path": "ynh/david", "description": "Dev harness", "version": "0.1.0", "keywords": ["dev"]}
		]
	}`)

	if err := registryFormatM.Run(dir); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "registry.json")); err == nil {
		t.Error("registry.json should have been removed")
	}

	mj, err := plugin.LoadMarketplaceJSON(dir)
	if err != nil {
		t.Fatalf("LoadMarketplaceJSON: %v", err)
	}
	if mj.Name != "my-registry" {
		t.Errorf("Name = %q, want %q", mj.Name, "my-registry")
	}
	if len(mj.Harnesses) != 1 {
		t.Fatalf("len(Harnesses) = %d, want 1", len(mj.Harnesses))
	}

	h := mj.Harnesses[0]
	if h.Name != "david" {
		t.Errorf("harness Name = %q, want %q", h.Name, "david")
	}

	src, ok := h.SourceRemote()
	if !ok {
		t.Fatal("expected remote source")
	}
	if src.Type != "github" {
		t.Errorf("source.type = %q, want %q", src.Type, "github")
	}
	if src.Repo != "eyelock/assistants" {
		t.Errorf("source.repo = %q, want %q", src.Repo, "eyelock/assistants")
	}
	if src.Path != "ynh/david" {
		t.Errorf("source.path = %q, want %q", src.Path, "ynh/david")
	}
}
