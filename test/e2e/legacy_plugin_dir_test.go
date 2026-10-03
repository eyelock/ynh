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

// assertDeprecationWarning checks that stderr carries the .ynh-plugin
// deprecation warning for dir exactly once, and stdout none of it.
func assertDeprecationWarning(t *testing.T, stdout, stderr, dir string) {
	t.Helper()
	if n := strings.Count(stderr, "keeps its manifest in .ynh-plugin/"); n != 1 {
		t.Errorf("want one deprecation warning on stderr, got %d:\n%s", n, stderr)
	}
	if !strings.Contains(stderr, "ynd migrate "+dir) {
		t.Errorf("the warning should name ynd migrate %s, got:\n%s", dir, stderr)
	}
	if strings.Contains(stdout, "deprecated") {
		t.Errorf("the warning must not reach stdout, got:\n%s", stdout)
	}
}

// TestLegacyPluginDir_YndStillReadsIt: the developer tools validate and
// preview a harness that has not moved to .agents/harness, warn that the
// layout is deprecated, and leave it alone: reading never migrates.
func TestLegacyPluginDir_YndStillReadsIt(t *testing.T) {
	dir := writeLegacyLayoutHarness(t, "old-layout")

	out, errOut := mustRunYndInDir(t, dir, "validate", dir)
	if !strings.Contains(out, "valid") {
		t.Errorf("validate should accept a .ynh-plugin harness, got:\n%s", out)
	}
	assertDeprecationWarning(t, out, errOut, dir)

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

// TestLegacyPluginDir_YndMigrateMovesIt: ynd migrate is the fix the warning
// names. It lists the move, makes it, and afterwards validate is silent.
func TestLegacyPluginDir_YndMigrateMovesIt(t *testing.T) {
	dir := writeLegacyLayoutHarness(t, "old-layout")

	out, _ := mustRunYndInDir(t, dir, "migrate", "--dry-run", dir)
	if !strings.Contains(out, ".ynh-plugin/ → .agents/harness/") {
		t.Errorf("dry run should list the move, got:\n%s", out)
	}

	out, errOut := mustRunYndInDir(t, dir, "migrate", "-y", dir)
	if strings.Contains(errOut, "deprecated") {
		t.Errorf("migrate must not warn about the tree it is migrating, got:\n%s", errOut)
	}
	assertFileExists(t, filepath.Join(dir, ".agents", "harness", "plugin.json"))
	if _, err := os.Lstat(filepath.Join(dir, ".ynh-plugin")); !os.IsNotExist(err) {
		t.Errorf(".ynh-plugin should be gone after migrate:\n%s", out)
	}

	out, errOut = mustRunYndInDir(t, dir, "validate", dir)
	if !strings.Contains(out, "valid") || errOut != "" {
		t.Errorf("validate after migrate should be clean and silent, stdout:\n%s\nstderr:\n%s", out, errOut)
	}
}

// TestLegacyPluginDir_YnhInstallsAndListsIt: the harness manager installs a
// .ynh-plugin harness from a local path and lists it like any other. A local
// install is a pointer to the user's tree, so ynh warns rather than moving it,
// and the JSON on stdout stays parseable.
func TestLegacyPluginDir_YnhInstallsAndListsIt(t *testing.T) {
	s := newSandbox(t)
	dir := writeLegacyLayoutHarness(t, "old-layout")

	s.mustRunYnh(t, "install", dir)

	out, errOut := s.mustRunYnh(t, "ls")
	if !strings.Contains(out, "old-layout") {
		t.Errorf("ls should show the installed legacy harness, got:\n%s", out)
	}
	assertDeprecationWarning(t, out, errOut, dir)

	out, errOut = s.mustRunYnh(t, "info", "local/old-layout", "--format", "json")
	if !strings.Contains(out, `"still on the old layout"`) {
		t.Errorf("info should read the legacy manifest, got:\n%s", out)
	}
	if !strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("info --format json stdout must be JSON alone, got:\n%s", out)
	}
	assertDeprecationWarning(t, out, errOut, dir)

	if _, err := os.Stat(filepath.Join(dir, ".ynh-plugin", "plugin.json")); err != nil {
		t.Errorf("ynh must not move a user's source tree: %v", err)
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
