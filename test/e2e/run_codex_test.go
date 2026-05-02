//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRun_Codex_InstallClean asserts that `ynh run -v codex --install`
// from a project directory creates the expected symlink layout under
// .codex/ (Codex uses symlinks like Cursor), and that `--clean` removes
// the symlinks.
func TestRun_Codex_InstallClean(t *testing.T) {
	s := newSandbox(t)
	harness := newSyntheticSkillHarness(t, "with-skill-codex")
	s.mustRunYnh(t, "install", harness)

	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}

	mustRunYnhInDir(t, s, project, "run", "with-skill-codex", "-v", "codex", "--install")

	codexSkillsDir := filepath.Join(project, ".codex", "skills")
	assertDirExists(t, codexSkillsDir)
	entries, err := os.ReadDir(codexSkillsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Errorf(".codex/skills/ should contain at least one entry, got 0")
	}
	for _, e := range entries {
		full := filepath.Join(codexSkillsDir, e.Name())
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
		if !strings.Contains(target, "with-skill-codex") || !strings.HasPrefix(target, s.home) {
			t.Errorf("symlink %s points to %s — expected something under YNH_HOME containing 'with-skill-codex'", full, target)
		}
	}

	mustRunYnhInDir(t, s, project, "run", "with-skill-codex", "-v", "codex", "--clean")

	if _, err := os.Stat(codexSkillsDir); err == nil {
		entries, _ := os.ReadDir(codexSkillsDir)
		for _, e := range entries {
			info, _ := os.Lstat(filepath.Join(codexSkillsDir, e.Name()))
			if info != nil && info.Mode()&os.ModeSymlink != 0 {
				t.Errorf(".codex/skills/%s still a symlink after --clean", e.Name())
			}
		}
	}
}
