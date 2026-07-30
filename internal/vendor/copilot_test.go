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

	if args[1] != "-i" || args[2] != "do the thing" {
		t.Errorf("expected -i flag with prompt right after binary name, got %v", args)
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

func TestCopilotGeneratePluginManifest_RunDirLayout(t *testing.T) {
	c := &Copilot{}
	hj := &plugin.HarnessJSON{
		Name:        "my-harness",
		Version:     "1.0.0",
		Description: "test harness",
	}

	// Mirrors ynh-run: the assembler always creates outputDir/.copilot/<artifactDir>
	// (even if empty) before GeneratePluginManifest is called.
	outputDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outputDir, ".copilot", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := c.GeneratePluginManifest(hj, outputDir)
	if err != nil {
		t.Fatal(err)
	}

	key := filepath.Join(".copilot", ".claude-plugin", "plugin.json")
	data, ok := result[key]
	if !ok {
		t.Fatalf("expected %s key, got keys: %v", key, mapKeys(result))
	}

	var pj copilotPluginJSON
	if err := json.Unmarshal(data, &pj); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if pj.Name != "my-harness" {
		t.Errorf("name = %q, want my-harness", pj.Name)
	}
}

func TestCopilotGeneratePluginManifest_ExportLayout(t *testing.T) {
	c := &Copilot{}
	hj := &plugin.HarnessJSON{Name: "my-harness", Version: "1.0.0"}

	// Mirrors `ynd export`: skills/agents are flattened directly under
	// outputDir, with no .copilot/ nesting (see exportForVendor/exportMerged
	// in internal/exporter). The manifest must sit alongside them — a
	// manifest nested under a phantom .copilot/ here would silently break
	// the exported plugin (confirmed by hand-testing: Copilot requires
	// .claude-plugin/plugin.json at the exact root it's pointed at).
	outputDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outputDir, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := c.GeneratePluginManifest(hj, outputDir)
	if err != nil {
		t.Fatal(err)
	}

	key := filepath.Join(".claude-plugin", "plugin.json")
	if _, ok := result[key]; !ok {
		t.Fatalf("expected flat %s key, got keys: %v", key, mapKeys(result))
	}
	if _, ok := result[filepath.Join(".copilot", ".claude-plugin", "plugin.json")]; ok {
		t.Error("manifest should not be nested under .copilot/ in export layout")
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
