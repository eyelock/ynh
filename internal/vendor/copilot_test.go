package vendor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

func TestBuildCopilotArgs_Basic(t *testing.T) {
	configPath := t.TempDir()
	projectDir := t.TempDir()
	t.Chdir(projectDir)

	args, err := buildCopilotArgs(configPath, "", []string{"--model", "gpt-5.4"})
	if err != nil {
		t.Fatal(err)
	}

	pluginDir := filepath.Join(configPath, ".copilot")
	expected := []string{
		"copilot",
		"--no-auto-update",
		"--plugin-dir", pluginDir,
		"--add-dir", configPath,
		"--model", "gpt-5.4",
	}
	if len(args) != len(expected) {
		t.Fatalf("args length = %d, want %d\ngot:  %v\nwant: %v", len(args), len(expected), args, expected)
	}
	for i := range expected {
		if args[i] != expected[i] {
			t.Errorf("args[%d] = %q, want %q", i, args[i], expected[i])
		}
	}
}

func TestBuildCopilotArgs_InitialPrompt(t *testing.T) {
	configPath := t.TempDir()
	t.Chdir(t.TempDir())

	args, err := buildCopilotArgs(configPath, "do the thing", nil)
	if err != nil {
		t.Fatal(err)
	}

	if args[2] != "-i" || args[3] != "do the thing" {
		t.Errorf("expected -i flag with prompt right after --no-auto-update, got %v", args)
	}
}

// TestBuildCopilotArgs_NoAutoUpdate guards against a confirmed regression:
// Copilot's auto-update-on-launch silently drops the initial prompt (and any
// other launch args) when it swaps its own binary mid-startup. See the
// buildCopilotArgs doc comment.
func TestBuildCopilotArgs_NoAutoUpdate(t *testing.T) {
	configPath := t.TempDir()
	t.Chdir(t.TempDir())

	args, err := buildCopilotArgs(configPath, "", nil)
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, a := range args {
		if a == "--no-auto-update" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected --no-auto-update in args, got %v", args)
	}
}

func TestBuildCopilotArgs_ExtraArgsLast(t *testing.T) {
	configPath := t.TempDir()
	t.Chdir(t.TempDir())

	extra := []string{"--model", "gpt-5.4", "--allow-all-tools"}
	args, err := buildCopilotArgs(configPath, "", extra)
	if err != nil {
		t.Fatal(err)
	}

	tail := args[len(args)-3:]
	for i, want := range extra {
		if tail[i] != want {
			t.Errorf("tail[%d] = %q, want %q", i, tail[i], want)
		}
	}
}

func TestBuildCopilotArgs_ProjectsInstructions(t *testing.T) {
	configPath := t.TempDir()
	projectDir := t.TempDir()
	t.Chdir(projectDir)

	if err := os.WriteFile(filepath.Join(configPath, "AGENTS.md"), []byte("You are a helpful harness."), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := buildCopilotArgs(configPath, "", nil); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(projectDir, copilotInstructionsRelPath))
	if err != nil {
		t.Fatalf("expected instructions file to be written: %v", err)
	}
	if !strings.HasPrefix(string(got), "---\napplyTo: \"**/*\"\n---\n") {
		t.Errorf("missing applyTo frontmatter, got:\n%s", got)
	}
	if !strings.Contains(string(got), "You are a helpful harness.") {
		t.Errorf("missing harness instructions content, got:\n%s", got)
	}
}

func TestBuildCopilotArgs_NoInstructionsFile_NoProjection(t *testing.T) {
	configPath := t.TempDir()
	projectDir := t.TempDir()
	t.Chdir(projectDir)

	if _, err := buildCopilotArgs(configPath, "", nil); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(projectDir, copilotInstructionsRelPath)); !os.IsNotExist(err) {
		t.Error("expected no instructions file to be written when AGENTS.md is absent")
	}
}

func TestBuildCopilotArgs_ProjectsMCPConfig(t *testing.T) {
	configPath := t.TempDir()
	projectDir := t.TempDir()
	t.Chdir(projectDir)

	mcpDir := filepath.Join(configPath, ".copilot")
	if err := os.MkdirAll(mcpDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mcpContent := `{"mcpServers":{"github":{"type":"local","command":"npx","tools":["*"]}}}` + "\n"
	if err := os.WriteFile(filepath.Join(mcpDir, ".mcp.json"), []byte(mcpContent), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := buildCopilotArgs(configPath, "", nil); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(projectDir, copilotProjectMCPRelPath))
	if err != nil {
		t.Fatalf("expected .github/mcp.json to be written: %v", err)
	}
	if string(got) != mcpContent {
		t.Errorf("projected MCP config = %q, want %q", got, mcpContent)
	}
}

func TestCopilotGenerateHookConfig_AlwaysNil(t *testing.T) {
	c := &Copilot{}
	hooks := map[string][]plugin.HookEntry{
		"before_tool": {{Command: "echo hi"}},
	}
	result, err := c.GenerateHookConfig(hooks)
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Error("expected nil hook config regardless of input — hooks are blocked on the trust-gating gap")
	}
}

func TestCopilotGenerateMCPConfig_NilServers(t *testing.T) {
	c := &Copilot{}
	result, err := c.GenerateMCPConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Error("expected nil for nil servers")
	}
}

func TestCopilotGenerateMCPConfig_EmptyServers(t *testing.T) {
	c := &Copilot{}
	result, err := c.GenerateMCPConfig(map[string]plugin.MCPServer{})
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Error("expected nil for empty servers")
	}
}

func TestCopilotGenerateMCPConfig_TypeTranslation(t *testing.T) {
	c := &Copilot{}
	servers := map[string]plugin.MCPServer{
		"local-server": {
			Command: "npx",
			Args:    []string{"-y", "@modelcontextprotocol/server-github"},
			Env:     map[string]string{"GITHUB_TOKEN": "${GITHUB_TOKEN}"},
		},
		"remote-server": {
			URL:     "https://mcp.example.com/mcp",
			Headers: map[string]string{"Authorization": "Bearer xyz"},
		},
	}

	result, err := c.GenerateMCPConfig(servers)
	if err != nil {
		t.Fatal(err)
	}
	data, ok := result[filepath.Join(".copilot", ".mcp.json")]
	if !ok {
		t.Fatal("expected .copilot/.mcp.json key")
	}

	var config struct {
		MCPServers map[string]struct {
			Type    string   `json:"type"`
			Command string   `json:"command"`
			URL     string   `json:"url"`
			Tools   []string `json:"tools"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	local, ok := config.MCPServers["local-server"]
	if !ok {
		t.Fatal("expected local-server entry")
	}
	if local.Type != "local" {
		t.Errorf("local-server type = %q, want %q", local.Type, "local")
	}
	if len(local.Tools) != 1 || local.Tools[0] != "*" {
		t.Errorf("local-server tools = %v, want [*]", local.Tools)
	}

	remote, ok := config.MCPServers["remote-server"]
	if !ok {
		t.Fatal("expected remote-server entry")
	}
	if remote.Type != "http" {
		t.Errorf("remote-server type = %q, want %q", remote.Type, "http")
	}
}

// TestCopilotManifestLayouts locks #471: each generator writes one layout,
// chosen by its caller, whatever files the output directory holds. The run
// dir nests the plugin under .copilot/ (the --plugin-dir target); an export
// keeps it at the plugin root beside the flattened skills. Copilot loads no
// plugin content unless .claude-plugin/plugin.json is at the root it is
// pointed at (hand-tested, v1.0.75), so the wrong layout breaks the plugin.
func TestCopilotManifestLayouts(t *testing.T) {
	c := &Copilot{}
	hj := &plugin.HarnessJSON{Name: "my-harness", Version: "1.0.0", Description: "test harness"}
	runDirManifest := filepath.Join(".copilot", ".claude-plugin", "plugin.json")
	exportManifest := filepath.Join(".claude-plugin", "plugin.json")

	// What an output directory may hold when the manifest is generated. The
	// last is the #471 trigger: an export that had just written
	// .copilot/.mcp.json.
	contents := map[string][]string{
		"empty":            nil,
		"flattened skills": {filepath.Join("skills", "s", "SKILL.md")},
		"nested skills":    {filepath.Join(".copilot", "skills", "s", "SKILL.md")},
		"nested mcp":       {filepath.Join(".copilot", ".mcp.json")},
	}
	for label, files := range contents {
		outputDir := t.TempDir()
		for _, f := range files {
			if err := os.MkdirAll(filepath.Dir(filepath.Join(outputDir, f)), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(outputDir, f), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}

		t.Run(label+"/run dir", func(t *testing.T) {
			result, err := c.GeneratePluginManifest(hj, outputDir)
			if err != nil {
				t.Fatal(err)
			}
			if got := mapKeys(result); len(got) != 1 || got[0] != runDirManifest {
				t.Fatalf("manifest written to %v, want %s", got, runDirManifest)
			}
			var pj claudePluginJSON
			if err := json.Unmarshal(result[runDirManifest], &pj); err != nil {
				t.Fatalf("invalid JSON: %v", err)
			}
			if pj.Name != "my-harness" {
				t.Errorf("name = %q, want my-harness", pj.Name)
			}
		})
		t.Run(label+"/export", func(t *testing.T) {
			result, err := c.GenerateExportPluginManifest(hj, outputDir)
			if err != nil {
				t.Fatal(err)
			}
			if got := mapKeys(result); len(got) != 1 || got[0] != exportManifest {
				t.Fatalf("manifest written to %v, want %s", got, exportManifest)
			}
		})
	}
}

// TestCopilotMCPConfigPaths locks where each Copilot MCP generator writes. A
// run dir carries .copilot/.mcp.json, which buildCopilotArgs projects into the
// project's .github/mcp.json. An exported plugin carries .github/mcp.json at
// its root, a default MCP path for a .claude-plugin Copilot plugin, and not
// .mcp.json, which Codex writes in a merged package (#471).
func TestCopilotMCPConfigPaths(t *testing.T) {
	servers := map[string]plugin.MCPServer{"local": {Command: "npx"}}
	c := &Copilot{}
	run, err := c.GenerateMCPConfig(servers)
	if err != nil {
		t.Fatal(err)
	}
	plug, err := c.GeneratePluginMCPConfig(servers)
	if err != nil {
		t.Fatal(err)
	}
	runPath := filepath.Join(".copilot", ".mcp.json")
	plugPath := filepath.Join(".github", "mcp.json")
	if got := mapKeys(run); len(got) != 1 || got[0] != runPath {
		t.Errorf("run MCP written to %v, want %s", got, runPath)
	}
	if got := mapKeys(plug); len(got) != 1 || got[0] != plugPath {
		t.Errorf("plugin MCP written to %v, want %s", got, plugPath)
	}
	if string(run[runPath]) != string(plug[plugPath]) {
		t.Errorf("run and plugin MCP documents differ:\n%s\n---\n%s", run[runPath], plug[plugPath])
	}

	for _, empty := range []map[string]plugin.MCPServer{nil, {}} {
		result, err := c.GeneratePluginMCPConfig(empty)
		if err != nil {
			t.Fatal(err)
		}
		if result != nil {
			t.Errorf("expected nil for no servers, got %v", mapKeys(result))
		}
	}
}

func mapKeys(m map[string][]byte) []string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestCopilotApplyRuntimeInstructions(t *testing.T) {
	c := &Copilot{}
	runDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(runDir, "AGENTS.md"), []byte("base instructions"), 0o644); err != nil {
		t.Fatal(err)
	}

	args, err := c.ApplyRuntimeInstructions(runDir, "PR #22 in eyelock/assistants")
	if err != nil {
		t.Fatal(err)
	}
	if args != nil {
		t.Errorf("expected nil args (file-based delivery), got %v", args)
	}

	got, err := os.ReadFile(filepath.Join(runDir, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "base instructions") {
		t.Error("expected original content preserved")
	}
	if !strings.Contains(string(got), "PR #22 in eyelock/assistants") {
		t.Error("expected runtime instructions appended")
	}
}

func TestCopilotGenerateSystemPrompt(t *testing.T) {
	c := &Copilot{}
	files := c.GenerateSystemPrompt([]byte("instructions content"))
	if string(files["AGENTS.md"]) != "instructions content" {
		t.Errorf("AGENTS.md = %q, want %q", files["AGENTS.md"], "instructions content")
	}
	if len(files) != 1 {
		t.Errorf("expected exactly one file, got %d: %v", len(files), files)
	}
}

func TestCopilotIdentity(t *testing.T) {
	c := &Copilot{}
	if c.Name() != "copilot" {
		t.Errorf("Name() = %q, want copilot", c.Name())
	}
	if c.CLIName() != "copilot" {
		t.Errorf("CLIName() = %q, want copilot", c.CLIName())
	}
	if c.ConfigDir() != ".copilot" {
		t.Errorf("ConfigDir() = %q, want .copilot", c.ConfigDir())
	}
	if c.InstructionsFile() != "AGENTS.md" {
		t.Errorf("InstructionsFile() = %q, want AGENTS.md", c.InstructionsFile())
	}
	if c.NeedsSymlinks() {
		t.Error("NeedsSymlinks() should be false — native --plugin-dir loading")
	}
	if !c.SupportsInitialPrompt() {
		t.Error("SupportsInitialPrompt() should be true — confirmed via -i flag")
	}
	if !c.SupportsExportDelegates() {
		t.Error("SupportsExportDelegates() should be true — custom agents are supported")
	}
}

func TestCopilotExportArtifactDirs(t *testing.T) {
	c := &Copilot{}
	dirs := c.ExportArtifactDirs()
	if _, ok := dirs["commands"]; ok {
		t.Error("commands should be excluded — not supported by Copilot CLI")
	}
	if _, ok := dirs["skills"]; !ok {
		t.Error("skills should be included")
	}
	if _, ok := dirs["agents"]; !ok {
		t.Error("agents should be included")
	}
}

func TestCopilotInstallCleanNoop(t *testing.T) {
	c := &Copilot{}
	entries, err := c.Install(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if entries != nil {
		t.Error("expected nil entries — no symlinks needed")
	}
	if err := c.Clean(entries); err != nil {
		t.Fatal(err)
	}
}

// Harness instructions that open with their own frontmatter must not give
// the projected instructions file a second block (#532): ynh's applyTo is
// merged into the source's, and a source applyTo cannot narrow it.
func TestBuildCopilotArgs_InstructionsFrontmatterMerged(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "source keys kept, applyTo owned by ynh",
			src:  "---\ndescription: DevOps harness\napplyTo: \"src/**\"\n---\n\nYou are a helpful harness.",
			want: "---\napplyTo: \"**/*\"\ndescription: DevOps harness\n---\n\nYou are a helpful harness.\n",
		},
		{
			name: "multi-line source applyTo is removed whole",
			src:  "---\napplyTo:\n  - src/**\nexcludeAgent: code-review\n---\nBody",
			want: "---\napplyTo: \"**/*\"\nexcludeAgent: code-review\n---\n\nBody\n",
		},
		{
			name: "no source frontmatter is unchanged",
			src:  "Body",
			want: "---\napplyTo: \"**/*\"\n---\nBody\n",
		},
		{
			name: "frontmatter only",
			src:  "---\ndescription: x\n---\n",
			want: "---\napplyTo: \"**/*\"\ndescription: x\n---\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := t.TempDir()
			projectDir := t.TempDir()
			t.Chdir(projectDir)
			if err := os.WriteFile(filepath.Join(configPath, "AGENTS.md"), []byte(tt.src), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := buildCopilotArgs(configPath, "", nil); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(filepath.Join(projectDir, copilotInstructionsRelPath))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("got:\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}

func TestIsolateMCP_PartialAndUnsupportedVendors(t *testing.T) {
	dir := t.TempDir()

	args, warning := (&Copilot{}).IsolateMCP(dir)
	if len(args) != 1 || args[0] != "--disable-builtin-mcps" {
		t.Errorf("copilot args = %v, want [--disable-builtin-mcps]", args)
	}
	if !strings.Contains(warning, "~/.copilot/mcp-config.json") {
		t.Errorf("copilot warning = %q, want it to name ~/.copilot/mcp-config.json", warning)
	}

	for name, a := range map[string]Adapter{"codex": &Codex{}, "cursor": &Cursor{}} {
		args, warning := a.IsolateMCP(dir)
		if len(args) != 0 {
			t.Errorf("%s args = %v, want none", name, args)
		}
		if !strings.Contains(warning, "not supported") || strings.Contains(warning, "\n") {
			t.Errorf("%s warning = %q, want a one-line 'not supported' warning", name, warning)
		}
	}
}
