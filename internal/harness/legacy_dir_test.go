package harness

import (
	"os"
	"path/filepath"
	"testing"

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

// A harness that has not moved its manifest to .agents/harness keeps
// loading. This is the compatibility promise of the move: no migration, no
// warning on load, no difference in what comes back.
func TestLoadDir_LegacyPluginDirFallback(t *testing.T) {
	dir := t.TempDir()
	writeLegacyHarness(t, dir, "legacy")

	if got := DetectFormat(dir); got != "plugin" {
		t.Fatalf("DetectFormat = %q, want plugin", got)
	}
	h, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if h.Name != "legacy" || h.DefaultVendor != "claude" {
		t.Errorf("loaded %+v, want name legacy and vendor claude", h)
	}
	if _, err := os.Stat(filepath.Join(dir, plugin.PluginDir)); !os.IsNotExist(err) {
		t.Errorf("loading must not create %s: reads are side-effect free", plugin.PluginDir)
	}
}

// ynh's editor commands (include, hook, profile, focus, mcp, delegate) all
// resolve their target through ResolveEditTarget and then save through the
// plugin package. A legacy manifest must be found, and must be edited where
// it is.
func TestResolveEditTarget_LegacyPluginDir(t *testing.T) {
	dir := t.TempDir()
	writeLegacyHarness(t, dir, "legacy")

	got, installed, err := ResolveEditTarget(dir)
	if err != nil {
		t.Fatal(err)
	}
	if installed {
		t.Error("a path ref is not an installed harness")
	}
	abs, _ := filepath.Abs(dir)
	if got != abs {
		t.Errorf("dir = %q, want %q", got, abs)
	}

	hj, err := plugin.LoadPluginJSON(got)
	if err != nil {
		t.Fatal(err)
	}
	hj.Description = "edited"
	if err := plugin.SavePluginJSON(got, hj); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, plugin.PluginDir)); !os.IsNotExist(err) {
		t.Errorf("editing a legacy manifest must not create %s", plugin.PluginDir)
	}
	back, err := plugin.LoadPluginJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	if back.Description != "edited" {
		t.Errorf("edit did not land in the legacy file: %+v", back)
	}
}

// The .harness.json migrator writes the converted manifest to the canonical
// directory, never to the legacy one, so a freshly migrated harness is
// already in the documented layout.
func TestLegacyHarnessFile_MigratesToCanonicalDir(t *testing.T) {
	dir := t.TempDir()
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
