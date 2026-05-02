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
	}{
		{vendor: "claude", mcpRel: filepath.Join(".claude", ".mcp.json")},
		{vendor: "codex", mcpRel: ".mcp.json"},
		{vendor: "cursor", mcpRel: filepath.Join(".cursor", "mcp.json")},
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
			mustRunYnhInDir(t, s, project, "run", name, "-v", tc.vendor, "--install")

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
		})
	}
}

func newMcpHarness(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(filepath.Join(dir, ".ynh-plugin"), 0o755); err != nil {
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
	if err := os.WriteFile(filepath.Join(dir, ".ynh-plugin", "plugin.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}
