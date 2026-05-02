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

// TestProfile_HookInheritsBaseEvent verifies the documented merge rule:
// hooks use per-event replace, so events the profile does NOT declare are
// inherited from the base. A profile that overrides only `before_tool`
// must leave `after_tool` carrying the base's hook command.
func TestProfile_HookInheritsBaseEvent(t *testing.T) {
	s := newSandbox(t)

	dir := filepath.Join(t.TempDir(), "hook-inherit")
	if err := os.MkdirAll(filepath.Join(dir, ".ynh-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "hook-inherit",
  "version": "0.1.0",
  "hooks": {
    "before_tool": [{"command": "echo BASE_BEFORE"}],
    "after_tool":  [{"command": "echo BASE_AFTER"}]
  },
  "profiles": {
    "p": {
      "hooks": {
        "before_tool": [{"command": "echo PROFILE_BEFORE"}]
      }
    }
  }
}
`
	if err := os.WriteFile(filepath.Join(dir, ".ynh-plugin", "plugin.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s.mustRunYnh(t, "install", dir)

	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	mustRunYnhInDir(t, s, project, "run", "hook-inherit", "-v", "cursor", "--profile", "p", "--install")

	hookFile := filepath.Join(s.home, "run", "hook-inherit", ".cursor", "hooks.json")
	got, err := os.ReadFile(hookFile)
	if err != nil {
		t.Fatalf("reading hook file: %v", err)
	}
	if !bytes.Contains(got, []byte("PROFILE_BEFORE")) {
		t.Errorf("profile override missing — expected PROFILE_BEFORE in:\n%s", got)
	}
	if !bytes.Contains(got, []byte("BASE_AFTER")) {
		t.Errorf("base event not inherited — expected BASE_AFTER in:\n%s", got)
	}
	if bytes.Contains(got, []byte("BASE_BEFORE")) {
		t.Errorf("profile-overridden event leaked base hook BASE_BEFORE:\n%s", got)
	}
}

// TestProfile_McpDeepMerge verifies the documented MCP merge rule: profile
// servers add to / override base servers; absent servers are inherited.
// Base has server "alpha"; profile adds "beta". Both must appear in the
// rendered MCP file when the profile is active.
func TestProfile_McpDeepMerge(t *testing.T) {
	s := newSandbox(t)

	dir := filepath.Join(t.TempDir(), "mcp-merge")
	if err := os.MkdirAll(filepath.Join(dir, ".ynh-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "mcp-merge",
  "version": "0.1.0",
  "mcp_servers": {
    "alpha": {"command": "echo", "args": ["alpha"]}
  },
  "profiles": {
    "p": {
      "mcp_servers": {
        "beta": {"command": "echo", "args": ["beta"]}
      }
    }
  }
}
`
	if err := os.WriteFile(filepath.Join(dir, ".ynh-plugin", "plugin.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s.mustRunYnh(t, "install", dir)

	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	mustRunYnhInDir(t, s, project, "run", "mcp-merge", "-v", "cursor", "--profile", "p", "--install")

	mcpFile := filepath.Join(s.home, "run", "mcp-merge", ".cursor", "mcp.json")
	got, err := os.ReadFile(mcpFile)
	if err != nil {
		t.Fatalf("reading MCP file: %v", err)
	}
	for _, want := range []string{"alpha", "beta"} {
		if !bytes.Contains(got, []byte(want)) {
			t.Errorf("MCP file missing server %q:\n%s", want, got)
		}
	}
}

// TestMcp_HttpUrl exercises the `url:` branch of MCP server validation —
// an HTTP-transport server (no command, just URL). Locks the alternative
// validation branch and confirms the URL passes through to the rendered
// vendor file.
func TestMcp_HttpUrl(t *testing.T) {
	s := newSandbox(t)
	name := "mcp-http"
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(filepath.Join(dir, ".ynh-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": %q,
  "version": "0.1.0",
  "mcp_servers": {
    "remote": {"url": "https://mcp.example.invalid/v1"}
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

	mcpFile := filepath.Join(s.home, "run", name, ".claude", ".mcp.json")
	body2, err := os.ReadFile(mcpFile)
	if err != nil {
		t.Fatalf("reading MCP file: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(body2, &raw); err != nil {
		t.Fatalf("MCP file is not valid JSON: %v\n%s", err, body2)
	}
	if !bytes.Contains(body2, []byte("mcp.example.invalid")) {
		t.Errorf("MCP file missing URL:\n%s", body2)
	}
}
