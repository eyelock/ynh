//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPrune_StaleLaunchers asserts `ynh prune` removes launcher scripts
// in ~/.ynh/bin/ for harnesses that no longer exist. Distinct from
// orphan-symlink sweeping (covered by TestPrune_Orphans).
func TestPrune_StaleLaunchers(t *testing.T) {
	s := newSandbox(t)

	// Drop a fake launcher script into bin/ that points at a nonexistent harness.
	binDir := filepath.Join(s.home, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stalePath := filepath.Join(binDir, "ghost")
	stale := "#!/bin/sh\nexec ynh run ghost\n"
	if err := os.WriteFile(stalePath, []byte(stale), 0o755); err != nil {
		t.Fatal(err)
	}

	out, _ := s.mustRunYnh(t, "prune")
	if !strings.Contains(out, "stale launcher") && !strings.Contains(out, "Removed stale launcher") {
		t.Errorf("expected prune output to mention removed stale launcher, got:\n%s", out)
	}
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Errorf("stale launcher should be gone after prune, err=%v", err)
	}
}

// TestPrune_StaleRunDirs asserts `ynh prune` removes run directories under
// ~/.ynh/run/ for harnesses that no longer exist.
func TestPrune_StaleRunDirs(t *testing.T) {
	s := newSandbox(t)

	// Drop a fake run dir for a harness that doesn't exist.
	runDir := filepath.Join(s.home, "run", "ghost-run")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "marker"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _ := s.mustRunYnh(t, "prune")
	if !strings.Contains(out, "stale run dir") && !strings.Contains(out, "Removed stale run") {
		t.Errorf("expected prune output to mention removed stale run dir, got:\n%s", out)
	}
	if _, err := os.Stat(runDir); !os.IsNotExist(err) {
		t.Errorf("stale run dir should be gone after prune, err=%v", err)
	}
}
