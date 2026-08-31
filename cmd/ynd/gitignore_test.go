package main

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitRepo(t *testing.T, ignore string) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if ignore != "" {
		writeFile(t, filepath.Join(dir, ".gitignore"), []byte(ignore))
	}
	return dir
}

// Discovery must not wander into files git was told to ignore.
//
// `.gitignore` here excludes `.claude/plans/` so a contributor can keep local
// notes beside the harness. Because `make check` runs `ynd lint .claude`, those
// notes could fail the project's own gate — which happened, over two blank
// lines in a private file, unrelated to the change being made.
func TestDiscover_SkipsGitignoredFiles(t *testing.T) {
	dir := gitRepo(t, "notes/\n")
	writeFile(t, filepath.Join(dir, "kept.md"), []byte("# kept\n"))
	writeFile(t, filepath.Join(dir, "notes", "private.md"), []byte("# private\n"))

	found, err := discoverAll(dir, []string{".md"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range found {
		names = append(names, filepath.Base(f))
	}
	joined := strings.Join(names, " ")
	if !strings.Contains(joined, "kept.md") {
		t.Errorf("tracked file missing from discovery: %v", names)
	}
	if strings.Contains(joined, "private.md") {
		t.Errorf("discovery reached a gitignored file: %v", names)
	}
}

// Naming a file outright is a clear instruction. The filter exists to stop a
// walk wandering into private notes, not to veto a direct request.
func TestDiscover_ExplicitlyNamedIgnoredFileIsHonoured(t *testing.T) {
	dir := gitRepo(t, "notes/\n")
	target := filepath.Join(dir, "notes", "private.md")
	writeFile(t, target, []byte("# private\n"))

	found, err := discoverAll(target, []string{".md"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0] != target {
		t.Errorf("an explicitly named file should be returned, got %v", found)
	}
}

// Outside a work tree there are no rules to consult, and every path stands.
// Failing open is the safe direction: this narrows what the tools touch, so a
// missing git or a plain directory must not stop `ynd lint` working.
func TestDiscover_OutsideAGitRepoNothingIsFiltered(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.md"), []byte("# a\n"))
	writeFile(t, filepath.Join(dir, "sub", "b.md"), []byte("# b\n"))

	found, err := discoverAll(dir, []string{".md"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 {
		t.Errorf("expected both files outside a repo, got %v", found)
	}
}

// Nested .gitignore files and negations are git's to resolve, not ours — the
// reason this asks git rather than matching patterns itself.
func TestDiscover_RespectsNestedIgnoreAndNegation(t *testing.T) {
	dir := gitRepo(t, "*.md\n!keep.md\n")
	writeFile(t, filepath.Join(dir, "keep.md"), []byte("# keep\n"))
	writeFile(t, filepath.Join(dir, "drop.md"), []byte("# drop\n"))

	found, err := discoverAll(dir, []string{".md"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range found {
		names = append(names, filepath.Base(f))
	}
	joined := strings.Join(names, " ")
	if !strings.Contains(joined, "keep.md") {
		t.Errorf("negated pattern should keep the file: %v", names)
	}
	if strings.Contains(joined, "drop.md") {
		t.Errorf("ignored file survived: %v", names)
	}
}
