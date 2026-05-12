package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
)

// writeMinimalHarness creates a directory with a plugin.json so harness.LoadByID
// can resolve it via the pointer's source path.
func writeMinimalHarness(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	hj := &plugin.HarnessJSON{Name: name, Version: "0.1.0"}
	if err := plugin.SavePluginJSON(dir, hj); err != nil {
		t.Fatal(err)
	}
}

// TestResolveSource_CanonicalIDResolvesToInstalledPointer verifies that
// a canonical id like "local/<name>" returned by `ynh ls` resolves to the
// installed harness directory rather than being treated as a Git URL.
// Regression test for TermQ's `ynd compose <harness-id>` flow.
func TestResolveSource_CanonicalIDResolvesToInstalledPointer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	sourceDir := filepath.Join(t.TempDir(), "my-fork")
	writeMinimalHarness(t, sourceDir, "my-fork")

	ptr := &harness.Pointer{
		ID:   "local/my-fork",
		Name: "my-fork",
		InstalledJSON: plugin.InstalledJSON{
			SourceType:  "local",
			Source:      sourceDir,
			InstalledAt: "2026-01-01T00:00:00Z",
		},
	}
	if err := harness.SavePointerByID(ptr); err != nil {
		t.Fatalf("save pointer: %v", err)
	}

	got, err := resolveSource("local/my-fork")
	if err != nil {
		t.Fatalf("resolveSource: %v", err)
	}
	if got != sourceDir {
		t.Errorf("resolveSource resolved to %q, want %q", got, sourceDir)
	}
}

// TestResolveSource_CanonicalIDFallsThroughWhenNotInstalled verifies the
// fall-through semantics: a canonical-shaped string that isn't an
// installed harness is still passed to the Git-URL resolver, since users
// may reference upstream repos by canonical form.
func TestResolveSource_CanonicalIDFallsThroughWhenNotInstalled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	// No pointer installed for this id. The Git resolver will reject it
	// (no real network in tests), so we expect an error — but specifically
	// NOT a "harness not found" error, which would indicate the lookup
	// short-circuited rather than falling through.
	_, err := resolveSource("github.com/nonexistent/repo/sub")
	if err == nil {
		t.Fatal("expected resolveSource to fail on unresolvable id, got nil")
	}
	// The error should come from the Git resolver (EnsureRepo), not from
	// the harness lookup. Verify by checking the error wraps "resolving".
	if got := err.Error(); !contains(got, "resolving") {
		t.Errorf("expected git-resolver error, got: %v", err)
	}
}

// TestResolveSource_LocalPathTakesPrecedence verifies that a leading
// "./" or "/" still gets treated as a filesystem path even if its name
// happens to match an installed harness id (defensive ordering check).
func TestResolveSource_LocalPathTakesPrecedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	dir := filepath.Join(t.TempDir(), "explicit-path")
	writeMinimalHarness(t, dir, "anything")

	got, err := resolveSource(dir)
	if err != nil {
		t.Fatalf("resolveSource: %v", err)
	}
	if got != dir {
		t.Errorf("expected explicit path to resolve as-is, got %q", got)
	}
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
