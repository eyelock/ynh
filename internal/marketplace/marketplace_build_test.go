package marketplace

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMarketplaceHarnessExport(t *testing.T) {
	configPath, configDir := setupMarketplace(t)
	outputDir := t.TempDir()

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}

	err = Build(cfg, BuildOptions{
		ConfigDir: configDir,
		OutputDir: outputDir,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Harness should have dual manifests
	harnessDir := filepath.Join(outputDir, "plugins", "export-test")
	assertFileExists(t, filepath.Join(harnessDir, ".claude-plugin", "plugin.json"))
	assertFileExists(t, filepath.Join(harnessDir, ".cursor-plugin", "plugin.json"))

	// Skills should be present
	assertFileExists(t, filepath.Join(harnessDir, "skills", "dev-project", "SKILL.md"))
}

func TestMarketplacePluginCopy(t *testing.T) {
	configPath, configDir := setupMarketplace(t)
	outputDir := t.TempDir()

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}

	err = Build(cfg, BuildOptions{
		ConfigDir: configDir,
		OutputDir: outputDir,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Plugin should be copied as-is
	pluginDir := filepath.Join(outputDir, "plugins", "my-tool")
	assertFileExists(t, filepath.Join(pluginDir, ".claude-plugin", "plugin.json"))
	assertFileExists(t, filepath.Join(pluginDir, "skills", "format", "SKILL.md"))
}

// TestBuildPluginEntryCopilotManifestAtRoot locks #471 for marketplace
// plugins: a copied plugin is a plugin, so a missing Copilot manifest is
// generated at its root whatever else the plugin carries, including a
// .copilot/ directory that once made the generator assume a `ynh run` layout.
func TestBuildPluginEntryCopilotManifestAtRoot(t *testing.T) {
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, ".agents", "harness"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(src, ".agents", "harness", "plugin.json"), map[string]any{
		"name": "has-copilot-dir", "version": "0.1.0",
	})
	if err := os.MkdirAll(filepath.Join(src, ".copilot"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, ".copilot", "notes.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "plugin")
	if err := buildPluginEntry(src, out, []string{"copilot"}); err != nil {
		t.Fatalf("buildPluginEntry: %v", err)
	}
	assertFileExists(t, filepath.Join(out, ".claude-plugin", "plugin.json"))
	assertFileNotExists(t, filepath.Join(out, ".copilot", ".claude-plugin"))
}

func TestMarketplacePluginMissingManifest(t *testing.T) {
	configPath, configDir := setupMarketplace(t)
	outputDir := t.TempDir()

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}

	err = Build(cfg, BuildOptions{
		ConfigDir: configDir,
		OutputDir: outputDir,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Plugin only had .claude-plugin/ — .cursor-plugin/ should be generated
	pluginDir := filepath.Join(outputDir, "plugins", "my-tool")
	assertFileExists(t, filepath.Join(pluginDir, ".cursor-plugin", "plugin.json"))
}

func TestMarketplaceCleanFlag(t *testing.T) {
	configPath, configDir := setupMarketplace(t)
	outputDir := t.TempDir()

	// Create stale content
	staleFile := filepath.Join(outputDir, "stale.txt")
	if err := os.WriteFile(staleFile, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}

	// Build (without clean — stale file should remain since we don't clean at package level)
	err = Build(cfg, BuildOptions{
		ConfigDir: configDir,
		OutputDir: outputDir,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Marketplace content should exist
	assertFileExists(t, filepath.Join(outputDir, ".claude-plugin", "marketplace.json"))
}

func TestMarketplaceDescriptionOverride(t *testing.T) {
	dir := t.TempDir()

	// Create plugin source (vendor-native format)
	writePluginManifest(t, filepath.Join(dir, "plugins", "widget"), "widget", "1.0.0", "Original description")

	// Marketplace config with description override
	configPath := filepath.Join(dir, "marketplace.json")
	writeJSON(t, configPath, map[string]any{
		"name":  "override-test",
		"owner": map[string]string{"name": "tester"},
		"harnesses": []map[string]string{
			{
				"type":        "plugin",
				"source":      "./plugins/widget",
				"description": "Overridden description",
			},
		},
	})

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}

	outputDir := t.TempDir()
	err = Build(cfg, BuildOptions{
		ConfigDir: dir,
		OutputDir: outputDir,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Check the index uses the overridden description
	data, err := os.ReadFile(filepath.Join(outputDir, ".claude-plugin", "marketplace.json"))
	if err != nil {
		t.Fatal(err)
	}

	var idx marketplaceJSON
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatal(err)
	}

	if idx.Plugins[0].Description != "Overridden description" {
		t.Errorf("description = %q, want %q", idx.Plugins[0].Description, "Overridden description")
	}
}

func TestMarketplaceBuildInitGitRepo(t *testing.T) {
	configPath, configDir := setupMarketplace(t)
	outputDir := t.TempDir()

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}

	err = Build(cfg, BuildOptions{
		ConfigDir: configDir,
		OutputDir: outputDir,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Output dir should now be a Git repo
	assertFileExists(t, filepath.Join(outputDir, ".git"))

	// Verify there's at least one commit
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = outputDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git rev-parse HEAD failed: %v\n%s", err, out)
	}
}

func TestMarketplaceBuildSkipsExistingGitRepo(t *testing.T) {
	configPath, configDir := setupMarketplace(t)
	outputDir := t.TempDir()

	// Pre-initialize a git repo with a known commit
	for _, args := range [][]string{
		{"init"},
		{"-c", "user.name=test", "-c", "user.email=test@test", "commit", "--allow-empty", "-m", "pre-existing"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = outputDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", args[0], err, out)
		}
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}

	err = Build(cfg, BuildOptions{
		ConfigDir: configDir,
		OutputDir: outputDir,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Should still have the original commit (Build should not re-init)
	cmd := exec.Command("git", "log", "--oneline")
	cmd.Dir = outputDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "pre-existing") {
		t.Errorf("expected pre-existing commit in log, got:\n%s", out)
	}
}

func TestMarketplaceVendorFiltering(t *testing.T) {
	configPath, configDir := setupMarketplace(t)
	outputDir := t.TempDir()

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}

	// Build for claude only
	err = Build(cfg, BuildOptions{
		ConfigDir: configDir,
		OutputDir: outputDir,
		Vendors:   []string{"claude"},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	assertFileExists(t, filepath.Join(outputDir, ".claude-plugin", "marketplace.json"))
	assertFileNotExists(t, filepath.Join(outputDir, ".cursor-plugin", "marketplace.json"))
}

func TestMarketplaceBuild_EntryPathTraversalBlocked(t *testing.T) {
	dir := t.TempDir()
	for _, badPath := range []string{"../../etc", "../secret", "/etc/passwd", "a/../../etc"} {
		cfg := &MarketplaceConfig{
			Name:  "test",
			Owner: MarketplaceOwner{Name: "tester"},
			Harnesses: []MarketplaceEntry{
				{Type: "plugin", Source: "./plugins/foo", Path: badPath},
			},
		}
		err := Build(cfg, BuildOptions{ConfigDir: dir, OutputDir: t.TempDir()})
		if err == nil {
			t.Errorf("path %q: expected error, got nil", badPath)
			continue
		}
		if !strings.Contains(err.Error(), "invalid path") {
			t.Errorf("path %q: unexpected error: %v", badPath, err)
		}
	}
}

// requireGit skips a test that needs a git binary on PATH rather than failing
// it, so a machine without git still runs the rest of the suite.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

func gitLines(t *testing.T, dir string, args ...string) []string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

func buildOnce(t *testing.T, configPath, configDir, outputDir string) {
	t.Helper()
	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := Build(cfg, BuildOptions{ConfigDir: configDir, OutputDir: outputDir}); err != nil {
		t.Fatalf("Build: %v", err)
	}
}

// OwnsRepo is how --clean tells a build output from somebody's source tree. A
// false positive would let --clean empty a real repository, so it needs both
// the marker and ynd's root commit, and nothing short of that may pass.
func TestOwnsRepo(t *testing.T) {
	requireGit(t)

	t.Run("a repo Build created is ours and carries the marker", func(t *testing.T) {
		configPath, configDir := setupMarketplace(t)
		out := t.TempDir()
		buildOnce(t, configPath, configDir, out)
		assertFileExists(t, filepath.Join(out, MarkerFile))
		if !OwnsRepo(out) {
			t.Error("OwnsRepo must recognise the repository Build just initialised")
		}
	})

	t.Run("a plain git init is not ours", func(t *testing.T) {
		dir := t.TempDir()
		gitLines(t, dir, "init")
		gitLines(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "--allow-empty", "-m", "theirs")
		if OwnsRepo(dir) {
			t.Error("a repository without the marker must never be ours")
		}
	})

	t.Run("a forged marker in a foreign repo is not enough", func(t *testing.T) {
		dir := t.TempDir()
		gitLines(t, dir, "init")
		gitLines(t, dir, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "--allow-empty", "-m", "theirs")
		if err := os.WriteFile(filepath.Join(dir, MarkerFile), []byte(markerContent), 0o644); err != nil {
			t.Fatal(err)
		}
		if OwnsRepo(dir) {
			t.Error("the marker alone must not claim a repository whose root commit is not ours")
		}
	})

	t.Run("a marker without a repository is not ours", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, MarkerFile), []byte(markerContent), 0o644); err != nil {
			t.Fatal(err)
		}
		if OwnsRepo(dir) {
			t.Error("no .git, nothing to own")
		}
	})

	t.Run("a .git file (worktree) is not ours even with the marker", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: /elsewhere"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, MarkerFile), []byte(markerContent), 0o644); err != nil {
			t.Fatal(err)
		}
		if OwnsRepo(dir) {
			t.Error("a worktree or submodule is somebody's source")
		}
	})
}

// Rebuilding into ynd's own repository commits on top of the previous build,
// so the history survives, and an identical rebuild adds nothing.
func TestMarketplaceBuild_RebuildIntoOwnRepoKeepsHistory(t *testing.T) {
	requireGit(t)
	configPath, configDir := setupMarketplace(t)
	out := t.TempDir()

	buildOnce(t, configPath, configDir, out)
	if got := len(gitLines(t, out, "log", "--format=%s")); got != 1 {
		t.Fatalf("after the first build: %d commits, want 1", got)
	}

	// Identical content: nothing to commit, and no failure from git saying so.
	buildOnce(t, configPath, configDir, out)
	if got := len(gitLines(t, out, "log", "--format=%s")); got != 1 {
		t.Errorf("an identical rebuild must not add a commit, got %d", got)
	}

	// Changed content: a second commit, and the first is still there.
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(string(raw), `"description": "Test marketplace"`, `"description": "Renamed"`, 1)
	if changed == string(raw) {
		t.Fatal("test fixture no longer contains the description this test edits")
	}
	if err := os.WriteFile(configPath, []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
	buildOnce(t, configPath, configDir, out)
	subjects := gitLines(t, out, "log", "--format=%s")
	if len(subjects) != 2 {
		t.Fatalf("after a changed rebuild: %d commits, want 2:\n%s", len(subjects), strings.Join(subjects, "\n"))
	}
	for _, s := range subjects {
		if s != commitMessage {
			t.Errorf("commit subject %q, want %q", s, commitMessage)
		}
	}
	if lines := gitLines(t, out, "status", "--porcelain"); len(lines) != 1 || lines[0] != "" {
		t.Errorf("the working tree should be clean after a build, got %q", lines)
	}
}
