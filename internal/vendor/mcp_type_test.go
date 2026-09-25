package vendor

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

// typedServers covers every canonical transport plus an untyped remote
// server, which is what every harness written before the type field carried.
var typedServers = map[string]plugin.MCPServer{
	"local":    {Command: "npx", Args: []string{"-y", "x"}, Cwd: "./data"},
	"remote":   {URL: "https://x/mcp", Headers: map[string]string{"X-A": "1"}},
	"legacy":   {Type: plugin.MCPTypeSSE, URL: "https://x/sse"},
	"explicit": {Type: plugin.MCPTypeStreamableHTTP, URL: "https://x/mcp2"},
}

func decodeServers(t *testing.T, data []byte) map[string]map[string]any {
	t.Helper()
	var doc struct {
		Servers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, data)
	}
	return doc.Servers
}

// pluginMCPGenerator is the plugin-side MCP writer a vendor whose plugin
// reads a different file from its session implements.
type pluginMCPGenerator interface {
	GeneratePluginMCPConfig(map[string]plugin.MCPServer) (map[string][]byte, error)
}

// mcpOutputs returns every MCP file the adapter writes for typedServers,
// session and plugin, keyed by path, so each spelling is checked wherever
// it lands.
func mcpOutputs(t *testing.T, a Adapter) map[string][]byte {
	t.Helper()
	out, err := a.GenerateMCPConfig(typedServers)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for p, d := range out {
		files[p] = d
	}
	if pg, ok := a.(pluginMCPGenerator); ok {
		pout, err := pg.GeneratePluginMCPConfig(typedServers)
		if err != nil {
			t.Fatal(err)
		}
		for p, d := range pout {
			files[p] = d
		}
	}
	return files
}

// Claude Code and Codex share one shape: "http" and "sse" on url entries,
// and no type on a stdio entry, which is what an untyped entry means there.
// Claude's session and plugin files carry the same document.
func TestClaudeAndCodexMCPType(t *testing.T) {
	for name, tc := range map[string]struct {
		adapter Adapter
		files   []string
	}{
		"claude": {&Claude{}, []string{filepath.Join(".claude", ".mcp.json"), filepath.Join("mcp", "claude.json")}},
		"codex":  {&Codex{}, []string{".mcp.json"}},
	} {
		t.Run(name, func(t *testing.T) {
			out := mcpOutputs(t, tc.adapter)
			for _, file := range tc.files {
				servers := decodeServers(t, out[file])
				for srv, want := range map[string]any{"local": nil, "remote": "http", "explicit": "http", "legacy": "sse"} {
					if got := servers[srv]["type"]; got != want {
						t.Errorf("%s: %s type = %v, want %v", file, srv, got, want)
					}
				}
				if servers["local"]["cwd"] != "./data" {
					t.Errorf("%s: cwd dropped: %v", file, servers["local"])
				}
			}
		})
	}
}

// Cursor's mcp.json defines no transport key and infers the kind from the
// fields, so nothing canonical leaks into the session or the plugin file.
func TestCursorMCPDropsType(t *testing.T) {
	out := mcpOutputs(t, &Cursor{})
	for _, file := range []string{filepath.Join(".cursor", "mcp.json"), "mcp.json"} {
		servers := decodeServers(t, out[file])
		if len(servers) != len(typedServers) {
			t.Fatalf("%s: %d servers, want %d", file, len(servers), len(typedServers))
		}
		for name, s := range servers {
			if _, has := s["type"]; has {
				t.Errorf("%s: %s carries a type key Cursor does not define", file, name)
			}
		}
	}
}

// Copilot's vocabulary is local/http/sse, keyed on the transport rather
// than on which fields happen to be set, in the run-dir and export files.
func TestCopilotMCPTypeFromTransport(t *testing.T) {
	out := mcpOutputs(t, &Copilot{})
	for _, file := range []string{filepath.Join(".copilot", ".mcp.json"), filepath.Join(".github", "mcp.json")} {
		servers := decodeServers(t, out[file])
		for srv, want := range map[string]string{"local": "local", "remote": "http", "explicit": "http", "legacy": "sse"} {
			if got := servers[srv]["type"]; got != want {
				t.Errorf("%s: %s type = %v, want %s", file, srv, got, want)
			}
		}
	}
}
