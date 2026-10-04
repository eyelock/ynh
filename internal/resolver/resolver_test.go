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

func TestShortGitURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"shorthand", "github.com/eyelock/assistants", "eyelock/assistants"},
		{"https", "https://github.com/eyelock/assistants", "eyelock/assistants"},
		{"https with .git", "https://github.com/eyelock/assistants.git", "eyelock/assistants"},
		{"https with trailing slash", "https://github.com/eyelock/assistants/", "eyelock/assistants"},
		{"http", "http://git.example.com/org/repo", "org/repo"},
		{"https with port", "https://git.example.com:8443/org/repo.git", "org/repo"},
		{"https host only", "https://github.com", "github.com"},
		{"scp ssh", "git@github.com:eyelock/assistants.git", "eyelock/assistants"},
		{"scp ssh without .git", "git@github.com:eyelock/assistants", "eyelock/assistants"},
		{"ssh scheme", "ssh://git@github.com/eyelock/assistants.git", "eyelock/assistants"},
		{"ssh scheme with port", "ssh://git@git.example.com:2222/org/repo.git", "org/repo"},
		{"file url", "file:///tmp/repos/inc", "/tmp/repos/inc"},
		{"absolute path", "/tmp/local", "/tmp/local"},
		{"relative path", "./relative", "./relative"},
		{"parent relative path", "../sibling", "../sibling"},
		{"single word", "solo", "solo"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ShortGitURL(tt.input); got != tt.want {
				t.Errorf("ShortGitURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestResolve_RelativeGitSourceUsesHarnessDir pins #461: a relative git
// source is cloned from the harness directory, not from wherever ynh happens
// to be running, so the clone agrees with the allow-list check.
func TestResolve_RelativeGitSourceUsesHarnessDir(t *testing.T) {
	root := t.TempDir()
	harnessDir := filepath.Join(root, "h")
	elsewhere := filepath.Join(root, "elsewhere")
	for _, d := range []string{harnessDir, elsewhere} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(elsewhere)

	var fetched []string
	fetch := func(url, ref string) (RepoResult, error) {
		fetched = append(fetched, url)
		return RepoResult{Path: t.TempDir()}, nil
	}

	p := &harness.Harness{
		Name: "h",
		Dir:  harnessDir,
		Includes: []harness.Include{
			{GitSource: harness.GitSource{Git: "./inc"}},
			{GitSource: harness.GitSource{Git: "../shared"}},
			{GitSource: harness.GitSource{Git: "/abs/repo"}},
			{GitSource: harness.GitSource{Git: "https://github.com/eyelock/assistants"}},
		},
	}
	if _, err := resolveWith(p, nil, fetch); err != nil {
		t.Fatalf("resolveWith: %v", err)
	}
	want := []string{
		filepath.Join(harnessDir, "inc"),
		filepath.Join(root, "shared"),
		"/abs/repo",
		"https://github.com/eyelock/assistants",
	}
	if strings.Join(fetched, "\n") != strings.Join(want, "\n") {
		t.Errorf("fetched\n  %q\nwant\n  %q", fetched, want)
	}
}

func TestResolveGitSource_RelativeUsesHarnessDir(t *testing.T) {
	harnessDir := t.TempDir()
	t.Chdir(t.TempDir())

	var fetched string
	fetch := func(url, ref string) (RepoResult, error) {
		fetched = url
		return RepoResult{Path: t.TempDir()}, nil
	}
	if _, _, err := resolveGitSourceWith(harness.GitSource{Git: "./del"}, harnessDir, fetch); err != nil {
		t.Fatalf("resolveGitSourceWith: %v", err)
	}
	if want := filepath.Join(harnessDir, "del"); fetched != want {
		t.Errorf("fetched %q, want %q", fetched, want)
	}
}

func TestGitSourceURL(t *testing.T) {
	cwd := t.TempDir()
	t.Chdir(cwd)
	// t.TempDir can sit behind a symlink (macOS /var -> /private/var), and
	// Abs works from the logical working directory, so compare against that.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name, source, dir, want string
	}{
		{"relative joined to harness", "./inc", "/h/one", "/h/one/inc"},
		{"parent joined to harness", "../shared", "/h/one", "/h/shared"},
		{"unclean relative is cleaned", "./a/../inc/", "/h/one", "/h/one/inc"},
		{"no harness dir falls back to cwd", "./inc", "", filepath.Join(wd, "inc")},
		{"absolute unchanged", "/abs/repo", "/h/one", "/abs/repo"},
		{"file url unchanged", "file:///abs/repo", "/h/one", "file:///abs/repo"},
		{"https unchanged", "https://github.com/o/r", "/h/one", "https://github.com/o/r"},
		{"ssh unchanged", "git@github.com:o/r.git", "/h/one", "git@github.com:o/r.git"},
		{"shorthand unchanged", "github.com/o/r", "/h/one", "github.com/o/r"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GitSourceURL(tt.source, tt.dir); got != tt.want {
				t.Errorf("GitSourceURL(%q, %q) = %q, want %q", tt.source, tt.dir, got, tt.want)
			}
		})
	}
}

// Two harnesses that each name "./inc" mean two different repos, so they must
// not share a cache entry.
func TestGitSourceURL_CacheKeyPerHarness(t *testing.T) {
	a := repoDirName(GitSourceURL("./inc", "/h/one"), "")
	b := repoDirName(GitSourceURL("./inc", "/h/two"), "")
	if a == b {
		t.Errorf("both harnesses map ./inc to cache entry %q", a)
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

func TestResolve_LocalInclude_Relative(t *testing.T) {
	// Harness root contains a child "extras/" with an artifact.
	harnessDir := t.TempDir()
	extras := filepath.Join(harnessDir, "extras", "skills", "demo")
	if err := os.MkdirAll(extras, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extras, "SKILL.md"),
		[]byte("---\nname: demo\ndescription: d\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := &harness.Harness{
		Name: "h",
		Dir:  harnessDir,
		Includes: []harness.Include{
			{GitSource: harness.GitSource{Local: "extras"}},
		},
	}

	results, err := Resolve(p, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	want := filepath.Join(harnessDir, "extras")
	if results[0].Content.BasePath != want {
		t.Errorf("BasePath = %q, want %q", results[0].Content.BasePath, want)
	}
	if !results[0].Cached || results[0].Cloned {
		t.Errorf("local include should report Cached=true Cloned=false, got Cached=%v Cloned=%v",
			results[0].Cached, results[0].Cloned)
	}
}

func TestResolve_LocalInclude_Absolute(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rules", "r.md"), []byte("r"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := &harness.Harness{
		Name: "h",
		Dir:  "/does/not/matter", // absolute Local ignores Dir
		Includes: []harness.Include{
			{GitSource: harness.GitSource{Local: dir}},
		},
	}

	results, err := Resolve(p, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if results[0].Content.BasePath != dir {
		t.Errorf("BasePath = %q, want %q", results[0].Content.BasePath, dir)
	}
}

func TestResolve_LocalInclude_Missing(t *testing.T) {
	harnessDir := t.TempDir()
	p := &harness.Harness{
		Name: "h",
		Dir:  harnessDir,
		Includes: []harness.Include{
			{GitSource: harness.GitSource{Local: "does-not-exist"}},
		},
	}

	_, err := Resolve(p, nil)
	if err == nil {
		t.Fatal("expected error for missing local include")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

func TestResolveLocalSource_TraversalBlocked(t *testing.T) {
	base := t.TempDir()
	// Create a real subdir so the local path resolves — the traversal in path should still be blocked.
	subdir := filepath.Join(base, "subdir")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		local string
		path  string
	}{
		// Relative local paths with .. are blocked
		{local: "../../../etc", path: ""},
		// Path component with .. is blocked even when local resolves
		{local: "subdir", path: "../../etc"},
		// Absolute path component is blocked
		{local: "subdir", path: "/etc/passwd"},
	} {
		gs := harness.GitSource{Local: tc.local, Path: tc.path}
		_, err := resolveLocalSource(gs, base)
		if err == nil {
			t.Errorf("local=%q path=%q: expected error, got nil", tc.local, tc.path)
			continue
		}
		if !strings.Contains(err.Error(), "must not traverse") && !strings.Contains(err.Error(), "must be relative") {
			t.Errorf("local=%q path=%q: unexpected error: %v", tc.local, tc.path, err)
		}
	}
}

func TestResolveGitSource_PathTraversalBlocked(t *testing.T) {
	repoDir := t.TempDir()
	fetch := func(url, ref string) (RepoResult, error) {
		return RepoResult{Path: repoDir}, nil
	}

	for _, badPath := range []string{"../../etc", "../secret", "/etc/passwd"} {
		gs := harness.GitSource{Git: "github.com/org/repo", Path: badPath}
		_, _, err := resolveGitSourceWith(gs, "", fetch)
		if err == nil {
			t.Errorf("path %q: expected error, got nil", badPath)
			continue
		}
		if !strings.Contains(err.Error(), "must not traverse") && !strings.Contains(err.Error(), "must be relative") {
			t.Errorf("path %q: unexpected error: %v", badPath, err)
		}
	}
}

// TestLsRemoteFunc_ExactRefMatch ensures that LsRemoteFunc resolves "main" to
// refs/heads/main only, even when the remote also contains branches whose names
// end in "/main" (e.g. "daisy/caffeinate/main"). Without exact-match filtering
// git ls-remote does suffix pattern matching and returns the ambiguous branch
// first, which caused phantom ref_available drift in ynh status.
func TestLsRemoteFunc_ExactRefMatch(t *testing.T) {
	// Build a local bare-ish repo with two branches: "main" and "shadow/main".
	srcDir := t.TempDir()
	runGit(t, srcDir, "init", "-b", "main")
	runGit(t, srcDir, "config", "user.email", "test@test.com")
	runGit(t, srcDir, "config", "user.name", "Test")

	// First commit on main.
	f := filepath.Join(srcDir, "file.txt")
	if err := os.WriteFile(f, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, srcDir, "add", ".")
	runGit(t, srcDir, "commit", "-m", "v1")
	mainSHA := strings.TrimSpace(func() string {
		cmd := exec.Command("git", "-C", srcDir, "rev-parse", "HEAD")
		out, _ := cmd.Output()
		return string(out)
	}())

	// Create shadow/main at a different commit.
	runGit(t, srcDir, "checkout", "-b", "shadow/main")
	if err := os.WriteFile(f, []byte("shadow"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, srcDir, "add", ".")
	runGit(t, srcDir, "commit", "-m", "shadow")
	shadowSHA := strings.TrimSpace(func() string {
		cmd := exec.Command("git", "-C", srcDir, "rev-parse", "HEAD")
		out, _ := cmd.Output()
		return string(out)
	}())

	// Switch back to main so HEAD points there.
	runGit(t, srcDir, "checkout", "main")

	if mainSHA == shadowSHA {
		t.Fatal("mainSHA == shadowSHA: branches are identical, test is invalid")
	}

	got, err := LsRemoteFunc(srcDir, "main")
	if err != nil {
		t.Fatalf("LsRemoteFunc: %v", err)
	}
	if got != mainSHA {
		t.Errorf("LsRemoteFunc(main) = %q, want %q (refs/heads/main SHA); got shadow/main SHA instead", got, mainSHA)
	}
	if got == shadowSHA {
		t.Errorf("LsRemoteFunc returned shadow/main SHA — suffix-match bug not fixed")
	}
}

// TestResolve_LocalIncludesAndAllowList: a "local" include inside the harness
// is part of the harness and never checked; one at an absolute path is a
// source like any other and must be listed.
func TestResolve_LocalIncludesAndAllowList(t *testing.T) {
	harnessDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(harnessDir, "bundled"), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()

	tests := []struct {
		name    string
		local   string
		allow   []string
		wantErr string
	}{
		{"bundled relative is not a source", "bundled", []string{}, ""},
		{"absolute unlisted is refused", outside, []string{"github.com/eyelock/**"},
			`include "` + outside + `": source "` + outside + `" is not in the allowed sources list (add "` + outside + `" to allowed_remote_sources)`},
		{"absolute listed is allowed", outside, []string{outside}, ""},
		{"absolute under a listed glob is allowed", outside, []string{filepath.Dir(outside) + "/*"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &harness.Harness{
				Name:     "local-incs",
				Dir:      harnessDir,
				Includes: []harness.Include{{GitSource: harness.GitSource{Local: tt.local}}},
			}
			_, err := Resolve(p, &config.Config{AllowedRemoteSources: tt.allow})
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Resolve: %v", err)
				}
				return
			}
			if err == nil || err.Error() != tt.wantErr {
				t.Fatalf("Resolve error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

// TestResolve_RelativeGitIncludeMatchedAgainstHarnessDir: a relative git
// include is resolved against the harness directory before matching, so the
// allow-list entry is an absolute path.
func TestResolve_RelativeGitIncludeMatchedAgainstHarnessDir(t *testing.T) {
	harnessDir := t.TempDir()
	p := &harness.Harness{
		Name:     "rel-git",
		Dir:      harnessDir,
		Includes: []harness.Include{{GitSource: harness.GitSource{Git: "./inc"}}},
	}
	_, err := Resolve(p, &config.Config{AllowedRemoteSources: []string{"github.com/eyelock/**"}})
	want := `(add "` + filepath.Join(harnessDir, "inc") + `" to allowed_remote_sources)`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("Resolve error = %v, want it to contain %q", err, want)
	}
}
