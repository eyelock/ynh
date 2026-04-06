package resolver

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
)

func TestNormalizeGitURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"github.com/user/repo", "git@github.com:user/repo.git"},
		{"https://github.com/user/repo.git", "https://github.com/user/repo.git"},
		{"git@github.com:user/repo.git", "git@github.com:user/repo.git"},
		{"https://gitlab.com/user/repo", "https://gitlab.com/user/repo"},
		{"/tmp/local-repo", "/tmp/local-repo"},
		{"./relative-repo", "./relative-repo"},
	}

	for _, tt := range tests {
		got := NormalizeGitURL(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeGitURL(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestRepoDirName(t *testing.T) {
	name1 := repoDirName("github.com/user/repo", "")
	name2 := repoDirName("github.com/other/repo", "")

	if name1 == name2 {
		t.Errorf("expected different cache dirs, both got %q", name1)
	}

	if len(name1) < 5 {
		t.Errorf("cache dir name too short: %q", name1)
	}
}

func TestRepoDirName_Deterministic(t *testing.T) {
	name1 := repoDirName("github.com/user/repo", "v1.0.0")
	name2 := repoDirName("github.com/user/repo", "v1.0.0")

	if name1 != name2 {
		t.Errorf("repoDirName not deterministic: %q != %q", name1, name2)
	}
}

func TestRepoDirName_ContainsOrgAndRepo(t *testing.T) {
	name := repoDirName("github.com/user/my-skills", "")
	if !strings.HasPrefix(name, "user--my-skills--") {
		t.Errorf("repoDirName should be org--repo--hash, got %q", name)
	}
}

func TestRepoDirName_SSHUrl(t *testing.T) {
	name := repoDirName("git@github.com:eyelock/claude-config.git", "")
	if !strings.HasPrefix(name, "eyelock--claude-config--") {
		t.Errorf("SSH URL should produce org--repo--hash, got %q", name)
	}
}

func TestRepoDirName_HTTPSUrl(t *testing.T) {
	name := repoDirName("https://github.com/brianlovin/claude-config.git", "")
	if !strings.HasPrefix(name, "brianlovin--claude-config--") {
		t.Errorf("HTTPS URL should produce org--repo--hash, got %q", name)
	}
}

func TestRepoDirName_DifferentRefsGetDifferentDirs(t *testing.T) {
	name1 := repoDirName("github.com/user/repo", "v1.0.0")
	name2 := repoDirName("github.com/user/repo", "v2.0.0")
	nameNoRef := repoDirName("github.com/user/repo", "")

	if name1 == name2 {
		t.Error("same repo at different refs should get different cache dirs")
	}
	if name1 == nameNoRef {
		t.Error("same repo with ref vs without ref should get different cache dirs")
	}
}

func TestResolve_EmptyIncludes(t *testing.T) {
	p := &harness.Harness{
		Name:     "empty",
		Includes: nil,
	}

	results, err := Resolve(p, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestEnsureRepo_LocalGitRepo(t *testing.T) {
	// Create a local git repo to test cloning
	srcDir := t.TempDir()
	runGit(t, srcDir, "init")
	runGit(t, srcDir, "config", "user.email", "test@test.com")
	runGit(t, srcDir, "config", "user.name", "Test")

	if err := os.WriteFile(filepath.Join(srcDir, "test.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, srcDir, "add", ".")
	runGit(t, srcDir, "commit", "-m", "init")

	// Override cache dir for testing
	cacheDir := t.TempDir()
	t.Setenv("YNH_HOME", "")
	t.Setenv("HOME", filepath.Dir(cacheDir))

	result, err := EnsureRepo(srcDir, "")
	if err != nil {
		t.Fatalf("ensureRepo failed: %v", err)
	}
	if !result.Cloned {
		t.Error("expected Cloned=true for first clone")
	}

	// Verify the cloned content
	data, err := os.ReadFile(filepath.Join(result.Path, "test.txt"))
	if err != nil {
		t.Fatalf("cloned file not found: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("cloned content = %q, want %q", string(data), "hello")
	}

	// Second call should reuse cache (not error)
	result2, err := EnsureRepo(srcDir, "")
	if err != nil {
		t.Fatalf("second ensureRepo failed: %v", err)
	}
	if result2.Path != result.Path {
		t.Errorf("cache not reused: %q != %q", result2.Path, result.Path)
	}
	if result2.Cloned {
		t.Error("expected Cloned=false for cached repo")
	}
	if result2.Changed {
		t.Error("expected Changed=false when nothing changed")
	}
}

func TestResolve_WithLocalRepo(t *testing.T) {
	// Create a local git repo with skills
	srcDir := t.TempDir()
	runGit(t, srcDir, "init")
	runGit(t, srcDir, "config", "user.email", "test@test.com")
	runGit(t, srcDir, "config", "user.name", "Test")

	if err := os.MkdirAll(filepath.Join(srcDir, "skills", "hello"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "skills", "hello", "SKILL.md"), []byte("hello skill"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, srcDir, "add", ".")
	runGit(t, srcDir, "commit", "-m", "init")

	t.Setenv("YNH_HOME", "")
	t.Setenv("HOME", t.TempDir())

	p := &harness.Harness{
		Name: "test",
		Includes: []harness.Include{
			{
				GitSource: harness.GitSource{Git: srcDir},
				Pick:      []string{"skills/hello"},
			},
		},
	}

	results, err := Resolve(p, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if len(results[0].Content.Paths) != 1 || results[0].Content.Paths[0] != "skills/hello" {
		t.Errorf("unexpected paths: %v", results[0].Content.Paths)
	}

	// Verify the file exists in the resolved base path
	skillPath := filepath.Join(results[0].Content.BasePath, "skills", "hello", "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		t.Errorf("skill not found in resolved content: %v", err)
	}
}

func TestResolve_WithPath_Monorepo(t *testing.T) {
	// Create a local git repo simulating a monorepo with nested content
	srcDir := t.TempDir()
	runGit(t, srcDir, "init")
	runGit(t, srcDir, "config", "user.email", "test@test.com")
	runGit(t, srcDir, "config", "user.name", "Test")

	// Monorepo structure: packages/ai-config/skills/deploy/SKILL.md
	for _, dir := range []string{
		filepath.Join("packages", "ai-config", "skills", "deploy"),
		filepath.Join("packages", "ai-config", "agents"),
		filepath.Join("packages", "webapp"),
	} {
		if err := os.MkdirAll(filepath.Join(srcDir, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range map[string]string{
		"packages/ai-config/skills/deploy/SKILL.md": "deploy skill",
		"packages/ai-config/agents/ops.md":          "ops agent",
		"packages/webapp/index.ts":                  "app code",
	} {
		if err := os.WriteFile(filepath.Join(srcDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	runGit(t, srcDir, "add", ".")
	runGit(t, srcDir, "commit", "-m", "init monorepo")

	t.Setenv("YNH_HOME", "")
	t.Setenv("HOME", t.TempDir())

	p := &harness.Harness{
		Name: "test-monorepo",
		Includes: []harness.Include{
			{
				GitSource: harness.GitSource{Git: srcDir, Path: "packages/ai-config"},
				Pick:      []string{"skills/deploy"},
			},
		},
	}

	results, err := Resolve(p, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// BasePath should point to the subdirectory, not the repo root
	expectedBase := filepath.Join(results[0].Content.BasePath)
	skillPath := filepath.Join(expectedBase, "skills", "deploy", "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		t.Errorf("skill not found at monorepo path: %v", err)
	}

	// The base path should NOT contain the webapp directory
	webappPath := filepath.Join(expectedBase, "packages", "webapp")
	if _, err := os.Stat(webappPath); err == nil {
		t.Error("base path should be scoped to packages/ai-config, not repo root")
	}
}

func TestResolve_WithPath_NotFound(t *testing.T) {
	srcDir := t.TempDir()
	runGit(t, srcDir, "init")
	runGit(t, srcDir, "config", "user.email", "test@test.com")
	runGit(t, srcDir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(srcDir, "README.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, srcDir, "add", ".")
	runGit(t, srcDir, "commit", "-m", "init")

	t.Setenv("YNH_HOME", "")
	t.Setenv("HOME", t.TempDir())

	p := &harness.Harness{
		Name: "test-bad-path",
		Includes: []harness.Include{
			{
				GitSource: harness.GitSource{Git: srcDir, Path: "nonexistent/path"},
			},
		},
	}

	_, err := Resolve(p, nil)
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
}

func TestResolve_WithPath_NoPickIncludesAll(t *testing.T) {
	// Monorepo with path but no pick - should include all artifacts from that path
	srcDir := t.TempDir()
	runGit(t, srcDir, "init")
	runGit(t, srcDir, "config", "user.email", "test@test.com")
	runGit(t, srcDir, "config", "user.name", "Test")

	for _, dir := range []string{
		filepath.Join("config", "skills", "lint"),
		filepath.Join("config", "rules"),
	} {
		if err := os.MkdirAll(filepath.Join(srcDir, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range map[string]string{
		"config/skills/lint/SKILL.md": "lint skill",
		"config/rules/strict.md":      "be strict",
	} {
		if err := os.WriteFile(filepath.Join(srcDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, srcDir, "add", ".")
	runGit(t, srcDir, "commit", "-m", "init")

	t.Setenv("YNH_HOME", "")
	t.Setenv("HOME", t.TempDir())

	p := &harness.Harness{
		Name: "test-path-all",
		Includes: []harness.Include{
			{
				GitSource: harness.GitSource{Git: srcDir, Path: "config"},
			},
		},
	}

	results, err := Resolve(p, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// No pick means Paths should be empty (include all)
	if len(results[0].Content.Paths) != 0 {
		t.Errorf("expected empty paths for no-pick, got %v", results[0].Content.Paths)
	}

	// Verify both artifacts are reachable from base path
	skillPath := filepath.Join(results[0].Content.BasePath, "skills", "lint", "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		t.Errorf("skill not found: %v", err)
	}
	rulePath := filepath.Join(results[0].Content.BasePath, "rules", "strict.md")
	if _, err := os.Stat(rulePath); err != nil {
		t.Errorf("rule not found: %v", err)
	}
}

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
	_ = result

	// Corrupt the remote by removing the source repo's .git
	if err := os.RemoveAll(filepath.Join(srcDir, ".git")); err != nil {
		t.Fatal(err)
	}

	// Second call: update should fail (fetch from a non-git directory)
	_, err = EnsureRepo(srcDir, "")
	if err == nil {
		t.Fatal("expected error when fetching from corrupted remote, got nil")
	}

	// Verify the error message contains useful context
	errStr := err.Error()
	if !strings.Contains(errStr, "git fetch") {
		t.Errorf("error should mention git fetch, got: %s", errStr)
	}

	// Verify the cached repo still exists (we don't blow it away on update failure)
	if _, statErr := os.Stat(result.Path); os.IsNotExist(statErr) {
		t.Error("cached repo should still exist after failed update")
	}
}

func TestResolve_BlockedByAllowList(t *testing.T) {
	cfg := &config.Config{
		AllowedRemoteSources: []string{
			"github.com/trusted-org/*",
		},
	}

	p := &harness.Harness{
		Name: "test-blocked",
		Includes: []harness.Include{
			{
				GitSource: harness.GitSource{Git: "github.com/untrusted-org/repo"},
			},
		},
	}

	_, err := Resolve(p, cfg)
	if err == nil {
		t.Fatal("expected error for blocked remote source")
	}
	if !strings.Contains(err.Error(), "not in the allowed sources list") {
		t.Errorf("error should mention allow list, got: %v", err)
	}
}

func TestResolve_AllowedByAllowList(t *testing.T) {
	// Create a local git repo to use as the "allowed" source
	srcDir := t.TempDir()
	runGit(t, srcDir, "init")
	runGit(t, srcDir, "config", "user.email", "test@test.com")
	runGit(t, srcDir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(srcDir, "README.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, srcDir, "add", ".")
	runGit(t, srcDir, "commit", "-m", "init")

	t.Setenv("YNH_HOME", "")
	t.Setenv("HOME", t.TempDir())

	// The source is a local path, but we use it as the Git field.
	// The allow-list pattern uses ** to match local paths too.
	cfg := &config.Config{
		AllowedRemoteSources: []string{
			// Local paths start with / so we need a pattern that matches them.
			// Use the exact path as a literal match.
			srcDir,
		},
	}

	p := &harness.Harness{
		Name: "test-allowed",
		Includes: []harness.Include{
			{
				GitSource: harness.GitSource{Git: srcDir},
			},
		},
	}

	results, err := Resolve(p, cfg)
	if err != nil {
		t.Fatalf("Resolve should succeed for allowed source: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

func TestResolve_EmptyAllowListBlocksAll(t *testing.T) {
	cfg := &config.Config{
		AllowedRemoteSources: []string{},
	}

	p := &harness.Harness{
		Name: "test-empty-list",
		Includes: []harness.Include{
			{
				GitSource: harness.GitSource{Git: "github.com/any-org/any-repo"},
			},
		},
	}

	_, err := Resolve(p, cfg)
	if err == nil {
		t.Fatal("empty allow list should block all remote sources")
	}
}

func TestResolve_NilConfigAllowsAll(t *testing.T) {
	p := &harness.Harness{
		Name:     "test-nil",
		Includes: nil,
	}

	// nil config should not panic or error
	results, err := Resolve(p, nil)
	if err != nil {
		t.Fatalf("nil config should allow all: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty includes, got %d", len(results))
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

func TestShortGitURL(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"github.com/eyelock/assistants", "eyelock/assistants"},
		{"/tmp/local", "/tmp/local"},
		{"./relative", "./relative"},
		{"solo", "solo"},
	}
	for _, tt := range tests {
		got := ShortGitURL(tt.input)
		if got != tt.want {
			t.Errorf("ShortGitURL(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	gitArgs := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", gitArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}
