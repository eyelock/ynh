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

	res, err := ComposeProjectInstructions(run, proj)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 1 || res.Files[0] != filepath.Join(proj, "AGENTS.md") {
		t.Errorf("files = %v", res.Files)
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
		res, err := ComposeProjectInstructions(run, p)
		if err != nil || len(res.Files) != 0 {
			t.Errorf("%s: files=%v err=%v", name, res.Files, err)
		}
	}
	writeFile(t, proj, "AGENTS.md", "")
	if res, _ := ComposeProjectInstructions(run, proj); len(res.Files) != 0 {
		t.Errorf("empty project file composed: %v", res.Files)
	}
	got, _ := os.ReadFile(filepath.Join(run, "AGENTS.md"))
	if string(got) != "HARNESS RULES" {
		t.Errorf("run file changed: %q", got)
	}
}

// Running from the harness's own directory: its AGENTS.md is already the
// run directory's, so it must not come in a second time (ynw-sofy).
func TestComposeProjectInstructionsSkipsHarnessOwnFile(t *testing.T) {
	run, proj := t.TempDir(), t.TempDir()
	writeFile(t, run, "AGENTS.md", "HARNESSWORD rules\n")
	writeFile(t, proj, "AGENTS.md", "HARNESSWORD rules\n")

	res, err := ComposeProjectInstructions(run, proj)
	if err != nil || len(res.Files) != 0 {
		t.Fatalf("files=%v err=%v", res.Files, err)
	}
	got, _ := os.ReadFile(filepath.Join(run, "AGENTS.md"))
	if n := strings.Count(string(got), "HARNESSWORD"); n != 1 {
		t.Errorf("harness text appears %d times:\n%s", n, got)
	}
}

// From a subdirectory of a Git repository, Codex reads the root's file, then
// each directory's down to the start directory; an override replaces the
// directory's AGENTS.md (ynw-8hor).
func TestComposeProjectInstructionsGitChain(t *testing.T) {
	run, repo := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	mid, sub := filepath.Join(repo, "mid"), filepath.Join(repo, "mid", "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, run, "AGENTS.md", "HARNESS")
	writeFile(t, repo, "AGENTS.md", "ROOTWORD")
	writeFile(t, mid, "AGENTS.md", "MIDWORD")
	writeFile(t, mid, "AGENTS.override.md", "MIDOVERRIDE")
	writeFile(t, sub, "AGENTS.md", "SUBWORD")

	res, err := ComposeProjectInstructions(run, sub)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 3 {
		t.Fatalf("files = %v", res.Files)
	}
	s, _ := os.ReadFile(filepath.Join(run, "AGENTS.md"))
	last := -1
	for _, w := range []string{"HARNESS", "ROOTWORD", "MIDOVERRIDE", "SUBWORD"} {
		i := strings.Index(string(s), w)
		if i < 0 || i < last {
			t.Fatalf("%s missing or out of order:\n%s", w, s)
		}
		last = i
	}
	if strings.Contains(string(s), "MIDWORD") {
		t.Errorf("overridden AGENTS.md composed:\n%s", s)
	}
}

func TestComposeProjectInstructionsReportsSize(t *testing.T) {
	run, proj := t.TempDir(), t.TempDir()
	writeFile(t, run, "AGENTS.md", strings.Repeat("h", CodexProjectDocMaxBytes))
	writeFile(t, proj, "AGENTS.md", "PROJECT")
	res, _ := ComposeProjectInstructions(run, proj)
	if res.Size <= CodexProjectDocMaxBytes {
		t.Errorf("size = %d", res.Size)
	}
}
