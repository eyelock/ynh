package resolver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
)

// writeNS writes the manifest hj into dir and returns dir.
func writeNS(t *testing.T, dir string, hj map[string]any) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, plugin.PluginDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := hj["version"]; !ok {
		hj["version"] = "1.0.0"
	}
	data, err := json.Marshal(hj)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, plugin.PluginDir, plugin.PluginFile), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func srv(command string) map[string]any { return map[string]any{"command": command} }

// namespaceRoot builds a root harness in a temp dir whose includes are the
// given metas, and returns it loaded.
func namespaceRoot(t *testing.T, root string, includes ...map[string]any) *harness.Harness {
	t.Helper()
	writeNS(t, root, map[string]any{"name": "root", "includes": includes})
	p, err := harness.LoadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func namespaces(rs []ResolveResult) []string {
	var out []string
	for _, r := range rs {
		if r.Harness != nil {
			out = append(out, r.Namespace)
		}
	}
	return out
}

func serverNames(t *testing.T, root *harness.Harness, rs []ResolveResult) []string {
	t.Helper()
	servers, _, err := harness.ComposeMCPServers(root, IncludedHarnesses(rs), t.TempDir(), func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for n := range servers {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

func TestNamespace_ByNameAliasAndTransitive(t *testing.T) {
	root := t.TempDir()
	writeNS(t, filepath.Join(root, "gh", "dep"), map[string]any{"name": "dep"})
	writeNS(t, filepath.Join(root, "gh"), map[string]any{"name": "github", "includes": []map[string]any{{"local": "dep"}}})
	writeNS(t, filepath.Join(root, "other"), map[string]any{"name": "other"})
	p := namespaceRoot(t, root, map[string]any{"local": "gh"}, map[string]any{"local": "other", "as": "alias"})

	results, err := Resolve(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := namespaces(results), []string{"dep", "github", "alias"}; !slices.Equal(got, want) {
		t.Errorf("namespaces = %v, want %v", got, want)
	}
}

func TestNamespace_SelectionErrors(t *testing.T) {
	root := t.TempDir()
	writeNS(t, filepath.Join(root, "a", "github"), map[string]any{"name": "github", "profiles": map[string]any{"ci": map[string]any{}}, "focuses": map[string]any{"triage": map[string]any{"prompt": "go"}}})
	writeNS(t, filepath.Join(root, "b", "github"), map[string]any{"name": "github"})
	writeNS(t, filepath.Join(root, "solo"), map[string]any{"name": "solo", "profiles": map[string]any{"ci": map[string]any{}, "local": map[string]any{}}})
	p := namespaceRoot(t, root,
		map[string]any{"local": "a/github"}, map[string]any{"local": "b/github"}, map[string]any{"local": "solo"})

	tests := []struct {
		name string
		sel  harness.Selection
		want string // substring of the error, empty for none
	}{
		{"unused ambiguity is fine", harness.Selection{Included: map[string]string{"solo": "ci"}}, ""},
		{"nothing selected is fine", harness.Selection{}, ""},
		{"ambiguous profile", harness.Selection{Included: map[string]string{"github": "ci"}},
			`namespace "github" is ambiguous: a/github and b/github; give one include an "as" alias`},
		{"ambiguous focus", harness.Selection{FocusNS: "github", FocusName: "triage"}, `namespace "github" is ambiguous`},
		{"unknown namespace", harness.Selection{Included: map[string]string{"x": "ci"}}, `no included harness has namespace "x" (available: github, solo)`},
		{"unknown profile", harness.Selection{Included: map[string]string{"solo": "nope"}}, `profile "nope" not defined in included harness "solo" (available: [ci local])`},
		{"unknown focus ns", harness.Selection{FocusNS: "x", FocusName: "f"}, `no included harness has namespace "x"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := ResolveSelected(p, nil, tt.sel)
			switch {
			case tt.want == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)):
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestNamespace_AliasResolvesAmbiguity(t *testing.T) {
	root := t.TempDir()
	writeNS(t, filepath.Join(root, "a"), map[string]any{"name": "github", "profiles": map[string]any{"ci": map[string]any{}}})
	writeNS(t, filepath.Join(root, "b"), map[string]any{"name": "github"})
	p := namespaceRoot(t, root, map[string]any{"local": "a", "as": "gh-work"}, map[string]any{"local": "b"})
	if _, _, err := ResolveSelected(p, nil, harness.Selection{Included: map[string]string{"gh-work": "ci", "github": ""}}); err != nil {
		t.Fatalf("alias should disambiguate: %v", err)
	}
}

func TestNamespace_UnknownNamespaceWithNoHarnessIncludes(t *testing.T) {
	root := t.TempDir()
	p := namespaceRoot(t, root)
	_, _, err := ResolveSelected(p, nil, harness.Selection{Included: map[string]string{"x": "ci"}})
	if err == nil || !strings.Contains(err.Error(), `no included harness has namespace "x"`) {
		t.Fatalf("error = %v", err)
	}
}

func TestNamespace_ProfileAppliesToThatIncludeOnly(t *testing.T) {
	root := t.TempDir()
	// dep arrives only through gh's "ci" profile.
	writeNS(t, filepath.Join(root, "gh", "ci-dep"), map[string]any{"name": "ci-dep", "mcp_servers": map[string]any{"cidep": srv("cidep")}})
	writeNS(t, filepath.Join(root, "gh"), map[string]any{
		"name":        "github",
		"mcp_servers": map[string]any{"base": srv("base"), "gone": srv("gone")},
		"profiles": map[string]any{"ci": map[string]any{
			"mcp_servers": map[string]any{"extra": srv("extra"), "gone": nil},
			"includes":    []map[string]any{{"local": "ci-dep"}},
		}},
	})
	writeNS(t, filepath.Join(root, "db"), map[string]any{
		"name":        "db",
		"mcp_servers": map[string]any{"dbsrv": srv("db")},
		"profiles":    map[string]any{"ci": map[string]any{"mcp_servers": map[string]any{"dbextra": srv("x")}}},
	})
	p := namespaceRoot(t, root, map[string]any{"local": "gh"}, map[string]any{"local": "db"})

	plain, _, err := ResolveSelected(p, nil, harness.Selection{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := serverNames(t, p, plain), []string{"base", "dbsrv", "gone"}; !slices.Equal(got, want) {
		t.Errorf("no selection: servers = %v, want %v", got, want)
	}

	picked, _, err := ResolveSelected(p, nil, harness.Selection{Included: map[string]string{"github": "ci"}})
	if err != nil {
		t.Fatal(err)
	}
	// github gains extra and the profile's include, loses gone; db is untouched.
	if got, want := serverNames(t, p, picked), []string{"base", "cidep", "dbsrv", "extra"}; !slices.Equal(got, want) {
		t.Errorf("--profile github:ci: servers = %v, want %v", got, want)
	}
	if len(p.MCPServers) != 0 || len(p.Includes) != 2 {
		t.Errorf("root was changed: servers=%v includes=%d", p.MCPServers, len(p.Includes))
	}

	both, _, err := ResolveSelected(p, nil, harness.Selection{Included: map[string]string{"github": "ci", "db": "ci"}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := serverNames(t, p, both), []string{"base", "cidep", "dbextra", "dbsrv", "extra"}; !slices.Equal(got, want) {
		t.Errorf("both profiles: servers = %v, want %v", got, want)
	}
}

func TestNamespace_FocusYieldsPromptAndAppliesItsProfile(t *testing.T) {
	root := t.TempDir()
	writeNS(t, filepath.Join(root, "gh"), map[string]any{
		"name":        "github",
		"mcp_servers": map[string]any{"base": srv("base")},
		"profiles":    map[string]any{"ci": map[string]any{"mcp_servers": map[string]any{"extra": srv("extra")}}},
		"focuses": map[string]any{
			"triage": map[string]any{"profile": "ci", "prompt": "triage the issues"},
			"plain":  map[string]any{"prompt": "just look"},
			"broken": map[string]any{"profile": "missing", "prompt": "x"},
		},
	})
	p := namespaceRoot(t, root, map[string]any{"local": "gh"})

	results, focus, err := ResolveSelected(p, nil, harness.Selection{FocusNS: "github", FocusName: "triage"})
	if err != nil {
		t.Fatal(err)
	}
	if focus == nil || focus.Prompt != "triage the issues" {
		t.Fatalf("focus = %+v", focus)
	}
	if got, want := serverNames(t, p, results), []string{"base", "extra"}; !slices.Equal(got, want) {
		t.Errorf("focus profile not applied: servers = %v, want %v", got, want)
	}

	results, focus, err = ResolveSelected(p, nil, harness.Selection{FocusNS: "github", FocusName: "plain"})
	if err != nil || focus == nil || focus.Prompt != "just look" {
		t.Fatalf("plain focus: %+v, %v", focus, err)
	}
	if got, want := serverNames(t, p, results), []string{"base"}; !slices.Equal(got, want) {
		t.Errorf("plain focus servers = %v, want %v", got, want)
	}

	for name, want := range map[string]string{
		"nope":   `focus "nope" not defined in included harness "github" (available: [broken plain triage])`,
		"broken": `profile "missing" not defined in included harness "github"`,
	} {
		_, _, err = ResolveSelected(p, nil, harness.Selection{FocusNS: "github", FocusName: name})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("focus %s: error = %v, want containing %q", name, err, want)
		}
	}
}

func TestNamespace_IncludedProfileRemovalDropsItsDependenciesServer(t *testing.T) {
	root := t.TempDir()
	writeNS(t, filepath.Join(root, "gh", "dep"), map[string]any{"name": "dep", "mcp_servers": map[string]any{"noisy": srv("noisy"), "keep": srv("keep")}})
	writeNS(t, filepath.Join(root, "gh"), map[string]any{
		"name":     "github",
		"includes": []map[string]any{{"local": "dep"}},
		"profiles": map[string]any{"quiet": map[string]any{"mcp_servers": map[string]any{"noisy": nil}}},
	})
	p := namespaceRoot(t, root, map[string]any{"local": "gh"})

	results, _, err := ResolveSelected(p, nil, harness.Selection{Included: map[string]string{"github": "quiet"}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := serverNames(t, p, results), []string{"keep"}; !slices.Equal(got, want) {
		t.Errorf("servers = %v, want %v", got, want)
	}
}
