package harness

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/agentplugin"
	"github.com/eyelock/ynh/internal/plugin"
)

func writeAgentPlugin(t *testing.T, dir string) {
	t.Helper()
	files := map[string]string{
		"plugin.json": `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"pkg","version":"1.2.3",
		  "extensions":{"com.openai":{"hooks":"./com.openai/hooks/hooks.json"}}}`,
		"skills/hello/SKILL.md": "---\nname: hello\ndescription: Hi.\n---\nHi.\n",
		"mcp.json": `{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{
		  "local":{"type":"stdio","command":"./bin/srv","args":["${PLUGIN_ROOT}/c"],"env":{"T":"${TOKEN}"},"cwd":"${PLUGIN_DATA}"},
		  "remote":{"type":"sse","url":"https://x/sse"}}}`,
		"com.github.copilot/agents/checker.md": "---\nname: checker\ndescription: c\ntools: Read\n---\n",
		"com.openai/hooks/hooks.json":          "{}",
		"AGENTS.md":                            "You are pkg.\n",
	}
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func snapshot(t *testing.T, dir string) string {
	t.Helper()
	var entries []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		info, _ := d.Info()
		entries = append(entries, rel+":"+info.ModTime().String())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(entries)
	return strings.Join(entries, "\n")
}

func TestLoadDir_AgentPluginDerivedWithoutWriting(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	dir := t.TempDir()
	writeAgentPlugin(t, dir)
	before := snapshot(t, dir)

	p, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if after := snapshot(t, dir); after != before {
		t.Errorf("loading wrote into the package:\n%s\nwant\n%s", after, before)
	}
	if p.Format != agentplugin.Format || p.Name != "pkg" || p.Version != "1.2.3" || p.Manifest == nil || p.Manifest.Name != "pkg" {
		t.Errorf("harness = %+v", p)
	}
	if len(p.Includes) != 1 || p.Includes[0].Local != "com.github.copilot" || !p.Includes[0].IsLocal() {
		t.Errorf("includes = %+v", p.Includes)
	}
	if len(p.MCPServers) != 2 || p.MCPServers["remote"].Type != plugin.MCPTypeSSE {
		t.Errorf("mcp = %+v", p.MCPServers)
	}
	if strings.Join(p.ImportedExtensions, ",") != "com.openai" {
		t.Errorf("extensions = %v", p.ImportedExtensions)
	}
	if len(p.Diagnostics) != 0 {
		t.Errorf("diagnostics = %v, want none for a clean package", p.Diagnostics)
	}
	if f, err := DetectFormat(dir); err != nil || f != agentplugin.Format || !IsHarnessDir(dir) {
		t.Errorf("format detection: %q, %v", f, err)
	}
}

func TestLoadDir_YnhManifestWinsOverAgentPlugin(t *testing.T) {
	dir := t.TempDir()
	writeAgentPlugin(t, dir)
	writeTestHarness(t, dir, "real")
	p, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.Format != "" || p.Name != "real" {
		t.Errorf("a ynh manifest must take precedence: %+v", p)
	}
}

func TestLoadDir_AgentPluginFatalManifest(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"Bad Name"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDir(dir); err == nil || !strings.Contains(err.Error(), "loading Agent Plugin") {
		t.Errorf("err = %v", err)
	}
}

func TestAssembleMCPServers(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	dir := t.TempDir()
	writeAgentPlugin(t, dir)
	p, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	data := PluginDataDir(p)
	if !strings.HasSuffix(data, filepath.Join("plugin-data", "local--pkg")) {
		t.Errorf("data dir = %q", data)
	}
	servers, err := AssembleMCPServers(p, data, func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatalf("a derived harness has no allowlist to fail: %v", err)
	}
	l := servers["local"]
	if l.Command != filepath.Join(p.Dir, "bin", "srv") || l.Args[0] != p.Dir+"/c" || l.Cwd != data {
		t.Errorf("local = %+v", l)
	}
	if l.Env["T"] != "${TOKEN}" {
		t.Errorf("credential reference must stay literal for a package: %v", l.Env)
	}
	if l.Env[plugin.MCPEnvRoot] != p.Dir || l.Env[plugin.MCPEnvData] != data {
		t.Errorf("PLUGIN_ROOT/PLUGIN_DATA not supplied: %v", l.Env)
	}
	if servers["remote"].URL != "https://x/sse" {
		t.Errorf("remote = %+v", servers["remote"])
	}

	// A ynh harness keeps its allowlist semantics and gets no reserved env.
	ydir := t.TempDir()
	writeTestHarness(t, ydir, "y")
	if err := AddMCP(ydir, "s", MCPAddOptions{Command: "./x", Env: map[string]string{"T": "${TOKEN}"}}); err != nil {
		t.Fatal(err)
	}
	y, err := LoadDir(ydir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AssembleMCPServers(y, "/d", func(string) (string, bool) { return "", false }); err == nil || !strings.Contains(err.Error(), "not in env_passthrough") {
		t.Errorf("err = %v", err)
	}
	y.EnvPassthrough = []string{"TOKEN"}
	out, err := AssembleMCPServers(y, "/d", func(string) (string, bool) { return "tok", true })
	if err != nil {
		t.Fatal(err)
	}
	if out["s"].Env["T"] != "tok" || out["s"].Env[plugin.MCPEnvRoot] != "" || out["s"].Command != filepath.Join(y.Dir, "x") {
		t.Errorf("ynh harness = %+v", out["s"])
	}
}

func TestEditorRefusesAgentPlugin(t *testing.T) {
	dir := t.TempDir()
	writeAgentPlugin(t, dir)
	err := AddMCP(dir, "n", MCPAddOptions{Command: "x"})
	if err == nil || !strings.Contains(err.Error(), "Agent Plugins package") {
		t.Errorf("err = %v", err)
	}
	for _, md := range []string{".agents", ".ynh-plugin"} {
		if _, statErr := os.Stat(filepath.Join(dir, md)); !os.IsNotExist(statErr) {
			t.Errorf("editor wrote %s into the package", md)
		}
	}
}
