package resolver

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
)

func TestEnsureRepo_CacheUpdatesWorkingTree(t *testing.T) {
	// Create a local git repo
	srcDir := t.TempDir()
	runGit(t, srcDir, "init")
	runGit(t, srcDir, "config", "user.email", "test@test.com")
	runGit(t, srcDir, "config", "user.name", "Test")

	if err := os.WriteFile(filepath.Join(srcDir, "file.txt"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, srcDir, "add", ".")
	runGit(t, srcDir, "commit", "-m", "v1")

	t.Setenv("YNH_HOME", "")
	t.Setenv("HOME", t.TempDir())

	// First clone
	result, err := EnsureRepo(srcDir, "")
	if err != nil {
		t.Fatalf("first ensureRepo failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(result.Path, "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "v1" {
		t.Fatalf("expected v1, got %q", string(data))
	}

	// Update source repo
	if err := os.WriteFile(filepath.Join(srcDir, "file.txt"), []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, srcDir, "add", ".")
	runGit(t, srcDir, "commit", "-m", "v2")

	// Second call should update working tree
	result2, err := EnsureRepo(srcDir, "")
	if err != nil {
		t.Fatalf("second ensureRepo failed: %v", err)
	}
	if !result2.Changed {
		t.Error("expected Changed=true after upstream commit")
	}

	data, err = os.ReadFile(filepath.Join(result2.Path, "file.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "v2" {
		t.Errorf("working tree not updated: got %q, want %q", string(data), "v2")
	}
}

func TestEnsureRepo_UpdateErrorsNotSwallowed(t *testing.T) {
	// Create a local git repo and clone it
	srcDir := t.TempDir()
	runGit(t, srcDir, "init")
	runGit(t, srcDir, "config", "user.email", "test@test.com")
	runGit(t, srcDir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(srcDir, "file.txt"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, srcDir, "add", ".")
	runGit(t, srcDir, "commit", "-m", "v1")

	t.Setenv("YNH_HOME", "")
	t.Setenv("HOME", t.TempDir())

	// First call: clone succeeds
	result, err := EnsureRepo(srcDir, "")
	if err != nil {
		t.Fatalf("initial clone failed: %v", err)
	}

	// Corrupt the remote by removing the source repo's .git
	if err := os.RemoveAll(filepath.Join(srcDir, ".git")); err != nil {
		t.Fatal(err)
	}

	// Second call: fetch fails → re-clone attempted → re-clone also fails (remote gone).
	// Error should surface from the clone attempt, not the original fetch.
	_, err = EnsureRepo(srcDir, "")
	if err == nil {
		t.Fatal("expected error when remote is gone, got nil")
	}
	if !strings.Contains(err.Error(), "git clone") {
		t.Errorf("error should mention git clone (re-clone after fetch failure), got: %s", err.Error())
	}
	_ = result
}

func TestEnsureRepo_AnyFetchFailure_TriggersReclone(t *testing.T) {
	srcDir := t.TempDir()
	runGit(t, srcDir, "init")
	runGit(t, srcDir, "config", "user.email", "test@test.com")
	runGit(t, srcDir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(srcDir, "data.txt"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, srcDir, "add", ".")
	runGit(t, srcDir, "commit", "-m", "init")

	t.Setenv("YNH_HOME", "")
	t.Setenv("HOME", t.TempDir())

	result, err := EnsureRepo(srcDir, "")
	if err != nil {
		t.Fatalf("initial clone: %v", err)
	}

	// Inject a fetch failure with an arbitrary git error (e.g. "remote did not
	// send all necessary objects") — should trigger nuke-and-reclone, not propagate.
	orig := gitCmdFunc
	t.Cleanup(func() { gitCmdFunc = orig })
	calls := 0
	gitCmdFunc = func(args ...string) error {
		calls++
		isFetch := len(args) > 0 && args[0] == "-C" && len(args) > 2 && args[2] == "fetch"
		if calls == 1 && isFetch {
			return errors.New("exit status 1\nerror: remote did not send all necessary objects")
		}
		return orig(args...)
	}

	result2, err := EnsureRepo(srcDir, "")
	if err != nil {
		t.Fatalf("EnsureRepo should recover via re-clone: %v", err)
	}
	if result2.Path != result.Path {
		t.Errorf("expected same cache path after recovery")
	}
	if !result2.Cloned {
		t.Error("expected Cloned=true after re-clone recovery")
	}
}

func TestCacheOnlyRepo_CacheHit(t *testing.T) {
	// Create a local git repo and clone it via EnsureRepo first
	srcDir := t.TempDir()
	runGit(t, srcDir, "init")
	runGit(t, srcDir, "config", "user.email", "test@test.com")
	runGit(t, srcDir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(srcDir, "test.txt"), []byte("cached"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, srcDir, "add", ".")
	runGit(t, srcDir, "commit", "-m", "init")

	t.Setenv("YNH_HOME", "")
	t.Setenv("HOME", t.TempDir())

	// Populate cache via EnsureRepo
	ensureResult, err := EnsureRepo(srcDir, "")
	if err != nil {
		t.Fatalf("EnsureRepo failed: %v", err)
	}

	// Now remove the source repo so any network fetch would fail
	if err := os.RemoveAll(filepath.Join(srcDir, ".git")); err != nil {
		t.Fatal(err)
	}

	// CacheOnlyRepo should return cached result without hitting the network
	result, err := CacheOnlyRepo(srcDir, "")
	if err != nil {
		t.Fatalf("CacheOnlyRepo should succeed on cache hit: %v", err)
	}
	if result.Path != ensureResult.Path {
		t.Errorf("expected same path %q, got %q", ensureResult.Path, result.Path)
	}
	if result.Cloned {
		t.Error("expected Cloned=false for cache hit")
	}

	// Verify content is still accessible
	data, err := os.ReadFile(filepath.Join(result.Path, "test.txt"))
	if err != nil {
		t.Fatalf("cached file not readable: %v", err)
	}
	if string(data) != "cached" {
		t.Errorf("cached content = %q, want %q", string(data), "cached")
	}
}

func TestCacheOnlyRepo_CacheMiss(t *testing.T) {
	// Create a local git repo — cache is empty, so CacheOnlyRepo should clone
	srcDir := t.TempDir()
	runGit(t, srcDir, "init")
	runGit(t, srcDir, "config", "user.email", "test@test.com")
	runGit(t, srcDir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(srcDir, "test.txt"), []byte("fresh"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, srcDir, "add", ".")
	runGit(t, srcDir, "commit", "-m", "init")

	t.Setenv("YNH_HOME", "")
	t.Setenv("HOME", t.TempDir())

	// CacheOnlyRepo with empty cache should fall back to EnsureRepo
	result, err := CacheOnlyRepo(srcDir, "")
	if err != nil {
		t.Fatalf("CacheOnlyRepo should fall back to clone: %v", err)
	}
	if !result.Cloned {
		t.Error("expected Cloned=true for cache miss fallback")
	}

	data, err := os.ReadFile(filepath.Join(result.Path, "test.txt"))
	if err != nil {
		t.Fatalf("cloned file not readable: %v", err)
	}
	if string(data) != "fresh" {
		t.Errorf("cloned content = %q, want %q", string(data), "fresh")
	}
}

func TestResolveFromCache_UsesCache(t *testing.T) {
	// Create a local git repo with skills
	srcDir := t.TempDir()
	runGit(t, srcDir, "init")
	runGit(t, srcDir, "config", "user.email", "test@test.com")
	runGit(t, srcDir, "config", "user.name", "Test")
	if err := os.MkdirAll(filepath.Join(srcDir, "skills", "hello"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "skills", "hello", "SKILL.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, srcDir, "add", ".")
	runGit(t, srcDir, "commit", "-m", "init")

	t.Setenv("YNH_HOME", "")
	t.Setenv("HOME", t.TempDir())

	// Pre-populate cache
	if _, err := EnsureRepo(srcDir, ""); err != nil {
		t.Fatalf("EnsureRepo failed: %v", err)
	}

	// Remove source so network access would fail
	if err := os.RemoveAll(filepath.Join(srcDir, ".git")); err != nil {
		t.Fatal(err)
	}

	p := &harness.Harness{
		Name: "test-cached",
		Includes: []harness.Include{
			{
				GitSource: harness.GitSource{Git: srcDir},
				Pick:      []string{"skills/hello"},
			},
		},
	}

	// ResolveFromCache should succeed from cache alone
	results, err := ResolveFromCache(p, nil)
	if err != nil {
		t.Fatalf("ResolveFromCache failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Cloned {
		t.Error("expected Cloned=false (served from cache)")
	}
	if !results[0].Cached {
		t.Error("expected Cached=true")
	}
}

func TestResolveFromCache_BlockedByAllowList(t *testing.T) {
	cfg := &config.Config{
		AllowedRemoteSources: []string{
			"github.com/trusted-org/*",
		},
	}

	p := &harness.Harness{
		Name: "test-blocked-cache",
		Includes: []harness.Include{
			{
				GitSource: harness.GitSource{Git: "github.com/untrusted-org/repo"},
			},
		},
	}

	_, err := ResolveFromCache(p, cfg)
	if err == nil {
		t.Fatal("expected error for blocked remote source")
	}
	if !strings.Contains(err.Error(), "not in the allowed sources list") {
		t.Errorf("error should mention allow list, got: %v", err)
	}
}

func TestResolveGitSourceFromCache_WithPath(t *testing.T) {
	// Create a repo with a subdirectory
	srcDir := t.TempDir()
	runGit(t, srcDir, "init")
	runGit(t, srcDir, "config", "user.email", "test@test.com")
	runGit(t, srcDir, "config", "user.name", "Test")
	if err := os.MkdirAll(filepath.Join(srcDir, "sub", "skills", "s1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "sub", "skills", "s1", "SKILL.md"), []byte("s1"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, srcDir, "add", ".")
	runGit(t, srcDir, "commit", "-m", "init")

	t.Setenv("YNH_HOME", "")
	t.Setenv("HOME", t.TempDir())

	// Pre-populate cache
	if _, err := EnsureRepo(srcDir, ""); err != nil {
		t.Fatalf("EnsureRepo failed: %v", err)
	}

	// Resolve with valid path
	gs := harness.GitSource{Git: srcDir, Path: "sub"}
	basePath, _, err := ResolveGitSourceFromCache(gs)
	if err != nil {
		t.Fatalf("ResolveGitSourceFromCache failed: %v", err)
	}
	skillPath := filepath.Join(basePath, "skills", "s1", "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		t.Errorf("skill not found at resolved path: %v", err)
	}

	// Resolve with invalid path
	gsBad := harness.GitSource{Git: srcDir, Path: "nonexistent"}
	_, _, err = ResolveGitSourceFromCache(gsBad)
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
}

func TestEnsureRepo_StaleLockFile_RecoversViaRetry(t *testing.T) {
	srcDir := t.TempDir()
	runGit(t, srcDir, "init")
	runGit(t, srcDir, "config", "user.email", "test@test.com")
	runGit(t, srcDir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(srcDir, "data.txt"), []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, srcDir, "add", ".")
	runGit(t, srcDir, "commit", "-m", "init")

	t.Setenv("YNH_HOME", "")
	t.Setenv("HOME", t.TempDir())

	// First clone
	result, err := EnsureRepo(srcDir, "")
	if err != nil {
		t.Fatalf("initial clone: %v", err)
	}

	// Simulate a stale shallow.lock left by an interrupted prior fetch
	lockFile := filepath.Join(result.Path, ".git", "shallow.lock")
	if err := os.WriteFile(lockFile, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	// Inject a gitCmdFunc that fails with a lock error on the first fetch,
	// then delegates to real git for the retry and all subsequent calls.
	callCount := 0
	orig := gitCmdFunc
	t.Cleanup(func() { gitCmdFunc = orig })
	gitCmdFunc = func(args ...string) error {
		callCount++
		isFetch := len(args) > 0 && args[0] == "-C" && len(args) > 2 && args[2] == "fetch"
		if callCount == 1 && isFetch {
			return errors.New("exit status 128\nfatal: Unable to create '...shallow.lock': File exists.")
		}
		return orig(args...)
	}

	result2, err := EnsureRepo(srcDir, "")
	if err != nil {
		t.Fatalf("EnsureRepo should recover from stale lock: %v", err)
	}
	if result2.Path != result.Path {
		t.Errorf("expected same cache path, got %q", result2.Path)
	}

	// Lock file should be gone after recovery
	if _, err := os.Stat(lockFile); !os.IsNotExist(err) {
		t.Error("shallow.lock should have been removed during recovery")
	}
}

func TestEnsureRepo_ShallowCorruption_RecoversViaReclone(t *testing.T) {
	// Set up a local git repo to clone
	srcDir := t.TempDir()
	runGit(t, srcDir, "init")
	runGit(t, srcDir, "config", "user.email", "test@test.com")
	runGit(t, srcDir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(srcDir, "data.txt"), []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, srcDir, "add", ".")
	runGit(t, srcDir, "commit", "-m", "init")

	t.Setenv("YNH_HOME", "")
	t.Setenv("HOME", t.TempDir())

	// First clone succeeds
	result, err := EnsureRepo(srcDir, "")
	if err != nil {
		t.Fatalf("initial clone failed: %v", err)
	}
	repoDir := result.Path

	// Simulate a shallow file corruption: replace gitCmdFunc so that the next
	// fetch returns the exact error git produces for this condition, then restores
	// to real git for the subsequent re-clone.
	callCount := 0
	orig := gitCmdFunc
	t.Cleanup(func() { gitCmdFunc = orig })
	gitCmdFunc = func(args ...string) error {
		callCount++
		isFetch := len(args) > 0 && args[0] == "-C" && len(args) > 2 && args[2] == "fetch"
		if callCount == 1 && isFetch {
			return errors.New("exit status 128\nfatal: shallow file has changed since we read it")
		}
		return orig(args...)
	}

	result2, err := EnsureRepo(srcDir, "")
	if err != nil {
		t.Fatalf("EnsureRepo should recover from shallow corruption: %v", err)
	}
	if result2.Path != repoDir {
		t.Errorf("expected same cache path after recovery, got %q", result2.Path)
	}
	if !result2.Cloned {
		t.Error("expected Cloned=true after re-clone recovery")
	}

	// Content should be accessible after recovery
	data, err := os.ReadFile(filepath.Join(result2.Path, "data.txt"))
	if err != nil {
		t.Fatalf("file not readable after recovery: %v", err)
	}
	if string(data) != "original" {
		t.Errorf("unexpected content after recovery: %q", string(data))
	}
}

func TestEnsureRepo_FreshClone_TransientShallowError_RetryRecovers(t *testing.T) {
	srcDir := t.TempDir()
	runGit(t, srcDir, "init")
	runGit(t, srcDir, "config", "user.email", "test@test.com")
	runGit(t, srcDir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(srcDir, "data.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, srcDir, "add", ".")
	runGit(t, srcDir, "commit", "-m", "init")

	t.Setenv("YNH_HOME", "")
	t.Setenv("HOME", t.TempDir())

	// First clone call fails with the exact transient error git emits when
	// its shallow file changes mid-operation. Subsequent calls delegate to
	// real git so the retry succeeds.
	callCount := 0
	orig := gitCmdFunc
	t.Cleanup(func() { gitCmdFunc = orig })
	gitCmdFunc = func(args ...string) error {
		callCount++
		isClone := len(args) > 0 && args[0] == "clone"
		if callCount == 1 && isClone {
			return errors.New("exit status 128\nfatal: shallow file has changed since we read it")
		}
		return orig(args...)
	}

	result, err := EnsureRepo(srcDir, "")
	if err != nil {
		t.Fatalf("EnsureRepo should retry on transient shallow error: %v", err)
	}
	if !result.Cloned {
		t.Error("expected Cloned=true after retry")
	}
	data, err := os.ReadFile(filepath.Join(result.Path, "data.txt"))
	if err != nil {
		t.Fatalf("file not readable after retry: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("unexpected content: %q", string(data))
	}
	if callCount < 2 {
		t.Errorf("expected at least 2 git calls (initial + retry), got %d", callCount)
	}
}

func TestEnsureRepo_FreshClone_NonTransientError_DoesNotRetry(t *testing.T) {
	t.Setenv("YNH_HOME", "")
	t.Setenv("HOME", t.TempDir())

	callCount := 0
	orig := gitCmdFunc
	t.Cleanup(func() { gitCmdFunc = orig })
	gitCmdFunc = func(args ...string) error {
		callCount++
		return errors.New("exit status 128\nfatal: repository not found")
	}

	_, err := EnsureRepo("https://example.invalid/missing.git", "")
	if err == nil {
		t.Fatal("expected error for non-transient failure")
	}
	if callCount != 1 {
		t.Errorf("expected exactly 1 git call (no retry on non-transient error), got %d", callCount)
	}
}

func TestEnsureRepo_ExistingDirWithoutGit_ClonesClean(t *testing.T) {
	srcDir := t.TempDir()
	runGit(t, srcDir, "init")
	runGit(t, srcDir, "config", "user.email", "test@test.com")
	runGit(t, srcDir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(srcDir, "file.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, srcDir, "add", ".")
	runGit(t, srcDir, "commit", "-m", "init")

	t.Setenv("YNH_HOME", "")
	t.Setenv("HOME", t.TempDir())

	// Pre-create the cache directory without a .git dir — simulates a prior
	// marketplace index fetch that left a non-clone artifact in the cache.
	cacheDir := config.CacheDir()
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	repoDir := filepath.Join(cacheDir, repoDirName(srcDir, ""))
	if err := os.MkdirAll(filepath.Join(repoDir, "some-leftover-dir"), 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := EnsureRepo(srcDir, "")
	if err != nil {
		t.Fatalf("EnsureRepo should clone over existing non-git dir: %v", err)
	}
	if !result.Cloned {
		t.Error("expected Cloned=true")
	}
	data, err := os.ReadFile(filepath.Join(result.Path, "file.txt"))
	if err != nil {
		t.Fatalf("cloned file not readable: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("unexpected content: %q", string(data))
	}
}

func TestPurgeCacheDirsForURL(t *testing.T) {
	t.Setenv("YNH_HOME", "")
	t.Setenv("HOME", t.TempDir())

	cacheDir := config.CacheDir()
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Create fake cache dirs matching the URL's org--repo prefix
	prefix := repoDirPrefix("https://github.com/myorg/myrepo")
	dirs := []string{
		prefix + "--aabbccdd",           // ref=""
		prefix + "--11223344",           // ref="v1"
		"otherorg--otherrepo--deadbeef", // different URL — must not be deleted
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(cacheDir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if err := PurgeCacheDirsForURL("https://github.com/myorg/myrepo"); err != nil {
		t.Fatalf("PurgeCacheDirsForURL: %v", err)
	}

	// Both matching dirs should be gone
	for _, d := range dirs[:2] {
		if _, err := os.Stat(filepath.Join(cacheDir, d)); !os.IsNotExist(err) {
			t.Errorf("expected %s to be deleted, stat err: %v", d, err)
		}
	}
	// Unrelated dir must survive
	if _, err := os.Stat(filepath.Join(cacheDir, dirs[2])); err != nil {
		t.Errorf("unrelated cache dir should survive: %v", err)
	}
}

func TestResolveFromCache_WithPath(t *testing.T) {
	// Create a monorepo-style repo
	srcDir := t.TempDir()
	runGit(t, srcDir, "init")
	runGit(t, srcDir, "config", "user.email", "test@test.com")
	runGit(t, srcDir, "config", "user.name", "Test")
	if err := os.MkdirAll(filepath.Join(srcDir, "pkg", "skills", "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "pkg", "skills", "deploy", "SKILL.md"), []byte("deploy"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, srcDir, "add", ".")
	runGit(t, srcDir, "commit", "-m", "init")

	t.Setenv("YNH_HOME", "")
	t.Setenv("HOME", t.TempDir())

	// Pre-populate cache
	if _, err := EnsureRepo(srcDir, ""); err != nil {
		t.Fatalf("EnsureRepo failed: %v", err)
	}

	p := &harness.Harness{
		Name: "test-path-cache",
		Includes: []harness.Include{
			{
				GitSource: harness.GitSource{Git: srcDir, Path: "pkg"},
				Pick:      []string{"skills/deploy"},
			},
		},
	}

	results, err := ResolveFromCache(p, nil)
	if err != nil {
		t.Fatalf("ResolveFromCache failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Path != "pkg" {
		t.Errorf("expected Path=%q, got %q", "pkg", results[0].Path)
	}
}
