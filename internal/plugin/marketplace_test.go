package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestHarnessEntry_SourcePath(t *testing.T) {
	e := HarnessEntry{Source: json.RawMessage(`"./ynh/david"`)}
	path, ok := e.SourcePath()
	if !ok {
		t.Fatal("expected string source to parse as path")
	}
	if path != "./ynh/david" {
		t.Errorf("path = %q, want %q", path, "./ynh/david")
	}

	_, ok = e.SourceRemote()
	if ok {
		t.Error("string source should not parse as RemoteSource")
	}
}

func TestHarnessEntry_SourceRemote_GitHub(t *testing.T) {
	e := HarnessEntry{Source: json.RawMessage(`{"type":"github","repo":"eyelock/assistants","path":"ynh/david","ref":"main"}`)}
	src, ok := e.SourceRemote()
	if !ok {
		t.Fatal("expected object source to parse")
	}
	if src.Type != "github" {
		t.Errorf("Type = %q, want github", src.Type)
	}
	if src.Repo != "eyelock/assistants" {
		t.Errorf("Repo = %q", src.Repo)
	}

	_, ok = e.SourcePath()
	if ok {
		t.Error("object source should not parse as string path")
	}
}

func TestHarnessEntry_SourceRemote_URL(t *testing.T) {
	e := HarnessEntry{Source: json.RawMessage(`{"type":"url","url":"https://gitlab.com/myorg/tools.git","ref":"v1.0.0","sha":"abc123"}`)}
	src, ok := e.SourceRemote()
	if !ok {
		t.Fatal("expected object source to parse")
	}
	if src.Type != "url" {
		t.Errorf("Type = %q, want url", src.Type)
	}
	if src.URL != "https://gitlab.com/myorg/tools.git" {
		t.Errorf("URL = %q", src.URL)
	}
	if src.SHA != "abc123" {
		t.Errorf("SHA = %q", src.SHA)
	}
}

func TestLoadSaveMarketplaceJSON_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	mj := &MarketplaceJSON{
		Name:  "test",
		Owner: &OwnerInfo{Name: "me"},
		Harnesses: []HarnessEntry{
			{Name: "a", Source: json.RawMessage(`"./a"`)},
			{Name: "b", Source: json.RawMessage(`{"type":"github","repo":"o/r"}`)},
		},
	}

	if err := SaveMarketplaceJSON(dir, mj); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// File should be at .ynh-plugin/marketplace.json
	if _, err := os.Stat(filepath.Join(dir, PluginDir, MarketplaceFile)); err != nil {
		t.Fatalf("marketplace.json not found: %v", err)
	}

	loaded, err := LoadMarketplaceJSON(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.Name != "test" {
		t.Errorf("Name = %q", loaded.Name)
	}
	if len(loaded.Harnesses) != 2 {
		t.Fatalf("len(Harnesses) = %d", len(loaded.Harnesses))
	}

	if path, ok := loaded.Harnesses[0].SourcePath(); !ok || path != "./a" {
		t.Errorf("first entry path = %q ok=%v", path, ok)
	}
	if src, ok := loaded.Harnesses[1].SourceRemote(); !ok || src.Type != "github" {
		t.Errorf("second entry remote = %v ok=%v", src, ok)
	}
}

func TestLoadSavePluginJSON_StripsInstalledFrom(t *testing.T) {
	dir := t.TempDir()
	hj := &HarnessJSON{
		Name:    "test",
		Version: "1.0.0",
		InstalledFrom: &ProvenanceMeta{
			SourceType:  "github",
			Source:      "https://github.com/o/r",
			InstalledAt: "2026-04-22T00:00:00Z",
		},
	}

	if err := SavePluginJSON(dir, hj); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := LoadPluginJSON(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.InstalledFrom != nil {
		t.Error("SavePluginJSON should strip InstalledFrom")
	}

	// Caller's struct must not be mutated
	if hj.InstalledFrom == nil {
		t.Error("SavePluginJSON mutated caller's struct")
	}
}

func TestLoadSaveInstalledJSON_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	ins := &InstalledJSON{
		SourceType:  "github",
		Source:      "https://github.com/eyelock/assistants",
		Ref:         "main",
		SHA:         "abc123",
		Namespace:   "eyelock/assistants",
		InstalledAt: "2026-04-22T00:00:00Z",
	}

	if err := SaveInstalledJSON(dir, ins); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := LoadInstalledJSON(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.Source != ins.Source {
		t.Errorf("Source = %q, want %q", loaded.Source, ins.Source)
	}
	if loaded.Namespace != ins.Namespace {
		t.Errorf("Namespace = %q, want %q", loaded.Namespace, ins.Namespace)
	}
	if loaded.SHA != ins.SHA {
		t.Errorf("SHA = %q, want %q", loaded.SHA, ins.SHA)
	}
}
