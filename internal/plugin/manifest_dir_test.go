package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

// The manifest directory moved from .ynh-plugin to .agents/harness, and the
// plugin package reads only the new one (#404). These tests pin that: the
// canonical location is read and written, and a file under the legacy one is
// invisible. Refusing such a tree, and moving the ones ynh owns, is the
// migration package's job.

func writeAt(t *testing.T, dir, manifestDir, file, body string) string {
	t.Helper()
	path := filepath.Join(dir, manifestDir, file)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const (
	manifestBody    = `{"name":"x","version":"1.0.0"}`
	installedBody   = `{"source_type":"local","source":"/src","installed_at":"now"}`
	marketplaceBody = `{"name":"reg","owner":{"name":"o"},"harnesses":[]}`
)

func TestIsPluginDir_OnlyTheCanonicalDir(t *testing.T) {
	cases := []struct {
		name string
		dirs []string
		want bool
	}{
		{"canonical", []string{PluginDir}, true},
		{"legacy only is not read", []string{LegacyPluginDir}, false},
		{"both", []string{PluginDir, LegacyPluginDir}, true},
		{"none", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, md := range tc.dirs {
				writeAt(t, dir, md, PluginFile, manifestBody)
			}
			if got := IsPluginDir(dir); got != tc.want {
				t.Errorf("IsPluginDir = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLoadPluginJSON_IgnoresLegacyDir(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, LegacyPluginDir, PluginFile, `{"name":"legacy","version":"1.0.0"}`)

	if _, err := LoadPluginJSON(dir); err == nil {
		t.Fatal("a manifest under .ynh-plugin must not be read")
	}

	writeAt(t, dir, PluginDir, PluginFile, `{"name":"canonical","version":"1.0.0"}`)
	hj, err := LoadPluginJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	if hj.Name != "canonical" {
		t.Errorf("Name = %q, want canonical", hj.Name)
	}
}

func TestLoadInstalledJSON_IgnoresLegacyDir(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, PluginDir, PluginFile, manifestBody)
	writeAt(t, dir, LegacyPluginDir, InstalledFile, installedBody)

	if got := InstalledPath(dir); got != filepath.Join(dir, PluginDir, InstalledFile) {
		t.Errorf("InstalledPath = %q, want the canonical path", got)
	}
	if _, err := LoadInstalledJSON(dir); err == nil {
		t.Error("provenance under .ynh-plugin must not be read")
	}
}

func TestMarketplaceJSON(t *testing.T) {
	t.Run("legacy index is not a registry", func(t *testing.T) {
		dir := t.TempDir()
		writeAt(t, dir, LegacyPluginDir, MarketplaceFile, marketplaceBody)
		if IsRegistryDir(dir) {
			t.Fatal("IsRegistryDir must not see an index under .ynh-plugin")
		}
	})
	t.Run("canonical read", func(t *testing.T) {
		dir := t.TempDir()
		writeAt(t, dir, PluginDir, MarketplaceFile, marketplaceBody)
		mj, err := LoadMarketplaceJSON(dir)
		if err != nil {
			t.Fatal(err)
		}
		if mj.Name != "reg" {
			t.Errorf("Name = %q, want reg", mj.Name)
		}
	})
	t.Run("write is canonical", func(t *testing.T) {
		dir := t.TempDir()
		if err := SaveMarketplaceJSON(dir, &MarketplaceJSON{Name: "reg", Owner: &OwnerInfo{Name: "o"}}); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, PluginDir, MarketplaceFile)); err != nil {
			t.Errorf("new index not at %s: %v", PluginDir, err)
		}
		if _, err := os.Stat(filepath.Join(dir, LegacyPluginDir)); !os.IsNotExist(err) {
			t.Errorf("nothing should create %s", LegacyPluginDir)
		}
	})
}

func TestSavePluginJSON_GoesToCanonical(t *testing.T) {
	dir := t.TempDir()
	if err := SavePluginJSON(dir, &HarnessJSON{Name: "x", Version: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, PluginDir, PluginFile)); err != nil {
		t.Errorf("manifest not written to %s: %v", PluginDir, err)
	}
	if _, err := os.Stat(filepath.Join(dir, LegacyPluginDir)); !os.IsNotExist(err) {
		t.Errorf("nothing should create %s any more", LegacyPluginDir)
	}
}

func TestSaveInstalledJSON_GoesBesidePluginJSON(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, PluginDir, PluginFile, manifestBody)
	if err := SaveInstalledJSON(dir, &InstalledJSON{SourceType: "local", Source: "/src", InstalledAt: "now"}); err != nil {
		t.Fatal(err)
	}
	ins, err := LoadInstalledJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	if ins.Source != "/src" {
		t.Errorf("Source = %q, want /src", ins.Source)
	}
}

func TestHarnessRoot(t *testing.T) {
	cases := []struct {
		path, want string
	}{
		{filepath.Join("repo", ".agents", "harness", "plugin.json"), "repo"},
		{filepath.Join(".agents", "harness", "plugin.json"), "."},
		{filepath.Join("a", "b", ".agents", "harness", "installed.json"), filepath.Join("a", "b")},
		{filepath.Join("repo", ".ynh-plugin", "plugin.json"), ""},
		{filepath.Join("repo", ".agents", "plugin.json"), ""},
		{filepath.Join("repo", ".claude-plugin", "plugin.json"), ""},
		{"plugin.json", ""},
	}
	for _, tc := range cases {
		if got := HarnessRoot(tc.path); got != tc.want {
			t.Errorf("HarnessRoot(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestInManifestDir(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{filepath.Join("repo", ".agents", "harness", "marketplace.json"), true},
		{filepath.Join(".agents", "harness", "marketplace.json"), true},
		{filepath.Join("repo", ".ynh-plugin", "marketplace.json"), false},
		{filepath.Join("repo", "marketplace.json"), false},
		{filepath.Join("repo", ".agents", "marketplace.json"), false},
		{filepath.Join("repo", ".claude-plugin", "marketplace.json"), false},
		{filepath.Join("repo", "harness", "marketplace.json"), false},
	}
	for _, tc := range cases {
		if got := InManifestDir(tc.path); got != tc.want {
			t.Errorf("InManifestDir(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestPluginPath_MissingReportsCanonical(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, PluginDir, PluginFile)
	if got := PluginPath(dir); got != want {
		t.Errorf("PluginPath on an empty dir = %q, want canonical %q so errors name the documented location", got, want)
	}
}
