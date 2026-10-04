//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRun_InlineHarness covers the documented `--harness-file <path>` mode:
// run a harness directly from a single manifest file without installing
// it into ~/.ynh/harnesses/ first. Used by CI runners and ephemeral
// environments where installing globally is undesirable.
func TestRun_InlineHarness(t *testing.T) {
	s := newSandbox(t)

	// Author a single-file harness manifest in a project directory.
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	harnessFile := filepath.Join(project, "ephemeral.json")
	body := `{
  "$schema": "https://eyelock.github.io/ynh/schema/harness.schema.json",
  "name": "ephemeral",
  "version": "0.1.0",
  "default_vendor": "cursor"
}
`
	if err := os.WriteFile(harnessFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	// Run with --install in cursor (creates symlinks but doesn't launch the CLI).
	mustRunYnhInDir(t, s, project, "run", "--harness-file", harnessFile, "-v", "cursor", "--install")

	// Inline runs use a hash-based stable run dir name prefixed with "_inline-".
	runRoot := filepath.Join(s.home, "run")
	entries, err := os.ReadDir(runRoot)
	if err != nil {
		t.Fatalf("reading run dir: %v", err)
	}
	var inlineDirs []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "_inline-") {
			inlineDirs = append(inlineDirs, e.Name())
		}
	}
	if len(inlineDirs) == 0 {
		t.Fatalf("expected at least one _inline-* run dir, got entries: %v", entries)
	}

	// Inline harness should NOT have been installed under harnesses/.
	if _, err := os.Stat(filepath.Join(s.home, "harnesses", "ephemeral")); err == nil {
		t.Errorf("inline run should not install into harnesses/ — found ephemeral/")
	}
}

// TestRun_LocalPath covers `ynh run <path>` (#448): a local harness directory
// runs without being installed, and a legacy tree is refused with the
// `ynd migrate` fix rather than the canonical-id hint.
func TestRun_LocalPath(t *testing.T) {
	s := newSandbox(t)
	work := t.TempDir()
	harness := newSyntheticSkillHarness(t, "my-dev")
	local := filepath.Join(work, "my-dev")
	if err := os.Rename(harness, local); err != nil {
		t.Fatal(err)
	}

	mustRunYnhInDir(t, s, work, "run", "./my-dev", "-v", "cursor", "--install")
	assertFileExists(t, filepath.Join(work, ".cursor", "skills", "hello", "SKILL.md"))
	if entries, err := os.ReadDir(filepath.Join(s.home, "harnesses")); err == nil && len(entries) > 0 {
		t.Errorf("running a path installed something: %v", entries)
	}

	legacy := filepath.Join(t.TempDir(), "old")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, ".harness.json"), []byte(`{"name":"old","version":"0.1.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, errOut, err := runYnhInDirRaw(t, s, work, "run", legacy, "-v", "cursor", "--install")
	if err == nil {
		t.Fatal("expected a legacy tree to be refused")
	}
	if !strings.Contains(errOut, "convert it with: ynd migrate "+legacy) {
		t.Errorf("expected the ynd migrate fix, got: %s", errOut)
	}
	_, errOut, err = runYnhInDirRaw(t, s, work, "run", "--harness-file", filepath.Join(legacy, ".harness.json"), "-v", "cursor", "--install")
	if err == nil {
		t.Fatal("expected --harness-file at a legacy .harness.json to be refused")
	}
	if !strings.Contains(errOut, "convert it with: ynd migrate "+legacy) {
		t.Errorf("expected the ynd migrate fix, got: %s", errOut)
	}
}
