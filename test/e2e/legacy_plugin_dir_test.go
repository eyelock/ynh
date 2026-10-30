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

// assertLegacyRefusal checks that a command failed with the .ynh-plugin
// refusal: it names the tree and `ynd migrate`, and prints nothing on stdout.
func assertLegacyRefusal(t *testing.T, stdout, stderr string, err error, dir string) {
	t.Helper()
	if err == nil {
		t.Fatalf("a source tree that keeps its manifest in .ynh-plugin must be refused\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	combined := stdout + stderr
	if !strings.Contains(combined, ".ynh-plugin/") || !strings.Contains(combined, "ynd migrate "+dir) {
		t.Errorf("the refusal should name .ynh-plugin/ and `ynd migrate %s`, got:\nstdout:\n%s\nstderr:\n%s", dir, stdout, stderr)
	}
}

// assertLegacyTreeUntouched checks that a refused read left the user's tree as it was.
func assertLegacyTreeUntouched(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(dir, ".agents")); !os.IsNotExist(err) {
		t.Errorf("a refused read must not create .agents/")
	}
	if _, err := os.Stat(filepath.Join(dir, ".ynh-plugin", "plugin.json")); err != nil {
		t.Errorf("the legacy manifest must be left where it was: %v", err)
	}
}

// TestLegacyPluginDir_YndRefusesIt: the developer tools no longer read a
// harness that keeps its manifest in .ynh-plugin. They say so, name
// `ynd migrate`, and leave the tree alone.
func TestLegacyPluginDir_YndRefusesIt(t *testing.T) {
	dir := writeLegacyLayoutHarness(t, "old-layout")
	for _, args := range [][]string{
		{"validate", dir},
		{"preview", "-v", "claude", dir},
	} {
		t.Run(args[0], func(t *testing.T) {
			out, errOut, err := runYndInDirEnv(t, dir, nil, args...)
			assertLegacyRefusal(t, out, errOut, err, dir)
			assertLegacyTreeUntouched(t, dir)
		})
	}
}

// TestLegacyPluginDir_YndMigrateMovesIt: ynd migrate is the fix the refusal
// names, and still reads the old layout. It lists the move, makes it, and
// afterwards the tree validates and previews.
func TestLegacyPluginDir_YndMigrateMovesIt(t *testing.T) {
	dir := writeLegacyLayoutHarness(t, "old-layout")

	out, _ := mustRunYndInDir(t, dir, "migrate", "--dry-run", dir)
	if !strings.Contains(out, ".ynh-plugin/ → .agents/harness/") {
		t.Errorf("dry run should list the move, got:\n%s", out)
	}
	assertLegacyTreeUntouched(t, dir)

	out, _ = mustRunYndInDir(t, dir, "migrate", "-y", dir)
	assertFileExists(t, filepath.Join(dir, ".agents", "harness", "plugin.json"))
	if _, err := os.Lstat(filepath.Join(dir, ".ynh-plugin")); !os.IsNotExist(err) {
		t.Errorf(".ynh-plugin should be gone after migrate:\n%s", out)
	}

	out, errOut := mustRunYndInDir(t, dir, "validate", dir)
	if !strings.Contains(out, "valid") || errOut != "" {
		t.Errorf("validate after migrate should be clean and silent, stdout:\n%s\nstderr:\n%s", out, errOut)
	}
	out, _ = mustRunYndInDir(t, dir, "preview", "-v", "claude", ".")
	if !strings.Contains(out, "always-test") {
		t.Errorf("preview after migrate should assemble the rules, got:\n%s", out)
	}
}

// TestLegacyPluginDir_YnhRefusesIt: the harness manager refuses to install,
// check or run a local path that keeps its manifest in .ynh-plugin, and never
// moves a user's tree.
func TestLegacyPluginDir_YnhRefusesIt(t *testing.T) {
	s := newSandbox(t)
	dir := writeLegacyLayoutHarness(t, "old-layout")

	for _, args := range [][]string{
		{"install", dir},
		{"run", dir, "-v", "claude"},
	} {
		t.Run(args[0], func(t *testing.T) {
			out, errOut, err := s.runYnh(t, args...)
			assertLegacyRefusal(t, out, errOut, err, dir)
			assertLegacyTreeUntouched(t, dir)
		})
	}
	out, _ := s.mustRunYnh(t, "ls")
	if strings.Contains(out, "old-layout") {
		t.Errorf("a refused install must not register the harness, got:\n%s", out)
	}

	// After ynd migrate the same path installs.
	mustRunYndInDir(t, dir, "migrate", "-y", dir)
	s.mustRunYnh(t, "install", dir)
	out, _ = s.mustRunYnh(t, "ls")
	if !strings.Contains(out, "old-layout") {
		t.Errorf("ls should show the migrated harness, got:\n%s", out)
	}
}

// TestLegacyPluginDir_BothLayoutsAreRefused: a tree that has both is an error
// naming both, and neither manifest changes.
func TestLegacyPluginDir_BothLayoutsAreRefused(t *testing.T) {
	dir := writeLegacyLayoutHarness(t, "both")
	canonical := filepath.Join(dir, ".agents", "harness", "plugin.json")
	if err := os.MkdirAll(filepath.Dir(canonical), 0o755); err != nil {
		t.Fatal(err)
	}
	const body = `{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"both","version":"0.1.0"}`
	if err := os.WriteFile(canonical, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	out, errOut, err := runYndInDirEnv(t, dir, nil, "validate", dir)
	if err == nil {
		t.Fatalf("validate should fail on a tree with both layouts:\n%s", out)
	}
	if combined := out + errOut; !strings.Contains(combined, ".agents/harness") || !strings.Contains(combined, ".ynh-plugin") || !strings.Contains(combined, "nothing was merged") {
		t.Errorf("the error should name both layouts, got:\n%s", combined)
	}
	if got, _ := os.ReadFile(canonical); string(got) != body {
		t.Errorf("canonical manifest changed: %s", got)
	}
	assertFileExists(t, filepath.Join(dir, ".ynh-plugin", "plugin.json"))
}

// TestLegacyPluginDir_RemoteSourceStillInstalls: a remote repository that has
// not moved to .agents/harness keeps working. ynh renames the manifest
// directory inside its own copies, the clone under cache/ and the install
// under harnesses/, and never in a tree the user owns. The upstream is a git
// repository served over file://, standing in for a remote.
func TestLegacyPluginDir_RemoteSourceStillInstalls(t *testing.T) {
	s := newSandbox(t)
	upstream := filepath.Join(t.TempDir(), "upstream")
	root := writeLegacyLayoutHarness(t, "remote-old")
	initRepo(t, upstream)
	if err := os.CopyFS(upstream, os.DirFS(root)); err != nil {
		t.Fatal(err)
	}
	mustGit(t, upstream, "add", "-A")
	mustGit(t, upstream, "commit", "--quiet", "-m", "old layout")
	url := "file://" + upstream

	s.mustRunYnh(t, "install", url)

	out, _ := s.mustRunYnh(t, "ls")
	if !strings.Contains(out, "remote-old") {
		t.Fatalf("ls should show the remote harness, got:\n%s", out)
	}
	installDir := filepath.Join(s.home, "harnesses", "local--remote-old")
	assertFileExists(t, filepath.Join(installDir, ".agents", "harness", "plugin.json"))
	if _, err := os.Lstat(filepath.Join(installDir, ".ynh-plugin")); !os.IsNotExist(err) {
		t.Errorf("the install should have moved off .ynh-plugin")
	}
	// The upstream working copy, which is the "remote" and not ynh's, is untouched.
	assertFileExists(t, filepath.Join(upstream, ".ynh-plugin", "plugin.json"))
	if _, err := os.Stat(filepath.Join(upstream, ".agents")); !os.IsNotExist(err) {
		t.Errorf("ynh must not touch the remote's own tree")
	}

	// A later update fetches the old layout again over the migrated clone.
	if err := os.WriteFile(filepath.Join(upstream, "AGENTS.md"), []byte("# remote-old\n\nUpdated.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, upstream, "add", "-A")
	mustGit(t, upstream, "commit", "--quiet", "-m", "update")
	s.mustRunYnh(t, "update", "local/remote-old")
	s.mustRunYnh(t, "info", "local/remote-old")
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
