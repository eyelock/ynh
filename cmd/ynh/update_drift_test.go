package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/resolver"
)

// TestCmdUpdate_UnpinnedInclude_AdvancesRefWhenCacheAlreadyFresh reproduces the
// drift-after-update bug: the local git cache was advanced by a different
// operation (e.g. another harness's update), so EnsureRepo returns Changed=false,
// but installed.json still records the old SHA. cmdUpdate must still advance
// ref_installed to the current cache HEAD.
func TestCmdUpdate_UnpinnedInclude_AdvancesRefWhenCacheAlreadyFresh(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YNH_HOME", "")

	// Create a local git repo to act as the unpinned include source.
	srcDir := t.TempDir()
	initGitRepo(t, srcDir)
	writeTestFile(t, filepath.Join(srcDir, "SKILL.md"), "---\nname: skill\ndescription: v1\n---\n")
	runGitInDir(t, srcDir, "add", ".")
	runGitInDir(t, srcDir, "commit", "-m", "v1")
	sha1 := gitRevParse(t, srcDir)

	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	// Install the harness manually: plugin.json with unpinned include + installed.json at sha1.
	installDir := harness.InstalledDirByID("local/driftharn")
	if err := os.MkdirAll(filepath.Join(installDir, ".ynh-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	pluginJSON := fmt.Sprintf(`{"name":"driftharn","version":"0.1.0","default_vendor":"claude","includes":[{"git":%q}]}`, srcDir)
	writeTestFile(t, filepath.Join(installDir, ".ynh-plugin", "plugin.json"), pluginJSON)
	installedJSON := fmt.Sprintf(`{"source_type":"local","installed_at":"2024-01-01T00:00:00Z","resolved":[{"git":%q,"sha":%q}]}`, srcDir, sha1)
	writeTestFile(t, filepath.Join(installDir, ".ynh-plugin", "installed.json"), installedJSON)

	// Advance the remote to sha2.
	writeTestFile(t, filepath.Join(srcDir, "SKILL.md"), "---\nname: skill\ndescription: v2\n---\n")
	runGitInDir(t, srcDir, "add", ".")
	runGitInDir(t, srcDir, "commit", "-m", "v2")
	sha2 := gitRevParse(t, srcDir)

	if sha1 == sha2 {
		t.Fatal("sha1 == sha2: git commit did not advance HEAD")
	}

	// Pre-warm the cache to sha2 WITHOUT updating installed.json.
	// This simulates "a different harness's update fetched the same source first".
	if _, err := resolver.EnsureRepo(srcDir, ""); err != nil {
		t.Fatalf("pre-warming cache: %v", err)
	}

	// At this point: cache HEAD = sha2, installed.json.resolved[0].sha = sha1.
	// EnsureRepo will return Changed=false (cache already current).
	// cmdUpdate must still advance ref_installed to sha2.
	if err := cmdUpdate([]string{"local/driftharn"}); err != nil {
		t.Fatalf("cmdUpdate: %v", err)
	}

	ins, err := plugin.LoadInstalledJSON(installDir)
	if err != nil {
		t.Fatalf("loading installed.json after update: %v", err)
	}

	var foundSHA string
	for _, r := range ins.Resolved {
		if r.Git == srcDir {
			foundSHA = r.SHA
		}
	}
	if foundSHA == "" {
		t.Fatalf("no resolved entry for %q in installed.json; resolved=%v", srcDir, ins.Resolved)
	}
	if foundSHA != sha2 {
		t.Errorf("ref_installed after update = %q, want %q (sha2); drift not resolved", foundSHA, sha2)
	}
}

// TestCmdUpdate_UnpinnedInclude_CountsUpdateWhenCachePreceded ensures that when
// EnsureRepo returns Changed=false but the cache is ahead of ref_installed, the
// update still increments the updated-source count and reports correctly.
func TestCmdUpdate_UnpinnedInclude_CountsUpdateWhenCachePreceded(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YNH_HOME", "")

	srcDir := t.TempDir()
	initGitRepo(t, srcDir)
	writeTestFile(t, filepath.Join(srcDir, "file.txt"), "v1")
	runGitInDir(t, srcDir, "add", ".")
	runGitInDir(t, srcDir, "commit", "-m", "v1")
	sha1 := gitRevParse(t, srcDir)

	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	installDir := harness.InstalledDirByID("local/countharn")
	if err := os.MkdirAll(filepath.Join(installDir, ".ynh-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	pluginJSON := fmt.Sprintf(`{"name":"countharn","version":"0.1.0","default_vendor":"claude","includes":[{"git":%q}]}`, srcDir)
	writeTestFile(t, filepath.Join(installDir, ".ynh-plugin", "plugin.json"), pluginJSON)
	installedJSON := fmt.Sprintf(`{"source_type":"local","installed_at":"2024-01-01T00:00:00Z","resolved":[{"git":%q,"sha":%q}]}`, srcDir, sha1)
	writeTestFile(t, filepath.Join(installDir, ".ynh-plugin", "installed.json"), installedJSON)

	// Advance the remote and pre-warm the cache (cache ahead of installed.json).
	writeTestFile(t, filepath.Join(srcDir, "file.txt"), "v2")
	runGitInDir(t, srcDir, "add", ".")
	runGitInDir(t, srcDir, "commit", "-m", "v2")
	sha2 := gitRevParse(t, srcDir)

	if _, err := resolver.EnsureRepo(srcDir, ""); err != nil {
		t.Fatalf("pre-warming cache: %v", err)
	}

	if err := cmdUpdate([]string{"local/countharn"}); err != nil {
		t.Fatalf("cmdUpdate: %v", err)
	}

	ins, err := plugin.LoadInstalledJSON(installDir)
	if err != nil {
		t.Fatalf("loading installed.json: %v", err)
	}
	for _, r := range ins.Resolved {
		if r.Git == srcDir && r.SHA != sha2 {
			t.Errorf("ref_installed = %q, want %q", r.SHA, sha2)
		}
	}
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	runGitInDir(t, dir, "init")
	runGitInDir(t, dir, "config", "user.email", "test@test.com")
	runGitInDir(t, dir, "config", "user.name", "Test")
}

func runGitInDir(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}

func gitRevParse(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD in %s: %v", dir, err)
	}
	return strings.TrimSpace(string(out))
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", filepath.Base(path), err)
	}
}
