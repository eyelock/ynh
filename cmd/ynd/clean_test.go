package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── The dangerous paths are tested against the PURE PREDICATE only. ──────────
//
// refuseToClean decides without touching anything, so passing it "/" or $HOME
// cannot delete "/" or $HOME. cleanOutputDir — the function that actually calls
// os.RemoveAll — is never given any of these. See
// .claude/rules/destructive-operations.md: that rule exists because a mutation
// test which removed this guard and ran the tests destroyed a home directory.

func TestRefuseToClean_RefusesPathsThatAreNeverBuildOutput(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct{ name, path, want string }{
		{"filesystem root", string(filepath.Separator), "filesystem root"},
		{"home directory", home, "home directory"},
		{"current directory", cwd, "current directory"},
		{"ancestor of cwd", filepath.Dir(cwd), "inside it"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reason := refuseToClean(c.path)
			if reason == "" {
				t.Fatalf("refuseToClean(%q) allowed it — this path is never a build output", c.path)
			}
			if !strings.Contains(reason, c.want) {
				t.Errorf("reason %q should mention %q so the operator knows why", reason, c.want)
			}
		})
	}
}

// The check that catches the realistic accident: an output path typed one
// directory too high, landing on somebody's source tree.
func TestRefuseToClean_RefusesAGitWorkingCopy(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if reason := refuseToClean(dir); !strings.Contains(reason, "git working copy") {
		t.Errorf("a directory with .git must be refused, got %q", reason)
	}
	// A .git *file* is a worktree or submodule — equally somebody's source.
	dir2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir2, ".git"), []byte("gitdir: /elsewhere"), 0o644); err != nil {
		t.Fatal(err)
	}
	if reason := refuseToClean(dir2); !strings.Contains(reason, "git working copy") {
		t.Errorf("a .git file (worktree/submodule) must be refused too, got %q", reason)
	}
}

// An ordinary build output must still be cleanable, or the guard is useless.
func TestRefuseToClean_AllowsAnOrdinaryOutputDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dist")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if reason := refuseToClean(dir); reason != "" {
		t.Errorf("an ordinary output dir must be cleanable, refused with %q", reason)
	}
}

// ── cleanOutputDir is only ever given paths inside the test's temp dir. ──────

// mustBeUnderTemp is belt and braces: it fails the test rather than letting a
// path outside the test's own temp directory reach the function that deletes.
// Nothing here should ever trip it — that is the point.
func mustBeUnderTemp(t *testing.T, tmp, path string) string {
	t.Helper()
	absTmp, _ := filepath.Abs(tmp)
	abs, _ := filepath.Abs(path)
	if !strings.HasPrefix(abs, absTmp) {
		t.Fatalf("refusing to hand %q to cleanOutputDir: it is outside the test temp dir %q", abs, absTmp)
	}
	return path
}

func TestCleanOutputDir_RemovesANonEmptyOutputDir(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "dist")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cleanOutputDir(mustBeUnderTemp(t, tmp, dir), true); err != nil {
		t.Fatalf("cleanOutputDir: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("the output dir should be gone")
	}
}

func TestCleanOutputDir_MissingOrEmptyIsANoOp(t *testing.T) {
	tmp := t.TempDir()
	missing := filepath.Join(tmp, "never-created")
	if err := cleanOutputDir(mustBeUnderTemp(t, tmp, missing), true); err != nil {
		t.Errorf("a missing dir has nothing to clean: %v", err)
	}
	empty := filepath.Join(tmp, "empty")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := cleanOutputDir(mustBeUnderTemp(t, tmp, empty), true); err != nil {
		t.Errorf("an empty dir needs no confirmation: %v", err)
	}
}

// t.Chdir makes the *temp dir* the current directory, so "refuse to delete cwd"
// is exercised through cleanOutputDir without the real cwd ever being passed.
func TestCleanOutputDir_RefusesTheCurrentDirectoryWithoutTouchingTheRealOne(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)
	err := cleanOutputDir(mustBeUnderTemp(t, tmp, tmp), true)
	if err == nil {
		t.Fatal("cleanOutputDir deleted the current directory")
	}
	if !strings.Contains(err.Error(), "current directory") {
		t.Errorf("error should say why, got %v", err)
	}
	if _, statErr := os.Stat(tmp); statErr != nil {
		t.Error("the directory was removed despite the refusal")
	}
}

// -y is consent to skip a question, not a licence to delete a source tree.
func TestCleanOutputDir_HardRefusalIgnoresSkipConfirm(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := cleanOutputDir(mustBeUnderTemp(t, tmp, repo), true)
	if err == nil {
		t.Fatal("--clean deleted a git working copy because -y was passed")
	}
	if _, statErr := os.Stat(filepath.Join(repo, "main.go")); statErr != nil {
		t.Error("the source tree was deleted despite the refusal")
	}
}

// promptAction returns choices[0] on empty input *or EOF*. A prompt labelled
// [y/N] that deletes when stdin is a pipe rather than a terminal is a lie to
// the operator, and CI pipes stdin.
func TestCleanOutputDir_DeclinedPromptDeletesNothing(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "dist")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	restore := promptActionFunc
	t.Cleanup(func() { promptActionFunc = restore })
	var sawChoices []string
	// Simulate EOF: the real promptAction returns choices[0] there.
	promptActionFunc = func(_ string, choices ...string) string {
		sawChoices = choices
		return choices[0]
	}

	err := cleanOutputDir(mustBeUnderTemp(t, tmp, dir), false)
	if err == nil {
		t.Fatal("an EOF/empty answer must not delete anything")
	}
	if len(sawChoices) == 0 || sawChoices[0] != "n" {
		t.Errorf("the refusing answer must be first, got %v", sawChoices)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "a.txt")); statErr != nil {
		t.Error("contents were deleted despite the decline")
	}
}

func TestCleanOutputDir_AcceptedPromptDeletes(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "dist")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := promptActionFunc
	t.Cleanup(func() { promptActionFunc = restore })
	promptActionFunc = func(_ string, _ ...string) string { return "y" }

	if err := cleanOutputDir(mustBeUnderTemp(t, tmp, dir), false); err != nil {
		t.Fatalf("an explicit yes should delete: %v", err)
	}
	if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
		t.Error("the dir should be gone after an explicit yes")
	}
}
