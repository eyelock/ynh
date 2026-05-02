//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRun_Claude_InstallClean asserts that `ynh run -v claude --install`
// assembles a Claude-native plugin layout under ~/.ynh/run/<harness>/ and
// reports that no symlink installation is needed (Claude uses native plugin
// loading via --plugin-dir, not symlinks like Cursor/Codex).
//
// `--clean` is likewise a no-op message — there are no symlinks to remove.
func TestRun_Claude_InstallClean(t *testing.T) {
	s := newSandbox(t)
	harness := newSyntheticSkillHarness(t, "with-skill-claude")
	s.mustRunYnh(t, "install", harness)

	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}

	out, _ := mustRunYnhInDir(t, s, project, "run", "with-skill-claude", "-v", "claude", "--install")
	if !strings.Contains(out, "native plugin loading") {
		t.Errorf("expected 'native plugin loading' message, got:\n%s", out)
	}

	// Assembly happened — assert the Claude-native layout exists in the run dir.
	runDir := filepath.Join(s.home, "run", "with-skill-claude")
	assertFileExists(t, filepath.Join(runDir, ".claude-plugin", "plugin.json"))
	assertFileExists(t, filepath.Join(runDir, ".claude", "skills", "hello", "SKILL.md"))

	// CLAUDE.md is only generated when the harness has an instructions file
	// (AGENTS.md / instructions.md). The synthetic harness has none, so we
	// don't assert on it here — the @AGENTS.md import is covered by unit tests.

	// --clean is a no-op for native vendors but should still exit 0.
	out, _ = mustRunYnhInDir(t, s, project, "run", "with-skill-claude", "-v", "claude", "--clean")
	if !strings.Contains(out, "no symlinks to clean") {
		t.Errorf("expected 'no symlinks to clean' message, got:\n%s", out)
	}

	// Project dir should NOT have a .claude/ — Claude uses --plugin-dir, not symlinks.
	if _, err := os.Stat(filepath.Join(project, ".claude")); err == nil {
		t.Errorf("project should not have .claude/ — Claude uses native plugin loading")
	}
}
