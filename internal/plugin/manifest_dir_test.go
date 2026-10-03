package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

// The manifest directory moved from .ynh-plugin to .agents/harness. These
// tests pin the contract of that move: the canonical location is read first
// and written by default, the legacy location is still read, and a file
// that already exists is edited where it is rather than relocated.

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

func TestIsPluginDir_EitherManifestDir(t *testing.T) {
	cases := []struct {
		name string
		dirs []string
		want bool
	}{
		{"canonical", []string{PluginDir}, true},
		{"legacy", []string{LegacyPluginDir}, true},
		{"both", []string{PluginDir, LegacyPluginDir}, true},
		{"none", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, md := range tc.dirs {
				writeAt(t, dir, md, PluginFile, `{"name":"x","version":"1.0.0"}`)
			}
			if got := IsPluginDir(dir); got != tc.want {
				t.Errorf("IsPluginDir = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLoadPluginJSON_LegacyDirFallback(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, LegacyPluginDir, PluginFile, `{"name":"legacy","version":"1.0.0"}`)

	hj, err := LoadPluginJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	if hj.Name != "legacy" {
		t.Errorf("Name = %q, want legacy", hj.Name)
	}
}

func TestLoadPluginJSON_CanonicalWinsOverLegacy(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, PluginDir, PluginFile, `{"name":"canonical","version":"1.0.0"}`)
	writeAt(t, dir, LegacyPluginDir, PluginFile, `{"name":"legacy","version":"1.0.0"}`)

	hj, err := LoadPluginJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	if hj.Name != "canonical" {
		t.Errorf("Name = %q, want canonical: .agents/harness must shadow .ynh-plugin", hj.Name)
	}
	if !ShadowedLegacyManifest(dir) {
		t.Error("ShadowedLegacyManifest should report both copies present")
	}
}

func TestShadowedLegacyManifest_FalseForOneCopy(t *testing.T) {
	for _, md := range []string{PluginDir, LegacyPluginDir} {
		dir := t.TempDir()
		writeAt(t, dir, md, PluginFile, `{"name":"x","version":"1.0.0"}`)
		if ShadowedLegacyManifest(dir) {
			t.Errorf("%s alone must not count as shadowed", md)
		}
	}
}

func TestSavePluginJSON_NewGoesToCanonical(t *testing.T) {
	dir := t.TempDir()
	if err := SavePluginJSON(dir, &HarnessJSON{Name: "x", Version: "1.0.0"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, PluginDir, PluginFile)); err != nil {
		t.Errorf("new manifest not written to %s: %v", PluginDir, err)
	}
	if _, err := os.Stat(filepath.Join(dir, LegacyPluginDir)); !os.IsNotExist(err) {
		t.Errorf("nothing should create %s any more", LegacyPluginDir)
	}
}

func TestSavePluginJSON_ExistingLegacyEditedInPlace(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, LegacyPluginDir, PluginFile, `{"name":"x","version":"1.0.0"}`)

	if err := SavePluginJSON(dir, &HarnessJSON{Name: "x", Version: "2.0.0"}); err != nil {
		t.Fatal(err)
	}
	hj, err := LoadPluginJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	if hj.Version != "2.0.0" {
		t.Errorf("Version = %q, want 2.0.0", hj.Version)
	}
	if _, err := os.Stat(filepath.Join(dir, PluginDir)); !os.IsNotExist(err) {
		t.Errorf("editing a %s manifest must not create %s: a silent relocation would leave the author editing a file nothing reads", LegacyPluginDir, PluginDir)
	}
}

func TestSaveInstalledJSON_FollowsPluginJSON(t *testing.T) {
	cases := []struct {
		name, pluginIn, want string
	}{
		{"canonical", PluginDir, PluginDir},
		{"legacy", LegacyPluginDir, LegacyPluginDir},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeAt(t, dir, tc.pluginIn, PluginFile, `{"name":"x","version":"1.0.0"}`)
			if err := SaveInstalledJSON(dir, &InstalledJSON{SourceType: "local", Source: "/src", InstalledAt: "now"}); err != nil {
				t.Fatal(err)
			}
			want := filepath.Join(dir, tc.want, InstalledFile)
			if _, err := os.Stat(want); err != nil {
				t.Errorf("installed.json should sit beside plugin.json at %s: %v", want, err)
			}
			ins, err := LoadInstalledJSON(dir)
			if err != nil {
				t.Fatal(err)
			}
			if ins.Source != "/src" {
				t.Errorf("Source = %q, want /src", ins.Source)
			}
		})
	}
}

func TestSaveInstalledJSON_NoPluginJSONGoesToCanonical(t *testing.T) {
	dir := t.TempDir()
	if err := SaveInstalledJSON(dir, &InstalledJSON{SourceType: "local", Source: "/src", InstalledAt: "now"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, PluginDir, InstalledFile)); err != nil {
		t.Errorf("installed.json without a plugin.json should default to %s: %v", PluginDir, err)
	}
}

func TestMarketplaceJSON_BothDirs(t *testing.T) {
	t.Run("legacy read", func(t *testing.T) {
		dir := t.TempDir()
		writeAt(t, dir, LegacyPluginDir, MarketplaceFile, `{"name":"reg","owner":{"name":"o"},"harnesses":[]}`)
		if !IsRegistryDir(dir) {
			t.Fatal("IsRegistryDir should find the legacy index")
		}
		mj, err := LoadMarketplaceJSON(dir)
		if err != nil {
			t.Fatal(err)
		}
		if mj.Name != "reg" {
			t.Errorf("Name = %q, want reg", mj.Name)
		}
	})
	t.Run("new write is canonical", func(t *testing.T) {
		dir := t.TempDir()
		if err := SaveMarketplaceJSON(dir, &MarketplaceJSON{Name: "reg", Owner: &OwnerInfo{Name: "o"}}); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, PluginDir, MarketplaceFile)); err != nil {
			t.Errorf("new index not at %s: %v", PluginDir, err)
		}
	})
	t.Run("legacy write in place", func(t *testing.T) {
		dir := t.TempDir()
		writeAt(t, dir, LegacyPluginDir, MarketplaceFile, `{"name":"old","owner":{"name":"o"},"harnesses":[]}`)
		if err := SaveMarketplaceJSON(dir, &MarketplaceJSON{Name: "new", Owner: &OwnerInfo{Name: "o"}}); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, PluginDir)); !os.IsNotExist(err) {
			t.Errorf("rewriting a legacy index must not create %s", PluginDir)
		}
		mj, err := LoadMarketplaceJSON(dir)
		if err != nil {
			t.Fatal(err)
		}
		if mj.Name != "new" {
			t.Errorf("Name = %q, want new", mj.Name)
		}
	})
}

func TestHarnessRoot(t *testing.T) {
	cases := []struct {
		path, want string
	}{
		{filepath.Join("repo", ".agents", "harness", "plugin.json"), "repo"},
		{filepath.Join("repo", ".ynh-plugin", "plugin.json"), "repo"},
		{filepath.Join(".agents", "harness", "plugin.json"), "."},
		{filepath.Join(".ynh-plugin", "marketplace.json"), "."},
		{filepath.Join("a", "b", ".agents", "harness", "installed.json"), filepath.Join("a", "b")},
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
		{filepath.Join("repo", ".ynh-plugin", "marketplace.json"), true},
		{filepath.Join(".agents", "harness", "marketplace.json"), true},
		{filepath.Join(".ynh-plugin", "marketplace.json"), true},
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
