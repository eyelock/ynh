//go:build e2e

package e2e

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestFork_UpdateRejected verifies the documented constraint that
// `ynh update` errors on a forked harness — forks may freely diverge
// from the original and so cannot pull upstream changes mechanically.
//
// Locks the error message wording: callers (CI, IDE plugins) parse for
// "fork" in the failure mode.
func TestFork_UpdateRejected(t *testing.T) {
	s := newSandbox(t)
	clone := cloneAssistantsAtSHA(t)
	srcPath := filepath.Join(clone, "e2e-fixtures", "fork-source")

	s.mustRunYnh(t, "install", srcPath)

	forkDir := filepath.Join(t.TempDir(), "my-fork-ur")
	// Develop forbids same-name fork while the source is installed; use --name
	// to disambiguate, then update against the renamed fork.
	s.mustRunYnh(t, "fork", "fork-source", "--to", forkDir, "--name", "my-fork-ur")
	s.mustRunYnh(t, "uninstall", "fork-source")
	s.mustRunYnh(t, "install", forkDir)

	_, errOut, err := s.runYnh(t, "update", "my-fork-ur")
	if err == nil {
		t.Fatalf("expected `ynh update <fork>` to fail, got success\nstderr:\n%s", errOut)
	}
	if !strings.Contains(errOut, "fork") {
		t.Errorf("expected error to mention 'fork', got: %s", errOut)
	}
}
