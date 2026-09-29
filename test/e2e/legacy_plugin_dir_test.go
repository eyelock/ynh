//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeLegacyLayoutHarness writes a harness whose manifest is still at the
// pre-move .ynh-plugin/plugin.json location, exactly as a repository that has
// not moved it would look.
func writeLegacyLayoutHarness(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(filepath.Join(dir, ".ynh-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "` + name + `",
  "version": "0.1.0",
  "description": "still on the old layout",
  "default_vendor": "claude"
}
`
	if err := os.WriteFile(filepath.Join(dir, ".ynh-plugin", "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rules", "always-test.md"), []byte("# Always test\n\nRun the tests.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# "+name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestLegacyPluginDir_YndStillReadsIt: the developer tools validate and
// preview a harness that has not moved to .agents/harness, and leave its
// layout alone.
func TestLegacyPluginDir_YndStillReadsIt(t *testing.T) {
	dir := writeLegacyLayoutHarness(t, "old-layout")

	out, _ := mustRunYndInDir(t, dir, "validate", ".")
	if !strings.Contains(out, "valid") {
		t.Errorf("validate should accept a .ynh-plugin harness, got:\n%s", out)
	}

	out, _ = mustRunYndInDir(t, dir, "preview", "-v", "claude", ".")
	if !strings.Contains(out, "always-test") {
		t.Errorf("preview should assemble the legacy harness's rules, got:\n%s", out)
	}

	if _, err := os.Stat(filepath.Join(dir, ".agents")); !os.IsNotExist(err) {
		t.Errorf("reading a legacy harness must not create .agents/: no migration happens on read")
	}
	if _, err := os.Stat(filepath.Join(dir, ".ynh-plugin", "plugin.json")); err != nil {
		t.Errorf("the legacy manifest must be left where it was: %v", err)
	}
}

// TestLegacyPluginDir_YnhInstallsAndListsIt: the harness manager installs a
// .ynh-plugin harness from a local path and lists it like any other.
func TestLegacyPluginDir_YnhInstallsAndListsIt(t *testing.T) {
	s := newSandbox(t)
	dir := writeLegacyLayoutHarness(t, "old-layout")

	s.mustRunYnh(t, "install", dir)

	out, _ := s.mustRunYnh(t, "ls")
	if !strings.Contains(out, "old-layout") {
		t.Errorf("ls should show the installed legacy harness, got:\n%s", out)
	}

	out, _ = s.mustRunYnh(t, "info", "local/old-layout", "--format", "json")
	if !strings.Contains(out, `"still on the old layout"`) {
		t.Errorf("info should read the legacy manifest, got:\n%s", out)
	}
}

// TestCanonicalPluginDir_CreateWritesIt: ynd create scaffolds the documented
// location and nothing else.
func TestCanonicalPluginDir_CreateWritesIt(t *testing.T) {
	dir := t.TempDir()
	mustRunYndInDir(t, dir, "create", "harness", "fresh")

	assertFileExists(t, filepath.Join(dir, "fresh", ".agents", "harness", "plugin.json"))
	if _, err := os.Stat(filepath.Join(dir, "fresh", ".ynh-plugin")); !os.IsNotExist(err) {
		t.Error("ynd create must not scaffold the legacy .ynh-plugin directory")
	}
}
