package exporter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/agentplugin"
)

// writePortableSource builds a harness that exercises every component the
// portable format has an opinion about.
func writePortableSource(t *testing.T, manifest string) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"skills/hello/SKILL.md":       "---\nname: hello\ndescription: Say hello. Use when greeting.\n---\nHello.\n",
		"agents/checker.md":           "---\nname: checker\ndescription: Checks.\ntools: Read\n---\nCheck.\n",
		"rules/terse.md":              "Be terse.\n",
		"commands/check.md":           "Run checks.\n",
		"instructions.md":             "You are a harness.\n",
		"scripts/guard.sh":            "#!/bin/sh\nexit 0\n",
		".agents/harness/plugin.json": manifest,
	}
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const portableManifest = `{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "My_Harness", "version": "0.1.0", "description": "Spot",
  "author": {"name": "D", "email": "d@example.com"}, "keywords": ["a"],
  "hooks": {"before_tool": [{"command": "./scripts/guard.sh", "matcher": "Write"}]},
  "env_passthrough": ["TOKEN"],
  "mcp_servers": {
    "local":  {"command": "npx", "args": ["-y", "x", "${PLUGIN_ROOT}/cfg"], "cwd": "data", "env": {"T": "${TOKEN}"}},
    "shelly": {"command": "node server.js"},
    "remote": {"url": "https://x.example.com/mcp"},
    "legacy": {"type": "sse", "url": "https://x.example.com/sse"},
    "plain":  {"url": "http://x.example.com/mcp"}
  }
}`

func exportPortable(t *testing.T, src string, vendors ...string) (string, ExportResult) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "pkg")
	results, err := Export(ExportOptions{SourceDir: src, OutputDir: out, Vendors: vendors, Mode: ModeAgentPlugin})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if len(results) != 1 || results[0].Vendor != AgentPluginVendor {
		t.Fatalf("results = %+v", results)
	}
	return out, results[0]
}

// assertClaudePointers checks that Claude Code's compatibility manifest names
// its hooks and MCP files, which must exist.
func assertClaudePointers(t *testing.T, out, hooks, mcp string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(out, ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Hooks      string `json:"hooks"`
		MCPServers string `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m.Hooks != hooks || m.MCPServers != mcp {
		t.Errorf(".claude-plugin/plugin.json hooks = %q, mcpServers = %q, want %q and %q", m.Hooks, m.MCPServers, hooks, mcp)
	}
	for _, p := range []string{m.Hooks, m.MCPServers} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(p))); err != nil {
			t.Errorf("manifest names %s: %v", p, err)
		}
	}
}

func listFiles(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func hasWarning(r ExportResult, substr string) bool {
	for _, w := range r.Warnings {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}

func TestAgentPlugin_AllVendorsLayout(t *testing.T) {
	src := writePortableSource(t, portableManifest)
	out, r := exportPortable(t, src)

	want := []string{
		".claude-plugin/plugin.json", // Claude Code compatibility manifest
		"AGENTS.md",
		"CLAUDE.md",
		"agents/checker.md", // Claude Code reads agents/ at the root
		"com.github.copilot/agents/checker.md",
		"com.openai/hooks/hooks.json",
		"commands/check.md",
		"hooks/claude.json", // Claude Code's hook file, named by its manifest
		"mcp/claude.json",   // Claude Code's MCP file, named by its manifest
		"mcp.json",
		"plugin.json",
		"rules/terse.md",
		"scripts/guard.sh", // the script the hooks run, shipped once
		"skills/hello/SKILL.md",
	}
	if got := listFiles(t, out); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("files:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if r.Skills != 1 || r.Agents != 2 {
		t.Errorf("counts = %d skills %d agents", r.Skills, r.Agents)
	}

	// The package is a conformant Agent Plugin.
	for _, issue := range agentplugin.Validate(out) {
		t.Errorf("exported package: %s", issue)
	}

	// Manifest: normalised name, metadata, and only the OpenAI hooks pointer.
	m, diags, err := agentplugin.ReadManifest(out)
	if err != nil || len(diags) != 0 {
		t.Fatalf("ReadManifest: %v %v", err, diags)
	}
	if m.Name != "my-harness" || m.Version != "0.1.0" || m.Author == nil || m.Author.Email != "d@example.com" || len(m.Keywords) != 1 {
		t.Errorf("manifest = %+v", m)
	}
	if string(m.Extensions["com.openai"]) != `{"hooks":"./com.openai/hooks/hooks.json"}` || len(m.Extensions) != 1 {
		t.Errorf("extensions = %v", m.Extensions)
	}
	if !hasWarning(r, `name "My_Harness" is not a valid Agent Plugins name; exported as "my-harness"`) {
		t.Errorf("warnings = %v, want the name warning", r.Warnings)
	}

	// Portable MCP: typed, cwd normalised, the two inexpressible servers left out.
	cfg, mdiags, err := agentplugin.ReadMCP(out)
	if err != nil || len(mdiags) != 0 {
		t.Fatalf("ReadMCP: %v %v", err, mdiags)
	}
	var names []string
	for n := range cfg.Servers {
		names = append(names, n)
	}
	if len(cfg.Servers) != 3 || cfg.Servers["local"].Cwd != "./data" || cfg.Servers["local"].Type != "stdio" || cfg.Servers["legacy"].Type != "sse" || cfg.Servers["remote"].Type != "streamable-http" {
		t.Errorf("servers = %+v (%v)", cfg.Servers, names)
	}
	for _, w := range []string{
		`shelly: skipped: command "node server.js" must be a single executable token`,
		`plain: skipped: url "http://x.example.com/mcp": a non-loopback endpoint must use https`,
		`mcp server "local": env.T references ${TOKEN}`,
		`mcp server "local" uses ${PLUGIN_ROOT} or ${PLUGIN_DATA}, which Cursor does not expand`,
	} {
		if !hasWarning(r, w) {
			t.Errorf("missing warning %q in %v", w, r.Warnings)
		}
	}

	// Claude Code's own files keep every server in Claude's shape.
	var claude struct {
		Servers map[string]map[string]any `json:"mcpServers"`
	}
	data, _ := os.ReadFile(filepath.Join(out, "mcp", "claude.json"))
	if err := json.Unmarshal(data, &claude); err != nil {
		t.Fatal(err)
	}
	if len(claude.Servers) != 5 || claude.Servers["remote"]["type"] != "http" || claude.Servers["legacy"]["type"] != "sse" {
		t.Errorf("mcp/claude.json = %v", claude.Servers)
	}
	assertClaudePointers(t, out, "./hooks/claude.json", "./mcp/claude.json")
	// Each hook file anchors the script at the plugin root in its own
	// client's variable (#498).
	hooks, _ := os.ReadFile(filepath.Join(out, "hooks", "claude.json"))
	if !strings.Contains(string(hooks), `\"${CLAUDE_PLUGIN_ROOT}\"/scripts/guard.sh`) {
		t.Errorf("hooks/claude.json = %s, want the command anchored at CLAUDE_PLUGIN_ROOT", hooks)
	}
	codexHooks, _ := os.ReadFile(filepath.Join(out, "com.openai", "hooks", "hooks.json"))
	if !strings.Contains(string(codexHooks), `\"${PLUGIN_ROOT}\"/scripts/guard.sh`) {
		t.Errorf("com.openai hooks = %s, want the command anchored at PLUGIN_ROOT", codexHooks)
	}
	if info, err := os.Stat(filepath.Join(out, "scripts", "guard.sh")); err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Errorf("scripts/guard.sh not shipped executable: %v", err)
	}
	if hasWarning(r, "not portable: no selected vendor") {
		t.Errorf("unexpected warning in %v", r.Warnings)
	}
}

func TestAgentPlugin_CursorOnlyIsPortableCore(t *testing.T) {
	src := writePortableSource(t, portableManifest)
	out, r := exportPortable(t, src, "cursor")
	want := []string{"AGENTS.md", "mcp.json", "plugin.json", "skills/hello/SKILL.md"}
	if got := listFiles(t, out); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("files = %v, want %v", got, want)
	}
	for _, w := range []string{
		"1 agents not portable: no selected vendor (cursor)",
		"1 rules not portable",
		"1 commands not portable",
		"hooks not portable: no selected vendor (cursor)",
	} {
		if !hasWarning(r, w) {
			t.Errorf("missing warning %q in %v", w, r.Warnings)
		}
	}
	if r.Agents != 0 {
		t.Errorf("agents = %d", r.Agents)
	}
	if _, ok := readManifest(t, out).Extensions["com.openai"]; ok {
		t.Error("com.openai extension written without codex selected")
	}
}

// Claude Code's hooks and MCP servers go where its own plugin export puts
// them, named by its manifest. Nothing lands at hooks/hooks.json, which
// Claude always loads and Codex discovers by default, or at .mcp.json, which
// Codex reads by default, so a Codex client of the same package finds
// neither (#469, #481).
func TestAgentPlugin_ClaudeOnlyCompatibilityFiles(t *testing.T) {
	src := writePortableSource(t, portableManifest)
	out, r := exportPortable(t, src, "claude")
	for _, rel := range []string{"hooks/hooks.json", ".mcp.json"} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Errorf("%s written; no vendor reads it from this package", rel)
		}
	}
	assertClaudePointers(t, out, "./hooks/claude.json", "./mcp/claude.json")
	if hasWarning(r, "hooks not portable") {
		t.Errorf("warnings = %v", r.Warnings)
	}
	if _, err := os.Stat(filepath.Join(out, "com.openai")); !os.IsNotExist(err) {
		t.Error("com.openai written without codex selected")
	}
	if len(readManifest(t, out).Extensions) != 0 {
		t.Error("extensions written without a namespace vendor selected")
	}
}

func TestAgentPlugin_CodexOnly(t *testing.T) {
	src := writePortableSource(t, portableManifest)
	out, r := exportPortable(t, src, "codex")
	want := []string{"AGENTS.md", "com.openai/hooks/hooks.json", "mcp.json", "plugin.json", "scripts/guard.sh", "skills/hello/SKILL.md"}
	if got := listFiles(t, out); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("files = %v, want %v", got, want)
	}
	if hasWarning(r, "hooks not portable") {
		t.Errorf("warnings = %v", r.Warnings)
	}
	if !hasWarning(r, "1 agents not portable: no selected vendor (codex)") {
		t.Errorf("warnings = %v", r.Warnings)
	}
}

func TestAgentPlugin_CopilotNamespace(t *testing.T) {
	src := writePortableSource(t, portableManifest)
	out, r := exportPortable(t, src, "copilot")
	want := []string{"AGENTS.md", "com.github.copilot/agents/checker.md", "mcp.json", "plugin.json", "skills/hello/SKILL.md"}
	if got := listFiles(t, out); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("files = %v, want %v", got, want)
	}
	// Copilot's legacy export excludes rules and commands; the namespace follows it.
	if !hasWarning(r, "1 rules not portable") || !hasWarning(r, "1 commands not portable") || !hasWarning(r, "hooks not portable") {
		t.Errorf("warnings = %v", r.Warnings)
	}
	if r.Agents != 1 {
		t.Errorf("agents = %d", r.Agents)
	}
}

func TestAgentPlugin_NoMCPNoHooksNoInstructions(t *testing.T) {
	src := writePortableSource(t, `{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"plain","version":"1.0.0"}`)
	if err := os.Remove(filepath.Join(src, "instructions.md")); err != nil {
		t.Fatal(err)
	}
	out, r := exportPortable(t, src, "cursor")
	want := []string{"plugin.json", "skills/hello/SKILL.md"}
	if got := listFiles(t, out); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("files = %v, want %v", got, want)
	}
	if hasWarning(r, "hooks") || hasWarning(r, "mcp") {
		t.Errorf("warnings = %v", r.Warnings)
	}
	for _, issue := range agentplugin.Validate(out) {
		t.Errorf("exported package: %s", issue)
	}
}

func TestAgentPlugin_AllServersInexpressible(t *testing.T) {
	src := writePortableSource(t, `{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"p","version":"1.0.0",
	  "mcp_servers":{"s":{"command":"/usr/bin/node"}}}`)
	out, r := exportPortable(t, src, "cursor")
	if _, err := os.Stat(filepath.Join(out, "mcp.json")); !os.IsNotExist(err) {
		t.Error("mcp.json written with no expressible server")
	}
	if !hasWarning(r, "no MCP server could be expressed portably; mcp.json not written") {
		t.Errorf("warnings = %v", r.Warnings)
	}
}

func readManifest(t *testing.T, out string) *agentplugin.Manifest {
	t.Helper()
	m, _, err := agentplugin.ReadManifest(out)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// A picked include names artifact types the portable core and a vendor's
// share copy in separate passes; a pick outside a pass is not an error.
func TestAgentPlugin_PickedIncludesSplitAcrossPasses(t *testing.T) {
	src := writePortableSource(t, `{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"picky","version":"1.0.0",
	  "includes":[{"local":"inc","pick":["skills/extra","agents/helper.md"]}]}`)
	for rel, content := range map[string]string{
		"inc/skills/extra/SKILL.md": "---\nname: extra\ndescription: Extra skill.\n---\nExtra.\n",
		"inc/agents/helper.md":      "---\nname: helper\ndescription: Helps.\ntools: Read\n---\nHelp.\n",
	} {
		p := filepath.Join(src, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out, r := exportPortable(t, src, "cursor", "claude")
	for _, rel := range []string{"skills/extra/SKILL.md", "skills/hello/SKILL.md", "agents/helper.md", "agents/checker.md"} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(rel))); err != nil {
			t.Errorf("missing %s", rel)
		}
	}
	if r.Skills != 2 || r.Agents != 2 {
		t.Errorf("counts = %d skills %d agents", r.Skills, r.Agents)
	}
}
