package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/namespace"
	"github.com/eyelock/ynh/internal/plugin"
)

// installGitHarness installs a harness as a git install would: a copy under
// the harnesses folder, with its provenance beside it.
func installGitHarness(t *testing.T, source, subpath, ref, name, fields string) string {
	t.Helper()
	id := namespace.CanonicalID(source, name)
	dir := filepath.Join(config.HarnessesDir(), namespace.IDToFSName(id))
	if err := os.MkdirAll(filepath.Join(dir, plugin.PluginDir), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"name":"` + name + `","version":"1.0.0"` + fields + `}`
	if err := os.WriteFile(filepath.Join(dir, plugin.PluginDir, plugin.PluginFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := plugin.SaveInstalledJSON(dir, &plugin.InstalledJSON{
		SourceType: "git", Source: source, Path: subpath, Ref: ref, InstalledAt: "2026-11-06T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

// installLocalHarness registers dir as a pointer-form install.
func installLocalHarness(t *testing.T, dir, name string) string {
	t.Helper()
	if err := harness.SavePointer(&harness.Pointer{
		Name: name,
		InstalledJSON: plugin.InstalledJSON{
			SourceType: "local", Source: dir, InstalledAt: "2026-11-06T00:00:00Z",
		},
	}); err != nil {
		t.Fatal(err)
	}
	return "local/" + name
}

func accepted(t *testing.T, id string) trustStore {
	t.Helper()
	h, err := harness.LoadByID(id)
	if err != nil {
		t.Fatal(err)
	}
	st := trustStatusFor(id, h, trustStore{Harnesses: map[string]trustRecord{}}, nil)
	return trustStore{Harnesses: map[string]trustRecord{id: {Digest: st.Digest, Scheme: trustScheme}}}
}

func remoteEntry(url, path, ref string) harness.Delegate {
	return harness.Delegate{GitSource: harness.GitSource{Git: url, Path: path, Ref: ref}}
}

func TestResolveDelegateRemoteEntry(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	id := installGitHarness(t, "https://github.com/eyelock/assistants", "harnesses/planner", "", "planner", mcpFields("p"))
	parent := &harness.Harness{Name: "parent", Dir: t.TempDir()}

	// The same repository spelled as ssh, with a trailing .git, resolves to it,
	// and the entry's focus is carried through.
	d := remoteEntry("git@github.com:eyelock/assistants.git", "harnesses/planner", "")
	d.Focus = "triage"
	got, err := resolveDelegate(parent, d, accepted(t, id))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.ID != id || got.Focus != "triage" {
		t.Errorf("resolved %+v, want id %s with focus triage", got, id)
	}

	// Another path in the same repository is a different harness.
	if _, err := resolveDelegate(parent, remoteEntry("https://github.com/eyelock/assistants", "harnesses/other", ""), accepted(t, id)); err == nil {
		t.Error("an entry with another path resolved to the planner")
	}
	// A different ref is a different harness.
	d2 := remoteEntry("https://github.com/eyelock/assistants", "harnesses/planner", "v2")
	inst := installGitHarness(t, "https://github.com/eyelock/assistants", "harnesses/planner", "v1", "planner", mcpFields("p"))
	if _, err := resolveDelegate(parent, d2, accepted(t, inst)); err == nil {
		t.Error("an entry pinned to v2 resolved to a v1 install")
	}
}

func TestResolveDelegateLocalEntries(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	repo := t.TempDir()
	writeIncludedHarness(t, filepath.Join(repo, "harnesses"), "del", mcpFields("d"))
	id := installLocalHarness(t, filepath.Join(repo, "harnesses", "del"), "del")
	parent := &harness.Harness{Name: "parent", Dir: filepath.Join(repo, "harnesses", "parent")}

	// git "." with a path, relative to the parent's own repository.
	repoParent := &harness.Harness{Name: "parent", Dir: repo}
	d := harness.Delegate{GitSource: harness.GitSource{Git: ".", Path: "harnesses/del"}}
	got, err := resolveDelegate(repoParent, d, accepted(t, id))
	if err != nil || got.ID != id {
		t.Fatalf("git . entry: %+v, %v", got, err)
	}

	// A local folder entry.
	l := harness.Delegate{GitSource: harness.GitSource{Local: "../del"}}
	if got, err := resolveDelegate(parent, l, accepted(t, id)); err != nil || got.ID != id {
		t.Fatalf("local entry: %+v, %v", got, err)
	}
}

// An absolute-path git: entry names a local folder, so it resolves to the
// local install its own hint ("ynh install <path>") makes (ynh #850).
func TestResolveDelegateAbsoluteGitPathEntry(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	repo := t.TempDir()
	writeIncludedHarness(t, filepath.Join(repo, "harnesses"), "del", mcpFields("d"))
	folder := filepath.Join(repo, "harnesses", "del")
	parent := &harness.Harness{Name: "parent", Dir: t.TempDir()}
	d := harness.Delegate{GitSource: harness.GitSource{Git: folder}}

	// Not installed: the hint names the folder, and following it resolves.
	_, err := resolveDelegate(parent, d, trustStore{Harnesses: map[string]trustRecord{}})
	if err == nil || !strings.Contains(err.Error(), "ynh install "+realDir(folder)) {
		t.Fatalf("want a not-installed error whose hint names the folder, got %v", err)
	}
	id := installLocalHarness(t, folder, "del")
	got, err := resolveDelegate(parent, d, accepted(t, id))
	if err != nil || got.ID != id {
		t.Fatalf("absolute git entry: %+v, %v", got, err)
	}

	// With a path, the folder is the joined one.
	dp := harness.Delegate{GitSource: harness.GitSource{Git: repo, Path: "harnesses/del"}}
	if got, err := resolveDelegate(parent, dp, accepted(t, id)); err != nil || got.ID != id {
		t.Fatalf("absolute git entry with path: %+v, %v", got, err)
	}
}

func TestResolveDelegateGitInstalledParentOwnRepo(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	repo := "https://github.com/eyelock/assistants"
	id := installGitHarness(t, repo, "harnesses/del", "", "del", mcpFields("d"))
	pid := installGitHarness(t, repo, "harnesses/parent", "", "parent", "")
	parent, err := harness.LoadByID(pid)
	if err != nil {
		t.Fatal(err)
	}
	d := harness.Delegate{GitSource: harness.GitSource{Git: ".", Path: "../del"}}
	got, err := resolveDelegate(parent, d, accepted(t, id))
	if err != nil || got.ID != id {
		t.Fatalf("own-repo entry of a git install: %+v, %v", got, err)
	}
}

func TestResolveDelegateRefusals(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	parent := &harness.Harness{Name: "parent", Dir: t.TempDir()}
	empty := trustStore{Harnesses: map[string]trustRecord{}}

	// Not installed: names ynh install.
	_, err := resolveDelegate(parent, remoteEntry("https://github.com/eyelock/assistants", "harnesses/planner", "v1"), empty)
	if err == nil || !strings.Contains(err.Error(), "ynh install https://github.com/eyelock/assistants --path harnesses/planner --ref v1") {
		t.Errorf("unmatched entry error = %v", err)
	}

	// Installed but never trusted: names ynh trust accept.
	id := installGitHarness(t, "https://github.com/eyelock/assistants", "harnesses/planner", "", "planner", mcpFields("p"))
	d := remoteEntry("https://github.com/eyelock/assistants", "harnesses/planner", "")
	_, err = resolveDelegate(parent, d, empty)
	if err == nil || !strings.Contains(err.Error(), "ynh trust accept "+id) {
		t.Errorf("untrusted match error = %v", err)
	}

	// Trusted, then its commands change: refused again.
	store := accepted(t, id)
	installGitHarness(t, "https://github.com/eyelock/assistants", "harnesses/planner", "", "planner", mcpFields("changed"))
	_, err = resolveDelegate(parent, d, store)
	if err == nil || !strings.Contains(err.Error(), "changed") || !strings.Contains(err.Error(), "ynh trust accept "+id) {
		t.Errorf("changed match error = %v", err)
	}
}

func TestResolveDelegateNoCommandsNeedsNoGrant(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	id := installGitHarness(t, "https://github.com/eyelock/assistants", "", "", "quiet", "")
	parent := &harness.Harness{Name: "parent", Dir: t.TempDir()}
	got, err := resolveDelegate(parent, remoteEntry("https://github.com/eyelock/assistants", "", ""),
		trustStore{Harnesses: map[string]trustRecord{}})
	if err != nil || got.ID != id {
		t.Errorf("a delegate with no commands: %+v, %v", got, err)
	}
}

// Two installs of one source match one entry. A trusted match wins whichever
// way the listing orders them, and with none trusted the refusal does not
// depend on the order either (ynw-6z0v).
func TestResolveDelegateSeveralMatchesPreferTrusted(t *testing.T) {
	const repo = "https://github.com/eyelock/assistants"
	parent := &harness.Harness{Name: "parent", Dir: t.TempDir()}
	d := remoteEntry(repo, "harnesses/planner", "")
	for _, trustedName := range []string{"alpha", "zeta"} {
		t.Run("trusted "+trustedName, func(t *testing.T) {
			t.Setenv("YNH_HOME", t.TempDir())
			a := installGitHarness(t, repo, "harnesses/planner", "", "alpha", mcpFields("a"))
			z := installGitHarness(t, repo, "harnesses/planner", "", "zeta", mcpFields("z"))
			want := map[string]string{"alpha": a, "zeta": z}[trustedName]
			got, err := resolveDelegate(parent, d, accepted(t, want))
			if err != nil || got.ID != want {
				t.Fatalf("resolved %+v, %v; want the trusted install %s", got, err, want)
			}
		})
	}
	t.Run("none trusted", func(t *testing.T) {
		t.Setenv("YNH_HOME", t.TempDir())
		a := installGitHarness(t, repo, "harnesses/planner", "", "alpha", mcpFields("a"))
		installGitHarness(t, repo, "harnesses/planner", "", "zeta", mcpFields("z"))
		_, err := resolveDelegate(parent, d, trustStore{Harnesses: map[string]trustRecord{}})
		if err == nil || !strings.Contains(err.Error(), "ynh trust accept "+a) {
			t.Errorf("error = %v, want the smallest id's refusal", err)
		}
	})
}
