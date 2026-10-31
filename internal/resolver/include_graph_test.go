package resolver

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/harness"
)

// Cycles, diamonds and versions in the include graph (#547).

func TestResolve_CycleAmongIncludesKeepsEveryHarnessOnce(t *testing.T) {
	type inc = map[string]any
	base := t.TempDir()
	a, b, c := filepath.Join(base, "a"), filepath.Join(base, "b"), filepath.Join(base, "c")
	writeNS(t, a, inc{"name": "a", "includes": []inc{{"local": b}}})
	writeNS(t, b, inc{"name": "b", "includes": []inc{{"local": c}}})
	writeNS(t, c, inc{"name": "c", "includes": []inc{{"local": a}}})

	p := &harness.Harness{Name: "root", Dir: t.TempDir(), Includes: []harness.Include{localInc(a)}}
	results, err := Resolve(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := basePaths(results), []string{c, b, a}; !slices.Equal(got, want) {
		t.Fatalf("order = %v, want every harness in the cycle once: %v", got, want)
	}
	want := "include cycle: root -> a -> b -> c -> a; a is already included, so this include adds nothing"
	if !slices.Equal(results[0].Notes, []string{want}) {
		t.Errorf("c's notes = %q, want %q", results[0].Notes, want)
	}
}

// The include that closes a cycle is not a route to the harness it names, so
// it does not withhold consent to its hooks.
func TestResolve_CycleDoesNotWithholdHooksConsent(t *testing.T) {
	type inc = map[string]any
	base := t.TempDir()
	a, b := filepath.Join(base, "a"), filepath.Join(base, "b")
	writeNS(t, a, inc{"name": "a", "includes": []inc{{"local": b, "hooks": true}}})
	writeNS(t, b, inc{"name": "b", "includes": []inc{{"local": a}}}) // no "hooks"

	p := &harness.Harness{Name: "root", Dir: t.TempDir(), Includes: []harness.Include{{GitSource: harness.GitSource{Local: a}, Hooks: true}}}
	results, err := Resolve(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if !r.HooksActive {
			t.Errorf("%s: hooks not active; the cycle's closing include must not count as a route", r.Content.BasePath)
		}
	}
}

// Nor does it narrow the harness's MCP servers.
func TestResolve_CycleDoesNotRemoveServers(t *testing.T) {
	type inc = map[string]any
	base := t.TempDir()
	a, b := filepath.Join(base, "a"), filepath.Join(base, "b")
	writeNS(t, a, inc{"name": "a", "includes": []inc{{"local": b}},
		"mcp_servers": inc{"s": inc{"command": "s"}}})
	writeNS(t, b, inc{"name": "b", "includes": []inc{{"local": a}},
		"mcp_servers": inc{"s": nil}})

	p := &harness.Harness{Name: "root", Dir: t.TempDir(), Includes: []harness.Include{localInc(a)}}
	results, err := Resolve(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := results[len(results)-1]
	if got.Content.BasePath != a {
		t.Fatalf("last result = %s, want a", got.Content.BasePath)
	}
	if _, ok := got.Harness.MCPServers["s"]; !ok {
		t.Error("a's server s was removed through the include that closes the cycle")
	}
}

// fakeRepos serves git includes from local directories: url@ref to a
// directory and the commit it resolves to.
type fakeRepo struct{ dir, sha string }

func fakeSource(t *testing.T, repos map[string]fakeRepo) sourceResolver {
	return func(gs harness.GitSource, harnessDir string) (string, *RepoResult, error) {
		r, ok := repos[gs.Git+"@"+gs.Ref]
		if !ok {
			t.Fatalf("unexpected source %s@%s", gs.Git, gs.Ref)
		}
		base := r.dir
		if gs.Path != "" {
			base = filepath.Join(base, gs.Path)
		}
		return base, &RepoResult{Path: r.dir, SHA: r.sha}, nil
	}
}

// gitMeta is an include of a git source at ref.
func gitMeta(url, ref string) map[string]any {
	m := map[string]any{"git": url}
	if ref != "" {
		m["ref"] = ref
	}
	return m
}

// diamond writes root -> b, root -> c, with b and c each including shared at
// the given refs, and resolves it.
func resolveVersionDiamond(t *testing.T, bRef, cRef string, repos map[string]fakeRepo) ([]ResolveResult, error) {
	t.Helper()
	type inc = map[string]any
	base := t.TempDir()
	b, c := filepath.Join(base, "b"), filepath.Join(base, "c")
	writeNS(t, b, inc{"name": "b", "includes": []inc{gitMeta("github.com/o/shared", bRef)}})
	writeNS(t, c, inc{"name": "c", "includes": []inc{gitMeta("github.com/o/shared", cRef)}})
	p := &harness.Harness{Name: "root", Dir: base, Includes: []harness.Include{localInc(b), localInc(c)}}
	return resolveWith(p, nil, fakeSource(t, repos))
}

func TestResolve_SameSourceAtTwoVersionsIsAnError(t *testing.T) {
	v1, v2 := t.TempDir(), t.TempDir()
	writeNS(t, v1, map[string]any{"name": "shared"})
	writeNS(t, v2, map[string]any{"name": "shared"})
	_, err := resolveVersionDiamond(t, "v1", "v2", map[string]fakeRepo{
		"github.com/o/shared@v1": {dir: v1, sha: "1111111"},
		"github.com/o/shared@v2": {dir: v2, sha: "2222222"},
	})
	if err == nil {
		t.Fatal("want an error for one source at two versions")
	}
	for _, want := range []string{"o/shared//", "1111111 by b > o/shared", "2222222 by c > o/shared", "include it at one version"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
}

// Two refs that resolve to one commit are one version: the diamond collapses.
func TestResolve_TwoRefsOfOneCommitAreOneHarness(t *testing.T) {
	shared := t.TempDir()
	writeNS(t, shared, map[string]any{"name": "shared"})
	results, err := resolveVersionDiamond(t, "v1", "abc1234", map[string]fakeRepo{
		"github.com/o/shared@v1":      {dir: shared, sha: "abc1234"},
		"github.com/o/shared@abc1234": {dir: shared, sha: "abc1234"},
	})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, r := range results {
		if r.Content.BasePath == shared {
			n++
		}
	}
	if n != 1 {
		t.Errorf("shared contributed %d times, want once: %v", n, basePaths(results))
	}
}

// With no commit known, differing refs are two versions.
func TestResolve_DifferentRefsWithoutCommitsAreTwoVersions(t *testing.T) {
	shared := t.TempDir()
	writeNS(t, shared, map[string]any{"name": "shared"})
	_, err := resolveVersionDiamond(t, "", "v2", map[string]fakeRepo{
		"github.com/o/shared@":   {dir: shared},
		"github.com/o/shared@v2": {dir: shared},
	})
	if err == nil || !strings.Contains(err.Error(), "the default branch by b > o/shared") {
		t.Errorf("error = %v, want one naming the default branch and v2", err)
	}
}
