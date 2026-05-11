//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"testing"
)

// TestYnd_Export_BasicLayout asserts `ynd export <harness> -o <dir>` writes
// vendor-specific layouts for all three vendors by default. Pure assembly,
// no install side effects.
func TestYnd_Export_BasicLayout(t *testing.T) {
	harness := newSyntheticSkillHarness(t, "exported")
	out := filepath.Join(t.TempDir(), "out")

	mustRunYnd(t, "export", "--harness", harness, "-o", out)

	for _, vendor := range []string{"claude", "codex", "cursor"} {
		dir := filepath.Join(out, vendor)
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("expected %s vendor layout at %s, got err=%v", vendor, dir, err)
		}
	}

	// Each vendor layout has skills/<name>/SKILL.md at the top level
	// and a vendor-specific plugin manifest dir alongside it.
	assertFileExists(t, filepath.Join(out, "claude", "skills", "hello", "SKILL.md"))
	assertFileExists(t, filepath.Join(out, "claude", ".claude-plugin", "plugin.json"))
	assertFileExists(t, filepath.Join(out, "cursor", "skills", "hello", "SKILL.md"))
	assertFileExists(t, filepath.Join(out, "cursor", ".cursor-plugin", "plugin.json"))
}

// TestYnd_Export_VendorFilter asserts `-v <vendor>` restricts output to a
// single vendor's layout — only .claude/ should be produced for `-v claude`.
func TestYnd_Export_VendorFilter(t *testing.T) {
	harness := newSyntheticSkillHarness(t, "exported-claude")
	out := filepath.Join(t.TempDir(), "out")

	mustRunYnd(t, "export", "--harness", harness, "-v", "claude", "-o", out)

	if _, err := os.Stat(filepath.Join(out, "claude")); err != nil {
		t.Errorf("expected claude/ output, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "cursor")); err == nil {
		t.Errorf("cursor/ output should not exist when -v claude was specified")
	}
	if _, err := os.Stat(filepath.Join(out, "codex")); err == nil {
		t.Errorf("codex/ output should not exist when -v claude was specified")
	}
}
