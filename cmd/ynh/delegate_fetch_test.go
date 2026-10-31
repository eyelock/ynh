package main

import (
	"fmt"
	"os"
	"path/filepath"
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
	// The root itself is not recorded.
	if !slices.Equal(fetched, []string{b, c}) {
		t.Errorf("fetched = %v, want [b c]", fetched)
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

// A nested relative source is recorded anchored to its declaring delegate.
func TestFetchNestedDelegates_RecordsAnchoredSource(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	a, rootDir := t.TempDir(), t.TempDir()
	delegateGit(t, a, "a", "../sibling-b")
	if _, err := resolver.EnsureRepo(a, ""); err != nil {
		t.Fatal(err)
	}
	cached, _, err := resolver.ResolveGitSourceFromCache(harness.GitSource{Git: a}, rootDir)
	if err != nil {
		t.Fatal(err)
	}
	sib := filepath.Join(filepath.Dir(cached), "sibling-b")
	if err := os.MkdirAll(sib, 0o755); err != nil {
		t.Fatal(err)
	}
	delegateGit(t, sib, "b")
	root := &harness.Harness{Name: "root", Dir: rootDir, DelegatesTo: []harness.Delegate{{GitSource: harness.GitSource{Git: a}}}}
	got, err := fetchNestedDelegates(root, &config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Git != sib {
		t.Fatalf("want one record anchored at %s, got %+v", sib, got)
	}
}

// An allow-list refusal is distinguishable from a fetch failure.
func TestFetchNestedDelegates_RefusalIsTyped(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	a, b, rootDir := t.TempDir(), t.TempDir(), t.TempDir()
	delegateGit(t, b, "b")
	delegateGit(t, a, "a", b)
	if _, err := resolver.EnsureRepo(a, ""); err != nil {
		t.Fatal(err)
	}
	root := &harness.Harness{Name: "root", Dir: rootDir, DelegatesTo: []harness.Delegate{{GitSource: harness.GitSource{Git: a}}}}
	_, err := fetchNestedDelegates(root, &config.Config{AllowedRemoteSources: []string{a}})
	if !isNestedRefusal(err) {
		t.Fatalf("want a refusal, got %v", err)
	}
	// A missing source is a fetch failure, not a refusal.
	a3 := t.TempDir()
	delegateGit(t, a3, "a3", filepath.Join(t.TempDir(), "gone"))
	if _, err := resolver.EnsureRepo(a3, ""); err != nil {
		t.Fatal(err)
	}
	root.DelegatesTo = []harness.Delegate{{GitSource: harness.GitSource{Git: a3}}}
	_, err = fetchNestedDelegates(root, &config.Config{})
	if err == nil || isNestedRefusal(err) {
		t.Fatalf("want a plain fetch failure, got %v", err)
	}
}

// installDelegating lays down an installed harness whose one delegate is a.
func installDelegating(t *testing.T, id, a string) {
	t.Helper()
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	dir := harness.InstalledDirByID(id)
	if err := os.MkdirAll(filepath.Join(dir, ".agents/harness"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, ".agents/harness", "plugin.json"),
		fmt.Sprintf(`{"name":"rootharn","version":"0.1.0","default_vendor":"claude","delegates_to":[{"git":%q}]}`, a))
	writeTestFile(t, filepath.Join(dir, ".agents/harness", "installed.json"),
		`{"source_type":"local","installed_at":"2024-01-01T00:00:00Z"}`)
}

// At update, a nested allow-list refusal is an error, as it is for a
// top-level delegate.
func TestCmdUpdate_NestedAllowListRefusalIsAnError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("YNH_HOME", "")
	a, b := t.TempDir(), t.TempDir()
	delegateGit(t, b, "b")
	delegateGit(t, a, "a", b)
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{AllowedRemoteSources: []string{a}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	installDelegating(t, "local/rootharn", a)
	err := cmdUpdate([]string{"local/rootharn"})
	if err == nil || !strings.Contains(err.Error(), "not in the allowed sources list") {
		t.Fatalf("want an allow-list error from update, got %v", err)
	}
}

// At update, a nested fetch failure stays a warning.
func TestCmdUpdate_NestedFetchFailureIsAWarning(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("YNH_HOME", "")
	a := t.TempDir()
	delegateGit(t, a, "a", filepath.Join(t.TempDir(), "gone"))
	installDelegating(t, "local/rootharn", a)
	if err := cmdUpdate([]string{"local/rootharn"}); err != nil {
		t.Fatalf("a nested fetch failure should only warn, got %v", err)
	}
}
