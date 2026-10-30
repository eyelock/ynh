package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/plugin"
)

// writeLegacyHarness writes a manifest at the pre-move .ynh-plugin location.
// The plugin package's own writers no longer create that directory, so the
// fixture is written by hand.
func writeLegacyHarness(t *testing.T, dir, name string) {
	t.Helper()
	legacy := filepath.Join(dir, plugin.LegacyPluginDir)
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"name":"` + name + `","version":"0.1.0","default_vendor":"claude"}`
	if err := os.WriteFile(filepath.Join(legacy, plugin.PluginFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// wantLegacyRefusal checks err is the refusal a read command gives for a
// source tree that keeps its manifest in .ynh-plugin: it names the tree and
// the command that fixes it.
func wantLegacyRefusal(t *testing.T, err error, dir string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected a refusal for a tree that keeps its manifest in .ynh-plugin")
	}
	for _, want := range []string{dir, plugin.LegacyPluginDir, "ynd migrate " + dir} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got: %v", want, err)
		}
	}
}

// A source tree that still has its manifest in .ynh-plugin is not read (#404):
// every way into it fails with the fix, and the tree is left as it was.
func TestLoadDir_LegacyPluginDirIsRefused(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	cases := []struct {
		name string
		run  func(dir string) error
	}{
		{"DetectFormat", func(dir string) error { _, err := DetectFormat(dir); return err }},
		{"LoadDir", func(dir string) error { _, err := LoadDir(dir); return err }},
		{"ResolveEditTarget", func(dir string) error { _, _, err := ResolveEditTarget(dir); return err }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeLegacyHarness(t, dir, "legacy")

			wantLegacyRefusal(t, tc.run(dir), dir)

			if _, err := os.Stat(filepath.Join(dir, plugin.PluginDir)); !os.IsNotExist(err) {
				t.Errorf("a refused read must not create %s", plugin.PluginDir)
			}
			if _, err := os.Stat(filepath.Join(dir, plugin.LegacyPluginDir, plugin.PluginFile)); err != nil {
				t.Errorf("the user's manifest must be left where it was: %v", err)
			}
		})
	}
}

// ynh's own copies keep loading from the old layout, so an old-layout remote
// source still works: an install under harnesses/ and a clone under cache/
// are renamed in place, and nothing else is touched.
func TestLoadDir_OwnedCopiesAreMigratedOnLoad(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	cases := []struct{ name, parent string }{
		{"install", config.HarnessesDir()},
		{"cached clone", config.CacheDir()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(tc.parent, "example--old")
			writeLegacyHarness(t, dir, "old")

			if got, err := DetectFormat(dir); err != nil || got != "plugin" {
				t.Fatalf("DetectFormat = %q, %v, want plugin", got, err)
			}
			h, err := LoadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if h.Name != "old" || h.DefaultVendor != "claude" {
				t.Errorf("loaded %+v, want name old and vendor claude", h)
			}
			if _, err := os.Stat(filepath.Join(dir, plugin.PluginDir, plugin.PluginFile)); err != nil {
				t.Errorf("the copy should have moved to %s: %v", plugin.PluginDir, err)
			}
			if _, err := os.Stat(filepath.Join(dir, plugin.LegacyPluginDir)); !os.IsNotExist(err) {
				t.Errorf("%s should be gone from the copy", plugin.LegacyPluginDir)
			}
		})
	}
}

// A tree with both layouts is refused wherever it is, naming both.
func TestLoadDir_BothLayoutsAreRefused(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	for _, tc := range []struct{ name, parent string }{
		{"source tree", t.TempDir()},
		{"cached clone", config.CacheDir()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(tc.parent, "both")
			writeLegacyHarness(t, dir, "both")
			if err := plugin.SavePluginJSON(dir, &plugin.HarnessJSON{Name: "both", Version: "0.1.0"}); err != nil {
				t.Fatal(err)
			}
			_, err := LoadDir(dir)
			if err == nil || !strings.Contains(err.Error(), plugin.PluginDir) || !strings.Contains(err.Error(), plugin.LegacyPluginDir) {
				t.Errorf("want an error naming both layouts, got: %v", err)
			}
		})
	}
}

// An install from an old binary, still on .harness.json, is ynh's own copy
// and converts on load. The migrator writes the converted manifest to the
// canonical directory, never to the legacy one.
func TestLegacyHarnessFile_InstallMigratesToCanonicalDir(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	dir := filepath.Join(config.HarnessesDir(), "local--old")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"name":"old","version":"0.1.0"}`
	if err := os.WriteFile(filepath.Join(dir, plugin.HarnessFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if h.Name != "old" {
		t.Errorf("Name = %q, want old", h.Name)
	}
	if _, err := os.Stat(filepath.Join(dir, plugin.PluginDir, plugin.PluginFile)); err != nil {
		t.Errorf("migrated manifest should be at %s: %v", plugin.PluginDir, err)
	}
	if _, err := os.Stat(filepath.Join(dir, plugin.LegacyPluginDir)); !os.IsNotExist(err) {
		t.Errorf("migration must not write to %s", plugin.LegacyPluginDir)
	}
}
