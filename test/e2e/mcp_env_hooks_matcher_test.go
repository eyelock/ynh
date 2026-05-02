//go:build e2e

package e2e

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestMcp_EnvPassthrough asserts that env vars declared on an MCP server
// reach the rendered vendor MCP file. Tooling depends on this — losing
// env passthrough silently means MCP servers run with the wrong config.
func TestMcp_EnvPassthrough(t *testing.T) {
	s := newSandbox(t)
	name := "mcp-env"
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(filepath.Join(dir, ".ynh-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": %q,
  "version": "0.1.0",
  "mcp_servers": {
    "withenv": {
      "command": "echo",
      "env": {"API_KEY_NAME": "TEST_TOKEN_VALUE"}
    }
  }
}
`, name)
	if err := os.WriteFile(filepath.Join(dir, ".ynh-plugin", "plugin.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s.mustRunYnh(t, "install", dir)

	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	mustRunYnhInDir(t, s, project, "run", name, "-v", "claude", "--install")

	mcp, err := os.ReadFile(filepath.Join(s.home, "run", name, ".claude", ".mcp.json"))
	if err != nil {
		t.Fatalf("reading MCP file: %v", err)
	}
	for _, want := range []string{"API_KEY_NAME", "TEST_TOKEN_VALUE"} {
		if !bytes.Contains(mcp, []byte(want)) {
			t.Errorf("MCP env var %q missing from rendered file:\n%s", want, mcp)
		}
	}
}

// TestHooks_Matcher asserts that the optional `matcher` field on a hook
// entry is preserved in the rendered vendor hook file. matcher narrows
// which tool calls a hook applies to (Claude's "PreToolUse: Bash" etc.) —
// dropping it silently would mean hooks fire too broadly.
func TestHooks_Matcher(t *testing.T) {
	s := newSandbox(t)
	name := "hooks-matcher"
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(filepath.Join(dir, ".ynh-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": %q,
  "version": "0.1.0",
  "hooks": {
    "before_tool": [{"matcher": "Bash", "command": "echo with-matcher"}]
  }
}
`, name)
	if err := os.WriteFile(filepath.Join(dir, ".ynh-plugin", "plugin.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s.mustRunYnh(t, "install", dir)

	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	mustRunYnhInDir(t, s, project, "run", name, "-v", "claude", "--install")

	hooks, err := os.ReadFile(filepath.Join(s.home, "run", name, ".claude", "hooks", "hooks.json"))
	if err != nil {
		t.Fatalf("reading hooks file: %v", err)
	}
	if !bytes.Contains(hooks, []byte("Bash")) {
		t.Errorf("matcher field 'Bash' missing from rendered hooks:\n%s", hooks)
	}
	if !bytes.Contains(hooks, []byte("with-matcher")) {
		t.Errorf("hook command missing from rendered hooks:\n%s", hooks)
	}
}
