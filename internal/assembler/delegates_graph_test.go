package assembler

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/vendor"
)

// graph makes a git repo per harness name, each declaring the delegates the
// map names for it, and returns each one's directory. The directories are
// made first, so the harnesses may name one another in any shape.
func graph(t *testing.T, edges map[string][]string) map[string]string {
	t.Helper()
	dirs := map[string]string{}
	for name := range edges {
		dirs[name] = t.TempDir()
	}
	for name, to := range edges {
		var dels []any
		for _, d := range to {
			dels = append(dels, map[string]any{"git": dirs[d], "transport": "native"})
		}
		delegateRepo(t, dirs[name], map[string]any{
			"name": name, "version": "0.1.0", "description": name + " agent",
			"delegates_to": dels,
		})
	}
	return dirs
}

// assembleGraph assembles root's delegates and returns the agent file names
// written, the agents by name, and the warnings.
func assembleGraph(t *testing.T, dirs map[string]string, root string, entries ...string) (agents map[string]string, warnings string, err error) {
	t.Helper()
	t.Setenv("YNH_HOME", t.TempDir())
	adapter, aerr := vendor.Get("claude")
	if aerr != nil {
		t.Fatal(aerr)
	}
	var dels []harness.Delegate
	for _, e := range entries {
		dels = append(dels, harness.Delegate{Transport: harness.TransportNative, GitSource: harness.GitSource{Git: dirs[e]}})
	}
	workDir := t.TempDir()
	var warn strings.Builder
	_, err = AssembleDelegates(workDir, adapter, dels, dirs[root], DelegateOptions{Warn: &warn, RootName: root})
	warnings = warn.String()
	if err != nil {
		return nil, warnings, err
	}
	agentsDir := filepath.Join(workDir, adapter.ConfigDir(), adapter.ArtifactDirs()["agents"])
	files, _ := os.ReadDir(agentsDir)
	agents = map[string]string{}
	for _, f := range files {
		data, rerr := os.ReadFile(filepath.Join(agentsDir, f.Name()))
		if rerr != nil {
			t.Fatal(rerr)
		}
		agents[strings.TrimSuffix(f.Name(), ".md")] = string(data)
	}
	return agents, warnings, nil
}

func agentNames(agents map[string]string) []string {
	var names []string
	for n := range agents {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

// A delegate's own delegates_to are followed: every harness reachable is one
// top-level agent, and each agent lists the delegates it declares itself.
func TestAssembleDelegates_FlattensChain(t *testing.T) {
	dirs := graph(t, map[string][]string{"root": {"a"}, "a": {"b"}, "b": nil})
	agents, _, err := assembleGraph(t, dirs, "root", "a")
	if err != nil {
		t.Fatal(err)
	}
	if got := agentNames(agents); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("agents = %v, want [a b]", got)
	}
	if !strings.Contains(agents["a"], "## Delegates\n\nThese delegates are available to you:\n\n- b\n") {
		t.Errorf("a should list b:\n%s", agents["a"])
	}
	if !strings.Contains(agents["a"], "Hand back to the main session") {
		t.Errorf("a should hand back to the main session:\n%s", agents["a"])
	}
	if strings.Contains(agents["b"], "## Delegates") {
		t.Errorf("b declares no delegates, so its prompt lists none:\n%s", agents["b"])
	}
}

// A diamond: two harnesses share a delegate. It is one agent, listed by both.
func TestAssembleDelegates_Diamond(t *testing.T) {
	dirs := graph(t, map[string][]string{"root": {"a", "b"}, "a": {"shared"}, "b": {"shared"}, "shared": nil})
	agents, _, err := assembleGraph(t, dirs, "root", "a", "b")
	if err != nil {
		t.Fatal(err)
	}
	if got := agentNames(agents); !slices.Equal(got, []string{"a", "b", "shared"}) {
		t.Fatalf("agents = %v, want [a b shared]", got)
	}
	for _, n := range []string{"a", "b"} {
		if !strings.Contains(agents[n], "- shared\n") {
			t.Errorf("%s should list shared:\n%s", n, agents[n])
		}
	}
}

// A cycle between delegates ends: each is one agent and lists the other.
func TestAssembleDelegates_Cycle(t *testing.T) {
	dirs := graph(t, map[string][]string{"root": {"a"}, "a": {"b"}, "b": {"a"}})
	agents, _, err := assembleGraph(t, dirs, "root", "a")
	if err != nil {
		t.Fatal(err)
	}
	if got := agentNames(agents); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("agents = %v, want [a b]", got)
	}
	if !strings.Contains(agents["a"], "- b\n") || !strings.Contains(agents["b"], "- a\n") {
		t.Errorf("a and b should list each other:\n%s\n%s", agents["a"], agents["b"])
	}
}

// A delegate that names itself is no delegate of its own.
func TestAssembleDelegates_SelfReference(t *testing.T) {
	dirs := graph(t, map[string][]string{"root": {"a"}, "a": {"a"}})
	agents, warnings, err := assembleGraph(t, dirs, "root", "a")
	if err != nil {
		t.Fatal(err)
	}
	if got := agentNames(agents); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("agents = %v, want [a]", got)
	}
	if strings.Contains(agents["a"], "## Delegates") {
		t.Errorf("a must not list itself:\n%s", agents["a"])
	}
	if !strings.Contains(warnings, "a names itself as a delegate") {
		t.Errorf("warnings should note the self-reference: %q", warnings)
	}
}

// The root is never its own delegate, whether a delegate reaches it or the
// root names itself.
func TestAssembleDelegates_RootIsNeverADelegate(t *testing.T) {
	dirs := graph(t, map[string][]string{"root": {"a", "root"}, "a": {"root"}})
	agents, warnings, err := assembleGraph(t, dirs, "root", "a", "root")
	if err != nil {
		t.Fatal(err)
	}
	if got := agentNames(agents); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("agents = %v, want [a]", got)
	}
	if strings.Contains(agents["a"], "## Delegates") {
		t.Errorf("a must not list the root:\n%s", agents["a"])
	}
	if !strings.Contains(warnings, "is the root harness root") {
		t.Errorf("warnings should note the root: %q", warnings)
	}
}

// The root is the root by name too: an installed root lives apart from the
// checkout a delegate reaches it by.
func TestAssembleDelegates_RootByNameWhenElsewhere(t *testing.T) {
	dirs := graph(t, map[string][]string{"root": nil, "a": {"root"}})
	t.Setenv("YNH_HOME", t.TempDir())
	adapter, _ := vendor.Get("claude")
	workDir := t.TempDir()
	var warn strings.Builder
	_, err := AssembleDelegates(workDir, adapter,
		[]harness.Delegate{{Transport: harness.TransportNative, GitSource: harness.GitSource{Git: dirs["a"]}}}, t.TempDir(), DelegateOptions{Warn: &warn, RootName: "root"})
	if err != nil {
		t.Fatal(err)
	}
	files, _ := os.ReadDir(filepath.Join(workDir, adapter.ConfigDir(), adapter.ArtifactDirs()["agents"]))
	if len(files) != 1 || files[0].Name() != "a.md" {
		t.Errorf("only a should be an agent, got %v", files)
	}
}

// The same harness reached with two different selections cannot be one
// agent: it is an error naming both routes.
func TestAssembleDelegates_TwoSelectionsClash(t *testing.T) {
	dirs := graph(t, map[string][]string{"root": nil})
	dirs["a"] = t.TempDir()
	delegateRepo(t, dirs["a"], map[string]any{"name": "a", "version": "0.1.0", "description": "a",
		"profiles": map[string]any{"p": map[string]any{}}})
	t.Setenv("YNH_HOME", t.TempDir())
	adapter, _ := vendor.Get("claude")
	src := harness.GitSource{Git: dirs["a"]}
	_, err := AssembleDelegates(t.TempDir(), adapter, []harness.Delegate{
		{Transport: harness.TransportNative, GitSource: src},
		{Transport: harness.TransportNative, GitSource: src, Profile: "p"},
	}, dirs["root"], DelegateOptions{RootName: "root"})
	if err == nil || !strings.Contains(err.Error(), "reached with two selections") {
		t.Fatalf("want a two-selections error, got %v", err)
	}
}

// A delegate's profile or focus still applies in a graph: it narrows the
// delegate it names, and its own delegates are followed.
func TestAssembleDelegates_FocusKeptInGraph(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	leaf := t.TempDir()
	delegateRepo(t, leaf, map[string]any{"name": "leaf", "version": "0.1.0", "description": "leaf"})
	mid := t.TempDir()
	delegateRepo(t, mid, map[string]any{
		"name": "mid", "version": "0.1.0", "description": "mid",
		"delegates_to": []any{map[string]any{"git": leaf, "transport": "native"}},
		"focuses":      map[string]any{"triage": map[string]any{"prompt": "Triage first."}},
	})
	root := t.TempDir()
	delegateRepo(t, root, map[string]any{"name": "root", "version": "0.1.0", "description": "root"})
	adapter, _ := vendor.Get("claude")
	workDir := t.TempDir()
	_, err := AssembleDelegates(workDir, adapter,
		[]harness.Delegate{{Transport: harness.TransportNative, GitSource: harness.GitSource{Git: mid}, Focus: "triage"}}, root, DelegateOptions{RootName: "root"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(workDir, adapter.ConfigDir(), adapter.ArtifactDirs()["agents"], "mid.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "## Focus: triage") || !strings.Contains(string(data), "- leaf\n") {
		t.Errorf("mid should carry its focus and list leaf:\n%s", data)
	}
}

// One repository holds the root and its seats, and a seat delegates to a
// sibling by a relative git source ("../.."). The root reaches both seats from
// a cached clone of the repository, and the seat reaches its sibling from
// inside that clone: the same repository, commit and path, so one agent.
func TestAssembleDelegates_RelativeGitSourceFromCachedClone(t *testing.T) {
	repo := t.TempDir()
	write := func(name, manifest string) {
		t.Helper()
		dir := filepath.Join(repo, "harnesses", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := writePluginJSONFile(dir, []byte(manifest)); err != nil {
			t.Fatal(err)
		}
	}
	write("a", `{"name":"a","version":"0.1.0"}`)
	write("b", `{"name":"b","version":"0.1.0","delegates_to":[{"transport":"native","git":"../..","path":"harnesses/a"}]}`)
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@test.com")
	runGit(t, repo, "config", "user.name", "Test")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "init")

	t.Setenv("YNH_HOME", t.TempDir())
	adapter, err := vendor.Get("claude")
	if err != nil {
		t.Fatal(err)
	}
	dels := []harness.Delegate{
		{Transport: harness.TransportNative, GitSource: harness.GitSource{Git: ".", Path: "harnesses/a"}},
		{Transport: harness.TransportNative, GitSource: harness.GitSource{Git: ".", Path: "harnesses/b"}},
	}
	workDir := t.TempDir()
	if _, err := AssembleDelegates(workDir, adapter, dels, repo, DelegateOptions{Warn: &strings.Builder{}, RootName: "root"}); err != nil {
		t.Fatalf("a harness reached by two routes to one source must be one agent: %v", err)
	}
	files, _ := os.ReadDir(filepath.Join(workDir, adapter.ConfigDir(), adapter.ArtifactDirs()["agents"]))
	var got []string
	for _, f := range files {
		got = append(got, strings.TrimSuffix(f.Name(), ".md"))
	}
	slices.Sort(got)
	if !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("agents = %v, want [a b]", got)
	}
}

// monorepo commits harnesses/<name> for each manifest and returns the repo.
func monorepo(t *testing.T, manifests map[string]string) string {
	t.Helper()
	repo := t.TempDir()
	for name, manifest := range manifests {
		dir := filepath.Join(repo, "harnesses", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := writePluginJSONFile(dir, []byte(manifest)); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@test.com")
	runGit(t, repo, "config", "user.name", "Test")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "init")
	return repo
}

func assembleEntries(t *testing.T, harnessDir, rootName string, dels ...harness.Delegate) (string, error) {
	t.Helper()
	t.Setenv("YNH_HOME", t.TempDir())
	adapter, err := vendor.Get("claude")
	if err != nil {
		t.Fatal(err)
	}
	var warn strings.Builder
	_, err = AssembleDelegates(t.TempDir(), adapter, dels, harnessDir, DelegateOptions{Warn: &warn, RootName: rootName})
	return warn.String(), err
}

// Two different repositories each holding a harness named a are two
// harnesses sharing a name, not one agent, however the graph keys them.
func TestAssembleDelegates_TwoReposSameNameClash(t *testing.T) {
	one, two := t.TempDir(), t.TempDir()
	for _, d := range []string{one, two} {
		delegateRepo(t, d, map[string]any{"name": "a", "version": "0.1.0", "description": "a"})
	}
	_, err := assembleEntries(t, t.TempDir(), "root",
		harness.Delegate{Transport: harness.TransportNative, GitSource: harness.GitSource{Git: one}},
		harness.Delegate{Transport: harness.TransportNative, GitSource: harness.GitSource{Git: two}})
	if err == nil || !strings.Contains(err.Error(), "two delegates are both named a") {
		t.Fatalf("want a name clash, got %v", err)
	}
}

// One repository and path at two commits is two harnesses: the name clashes
// rather than the second silently becoming the first.
func TestAssembleDelegates_SameRepoTwoCommitsClash(t *testing.T) {
	repo := delegateRepo(t, t.TempDir(), map[string]any{"name": "a", "version": "0.1.0", "description": "first"})
	runGit(t, repo, "tag", "v1")
	delegateRepo2 := `{"name":"a","version":"0.2.0","description":"second"}`
	if err := writePluginJSONFile(repo, []byte(delegateRepo2)); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "commit", "-am", "second")
	runGit(t, repo, "tag", "v2")
	_, err := assembleEntries(t, t.TempDir(), "root",
		harness.Delegate{Transport: harness.TransportNative, GitSource: harness.GitSource{Git: repo, Ref: "v1"}},
		harness.Delegate{Transport: harness.TransportNative, GitSource: harness.GitSource{Git: repo, Ref: "v2"}})
	if err == nil || !strings.Contains(err.Error(), "two delegates are both named a") {
		t.Fatalf("want a name clash across two commits, got %v", err)
	}
}

// Two selections of one harness still clash when the second route reaches it
// through a clone of the repository by a relative git source.
func TestAssembleDelegates_TwoSelectionsClashViaRelativeSource(t *testing.T) {
	repo := monorepo(t, map[string]string{
		"a": `{"name":"a","version":"0.1.0","profiles":{"p":{}}}`,
		"b": `{"name":"b","version":"0.1.0","delegates_to":[{"transport":"native","git":"../..","path":"harnesses/a","profile":"p"}]}`,
	})
	_, err := assembleEntries(t, repo, "root",
		harness.Delegate{Transport: harness.TransportNative, GitSource: harness.GitSource{Git: ".", Path: "harnesses/a"}},
		harness.Delegate{Transport: harness.TransportNative, GitSource: harness.GitSource{Git: ".", Path: "harnesses/b"}})
	if err == nil || !strings.Contains(err.Error(), "reached with two selections") {
		t.Fatalf("want a two-selections error, got %v", err)
	}
}

// The root is recognised by its source as well as its name: a seat that
// reaches the root's harness through a clone of its repository is skipped,
// with a note, even when the root's name is not known.
func TestAssembleDelegates_RootRecognisedBySource(t *testing.T) {
	repo := monorepo(t, map[string]string{
		"rootseat": `{"name":"rootseat","version":"0.1.0"}`,
		"a":        `{"name":"a","version":"0.1.0","delegates_to":[{"transport":"native","git":"../..","path":"harnesses/rootseat"}]}`,
	})
	rootDir := filepath.Join(repo, "harnesses", "rootseat")
	warn, err := assembleEntries(t, rootDir, "",
		harness.Delegate{Transport: harness.TransportNative, GitSource: harness.GitSource{Git: "../..", Path: "harnesses/a"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(warn, "is the root harness") {
		t.Errorf("want a note that a names the root, got %q", warn)
	}
}
