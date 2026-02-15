package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestIsPluginDir_True(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".claude-plugin", "plugin.json"), []byte(`{"name":"test"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if !IsPluginDir(dir) {
		t.Error("expected IsPluginDir to return true")
	}
}

func TestIsPluginDir_False(t *testing.T) {
	dir := t.TempDir()
	if IsPluginDir(dir) {
		t.Error("expected IsPluginDir to return false for empty dir")
	}
}

func TestLoadPluginJSON_Valid(t *testing.T) {
	dir := t.TempDir()
	writePluginJSON(t, dir, PluginJSON{
		Name:    "test-persona",
		Version: "1.0.0",
	})

	pj, err := LoadPluginJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	if pj.Name != "test-persona" {
		t.Errorf("Name = %q, want %q", pj.Name, "test-persona")
	}
	if pj.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", pj.Version, "1.0.0")
	}
}

func TestLoadPluginJSON_MissingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadPluginJSON(dir)
	if err == nil {
		t.Fatal("expected error for missing plugin.json")
	}
}

func TestLoadPluginJSON_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".claude-plugin", "plugin.json"), []byte(`{invalid`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadPluginJSON(dir)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestLoadPluginJSON_MissingName(t *testing.T) {
	dir := t.TempDir()
	writePluginJSON(t, dir, PluginJSON{Version: "1.0.0"})

	_, err := LoadPluginJSON(dir)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestLoadMetadataJSON_WithYNH(t *testing.T) {
	dir := t.TempDir()
	writeMetadataJSON(t, dir, MetadataJSON{
		YNH: &YNHMetadata{
			DefaultVendor: "claude",
			Includes: []IncludeMeta{
				{Git: "github.com/example/repo", Pick: []string{"skills/hello"}},
			},
			DelegatesTo: []DelegateMeta{
				{Git: "github.com/example/team"},
			},
		},
	})

	meta, err := LoadMetadataJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.YNH == nil {
		t.Fatal("YNH is nil")
	}
	if meta.YNH.DefaultVendor != "claude" {
		t.Errorf("DefaultVendor = %q, want %q", meta.YNH.DefaultVendor, "claude")
	}
	if len(meta.YNH.Includes) != 1 {
		t.Fatalf("Includes = %d, want 1", len(meta.YNH.Includes))
	}
	if len(meta.YNH.DelegatesTo) != 1 {
		t.Fatalf("DelegatesTo = %d, want 1", len(meta.YNH.DelegatesTo))
	}
}

func TestLoadMetadataJSON_FileNotExists(t *testing.T) {
	dir := t.TempDir()
	meta, err := LoadMetadataJSON(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta != nil {
		t.Error("expected nil for missing metadata.json")
	}
}

func TestLoadMetadataJSON_NoYNHKey(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(`{"other_tool": {}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	meta, err := LoadMetadataJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.YNH != nil {
		t.Error("expected YNH to be nil")
	}
}

func TestLoadMetadataJSON_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(`{bad`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadMetadataJSON(dir)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func writePluginJSON(t *testing.T, dir string, pj PluginJSON) {
	t.Helper()
	pluginDir := filepath.Join(dir, ".claude-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(pj)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeMetadataJSON(t *testing.T, dir string, meta MetadataJSON) {
	t.Helper()
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}
