//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInstall_RefRejectedWithLocalPath verifies the documented constraint
// that --ref only applies to git-source installs. A local-path install
// with --ref must error clearly rather than silently ignoring the ref.
func TestInstall_RefRejectedWithLocalPath(t *testing.T) {
	s := newSandbox(t)
	harness := newSyntheticSkillHarness(t, "ref-local")

	_, errOut, err := s.runYnh(t, "install", harness, "--ref", "v1.0.0")
	if err == nil {
		t.Fatalf("expected --ref + local path to fail, got success")
	}
	if !strings.Contains(errOut, "--ref") || !strings.Contains(errOut, "local") {
		t.Errorf("expected error to mention '--ref' and 'local', got: %s", errOut)
	}
}

// TestInstall_PickFiltersFiles asserts that includes[].pick selects a
// subset of artifacts from the source. Files outside the pick list must
// not appear in the assembled run directory.
//
// Uses a local-path include (no git needed) — exercises the same
// resolver+assembler pick path as git-source includes.
func TestInstall_PickFiltersFiles(t *testing.T) {
	s := newSandbox(t)

	// Build an "upstream" directory containing two skills.
	upstream := filepath.Join(t.TempDir(), "upstream")
	for _, name := range []string{"keep", "drop"} {
		dir := filepath.Join(upstream, "skills", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := fmt.Sprintf("---\nname: %s\ndescription: marker for %s skill.\n---\n", name, name)
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Harness picks only skills/keep.
	harness := filepath.Join(t.TempDir(), "picky")
	if err := os.MkdirAll(filepath.Join(harness, ".ynh-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "picky",
  "version": "0.1.0",
  "default_vendor": "claude",
  "includes": [{"local": %q, "pick": ["skills/keep"]}]
}
`, upstream)
	if err := os.WriteFile(filepath.Join(harness, ".ynh-plugin", "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	s.mustRunYnh(t, "install", harness)

	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	mustRunYnhInDir(t, s, project, "run", "picky", "-v", "claude", "--install")

	runDir := filepath.Join(s.home, "run", "picky")
	keep := filepath.Join(runDir, ".claude", "skills", "keep", "SKILL.md")
	drop := filepath.Join(runDir, ".claude", "skills", "drop", "SKILL.md")

	if _, err := os.Stat(keep); err != nil {
		t.Errorf("expected skills/keep to be assembled, got err=%v", err)
	}
	if _, err := os.Stat(drop); err == nil {
		t.Errorf("skills/drop should have been filtered out by pick")
	}
}
