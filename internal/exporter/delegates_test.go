package exporter

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/vendor"
)

// No delegates must write nothing at all, not an empty agents/ directory. An
// export that creates a stray directory changes what ships.
func TestWriteDelegates_NoneWritesNothing(t *testing.T) {
	out := t.TempDir()
	if err := WriteDelegates(out, nil, nil); err != nil {
		t.Fatalf("WriteDelegates: %v", err)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("no delegates should create no directories, got %v", entries)
	}
}

// A delegate that cannot be resolved must fail loudly and name itself. Export
// silently dropping a delegate would ship a harness missing a hand-off the
// manifest promised.
func TestResolveExportDelegates_UnresolvableIsAnError(t *testing.T) {
	_, err := ResolveExportDelegates([]harness.Delegate{
		{GitSource: harness.GitSource{Git: "example.invalid/nope/nothing-here"}},
	}, "", nil)
	if err == nil {
		t.Fatal("an unresolvable delegate must be an error, not a silent skip")
	}
	if !strings.Contains(err.Error(), "delegate") {
		t.Errorf("error should identify what failed, got: %v", err)
	}
}

// delegateRepo makes a delegate harness as a local git repo, with the given
// MCP servers, and returns its path.
func delegateRepo(t *testing.T, name string, servers map[string]any) string {
	t.Helper()
	dir := t.TempDir()
	hj := map[string]any{"name": name, "version": "0.1.0", "description": "A delegate."}
	if servers != nil {
		hj["mcp_servers"] = servers
	}
	data, err := json.Marshal(hj)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, plugin.PluginDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, plugin.PluginDir, plugin.PluginFile), data, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init"}, {"config", "user.email", "t@t"}, {"config", "user.name", "t"}, {"add", "."}, {"commit", "-m", "init"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func exportDelegatesFor(t *testing.T, repo string) []ExportDelegate {
	t.Helper()
	t.Setenv("YNH_HOME", t.TempDir())
	ds, err := ResolveExportDelegates([]harness.Delegate{{GitSource: harness.GitSource{Git: repo}}}, "", nil)
	if err != nil {
		t.Fatalf("ResolveExportDelegates: %v", err)
	}
	return ds
}

// ExportDelegate writes to <outputDir>/agents/, deliberately not inside the
// vendor ConfigDir the way assembler.AssembleDelegates does. The two are easy
// to confuse and the plugin layout depends on the difference. An export
// carries a delegate's servers with ${VAR} left literal, for the consumer.
func TestWriteDelegates_ClaudeCarriesServersUnexpanded(t *testing.T) {
	t.Setenv("DOCS_TOKEN", "secret-value")
	repo := delegateRepo(t, "docs-helper", map[string]any{
		"docs": map[string]any{"url": "https://docs.example.com/mcp", "headers": map[string]string{"Authorization": "Bearer ${DOCS_TOKEN}"}},
	})
	ds := exportDelegatesFor(t, repo)
	claude, err := vendor.Get("claude")
	if err != nil {
		t.Fatal(err)
	}

	out := t.TempDir()
	if err := WriteDelegates(out, ds, delegateCarrier(claude)); err != nil {
		t.Fatalf("WriteDelegates: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(out, "agents", "docs-helper.md"))
	if err != nil {
		t.Fatalf("agent file should be at <output>/agents/: %v", err)
	}
	content := string(data)
	want := `mcpServers: [{"docs":{"type":"http","url":"https://docs.example.com/mcp","headers":{"Authorization":"Bearer ${DOCS_TOKEN}"}}}]`
	if !strings.Contains(content, want) {
		t.Errorf("agent frontmatter missing %s\n%s", want, content)
	}
	if strings.Contains(content, "secret-value") {
		t.Error("an export must not expand ${VAR}")
	}
	for _, wrong := range []string{".claude", ".cursor", ".codex"} {
		if _, err := os.Stat(filepath.Join(out, wrong)); err == nil {
			t.Errorf("delegates must not be written inside %s on export", wrong)
		}
	}
}

// A vendor that cannot carry the servers gets no field and one warning per
// delegate.
func TestDelegateMCPWarnings(t *testing.T) {
	repo := delegateRepo(t, "docs-helper", map[string]any{
		"docs": map[string]any{"url": "https://docs.example.com/mcp"},
	})
	ds := exportDelegatesFor(t, repo)

	cursor, err := vendor.Get("cursor")
	if err != nil {
		t.Fatal(err)
	}
	warnings := DelegateMCPWarnings(ds, cursor)
	want := "delegate docs-helper declares MCP servers (docs) that Cursor subagents cannot carry; they are not available to it"
	if len(warnings) != 1 || warnings[0] != want {
		t.Errorf("warnings = %q, want [%q]", warnings, want)
	}

	claude, err := vendor.Get("claude")
	if err != nil {
		t.Fatal(err)
	}
	wantClaude := "delegate docs-helper declares MCP servers (docs); Claude Code ignores mcpServers in plugin agents, so they will not load when this plugin is installed"
	if got := DelegateMCPWarnings(ds, claude); len(got) != 1 || got[0] != wantClaude {
		t.Errorf("Claude warnings = %q, want [%q]", got, wantClaude)
	}

	out := t.TempDir()
	if err := WriteDelegates(out, ds, delegateCarrier(cursor)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, "agents", "docs-helper.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "mcpServers") {
		t.Errorf("a vendor that cannot carry them must not get the field:\n%s", data)
	}
}

// A delegate server that points into the delegate's own directory cannot be
// exported: the plugin carries the agent file, not the delegate's directory.
func TestResolveExportDelegates_RefusesPathRelativeServer(t *testing.T) {
	for name, server := range map[string]map[string]any{
		"relative command": {"command": "./serve.sh"},
		"plugin root":      {"command": "node", "args": []string{"${PLUGIN_ROOT}/server.js"}},
	} {
		t.Run(name, func(t *testing.T) {
			repo := delegateRepo(t, "local-helper", map[string]any{"local-srv": server})
			t.Setenv("YNH_HOME", t.TempDir())
			_, err := ResolveExportDelegates([]harness.Delegate{{GitSource: harness.GitSource{Git: repo}}}, "", nil)
			if err == nil {
				t.Fatal("a server inside the delegate's directory must not export")
			}
			for _, w := range []string{"local-helper", "local-srv"} {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error should name %q: %v", w, err)
				}
			}
		})
	}
}
