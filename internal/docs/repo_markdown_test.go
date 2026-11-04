package docs_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeTree creates each file, with its directories, under root.
func writeTree(t *testing.T, root string, files ...string) {
	t.Helper()
	for _, rel := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		body := "# Page\n\n[broken](./missing.md)\n"
		if rel == ".gitignore" {
			body = "**/.terraform/\ncache/\n"
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = withoutGitLocation(os.Environ())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestRepoMarkdownSkipsIgnoredFiles is the case that failed make check in a
// real checkout: a gitignored terraform provider cache with a broken link.
// Ignored files are not the repository's and must not be policed; tracked and
// new-but-not-ignored files must be.
func TestRepoMarkdownSkipsIgnoredFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	runGit(t, root, "init", "-q")
	writeTree(t, root,
		".gitignore",
		"README.md",
		".github/CONTRIBUTING.md",
		"infra/github/.terraform/providers/README.md", // ignored by **/.terraform/
		"cache/notes.md",      // ignored by cache/
		"new-page.md",         // untracked, not ignored: still ours
		"docs/guide.md",       // docs/ has its own checker
		"testdata/fixture.md", // fixtures are skipped
	)
	runGit(t, root, "add", "README.md", ".github/CONTRIBUTING.md", ".gitignore")

	got, gitErr, err := repoMarkdownFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if gitErr != nil {
		t.Fatalf("expected git to list the files in a git repository: %v", gitErr)
	}
	want := []string{
		filepath.FromSlash(".github/CONTRIBUTING.md"),
		"README.md",
		"new-page.md",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("repoMarkdownFiles = %q, want %q", got, want)
	}
}

// TestRepoMarkdownFallsBackWithoutGit covers a source tree that is not a
// repository (or a machine without git): the disk is walked instead, still
// skipping the directories that are never ours.
func TestRepoMarkdownFallsBackWithoutGit(t *testing.T) {
	root := t.TempDir()
	// Stop git from finding a repository above the temp dir.
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(root))
	writeTree(t, root,
		"README.md",
		"infra/github/.terraform/providers/README.md",
		"docs/guide.md",
		"node_modules/pkg/README.md",
	)

	got, gitErr, err := repoMarkdownFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if gitErr == nil {
		t.Fatal("expected the disk walk outside a git repository")
	}
	if want := []string{"README.md"}; !reflect.DeepEqual(got, want) {
		t.Errorf("repoMarkdownFiles = %q, want %q", got, want)
	}
}

// TestRepoMarkdownFallsBackInsideAnotherRepo covers a source tree extracted
// into a directory a parent repository ignores: git answers for the parent and
// lists nothing, so the disk must be walked instead, and the error says why.
func TestRepoMarkdownFallsBackInsideAnotherRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	parent := t.TempDir()
	runGit(t, parent, "init", "-q")
	writeTree(t, parent, "src/README.md", "src/docs/guide.md")
	if err := os.WriteFile(filepath.Join(parent, ".gitignore"), []byte("src/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "src")

	got, gitErr, err := repoMarkdownFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	if gitErr == nil || !strings.Contains(gitErr.Error(), "top level") {
		t.Fatalf("expected a not-top-level error, got %v", gitErr)
	}
	if want := []string{"README.md"}; !reflect.DeepEqual(got, want) {
		t.Errorf("repoMarkdownFiles = %q, want %q", got, want)
	}
}
