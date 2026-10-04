package vendor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

// TestClaudeMCPConfigPaths locks which file each Claude MCP generator writes.
// A session (`ynh run`, `ynd preview`, the agent loop) passes .claude/ to
// --plugin-dir, so its MCP file is .mcp.json at that plugin's root. An
// exported plugin carries mcp/claude.json, which its manifest names in
// "mcpServers": a Claude plugin reads .mcp.json at its root or what that
// field names, never .claude/.mcp.json (#481), and the root .mcp.json is
// Codex's in a merged package.
func TestClaudeMCPConfigPaths(t *testing.T) {
	servers := map[string]plugin.MCPServer{"github": {Command: "npx", Args: []string{"-y", "server"}}}
	c := &Claude{}

	session, err := c.GenerateMCPConfig(servers)
	if err != nil {
		t.Fatal(err)
	}
	plug, err := c.GeneratePluginMCPConfig(servers)
	if err != nil {
		t.Fatal(err)
	}
	sessionPath := filepath.Join(".claude", ".mcp.json")
	pluginPath := filepath.Join("mcp", "claude.json")
	if len(session) != 1 || session[sessionPath] == nil {
		t.Errorf("session MCP files = %v, want only %s", keysOf(session), sessionPath)
	}
	if len(plug) != 1 || plug[pluginPath] == nil {
		t.Errorf("plugin MCP files = %v, want only %s", keysOf(plug), pluginPath)
	}
	if string(session[sessionPath]) != string(plug[pluginPath]) {
		t.Error("session and plugin MCP documents differ")
	}
	var doc struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(plug[pluginPath], &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc.MCPServers["github"]; !ok {
		t.Errorf("plugin MCP file has no github server under mcpServers:\n%s", plug[pluginPath])
	}

	for _, none := range []map[string]plugin.MCPServer{nil, {}} {
		got, err := c.GeneratePluginMCPConfig(none)
		if err != nil {
			t.Fatal(err)
		}
		if got != nil {
			t.Errorf("no servers: got %v, want nil", keysOf(got))
		}
	}
}

// TestPluginManifestMCPPointer: a manifest names the vendor's plugin MCP file
// in "mcpServers" when it is in the output directory, and omits the field
// otherwise, since a plugin loader rejects a path that does not exist.
// Claude's manifest names only Claude's file: another vendor's file, such as
// Codex's root .mcp.json, gives it no pointer.
func TestPluginManifestMCPPointer(t *testing.T) {
	hj := &plugin.HarnessJSON{Name: "h", Version: "1.0.0"}
	tests := []struct {
		adapter  Adapter
		manifest string
		mcpFile  string
		other    string
	}{
		{&Claude{}, filepath.Join(".claude-plugin", "plugin.json"), filepath.Join("mcp", "claude.json"), ".mcp.json"},
		{&Codex{}, filepath.Join(".codex-plugin", "plugin.json"), ".mcp.json", filepath.Join("mcp", "claude.json")},
	}
	for _, tt := range tests {
		t.Run(tt.adapter.Name(), func(t *testing.T) {
			dir := t.TempDir()
			if got := manifestMCPField(t, tt.adapter, hj, dir, tt.manifest); got != "" {
				t.Errorf("without MCP file: mcpServers = %q, want none", got)
			}
			writeTestFile(t, filepath.Join(dir, tt.other))
			if got := manifestMCPField(t, tt.adapter, hj, dir, tt.manifest); got != "" {
				t.Errorf("with only %s: mcpServers = %q, want none", tt.other, got)
			}
			writeTestFile(t, filepath.Join(dir, tt.mcpFile))
			if got, want := manifestMCPField(t, tt.adapter, hj, dir, tt.manifest), "./"+filepath.ToSlash(tt.mcpFile); got != want {
				t.Errorf("with MCP file: mcpServers = %q, want %q", got, want)
			}
		})
	}
}

// A session layout never gets an MCP pointer: its Claude MCP file is
// .claude/.mcp.json, the default of the plugin --plugin-dir loads.
func TestClaudeSessionManifestHasNoMCPPointer(t *testing.T) {
	hj := &plugin.HarnessJSON{Name: "h", Version: "1.0.0"}
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, ".claude", ".mcp.json"))
	if got := manifestMCPField(t, &Claude{}, hj, dir, filepath.Join(".claude-plugin", "plugin.json")); got != "" {
		t.Errorf("session manifest mcpServers = %q, want none", got)
	}
}

func writeTestFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func manifestMCPField(t *testing.T, a Adapter, hj *plugin.HarnessJSON, dir, manifest string) string {
	t.Helper()
	files, err := a.GeneratePluginManifest(hj, dir)
	if err != nil {
		t.Fatal(err)
	}
	data, ok := files[manifest]
	if !ok {
		t.Fatalf("no %s in %v", manifest, keysOf(files))
	}
	var m struct {
		MCPServers string `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m.MCPServers
}
