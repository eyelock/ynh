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

// Claude Code and Codex share one shape: "http" and "sse" on url entries,
// and no type on a stdio entry, which is what an untyped entry means there.
func TestClaudeAndCodexMCPType(t *testing.T) {
	for name, tc := range map[string]struct {
		adapter Adapter
		file    string
	}{
		"claude": {&Claude{}, filepath.Join(".claude", ".mcp.json")},
		"codex":  {&Codex{}, ".mcp.json"},
	} {
		t.Run(name, func(t *testing.T) {
			out, err := tc.adapter.GenerateMCPConfig(typedServers)
			if err != nil {
				t.Fatal(err)
			}
			servers := decodeServers(t, out[tc.file])
			for srv, want := range map[string]any{"local": nil, "remote": "http", "explicit": "http", "legacy": "sse"} {
				if got := servers[srv]["type"]; got != want {
					t.Errorf("%s type = %v, want %v", srv, got, want)
				}
			}
			if servers["local"]["cwd"] != "./data" {
				t.Errorf("cwd dropped: %v", servers["local"])
			}
		})
	}
}

// Cursor's mcp.json defines no transport key and infers the kind from the
// fields, so nothing canonical leaks into it.
func TestCursorMCPDropsType(t *testing.T) {
	out, err := (&Cursor{}).GenerateMCPConfig(typedServers)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{filepath.Join(".cursor", "mcp.json"), "mcp.json"} {
		for name, s := range decodeServers(t, out[file]) {
			if _, has := s["type"]; has {
				t.Errorf("%s: %s carries a type key Cursor does not define", file, name)
			}
		}
	}
}

// Copilot's vocabulary is local/http/sse, keyed on the transport rather
// than on which fields happen to be set.
func TestCopilotMCPTypeFromTransport(t *testing.T) {
	out, err := (&Copilot{}).GenerateMCPConfig(typedServers)
	if err != nil {
		t.Fatal(err)
	}
	servers := decodeServers(t, out[filepath.Join(".copilot", ".mcp.json")])
	for srv, want := range map[string]string{"local": "local", "remote": "http", "explicit": "http", "legacy": "sse"} {
		if got := servers[srv]["type"]; got != want {
			t.Errorf("%s type = %v, want %s", srv, got, want)
		}
	}
}
