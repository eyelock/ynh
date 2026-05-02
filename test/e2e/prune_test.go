//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPrune_Orphans asserts that `ynh prune` detects and removes
// installations whose project directory no longer exists, while leaving
// live installations alone.
func TestPrune_Orphans(t *testing.T) {
	s := newSandbox(t)
	harness := newSyntheticSkillHarness(t, "prunable")
	s.mustRunYnh(t, "install", harness)

	live := filepath.Join(t.TempDir(), "live")
	orphan := filepath.Join(t.TempDir(), "orphan")
	if err := os.MkdirAll(live, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(orphan, 0o755); err != nil {
		t.Fatal(err)
	}

	mustRunYnhInDir(t, s, live, "run", "prunable", "-v", "cursor", "--install")
	mustRunYnhInDir(t, s, orphan, "run", "prunable", "-v", "cursor", "--install")

	// Confirm both installations are recorded by status.
	out, _ := s.mustRunYnh(t, "status")
	if !strings.Contains(out, live) || !strings.Contains(out, orphan) {
		t.Fatalf("status missing one of the install paths:\n%s", out)
	}

	if err := os.RemoveAll(orphan); err != nil {
		t.Fatal(err)
	}

	pruneOut, _ := s.mustRunYnh(t, "prune")
	if !strings.Contains(pruneOut, orphan) {
		t.Errorf("prune output should mention orphan project %q:\n%s", orphan, pruneOut)
	}

	out, _ = s.mustRunYnh(t, "status")
	if strings.Contains(out, orphan) {
		t.Errorf("post-prune status still references orphan %q:\n%s", orphan, out)
	}
	if !strings.Contains(out, live) {
		t.Errorf("post-prune status no longer references live install %q:\n%s", live, out)
	}
}
