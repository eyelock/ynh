package resolver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
)

// writeIncludedHarness writes a harness manifest into dir with the given
// includes (each a local path) and optional MCP servers, and returns dir.
func writeIncludedHarness(t *testing.T, dir, name string, includes []map[string]any, servers map[string]any) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, plugin.PluginDir), 0o755); err != nil {
		t.Fatal(err)
	}
	hj := map[string]any{"name": name, "version": "1.0.0"}
	if includes != nil {
		hj["includes"] = includes
	}
	if servers != nil {
		hj["mcp_servers"] = servers
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

func localInc(path string) harness.Include {
	return harness.Include{GitSource: harness.GitSource{Local: path}}
}

func localMeta(path string, pick ...string) map[string]any {
	m := map[string]any{"local": path}
	if len(pick) > 0 {
		m["pick"] = pick
	}
	return m
}

func basePaths(rs []ResolveResult) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Content.BasePath)
	}
	return out
}

func TestResolve_PlainPackageIsUnchanged(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pkg", "skills", "s"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := &harness.Harness{Name: "h", Dir: root, Includes: []harness.Include{localInc("pkg")}}
	results, err := Resolve(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Harness != nil {
		t.Fatalf("results = %+v, want one entry with no Harness", results)
	}
	if results[0].Chain != "pkg" {
		t.Errorf("Chain = %q, want pkg", results[0].Chain)
	}
}

func TestResolve_HarnessIncludeIsLoaded(t *testing.T) {
	root := t.TempDir()
	writeIncludedHarness(t, filepath.Join(root, "inc"), "inc", nil,
		map[string]any{"db": map[string]any{"command": "db"}})
	p := &harness.Harness{Name: "h", Dir: root, Includes: []harness.Include{localInc("inc")}}
	results, err := Resolve(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Harness == nil {
		t.Fatalf("results = %+v, want one harness entry", results)
	}
	if got := results[0].Harness.Name; got != "inc" {
		t.Errorf("Harness.Name = %q, want inc", got)
	}
	if _, ok := results[0].Harness.MCPServers["db"]; !ok {
		t.Error("included harness lost its MCP servers")
	}
	inc := IncludedHarnesses(results)
	if len(inc) != 1 || inc[0].Source != "inc" {
		t.Errorf("IncludedHarnesses = %+v", inc)
	}
}

func TestResolve_TransitiveOrder(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a")
	writeIncludedHarness(t, filepath.Join(a, "dep1"), "dep1", nil, nil)
	writeIncludedHarness(t, filepath.Join(a, "dep2"), "dep2", nil, nil)
	writeIncludedHarness(t, a, "a", []map[string]any{localMeta("dep1"), localMeta("dep2")}, nil)
	if err := os.MkdirAll(filepath.Join(root, "plain"), 0o755); err != nil {
		t.Fatal(err)
	}

	p := &harness.Harness{Name: "h", Dir: root, Includes: []harness.Include{localInc("a"), localInc("plain")}}
	results, err := Resolve(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(a, "dep1"), filepath.Join(a, "dep2"), a, filepath.Join(root, "plain")}
	if got := basePaths(results); !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	if got := results[0].Chain; got != "a > dep1" {
		t.Errorf("transitive Chain = %q, want %q", got, "a > dep1")
	}
}

func TestResolve_PickedHarnessDoesNotFollowItsIncludes(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a")
	writeIncludedHarness(t, filepath.Join(a, "dep"), "dep", nil, nil)
	writeIncludedHarness(t, a, "a", []map[string]any{localMeta("dep")},
		map[string]any{"db": map[string]any{"command": "db"}})

	p := &harness.Harness{Name: "h", Dir: root, Includes: []harness.Include{
		{GitSource: harness.GitSource{Local: "a"}, Pick: []string{"skills/x"}},
	}}
	results, err := Resolve(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1: %v", len(results), basePaths(results))
	}
	if !slices.Equal(results[0].Content.Paths, []string{"skills/x"}) {
		t.Errorf("Paths = %v", results[0].Content.Paths)
	}
	if results[0].Harness == nil || len(results[0].Harness.MCPServers) != 1 {
		t.Error("a picked harness include must still carry its MCP servers")
	}
}

func TestResolve_CycleIsAnError(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a")
	b := t.TempDir()
	writeIncludedHarness(t, root, "root", nil, nil)
	writeIncludedHarness(t, a, "a", []map[string]any{localMeta(b)}, nil)
	writeIncludedHarness(t, b, "b", []map[string]any{localMeta(root)}, nil)

	p := &harness.Harness{Name: "root", Dir: root, Includes: []harness.Include{localInc("a")}}
	_, err := Resolve(p, nil)
	want := "include cycle: root -> a -> " + b + " -> root"
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
}

func TestResolve_SelfIncludeIsACycle(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a")
	writeIncludedHarness(t, a, "a", []map[string]any{localMeta(a)}, nil)
	p := &harness.Harness{Name: "root", Dir: root, Includes: []harness.Include{localInc("a")}}
	_, err := Resolve(p, nil)
	want := "include cycle: root -> a -> a"
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
}

func TestResolve_DiamondContributesOnce(t *testing.T) {
	root := t.TempDir()
	shared := t.TempDir()
	writeIncludedHarness(t, shared, "shared", nil, nil)
	a := filepath.Join(root, "a")
	b := filepath.Join(root, "b")
	writeIncludedHarness(t, a, "a", []map[string]any{localMeta(shared)}, nil)
	writeIncludedHarness(t, b, "b", []map[string]any{localMeta(shared)}, nil)

	p := &harness.Harness{Name: "h", Dir: root, Includes: []harness.Include{localInc("a"), localInc("b")}}
	results, err := Resolve(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{shared, a, b}
	if got := basePaths(results); !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestResolve_IncludedHarnessIncludesAreCheckedAgainstAllowList(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	a := filepath.Join(root, "a")
	writeIncludedHarness(t, a, "a", []map[string]any{localMeta(outside)}, nil)
	p := &harness.Harness{Name: "h", Dir: root, Includes: []harness.Include{localInc("a")}}
	_, err := Resolve(p, &config.Config{AllowedRemoteSources: []string{"github.com/eyelock/**"}})
	if err == nil {
		t.Fatal("expected the absolute include of an included harness to be refused")
	}
}
