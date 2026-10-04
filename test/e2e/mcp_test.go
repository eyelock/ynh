//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestMcp_PerVendor verifies that mcp_servers declared in plugin.json get
// translated into each vendor's native MCP config file:
//
//   - Claude: .claude/.mcp.json
//   - Codex:  .mcp.json (plugin root)
//   - Cursor: .cursor/mcp.json
//
// Locks the per-vendor file path. Silently breaking it means MCP servers
// stop being discovered by the vendor CLI.
func TestMcp_PerVendor(t *testing.T) {
	cases := []struct {
		vendor string
		mcpRel string // path relative to runDir
		absent string // relative to runDir; an MCP file the vendor never reads in a project
	}{
		{vendor: "claude", mcpRel: filepath.Join(".claude", ".mcp.json")},
		{vendor: "codex", mcpRel: ".mcp.json"},
		// mcp.json at the root is the Cursor plugin path; a project session
		// reads only .cursor/mcp.json, so the run assembly must not carry it (#470).
		{vendor: "cursor", mcpRel: filepath.Join(".cursor", "mcp.json"), absent: "mcp.json"},
	}

	for _, tc := range cases {
		t.Run(tc.vendor, func(t *testing.T) {
			s := newSandbox(t)
			name := fmt.Sprintf("mcp-%s", tc.vendor)
			harness := newMcpHarness(t, name)
			s.mustRunYnh(t, "install", harness)

			project := filepath.Join(t.TempDir(), "project")
			if err := os.MkdirAll(project, 0o755); err != nil {
				t.Fatal(err)
			}
			mustRunYnhInDir(t, s, project, "run", "local/"+name, "-v", tc.vendor, "--install")

			runDir := filepath.Join(s.home, "run", name)
			body, err := os.ReadFile(filepath.Join(runDir, tc.mcpRel))
			if err != nil {
				t.Fatalf("expected MCP file %s: %v", tc.mcpRel, err)
			}
			var raw map[string]any
			if err := json.Unmarshal(body, &raw); err != nil {
				t.Fatalf("MCP file is not valid JSON: %v\n%s", err, body)
			}
			// Server name "echoer" appears as a key somewhere in the rendered file.
			if !bytes.Contains(body, []byte("echoer")) {
				t.Errorf("MCP file missing server name 'echoer':\n%s", body)
			}
			if tc.absent != "" {
				if _, err := os.Stat(filepath.Join(runDir, tc.absent)); !os.IsNotExist(err) {
					t.Errorf("%s must not be assembled for %s, stat err = %v", tc.absent, tc.vendor, err)
				}
			}
		})
	}
}

func newMcpHarness(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(filepath.Join(dir, ".agents/harness"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": %q,
  "version": "0.1.0",
  "mcp_servers": {
    "echoer": {"command": "echo", "args": ["hello"]}
  }
}
`, name)
	if err := os.WriteFile(filepath.Join(dir, ".agents/harness", "plugin.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestMcp_Export verifies that an exported plugin carries each vendor's MCP
// file where its plugin reads it, that every manifest pointer names a file
// that is there, and that no two vendors share a path in a merged package.
// Claude's file is mcp/claude.json, named by its manifest, never the session
// path .claude/.mcp.json (#481); Codex's manifest names .mcp.json in merged
// mode too, because its manifest is written after the file (#482).
func TestMcp_Export(t *testing.T) {
	harness := newMcpHarness(t, "mcp-export")
	type want struct {
		manifest, file, pointer string
	}
	vendors := map[string]want{
		"claude":  {".claude-plugin/plugin.json", "mcp/claude.json", "./mcp/claude.json"},
		"codex":   {".codex-plugin/plugin.json", ".mcp.json", "./.mcp.json"},
		"cursor":  {".cursor-plugin/plugin.json", "mcp.json", ""},
		"copilot": {".claude-plugin/plugin.json", ".github/mcp.json", ""},
	}
	pointer := func(t *testing.T, path string) string {
		t.Helper()
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("manifest: %v", err)
		}
		var m struct {
			MCPServers string `json:"mcpServers"`
		}
		if err := json.Unmarshal(body, &m); err != nil {
			t.Fatalf("manifest %s: %v", path, err)
		}
		return m.MCPServers
	}
	check := func(t *testing.T, root, v, wantPointer string) {
		t.Helper()
		w := vendors[v]
		body, err := os.ReadFile(filepath.Join(root, w.file))
		if err != nil {
			t.Fatalf("%s MCP file: %v", v, err)
		}
		if !bytes.Contains(body, []byte("echoer")) {
			t.Errorf("%s: %s missing server 'echoer':\n%s", v, w.file, body)
		}
		if got := pointer(t, filepath.Join(root, w.manifest)); got != wantPointer {
			t.Errorf("%s: %s mcpServers = %q, want %q", v, w.manifest, got, wantPointer)
		}
		for _, p := range []string{".claude", ".cursor", ".copilot"} {
			if _, err := os.Stat(filepath.Join(root, p)); !os.IsNotExist(err) {
				t.Errorf("%s must not be in a plugin export, stat err = %v", p, err)
			}
		}
	}

	t.Run("per vendor", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "out")
		mustRunYnd(t, "export", harness, "-v", "claude,codex,cursor,copilot", "-o", out)
		for v, w := range vendors {
			check(t, filepath.Join(out, v), v, w.pointer)
		}
	})
	t.Run("merged", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "out")
		mustRunYnd(t, "export", harness, "--merged", "-o", out)
		for v, w := range vendors {
			wantPointer := w.pointer
			if v == "copilot" {
				// Copilot shares Claude's manifest, which names Claude's file.
				wantPointer = vendors["claude"].pointer
			}
			check(t, out, v, wantPointer)
		}
	})
}
