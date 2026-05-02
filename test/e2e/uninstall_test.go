//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestUninstall_RemovesHarness verifies `ynh uninstall <name>` removes the
// harness directory under ~/.ynh/harnesses/, drops the entry from `ynh ls`,
// and removes the launcher script in ~/.ynh/bin/. Distinct from
// TestPrune_Orphans, which only sweeps stale symlink entries.
func TestUninstall_RemovesHarness(t *testing.T) {
	s := newSandbox(t)
	harness := newSyntheticSkillHarness(t, "doomed")
	s.mustRunYnh(t, "install", harness)

	// Sanity check: harness is visible to ls and the install dir + launcher exist.
	installDir := filepath.Join(s.home, "harnesses", "doomed")
	assertDirExists(t, installDir)
	launcher := filepath.Join(s.home, "bin", "doomed")
	if _, err := os.Stat(launcher); err != nil {
		t.Fatalf("launcher should exist before uninstall: %v", err)
	}

	out, _ := s.mustRunYnh(t, "uninstall", "doomed")
	if !strings.Contains(out, "Uninstalled") {
		t.Errorf("expected 'Uninstalled' confirmation, got: %s", out)
	}

	// Install dir gone.
	if _, err := os.Stat(installDir); !os.IsNotExist(err) {
		t.Errorf("install dir still exists after uninstall: %s", installDir)
	}
	// Launcher gone.
	if _, err := os.Stat(launcher); !os.IsNotExist(err) {
		t.Errorf("launcher still exists after uninstall: %s", launcher)
	}

	// ls --format json must report no harnesses.
	out, _ = s.mustRunYnh(t, "ls", "--format", "json")
	var got envelopeLs
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("parsing ls JSON: %v\n%s", err, out)
	}
	if len(got.Harnesses) != 0 {
		t.Errorf("expected 0 harnesses after uninstall, got %d", len(got.Harnesses))
	}
}
