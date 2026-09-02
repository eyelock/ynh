package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A path that does not exist and a path that exists but is not a harness were
// reported identically, and the shared message asserted the second. A caller
// who mistyped went looking for a missing manifest instead of a missing
// directory, which is where the diagnosis time went (#334).
func TestLoadOrSynthesizeHarness_DistinguishesAbsenceFromMalformed(t *testing.T) {
	root := t.TempDir()

	missing := filepath.Join(root, "not-here")
	empty := filepath.Join(root, "empty")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}

	_, missingErr := loadOrSynthesizeHarness(missing)
	if missingErr == nil {
		t.Fatal("a missing directory must be an error")
	}
	_, emptyErr := loadOrSynthesizeHarness(empty)
	if emptyErr == nil {
		t.Fatal("a directory with no manifest must be an error")
	}

	// The bug, stated directly: these two were the same string.
	if missingErr.Error() == emptyErr.Error() {
		t.Fatalf("absence and malformed report identically:\n  %v", missingErr)
	}

	if !strings.Contains(missingErr.Error(), "not found") {
		t.Errorf("a missing directory should say so, got: %v", missingErr)
	}
	if strings.Contains(missingErr.Error(), "harness manifest") {
		t.Errorf("a missing directory must not be reported as a manifest problem, got: %v", missingErr)
	}

	// The existing message for a real-but-unusable directory is unchanged.
	if !strings.Contains(emptyErr.Error(), "has no harness manifest or AGENTS.md") {
		t.Errorf("an existing non-harness directory should keep its message, got: %v", emptyErr)
	}
}

// A file is not a directory, and saying "has no harness manifest" about one is
// the same category of wrong.
func TestLoadOrSynthesizeHarness_RejectsAFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := loadOrSynthesizeHarness(f)
	if err == nil {
		t.Fatal("a file is not a harness source")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("expected a not-a-directory error, got: %v", err)
	}
}

// The guard must not reject a directory that IS a harness: it runs before the
// migration chain, so a mistake here would break every local install.
func TestLoadOrSynthesizeHarness_AcceptsARealHarness(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, ".ynh-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"name":"demo","version":"1.0.0","description":"d"}`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	h, err := loadOrSynthesizeHarness(dir)
	if err != nil {
		t.Fatalf("a real harness must still load: %v", err)
	}
	if h.Name != "demo" {
		t.Errorf("name = %q, want demo", h.Name)
	}
}

// And a bare AGENTS.md directory must still be synthesized, which is the
// branch the guard sits directly above.
func TestLoadOrSynthesizeHarness_StillSynthesizesFromAgentsMD(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := loadOrSynthesizeHarness(dir)
	if err != nil {
		t.Fatalf("a bare AGENTS.md directory must still synthesize: %v", err)
	}
	if h.Name != filepath.Base(dir) {
		t.Errorf("name = %q, want %q", h.Name, filepath.Base(dir))
	}
}
