//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestRun_Cursor_InstallClean asserts that `ynh run -v cursor --install`
// from a project directory creates the expected symlink layout under
// .cursor/, and that `--clean` removes it without leaving orphans.
func TestRun_Cursor_InstallClean(t *testing.T) {
	s := newSandbox(t)
	harness := newSyntheticSkillHarness(t, "with-skill")
	s.mustRunYnh(t, "install", harness)

	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}

	runYnhInDir(t, s, project, "run", "with-skill", "-v", "cursor", "--install")

	cursorSkillsDir := filepath.Join(project, ".cursor", "skills")
	assertDirExists(t, cursorSkillsDir)
	entries, err := os.ReadDir(cursorSkillsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Errorf(".cursor/skills/ should contain at least one entry, got 0")
	}
	for _, e := range entries {
		full := filepath.Join(cursorSkillsDir, e.Name())
		info, err := os.Lstat(full)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s expected to be a symlink, got mode %v", full, info.Mode())
		}
		target, err := os.Readlink(full)
		if err != nil {
			t.Fatalf("readlink %s: %v", full, err)
		}
		if !strings.Contains(target, "with-skill") || !strings.HasPrefix(target, s.home) {
			t.Errorf("symlink %s points to %s — expected something under YNH_HOME containing 'with-skill'", full, target)
		}
	}

	runYnhInDir(t, s, project, "run", "with-skill", "-v", "cursor", "--clean")

	if _, err := os.Stat(cursorSkillsDir); err == nil {
		// The dir may legitimately remain empty after clean — only fail if it still has symlinks.
		entries, _ := os.ReadDir(cursorSkillsDir)
		for _, e := range entries {
			info, _ := os.Lstat(filepath.Join(cursorSkillsDir, e.Name()))
			if info != nil && info.Mode()&os.ModeSymlink != 0 {
				t.Errorf(".cursor/skills/%s still a symlink after --clean", e.Name())
			}
		}
	}
}

// runYnhInDir runs `ynh args...` with cwd=dir inside sandbox s.
func runYnhInDir(t *testing.T, s *sandbox, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(ynhBinary(t), args...)
	cmd.Env = append(os.Environ(), "YNH_HOME="+s.home)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ynh %s failed in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
}

// newSyntheticSkillHarness writes a harness directory with one skill,
// suitable for symlink-layout assertions.
func newSyntheticSkillHarness(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(filepath.Join(dir, ".ynh-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "skills", "hello"), 0o755); err != nil {
		t.Fatal(err)
	}
	plugin := `{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "` + name + `",
  "version": "0.1.0",
  "default_vendor": "claude"
}
`
	if err := os.WriteFile(filepath.Join(dir, ".ynh-plugin", "plugin.json"), []byte(plugin), 0o644); err != nil {
		t.Fatal(err)
	}
	skill := "---\nname: hello\ndescription: A trivial skill for E2E symlink-layout tests.\n---\n\n# hello\n"
	if err := os.WriteFile(filepath.Join(dir, "skills", "hello", "SKILL.md"), []byte(skill), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}
