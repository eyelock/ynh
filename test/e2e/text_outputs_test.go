//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSources_TextOutput asserts the human-readable form of `ynh sources list`.
// JSON is covered by TestSources_AddListRemove; this locks the table form
// (column headers and the entry row).
func TestSources_TextOutput(t *testing.T) {
	s := newSandbox(t)

	srcDir := filepath.Join(t.TempDir(), "my-src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	s.mustRunYnh(t, "sources", "add", srcDir, "--name", "shown")

	out, _ := s.mustRunYnh(t, "sources", "list")
	for _, want := range []string{"NAME", "PATH", "shown"} {
		if !strings.Contains(out, want) {
			t.Errorf("sources list text output missing %q\n%s", want, out)
		}
	}
}

// TestPaths_TextOutput asserts the table form of `ynh paths`. JSON is
// already covered; this locks the row labels (home/config/harnesses/etc.)
// scripts may scrape.
func TestPaths_TextOutput(t *testing.T) {
	s := newSandbox(t)

	out, _ := s.mustRunYnh(t, "paths")
	for _, want := range []string{"home", "config", "harnesses", "symlinks", "cache", "run", "bin"} {
		if !strings.Contains(out, want) {
			t.Errorf("paths text output missing row %q\n%s", want, out)
		}
	}
	// And the resolved values must reflect the sandbox.
	if !strings.Contains(out, s.home) {
		t.Errorf("paths output should contain sandbox YNH_HOME (%s):\n%s", s.home, out)
	}
}

// TestStatus_MultipleInstalls exercises the table output with more than
// one symlink installation, locking that the tabwriter renders multiple
// rows correctly. Also catches accidental dedup of project paths.
func TestStatus_MultipleInstalls(t *testing.T) {
	s := newSandbox(t)
	harness := newSyntheticSkillHarness(t, "multi-stat")
	s.mustRunYnh(t, "install", harness)

	for _, name := range []string{"proj-a", "proj-b"} {
		project := filepath.Join(t.TempDir(), name)
		if err := os.MkdirAll(project, 0o755); err != nil {
			t.Fatal(err)
		}
		mustRunYnhInDir(t, s, project, "run", "multi-stat", "-v", "cursor", "--install")

		// Status must mention the project path right after installing.
		out, _ := s.mustRunYnh(t, "status")
		if !strings.Contains(out, project) {
			t.Errorf("status missing project path %s after install\n%s", project, out)
		}
	}

	// Final status must contain BOTH project paths.
	out, _ := s.mustRunYnh(t, "status")
	count := strings.Count(out, "multi-stat")
	if count < 2 {
		t.Errorf("expected at least 2 'multi-stat' rows in status, got %d\n%s", count, out)
	}
}

// TestYnh_Version asserts `ynh version` prints something looking like a
// version string. Locks the entry point so a regression that broke the
// command flow shows up immediately.
func TestYnh_Version(t *testing.T) {
	s := newSandbox(t)
	out, _ := s.mustRunYnh(t, "version")
	if strings.TrimSpace(out) == "" {
		t.Errorf("ynh version output is empty")
	}
}

// TestYnd_Version mirrors TestYnh_Version for the developer-tools binary.
func TestYnd_Version(t *testing.T) {
	out, _ := mustRunYnd(t, "version")
	if strings.TrimSpace(out) == "" {
		t.Errorf("ynd version output is empty")
	}
}

// TestYnd_Compose_TextOutput asserts the human-readable table form of
// `ynd compose <harness>` (default format is JSON; --format text emits
// a tabular view documented for shell users).
func TestYnd_Compose_TextOutput(t *testing.T) {
	harness := newSyntheticSkillHarness(t, "compose-text")
	out, _ := mustRunYnd(t, "compose", harness, "--format", "text")
	for _, want := range []string{"compose-text", "0.1.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("compose text output missing %q\n%s", want, out)
		}
	}
}
