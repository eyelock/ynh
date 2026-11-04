package vendor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestComposeProjectInstructionsKeepsBoth(t *testing.T) {
	run, proj := t.TempDir(), t.TempDir()
	writeFile(t, run, "AGENTS.md", "HARNESS RULES")
	writeFile(t, proj, "AGENTS.md", "PROJECT RULES\n")

	src, err := ComposeProjectInstructions(run, proj)
	if err != nil {
		t.Fatal(err)
	}
	if src != filepath.Join(proj, "AGENTS.md") {
		t.Errorf("src = %q", src)
	}
	got, _ := os.ReadFile(filepath.Join(run, "AGENTS.md"))
	s := string(got)
	h, p := strings.Index(s, "HARNESS RULES"), strings.Index(s, "PROJECT RULES")
	if h < 0 || p < 0 || h > p {
		t.Errorf("want harness then project, got:\n%s", s)
	}
}

func TestComposeProjectInstructionsWithoutHarnessFile(t *testing.T) {
	run, proj := t.TempDir(), t.TempDir()
	writeFile(t, proj, "AGENTS.md", "PROJECT RULES")
	if _, err := ComposeProjectInstructions(run, proj); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(run, "AGENTS.md"))
	if !strings.Contains(string(got), "PROJECT RULES") {
		t.Errorf("got %q", got)
	}
}

func TestComposeProjectInstructionsNoOps(t *testing.T) {
	run, proj := t.TempDir(), t.TempDir()
	writeFile(t, run, "AGENTS.md", "HARNESS RULES")

	for name, p := range map[string]string{"no project file": proj, "empty dir arg": "", "run dir itself": run} {
		src, err := ComposeProjectInstructions(run, p)
		if err != nil || src != "" {
			t.Errorf("%s: src=%q err=%v", name, src, err)
		}
	}
	writeFile(t, proj, "AGENTS.md", "")
	if src, _ := ComposeProjectInstructions(run, proj); src != "" {
		t.Errorf("empty project file composed: %q", src)
	}
	got, _ := os.ReadFile(filepath.Join(run, "AGENTS.md"))
	if string(got) != "HARNESS RULES" {
		t.Errorf("run file changed: %q", got)
	}
}
