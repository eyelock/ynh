package plugin

import (
	"os"
	"path/filepath"
	"strings"
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
	if got := MisplacedManifestFiles(dir); len(got) != 1 || got[0] != ".ynh-plugin/plugin.json" {
		t.Errorf("MisplacedManifestFiles = %v, want the shadowed .ynh-plugin/plugin.json", got)
	}
}

const (
	manifestBody    = `{"name":"x","version":"1.0.0"}`
	installedBody   = `{"source_type":"local","source":"/src","installed_at":"now"}`
	marketplaceBody = `{"name":"reg","owner":{"name":"o"},"harnesses":[]}`
)

func TestManifestDir(t *testing.T) {
	cases := []struct {
		name   string
		files  map[string]string // file -> manifest dir
		want   string
		wantOK bool
	}{
		{"canonical", map[string]string{PluginFile: PluginDir}, PluginDir, true},
		{"legacy", map[string]string{PluginFile: LegacyPluginDir}, LegacyPluginDir, true},
		{"siblings do not decide", map[string]string{InstalledFile: PluginDir, MarketplaceFile: PluginDir}, "", false},
		{"legacy plugin, canonical sibling", map[string]string{PluginFile: LegacyPluginDir, InstalledFile: PluginDir}, LegacyPluginDir, true},
		{"none", nil, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for f, md := range tc.files {
				writeAt(t, dir, md, f, "{}")
			}
			got, ok := ManifestDir(dir)
			if got != tc.want || ok != tc.wantOK {
				t.Errorf("ManifestDir = (%q, %v), want (%q, %v)", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestMisplacedManifestFiles(t *testing.T) {
	type file struct{ md, name string }
	cases := []struct {
		name  string
		files []file
		want  []string
	}{
		{"canonical only", []file{{PluginDir, PluginFile}, {PluginDir, InstalledFile}, {PluginDir, MarketplaceFile}}, nil},
		{"legacy only", []file{{LegacyPluginDir, PluginFile}, {LegacyPluginDir, InstalledFile}, {LegacyPluginDir, MarketplaceFile}}, nil},
		{"canonical plugin alone", []file{{PluginDir, PluginFile}}, nil},
		{"legacy plugin alone", []file{{LegacyPluginDir, PluginFile}}, nil},
		{"canonical plugin, legacy siblings",
			[]file{{PluginDir, PluginFile}, {LegacyPluginDir, InstalledFile}, {LegacyPluginDir, MarketplaceFile}},
			[]string{".ynh-plugin/installed.json", ".ynh-plugin/marketplace.json"}},
		{"legacy plugin, canonical sibling",
			[]file{{LegacyPluginDir, PluginFile}, {PluginDir, InstalledFile}},
			[]string{".agents/harness/installed.json"}},
		{"shadowed plugin and sibling",
			[]file{{PluginDir, PluginFile}, {LegacyPluginDir, PluginFile}, {LegacyPluginDir, InstalledFile}},
			[]string{".ynh-plugin/plugin.json", ".ynh-plugin/installed.json"}},
		{"no plugin.json anywhere", []file{{PluginDir, InstalledFile}, {LegacyPluginDir, MarketplaceFile}}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, f := range tc.files {
				writeAt(t, dir, f.md, f.name, "{}")
			}
			got := MisplacedManifestFiles(dir)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("MisplacedManifestFiles = %v, want %v", got, tc.want)
			}
		})
	}
}

// A tree with plugin.json in .agents/harness reads every sibling from
// .agents/harness too, never from .ynh-plugin.
func TestMixedTree_SiblingsFollowPluginJSON(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, PluginDir, PluginFile, manifestBody)
	writeAt(t, dir, LegacyPluginDir, MarketplaceFile, marketplaceBody)

	if want := filepath.Join(dir, PluginDir, MarketplaceFile); MarketplacePath(dir) != want {
		t.Errorf("MarketplacePath = %q, want %q beside plugin.json", MarketplacePath(dir), want)
	}
	if IsRegistryDir(dir) {
		t.Error("IsRegistryDir must agree with LoadMarketplaceJSON and ignore an index split from plugin.json")
	}

	writeAt(t, dir, PluginDir, MarketplaceFile, `{"name":"canonical","owner":{"name":"o"},"harnesses":[]}`)
	mj, err := LoadMarketplaceJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	if mj.Name != "canonical" {
		t.Errorf("Name = %q, want canonical", mj.Name)
	}
}

// An install made before this change can have plugin.json in .agents/harness
// and installed.json in .ynh-plugin. Its provenance must still load.
func TestMixedTree_InstalledJSONFallback(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, PluginDir, PluginFile, manifestBody)
	legacy := writeAt(t, dir, LegacyPluginDir, InstalledFile, installedBody)

	if got := InstalledPath(dir); got != legacy {
		t.Errorf("InstalledPath = %q, want the split %q", got, legacy)
	}
	ins, err := LoadInstalledJSON(dir)
	if err != nil {
		t.Fatalf("provenance lost: %v", err)
	}
	if ins.Source != "/src" {
		t.Errorf("Source = %q, want /src", ins.Source)
	}
}

// Rewriting provenance on a split tree puts it beside plugin.json and removes
// the stale copy, so ynh never leaves a split behind.
func TestSaveInstalledJSON_HealsSplitTree(t *testing.T) {
	cases := []struct{ pluginIn, staleIn string }{
		{PluginDir, LegacyPluginDir},
		{LegacyPluginDir, PluginDir},
	}
	for _, tc := range cases {
		t.Run(tc.pluginIn, func(t *testing.T) {
			dir := t.TempDir()
			writeAt(t, dir, tc.pluginIn, PluginFile, manifestBody)
			stale := writeAt(t, dir, tc.staleIn, InstalledFile, installedBody)

			if err := SaveInstalledJSON(dir, &InstalledJSON{SourceType: "local", Source: "/new", InstalledAt: "now"}); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(dir, tc.pluginIn, InstalledFile)); err != nil {
				t.Errorf("installed.json not beside plugin.json: %v", err)
			}
			if _, err := os.Stat(stale); !os.IsNotExist(err) {
				t.Errorf("stale %s should be gone: %v", stale, err)
			}
			if got := MisplacedManifestFiles(dir); got != nil {
				t.Errorf("tree still split after save: %v", got)
			}
			ins, err := LoadInstalledJSON(dir)
			if err != nil {
				t.Fatal(err)
			}
			if ins.Source != "/new" {
				t.Errorf("Source = %q, want /new", ins.Source)
			}
		})
	}
}

// A new sibling of a legacy plugin.json joins it, even when a stray copy of
// that sibling sits in .agents/harness.
func TestSaveMarketplaceJSON_FollowsPluginJSON(t *testing.T) {
	dir := t.TempDir()
	writeAt(t, dir, LegacyPluginDir, PluginFile, manifestBody)
	writeAt(t, dir, PluginDir, MarketplaceFile, marketplaceBody)

	if err := SaveMarketplaceJSON(dir, &MarketplaceJSON{Name: "new", Owner: &OwnerInfo{Name: "o"}}); err != nil {
		t.Fatal(err)
	}
	mj, err := LoadMarketplaceJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	if mj.Name != "new" {
		t.Errorf("Name = %q, want new", mj.Name)
	}
	if MarketplacePath(dir) != filepath.Join(dir, LegacyPluginDir, MarketplaceFile) {
		t.Errorf("MarketplacePath = %q, want beside the legacy plugin.json", MarketplacePath(dir))
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
