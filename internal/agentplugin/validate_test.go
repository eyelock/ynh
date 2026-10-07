package agentplugin

import (
	"strings"
	"testing"
)

func fatal(issues []Issue) (out []string) {
	for _, i := range issues {
		if i.Fatal {
			out = append(out, i.String())
		}
	}
	return
}

func TestValidate_ConformingPackage(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "plugin.json", fullManifest)
	write(t, dir, "skills/summarize/SKILL.md", skill("summarize", "Summarise documents. Use when asked for a summary."))
	write(t, dir, "mcp.json", specMCP)
	write(t, dir, "com.example.client/hooks/hooks.json", `{}`)
	write(t, dir, "LICENSE", "MIT")
	if issues := Validate(dir); len(issues) != 0 {
		t.Errorf("issues = %v, want none", issues)
	}
}

func TestValidate_MinimalPackage(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "plugin.json", minimalManifest)
	if issues := Validate(dir); len(issues) != 0 {
		t.Errorf("a manifest alone is a valid plugin (§4.1): %v", issues)
	}
}

func TestValidate_NotAPlugin(t *testing.T) {
	issues := Validate(t.TempDir())
	if len(issues) != 1 || !issues[0].Fatal || !strings.Contains(issues[0].Message, "missing") {
		t.Errorf("issues = %v", issues)
	}
}

func TestValidate_FatalManifestStopsThere(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "plugin.json", `{"$schema":"`+PluginSchemaID+`","name":"Bad Name"}`)
	write(t, dir, "skills/x/SKILL.md", "no frontmatter")
	issues := Validate(dir)
	if len(issues) != 1 || !issues[0].Fatal {
		t.Errorf("issues = %v, want only the manifest rejection (§5.2: components are not discovered)", issues)
	}
}

func TestValidate_ReportsEveryBoundary(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "plugin.json", `{"$schema":"`+PluginSchemaID+`","name":"x","hooks":"hooks.json"}`)
	write(t, dir, "skills/good/SKILL.md", skill("good", "Fine."))
	write(t, dir, "skills/bad/SKILL.md", "nope")
	write(t, dir, "mcp.json", `{"$schema":"`+MCPSchemaID+`","mcpServers":{"ok":{"type":"stdio","command":"x"},"bad":{"type":"stdio","command":"a b"}}}`)
	issues := Validate(dir)
	var got []string
	for _, i := range issues {
		got = append(got, i.Path)
	}
	if want := "plugin.json skills/bad/SKILL.md mcp.json server bad"; strings.Join(got, " ") != want {
		t.Errorf("issue paths = %v, want %q", got, want)
	}
	if len(fatal(issues)) != 0 {
		t.Errorf("none of these is fatal to the plugin: %v", fatal(issues))
	}
}

func TestValidate_TopLevelMCPFailureIsFatalToMCPOnly(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "plugin.json", minimalManifest)
	write(t, dir, "mcp.json", `{"mcpServers":{}}`)
	issues := Validate(dir)
	if len(issues) != 1 || !issues[0].Fatal || !strings.Contains(issues[0].Message, "MCP disabled") {
		t.Errorf("issues = %v", issues)
	}
}

func TestIssueAndDiagnosticString(t *testing.T) {
	if got := (Issue{"plugin.json", "bad", true}).String(); got != "plugin.json: bad" {
		t.Errorf("Issue.String = %q", got)
	}
	if got := (Issue{"", "bad", true}).String(); got != "bad" {
		t.Errorf("Issue.String = %q", got)
	}
	if got := (Diagnostic{"skills/x", "skipped"}).String(); got != "skills/x: skipped" {
		t.Errorf("Diagnostic.String = %q", got)
	}
	if got := (Diagnostic{"", "skipped"}).String(); got != "skipped" {
		t.Errorf("Diagnostic.String = %q", got)
	}
}
