package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
)

// `ynh run <folder> --trust` fetches a remote include pinned to a full SHA, but only one the
// allow-list names: with allowed_remote_sources empty it is refused before any fetch, and nothing
// reaches the cache (ynh #1058).
func TestFolderTrustHonoursTheAllowListBeforeFetching(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YNH_HOME", filepath.Join(home, ".ynh"))
	if err := os.MkdirAll(os.Getenv("YNH_HOME"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(os.Getenv("YNH_HOME"), "config.json"), []byte(`{"allowed_remote_sources":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	inc, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(inc, "skills", "hello"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inc, "skills", "hello", "SKILL.md"), []byte("---\nname: hello\ndescription: d\n---\nhi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, inc)
	runGitInDir(t, inc, "add", ".")
	runGitInDir(t, inc, "commit", "-m", "init")
	sha := gitRevParse(t, inc)

	folder := t.TempDir()
	hj := &plugin.HarnessJSON{Name: "fold", Version: "0.1.0", Includes: []plugin.IncludeMeta{{Git: "file://" + inc, Ref: sha}}}
	if err := plugin.SavePluginJSON(folder, hj); err != nil {
		t.Fatal(err)
	}
	p, err := harness.LoadDir(folder)
	if err != nil {
		t.Fatal(err)
	}

	err = acceptFolderRunOf(p, os.Stderr)
	if err == nil || !strings.Contains(err.Error(), "allowed sources list") {
		t.Fatalf("an include the allow-list does not name must be refused: %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(os.Getenv("YNH_HOME"), "cache")); len(entries) > 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("the include was fetched before the allow-list refused it: %s", strconv.Quote(strings.Join(names, ",")))
	}
}
