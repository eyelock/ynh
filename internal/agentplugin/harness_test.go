package agentplugin

import (
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

// writePackage lays out a package the way ynd export --format agent-plugin
// does for every vendor: portable core, Copilot's namespace, Codex's hooks
// pointer, and Claude Code's compatibility files.
func writePackage(t *testing.T, dir string) {
	t.Helper()
	write(t, dir, "plugin.json", `{"$schema":"`+PluginSchemaID+`","name":"pkg","version":"2.0.0","description":"D",
	  "author":{"name":"A","email":"a@x"},"keywords":["k"],
	  "extensions":{"com.openai":{"hooks":"./com.openai/hooks/hooks.json"},"com.example":{}}}`)
	write(t, dir, "skills/hello/SKILL.md", skill("hello", "Hi."))
	write(t, dir, "mcp.json", specMCP)
	write(t, dir, "com.github.copilot/agents/checker.md", "---\nname: checker\ndescription: c\n---\n")
	write(t, dir, "com.openai/hooks/hooks.json", "{}")
	write(t, dir, "hooks/hooks.json", "{}")
	write(t, dir, ".claude-plugin/plugin.json", `{"name":"pkg"}`)
	write(t, dir, "AGENTS.md", "instructions\n")
}

func TestDeriveHarness_FullPackage(t *testing.T) {
	dir := t.TempDir()
	writePackage(t, dir)
	d, err := DeriveHarness(dir, []string{"com.github.copilot"})
	if err != nil {
		t.Fatalf("DeriveHarness: %v", err)
	}
	hj := d.Manifest
	if hj.Name != "pkg" || hj.Version != "2.0.0" || hj.Description != "D" || hj.Author == nil || hj.Author.Email != "a@x" || len(hj.Keywords) != 1 {
		t.Errorf("identity = %+v", hj)
	}
	if len(hj.MCPServers) != 3 || hj.MCPServers["legacy-events"].Type != plugin.MCPTypeSSE || hj.MCPServers["local-validator"].Cwd != PlaceholderRoot {
		t.Errorf("mcp = %+v", hj.MCPServers)
	}
	if len(hj.Includes) != 1 || hj.Includes[0].Local != "com.github.copilot" {
		t.Errorf("includes = %+v, want the Copilot namespace as a local include", hj.Includes)
	}
	if strings.Join(d.Extensions, ",") != "com.example,com.openai" {
		t.Errorf("extensions = %v", d.Extensions)
	}
	var msgs []string
	for _, diag := range d.Diagnostics {
		msgs = append(msgs, diag.String())
	}
	want := "com.github.copilot/hooks/hooks.json"
	if strings.Join(msgs, "\n") != "hooks/hooks.json: client-specific hooks are not imported" || strings.Contains(strings.Join(msgs, ""), want) {
		t.Errorf("diagnostics = %v", msgs)
	}
	if hj.Hooks != nil || hj.Profiles != nil || hj.EnvPassthrough != nil {
		t.Errorf("derived manifest carries fields the package cannot express: %+v", hj)
	}
}

func TestDeriveHarness_Minimal(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "plugin.json", minimalManifest)
	d, err := DeriveHarness(dir, []string{"com.github.copilot"})
	if err != nil {
		t.Fatal(err)
	}
	if d.Manifest.Version != "0.0.0" || d.Manifest.MCPServers != nil || d.Manifest.Includes != nil || len(d.Diagnostics) != 0 || len(d.Extensions) != 0 {
		t.Errorf("derived = %+v diags=%v", d.Manifest, d.Diagnostics)
	}
}

func TestDeriveHarness_Boundaries(t *testing.T) {
	t.Run("fatal manifest rejects", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "plugin.json", `{"$schema":"`+PluginSchemaID+`","name":"Bad"}`)
		if _, err := DeriveHarness(dir, nil); err == nil {
			t.Error("expected rejection")
		}
	})
	t.Run("invalid mcp.json disables MCP only", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "plugin.json", minimalManifest)
		write(t, dir, "mcp.json", `{"mcpServers":{}}`)
		write(t, dir, "skills/hello/SKILL.md", skill("hello", "Hi."))
		d, err := DeriveHarness(dir, nil)
		if err != nil {
			t.Fatal(err)
		}
		if d.Manifest.MCPServers != nil || len(d.Diagnostics) != 1 || !strings.Contains(d.Diagnostics[0].Message, "MCP disabled") {
			t.Errorf("mcp=%v diags=%v", d.Manifest.MCPServers, d.Diagnostics)
		}
	})
	t.Run("skipped server and skill are reported", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "plugin.json", minimalManifest)
		write(t, dir, "mcp.json", `{"$schema":"`+MCPSchemaID+`","mcpServers":{"ok":{"type":"stdio","command":"x"},"bad":{"type":"stdio","command":"a b"}}}`)
		write(t, dir, "skills/bad/SKILL.md", "nope")
		d, err := DeriveHarness(dir, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(d.Manifest.MCPServers) != 1 || len(d.Diagnostics) != 2 {
			t.Errorf("mcp=%v diags=%v", d.Manifest.MCPServers, d.Diagnostics)
		}
	})
	t.Run("empty namespace dir is not an include", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "plugin.json", minimalManifest)
		write(t, dir, "com.github.copilot/hooks/hooks.json", "{}")
		d, err := DeriveHarness(dir, []string{"com.github.copilot"})
		if err != nil {
			t.Fatal(err)
		}
		if d.Manifest.Includes != nil || len(d.Diagnostics) != 1 {
			t.Errorf("includes=%v diags=%v", d.Manifest.Includes, d.Diagnostics)
		}
	})
}
