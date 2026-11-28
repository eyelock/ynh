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

// Every command that fetches a git source into the cache and writes the manifest that names it holds
// the allow-list first: with allowed_remote_sources empty each is refused, nothing is cached and the
// manifest is unchanged (ynh #1058).
func TestEditCommandsHoldTheAllowListBeforeFetching(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YNH_HOME", filepath.Join(home, ".ynh"))
	if err := os.MkdirAll(os.Getenv("YNH_HOME"), 0o755); err != nil {
		t.Fatal(err)
	}
	const sha = "0123456789abcdef0123456789abcdef01234567"
	const sha2 = "89abcdef0123456789abcdef0123456789abcdef"
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repo)
	runGitInDir(t, repo, "add", ".")
	runGitInDir(t, repo, "commit", "-m", "init")
	inc := "file://" + repo
	id := installGitHarness(t, "github.com/o/plain", "", sha, "plain",
		`,"includes":[{"git":`+strconv.Quote(inc)+`,"ref":"`+sha+`"}],`+
			`"delegates_to":[{"git":"github.com/o/dg","ref":"`+sha+`"}],`+
			`"profiles":{"p":{"includes":[{"git":`+strconv.Quote(inc)+`,"ref":"`+sha+`"}]}}`)
	if err := os.WriteFile(filepath.Join(os.Getenv("YNH_HOME"), "config.json"), []byte(`{"allowed_remote_sources":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := func() string {
		b, _ := os.ReadFile(filepath.Join(harness.InstalledDirByID(id), ".agents", "harness", "plugin.json"))
		return string(b)
	}
	before := manifest()
	cases := map[string]func() error{
		"include add":            func() error { return cmdInclude([]string{"add", id, inc + "x", "--ref", sha, "--git"}) },
		"include update":         func() error { return cmdInclude([]string{"update", id, inc, "--ref", sha2}) },
		"profile include add":    func() error { return cmdProfile([]string{"include", "add", id, "p", inc + "x", "--ref", sha, "--git"}) },
		"profile include update": func() error { return cmdProfile([]string{"include", "update", id, "p", inc, "--ref", sha2}) },
		"delegate add":           func() error { return cmdDelegate([]string{"add", id, "https://example.invalid/dg2", "--ref", sha}) },
		"delegate update":        func() error { return cmdDelegate([]string{"update", id, "github.com/o/dg", "--ref", sha2}) },
	}
	for name, run := range cases {
		err := run()
		if err == nil || !strings.Contains(err.Error(), "allowed sources list") {
			t.Errorf("%s: a source the allow-list does not name must be refused, got %v", name, err)
		}
		if entries, _ := os.ReadDir(filepath.Join(os.Getenv("YNH_HOME"), "cache")); len(entries) > 0 {
			t.Errorf("%s: fetched into the cache before the allow-list refused it", name)
		}
	}
	if after := manifest(); after != before {
		t.Errorf("a refused command changed the manifest:\n%s\n---\n%s", before, after)
	}
}
