//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSymlinks_StableAcrossReinstall verifies that a project's vendor
// symlinks (created by `ynh run -v <vendor> --install`) remain functional
// after the harness is reinstalled. Previously this would silently break
// if reinstall changed paths under ~/.ynh/run/ — users would lose tooling
// and not know why.
//
// Cursor uses symlinks (Codex too). Reinstall the source, re-run --install,
// then walk the project's symlinks and confirm they all resolve.
func TestSymlinks_StableAcrossReinstall(t *testing.T) {
	s := newSandbox(t)
	harness := newSyntheticSkillHarness(t, "stable")
	s.mustRunYnh(t, "install", harness)

	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	mustRunYnhInDir(t, s, project, "run", "stable", "-v", "cursor", "--install")

	skillsDir := filepath.Join(project, ".cursor", "skills")
	before, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Fatalf("reading skills dir: %v", err)
	}
	if len(before) == 0 {
		t.Fatal("setup: expected at least one skill symlink after first install")
	}

	// Reinstall the same harness — installs to the same dir, recreates content.
	s.mustRunYnh(t, "install", harness)

	// Re-run to refresh assembled output (matches the documented post-reinstall workflow).
	mustRunYnhInDir(t, s, project, "run", "stable", "-v", "cursor", "--install")

	// Every project symlink must resolve to a real file under YNH_HOME.
	after, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Fatalf("reading skills dir after reinstall: %v", err)
	}
	if len(after) == 0 {
		t.Fatal("expected skills to remain after reinstall")
	}
	for _, e := range after {
		full := filepath.Join(skillsDir, e.Name())
		// Stat (not Lstat) follows the symlink; if it fails, the link is dangling.
		if _, err := os.Stat(full); err != nil {
			t.Errorf("symlink %s does not resolve after reinstall: %v", full, err)
		}
	}
}
