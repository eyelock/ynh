package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/resolver"
)

// delegateGit makes a git repo holding a harness that declares the given
// delegates.
func delegateGit(t *testing.T, dir, name string, to ...string) {
	t.Helper()
	hj := &plugin.HarnessJSON{Name: name, Version: "0.1.0"}
	for _, d := range to {
		hj.DelegatesTo = append(hj.DelegatesTo, plugin.DelegateMeta{Git: d})
	}
	if err := plugin.SavePluginJSON(dir, hj); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, dir)
	runGitInDir(t, dir, "add", ".")
	runGitInDir(t, dir, "commit", "-m", "init")
}

// Install and update fetch a delegate's own delegates, so the whole flattened
// graph is cached before a run and `ynh trust` can read it.
func TestFetchNestedDelegates_FetchesTheWholeGraph(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	a, b, c, rootDir := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	delegateGit(t, c, "c")
	delegateGit(t, b, "b", c, rootDir)
	delegateGit(t, a, "a", b)
	delegateGit(t, rootDir, "root", a)
	// a is the root's own delegate, so the install loop fetched it already.
	if _, err := resolver.EnsureRepo(a, ""); err != nil {
		t.Fatal(err)
	}
	root := &harness.Harness{Name: "root", Dir: rootDir, DelegatesTo: []harness.Delegate{{GitSource: harness.GitSource{Git: a}}}}

	got, err := fetchNestedDelegates(root, &config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	var fetched []string
	for _, g := range got {
		fetched = append(fetched, g.Git)
		if g.SHA == "" {
			t.Errorf("%s has no recorded SHA", g.Git)
		}
	}
	// b names the root, which is only known to be the root once resolved, so
	// it is fetched too; it is never a delegate (views below).
	if !slices.Equal(fetched, []string{b, c, rootDir}) {
		t.Errorf("fetched = %v, want [b c root]", fetched)
	}
	// Nothing needs the network now: trust reads from the cache alone.
	views, err := delegateViews(root)
	if err != nil {
		t.Fatalf("trust cannot read the graph after the fetch: %v", err)
	}
	if len(views) != 3 {
		t.Errorf("delegateViews = %d delegates, want 3 (a, b, c)", len(views))
	}
}

// A nested delegate is held to the allow-list like any other source.
func TestFetchNestedDelegates_HonoursAllowList(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	a, b, rootDir := t.TempDir(), t.TempDir(), t.TempDir()
	delegateGit(t, b, "b")
	delegateGit(t, a, "a", b)
	if _, err := resolver.EnsureRepo(a, ""); err != nil {
		t.Fatal(err)
	}
	root := &harness.Harness{Name: "root", Dir: rootDir, DelegatesTo: []harness.Delegate{{GitSource: harness.GitSource{Git: a}}}}

	_, err := fetchNestedDelegates(root, &config.Config{AllowedRemoteSources: []string{a}})
	if err == nil || !strings.Contains(err.Error(), "not in the allowed sources list") {
		t.Fatalf("want an allow-list refusal naming the nested source, got %v", err)
	}
}
