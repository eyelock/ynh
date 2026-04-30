//go:build e2e

package e2e

import (
	"path/filepath"
	"testing"
)

// TestFork_BasicProvenance asserts that `ynh fork` records the source
// harness's provenance in the new harness's installed.json.forked_from.
func TestFork_BasicProvenance(t *testing.T) {
	s := newSandbox(t)
	clone := cloneAssistantsAtSHA(t)
	srcPath := filepath.Join(clone, "e2e-fixtures", "fork-source")

	s.mustRunYnh(t, "install", srcPath)

	forkDir := filepath.Join(t.TempDir(), "my-fork")
	s.mustRunYnh(t, "fork", "fork-source", "--to", forkDir)

	got := readInstalledJSON(t, forkDir)
	assertEqual(t, "source_type", got.SourceType, "local")
	assertEqual(t, "source", got.Source, forkDir)
	if got.ForkedFrom == nil {
		t.Fatal("expected forked_from to be populated")
	}
	assertEqual(t, "forked_from.source_type", got.ForkedFrom.SourceType, "local")
	assertEqual(t, "forked_from.source", got.ForkedFrom.Source, srcPath)
	assertEqual(t, "forked_from.version", got.ForkedFrom.Version, "0.1.0")
}

// TestFork_CarryForward asserts that installing a previously-forked
// directory preserves the original forked_from provenance — the chain
// of "this came from there" survives re-installation.
func TestFork_CarryForward(t *testing.T) {
	s := newSandbox(t)
	clone := cloneAssistantsAtSHA(t)
	srcPath := filepath.Join(clone, "e2e-fixtures", "fork-source")

	s.mustRunYnh(t, "install", srcPath)
	forkDir := filepath.Join(t.TempDir(), "my-fork")
	s.mustRunYnh(t, "fork", "fork-source", "--to", forkDir)

	s.mustRunYnh(t, "uninstall", "fork-source")
	s.mustRunYnh(t, "install", forkDir)

	installed := readInstalledJSON(t, filepath.Join(s.home, "harnesses", "fork-source"))
	if installed.ForkedFrom == nil {
		t.Fatal("expected forked_from to carry forward into re-installed harness")
	}
	assertEqual(t, "forked_from.source", installed.ForkedFrom.Source, srcPath)
}
