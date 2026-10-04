package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/marketplace"
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

// ── A repository ynd itself created is the one git working copy --clean may ──
// ── empty. Everything below still only hands dangerous paths to the predicate. ──

// buildOwnMarketplace runs the real `ynd marketplace build` into a directory
// under the test's temp dir, so the repository it returns is exactly what a
// user would have after a first build: ynd's `.git`, ynd's marker, ynd's root
// commit. Replicating that by hand would let the test drift from the tool.
func buildOwnMarketplace(t *testing.T, tmp string) (configFile, outputDir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	configFile = setupMarketplaceTest(t)
	outputDir = filepath.Join(tmp, "out")
	if err := cmdMarketplace([]string{"build", configFile, "-o", outputDir}); err != nil {
		t.Fatalf("first build: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, ".git")); err != nil {
		t.Fatalf("the build should have initialised a repository: %v", err)
	}
	return configFile, outputDir
}

func TestRefuseToClean_AllowsTheRepositoryYndCreated(t *testing.T) {
	_, out := buildOwnMarketplace(t, t.TempDir())
	if reason := refuseToClean(out); reason != "" {
		t.Errorf("ynd's own marketplace repository must be cleanable, refused with %q", reason)
	}
	if keep := keepOnClean(out); !keep[".git"] {
		t.Errorf("cleaning ynd's own repository must keep .git, keep = %v", keep)
	}
}

// The marker does not weaken the hard refusals: the root, $HOME, the current
// directory and its ancestors are refused before git is even considered. HOME
// is pointed at a temp dir and the cwd moved into one, so the real ones are
// never passed anywhere.
func TestRefuseToClean_HardRefusalsIgnoreTheMarker(t *testing.T) {
	tmp := t.TempDir()
	_, out := buildOwnMarketplace(t, tmp)

	t.Run("home directory", func(t *testing.T) {
		t.Setenv("HOME", out)
		if reason := refuseToClean(out); !strings.Contains(reason, "home directory") {
			t.Errorf("a home directory carrying the marker must still be refused, got %q", reason)
		}
	})
	t.Run("current directory", func(t *testing.T) {
		t.Chdir(out)
		if reason := refuseToClean(out); !strings.Contains(reason, "current directory") {
			t.Errorf("the current directory carrying the marker must still be refused, got %q", reason)
		}
	})
	t.Run("ancestor of current directory", func(t *testing.T) {
		t.Chdir(filepath.Join(out, "plugins"))
		if reason := refuseToClean(out); !strings.Contains(reason, "inside it") {
			t.Errorf("an ancestor of cwd carrying the marker must still be refused, got %q", reason)
		}
	})
}

// A real repository that happens to contain the marker is still refused: the
// root commit has to be ynd's as well. Otherwise a stray file could unlock
// --clean on somebody's source tree.
func TestRefuseToClean_ForgedMarkerDoesNotUnlockAForeignRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "--allow-empty", "-m", "theirs"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", args[0], err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, marketplace.MarkerFile), []byte("ynd marketplace 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reason := refuseToClean(dir)
	if !strings.Contains(reason, "git working copy") || !strings.Contains(reason, "not created by ynd") {
		t.Errorf("a foreign repo with a forged marker must be refused and say so, got %q", reason)
	}
	if keep := keepOnClean(dir); keep != nil {
		t.Errorf("nothing about a foreign repo is ours to keep or delete, keep = %v", keep)
	}
}

// The deleting path, on ynd's own repository, under the temp dir only: the
// stale build goes, `.git` and the marker stay.
func TestCleanOutputDir_EmptiesYndsOwnRepoButKeepsGit(t *testing.T) {
	tmp := t.TempDir()
	_, out := buildOwnMarketplace(t, tmp)
	stale := filepath.Join(out, "stale.txt")
	if err := os.WriteFile(stale, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := cleanOutputDir(mustBeUnderTemp(t, tmp, out), true); err != nil {
		t.Fatalf("cleanOutputDir on ynd's own repo: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("the stale file should be gone")
	}
	if _, err := os.Stat(filepath.Join(out, "plugins")); !os.IsNotExist(err) {
		t.Error("the previous build output should be gone")
	}
	if _, err := os.Stat(filepath.Join(out, ".git")); err != nil {
		t.Error(".git must survive a clean of ynd's own repository")
	}
	if _, err := os.Stat(filepath.Join(out, marketplace.MarkerFile)); err != nil {
		t.Error("the marker must survive, or the next build could not prove ownership")
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("only .git and the marker should remain, got %d entries", len(entries))
	}
}

// ── Messages name the path as the user gave it (#466). ───────────────────────
//
// On macOS /tmp is a symlink to /private/tmp, so a message built from the
// resolved path never matched what the user typed. The guards still decide on
// the resolved path; only the text changes. Every path below, link and target
// alike, is under the test's temp dir.

// symlinkedDir makes tmp/<name> a symlink to a real directory tmp/<name>-target
// holding one file, and returns the link and the target.
func symlinkedDir(t *testing.T, tmp, name string) (link, target string) {
	t.Helper()
	target = filepath.Join(tmp, name+"-target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link = filepath.Join(tmp, name)
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	return link, target
}

// assertNamesLink fails unless text names link and neither spelling of target.
func assertNamesLink(t *testing.T, what, text, link, target string) {
	t.Helper()
	if !strings.Contains(text, link) {
		t.Errorf("%s should name the path as given (%s), got:\n%s", what, link, text)
	}
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{target, resolved} {
		if strings.Contains(text, p) {
			t.Errorf("%s should not name the symlink-resolved path %s, got:\n%s", what, p, text)
		}
	}
}

func TestCleanOutputDir_PromptAndDeclineNameThePathAsGiven(t *testing.T) {
	tmp := t.TempDir()
	link, target := symlinkedDir(t, tmp, "dist")

	restore := promptActionFunc
	t.Cleanup(func() { promptActionFunc = restore })
	promptActionFunc = func(_ string, choices ...string) string { return choices[0] }

	var out bytes.Buffer
	var err error
	withStdout(t, &out, func() { err = cleanOutputDir(mustBeUnderTemp(t, tmp, link), false) })
	if err == nil {
		t.Fatal("a declined prompt must not delete anything")
	}
	assertNamesLink(t, "the --clean prompt", out.String(), link, target)
	assertNamesLink(t, "the decline message", err.Error(), link, target)
	if _, statErr := os.Stat(filepath.Join(target, "a.txt")); statErr != nil {
		t.Error("contents were deleted despite the decline")
	}
}

func TestCleanOutputDir_RefusalNamesThePathAsGiven(t *testing.T) {
	tmp := t.TempDir()
	link, target := symlinkedDir(t, tmp, "repo")
	if err := os.MkdirAll(filepath.Join(target, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := cleanOutputDir(mustBeUnderTemp(t, tmp, link), true)
	if err == nil {
		t.Fatal("--clean deleted a git working copy reached through a symlink")
	}
	assertNamesLink(t, "the refusal", err.Error(), link, target)
	if _, statErr := os.Stat(filepath.Join(target, "a.txt")); statErr != nil {
		t.Error("the source tree was deleted despite the refusal")
	}
}

// A symlink must not walk past the home refusal. HOME is faked to a directory
// under the test's temp dir; the real home is never involved.
func TestCleanOutputDir_SymlinkToHomeIsStillRefused(t *testing.T) {
	tmp := t.TempDir()
	link, fakeHome := symlinkedDir(t, tmp, "home")
	t.Setenv("HOME", fakeHome)

	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatal(err)
	}
	if reason := refuseToClean(resolved); !strings.Contains(reason, "home directory") {
		t.Fatalf("refuseToClean(%q) must refuse the faked home, got %q", resolved, reason)
	}

	cleanErr := cleanOutputDir(mustBeUnderTemp(t, tmp, link), true)
	if cleanErr == nil || !strings.Contains(cleanErr.Error(), "home directory") {
		t.Fatalf("a symlink to $HOME must be refused, got %v", cleanErr)
	}
	assertNamesLink(t, "the refusal", cleanErr.Error(), link, fakeHome)
	if _, statErr := os.Stat(filepath.Join(fakeHome, "a.txt")); statErr != nil {
		t.Error("the faked home was touched despite the refusal")
	}
}
