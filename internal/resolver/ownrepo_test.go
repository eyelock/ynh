package resolver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/harness"
)

// newRepo makes a git repo with one committed file and returns its resolved
// path (macOS temp dirs are symlinks).
func newRepo(t *testing.T, files ...string) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return newRepoAt(t, dir, files...)
}

func newRepoAt(t *testing.T, dir string, files ...string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")
	commitFiles(t, dir, files...)
	return dir
}

func commitFiles(t *testing.T, dir string, files ...string) {
	t.Helper()
	for _, f := range files {
		p := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "commit")
}

func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("YNH_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
}

func TestIsOwnRepoSource(t *testing.T) {
	isolate(t)
	own := newRepo(t, "harnesses/a/x")
	other := newRepo(t, "y")
	notRepo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		git        string
		harnessDir string
		want       bool
	}{
		{"dot", ".", own, true},
		{"dot sub", "./harnesses/a", own, true},
		{"dot from harness subdir", "../..", filepath.Join(own, "harnesses", "a"), true},
		{"other repo by relative path", "../" + filepath.Base(other), own, false},
		{"absolute local path of own repo", own, own, false},
		{"file url", "file://" + own, own, false},
		{"remote", "github.com/eyelock/x", own, false},
		{"installed copy, not a git repo", ".", notRepo, false},
		{"no harness dir", ".", "", false},
		{"empty", "", own, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsOwnRepoSource(harness.GitSource{Git: tt.git}, tt.harnessDir)
			if got != tt.want {
				t.Errorf("IsOwnRepoSource(%q, %q) = %v, want %v", tt.git, tt.harnessDir, got, tt.want)
			}
		})
	}
}

func TestResolveSourceForRun_OwnRepoSeesLaterCommits(t *testing.T) {
	isolate(t)
	repo := newRepo(t, "harnesses/a/plugin.json")

	gsA := harness.GitSource{Git: ".", Path: "harnesses/a"}
	if _, _, err := ResolveSourceForRun(gsA, repo); err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	commitFiles(t, repo, "harnesses/b/plugin.json")

	base, _, err := ResolveSourceForRun(harness.GitSource{Git: ".", Path: "harnesses/b"}, repo)
	if err != nil {
		t.Fatalf("delegate committed later must resolve without an update: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "plugin.json")); err != nil {
		t.Error(err)
	}
}

func TestResolveSourceForRun_OtherLocalSourcesStayCached(t *testing.T) {
	tests := []struct {
		name string
		git  func(other string) string
	}{
		{"sibling repo", func(other string) string { return "../other" }},
		{"absolute path", func(other string) string { return other }},
		{"file url", func(other string) string { return "file://" + other }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			parent, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			h := newRepoAt(t, filepath.Join(parent, "h"), "h")
			other := newRepoAt(t, filepath.Join(parent, "other"), "harnesses/a/plugin.json")
			git := tt.git(other)

			if _, _, err := ResolveSourceForRun(harness.GitSource{Git: git, Path: "harnesses/a"}, h); err != nil {
				t.Fatalf("first resolve: %v", err)
			}
			commitFiles(t, other, "harnesses/b/plugin.json")
			_, _, err = ResolveSourceForRun(harness.GitSource{Git: git, Path: "harnesses/b"}, h)
			if err == nil {
				t.Fatal("another repo must not be refreshed on resolution")
			}
			if !strings.Contains(err.Error(), "ynh update") {
				t.Errorf("want stale-cache hint, got: %v", err)
			}
		})
	}
}

func TestResolveSourceForRun_InstalledCopyNeverFetches(t *testing.T) {
	isolate(t)
	installed := t.TempDir() // an installed copy: not a git repo
	cached := func(url, ref string) (RepoResult, error) {
		return RepoResult{Path: t.TempDir()}, nil
	}
	fetch := func(url, ref string) (RepoResult, error) {
		t.Fatal("EnsureRepo-style fetch must not be called")
		return RepoResult{}, nil
	}
	_, _, err := resolveForRunWith(harness.GitSource{Git: "."}, installed, cached, fetch)
	if err != nil {
		t.Fatal(err)
	}
}

func TestResolveSourceForRun_RemoteDoesNotFetch(t *testing.T) {
	isolate(t)
	cached := func(url, ref string) (RepoResult, error) {
		return RepoResult{Path: t.TempDir()}, nil
	}
	fetch := func(url, ref string) (RepoResult, error) {
		t.Fatal("remote source must not be fetched")
		return RepoResult{}, nil
	}
	gs := harness.GitSource{Git: "github.com/eyelock/x"}
	if _, _, err := resolveForRunWith(gs, newRepo(t, "f"), cached, fetch); err != nil {
		t.Fatal(err)
	}
}

func TestResolveSourceForRun_StaleHint(t *testing.T) {
	empty := func(url, ref string) (RepoResult, error) { return RepoResult{Path: t.TempDir()}, nil }
	cloned := func(url, ref string) (RepoResult, error) { return RepoResult{Path: t.TempDir(), Cloned: true}, nil }
	gs := harness.GitSource{Git: "github.com/eyelock/x", Path: "missing"}

	tests := []struct {
		name     string
		cached   repoFunc
		wantHint bool
	}{
		{"warm cache, not fetched", empty, true},
		{"freshly cloned", cloned, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := resolveForRunWith(gs, "", tt.cached, nil)
			if err == nil || !strings.Contains(err.Error(), `path "missing" not found in github.com/eyelock/x`) {
				t.Fatalf("want not-found error, got %v", err)
			}
			if got := strings.Contains(err.Error(), "ynh update"); got != tt.wantHint {
				t.Errorf("hint present = %v, want %v: %v", got, tt.wantHint, err)
			}
		})
	}
}

func TestResolveSourceForRun_FetchedOwnRepoHasNoHint(t *testing.T) {
	isolate(t)
	repo := newRepo(t, "f")
	_, _, err := ResolveSourceForRun(harness.GitSource{Git: ".", Path: "missing"}, repo)
	if err == nil {
		t.Fatal("expected not-found")
	}
	if strings.Contains(err.Error(), "ynh update") {
		t.Errorf("a freshly fetched repo must not suggest an update: %v", err)
	}
}

// An include committed to the harness's own repo after the first run is found
// on the next run; an include from another repo is not refreshed (#596).
func TestResolveFromCache_IncludeRefresh(t *testing.T) {
	tests := []struct {
		name        string
		git         func(other string) string
		wantRefresh bool
	}{
		{"own repo", func(string) string { return "." }, true},
		{"sibling repo", func(string) string { return "../other" }, false},
		{"absolute path", func(other string) string { return other }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			parent, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			h := newRepoAt(t, filepath.Join(parent, "h"), "skills/one/SKILL.md")
			other := newRepoAt(t, filepath.Join(parent, "other"), "skills/one/SKILL.md")
			source := h
			if tt.name != "own repo" {
				source = other
			}
			git := tt.git(other)

			resolve := func(pick string) error {
				p := &harness.Harness{Name: "h", Dir: h, Includes: []harness.Include{
					{GitSource: harness.GitSource{Git: git}, Pick: []string{pick}},
				}}
				res, err := ResolveFromCache(p, nil)
				if err != nil {
					return err
				}
				_, err = os.Stat(filepath.Join(res[0].Content.BasePath, pick))
				return err
			}

			if err := resolve("skills/one"); err != nil {
				t.Fatalf("first run: %v", err)
			}
			commitFiles(t, source, "skills/two/SKILL.md")
			err = resolve("skills/two")
			if tt.wantRefresh && err != nil {
				t.Errorf("include committed after the first run must be found: %v", err)
			}
			if !tt.wantRefresh && err == nil {
				t.Error("another repo must not be refreshed on resolution")
			}
		})
	}
}
