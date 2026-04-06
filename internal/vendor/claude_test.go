package vendor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

func TestBuildClaudeArgs_WithInstructions(t *testing.T) {
	configPath := t.TempDir()

	claudeDir := filepath.Join(configPath, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	instructions := "You are a helpful harness."
	if err := os.WriteFile(filepath.Join(configPath, "CLAUDE.md"), []byte(instructions), 0o644); err != nil {
		t.Fatal(err)
	}

	args := buildClaudeArgs(configPath, []string{"--model", "opus"})

	expected := []string{
		"claude",
		"--plugin-dir", claudeDir,
		"--append-system-prompt", instructions,
		"--model", "opus",
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

func TestBuildClaudeArgs_NoInstructions(t *testing.T) {
	configPath := t.TempDir()

	claudeDir := filepath.Join(configPath, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	args := buildClaudeArgs(configPath, nil)

	expected := []string{
		"claude",
		"--plugin-dir", claudeDir,
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

func TestBuildClaudeArgs_EmptyInstructions(t *testing.T) {
	configPath := t.TempDir()

	if err := os.MkdirAll(filepath.Join(configPath, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(configPath, "CLAUDE.md"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	args := buildClaudeArgs(configPath, nil)

	for _, arg := range args {
		if arg == "--append-system-prompt" {
			t.Error("empty instructions should not produce --append-system-prompt")
		}
	}
}

func TestBuildClaudeArgs_ExtraArgsLast(t *testing.T) {
	configPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(configPath, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	extra := []string{"--verbose", "--model", "sonnet"}
	args := buildClaudeArgs(configPath, extra)

	tail := args[len(args)-3:]
	for i, want := range extra {
		if tail[i] != want {
			t.Errorf("tail[%d] = %q, want %q", i, tail[i], want)
		}
	}
}

func TestBuildClaudeArgs_NonInteractive(t *testing.T) {
	configPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(configPath, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configPath, "CLAUDE.md"), []byte("instructions"), 0o644); err != nil {
		t.Fatal(err)
	}

	args := buildClaudeArgs(configPath, []string{"-p", "fix the bug"})

	foundPlugin := false
	foundPrompt := false
	for i, arg := range args {
		if arg == "--plugin-dir" {
			foundPlugin = true
		}
		if arg == "-p" && i+1 < len(args) && args[i+1] == "fix the bug" {
			foundPrompt = true
		}
	}
	if !foundPlugin {
		t.Error("missing --plugin-dir")
	}
	if !foundPrompt {
		t.Error("missing -p prompt")
	}
}

func TestClaudeGenerateHookConfig_NilHooks(t *testing.T) {
	c := &Claude{}
	result := c.GenerateHookConfig(nil)
	if result != nil {
		t.Error("expected nil for nil hooks")
	}
}

func TestClaudeGenerateHookConfig_EmptyHooks(t *testing.T) {
	c := &Claude{}
	result := c.GenerateHookConfig(map[string][]plugin.HookEntry{})
	if result != nil {
		t.Error("expected nil for empty hooks")
	}
}

func TestClaudeGenerateHookConfig_ThreeLevelNesting(t *testing.T) {
	c := &Claude{}
	hooks := map[string][]plugin.HookEntry{
		"before_tool": {
			{Matcher: "Bash", Command: "echo before bash"},
			{Matcher: "Bash", Command: "echo also before bash"},
			{Command: "echo before all"},
		},
		"on_stop": {
			{Command: "echo done"},
		},
	}

	result := c.GenerateHookConfig(hooks)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	data, ok := result[filepath.Join(".claude", "settings.json")]
	if !ok {
		t.Fatal("expected .claude/settings.json key")
	}

	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	hooksObj, ok := settings["hooks"].(map[string]any)
	if !ok {
		t.Fatal("expected hooks object")
	}

	// Check PreToolUse
	preToolUse, ok := hooksObj["PreToolUse"].([]any)
	if !ok {
		t.Fatal("expected PreToolUse array")
	}
	// Two groups: one for "Bash" matcher, one for no matcher
	if len(preToolUse) != 2 {
		t.Fatalf("PreToolUse groups = %d, want 2", len(preToolUse))
	}

	// First group: Bash matcher with 2 hooks
	group0 := preToolUse[0].(map[string]any)
	if group0["matcher"] != "Bash" {
		t.Errorf("first group matcher = %v, want Bash", group0["matcher"])
	}
	innerHooks0 := group0["hooks"].([]any)
	if len(innerHooks0) != 2 {
		t.Errorf("Bash group has %d hooks, want 2", len(innerHooks0))
	}
	hook0 := innerHooks0[0].(map[string]any)
	if hook0["type"] != "command" {
		t.Errorf("hook type = %v, want command", hook0["type"])
	}

	// Second group: no matcher with 1 hook
	group1 := preToolUse[1].(map[string]any)
	if _, hasMatcher := group1["matcher"]; hasMatcher {
		// matcher should be omitted (empty string)
		if group1["matcher"] != "" {
			t.Errorf("second group should have no matcher, got %v", group1["matcher"])
		}
	}

	// Check Stop
	stop, ok := hooksObj["Stop"].([]any)
	if !ok {
		t.Fatal("expected Stop array")
	}
	if len(stop) != 1 {
		t.Fatalf("Stop groups = %d, want 1", len(stop))
	}
}

func TestClaudeGenerateMCPConfig_NilServers(t *testing.T) {
	c := &Claude{}
	result := c.GenerateMCPConfig(nil)
	if result != nil {
		t.Error("expected nil for nil servers")
	}
}

func TestClaudeGenerateMCPConfig_EmptyServers(t *testing.T) {
	c := &Claude{}
	result := c.GenerateMCPConfig(map[string]plugin.MCPServer{})
	if result != nil {
		t.Error("expected nil for empty servers")
	}
}

func TestClaudeGenerateMCPConfig_Passthrough(t *testing.T) {
	c := &Claude{}
	servers := map[string]plugin.MCPServer{
		"github": {
			Command: "npx",
			Args:    []string{"-y", "@modelcontextprotocol/server-github"},
			Env:     map[string]string{"GITHUB_TOKEN": "${GITHUB_TOKEN}"},
		},
	}

	result := c.GenerateMCPConfig(servers)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	data, ok := result[".mcp.json"]
	if !ok {
		t.Fatal("expected .mcp.json key")
	}

	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	mcpServers, ok := config["mcpServers"].(map[string]any)
	if !ok {
		t.Fatal("expected mcpServers object")
	}

	github, ok := mcpServers["github"].(map[string]any)
	if !ok {
		t.Fatal("expected github server object")
	}

	if github["command"] != "npx" {
		t.Errorf("command = %v, want npx", github["command"])
	}

	args, ok := github["args"].([]any)
	if !ok || len(args) != 2 {
		t.Errorf("args = %v, want [-y @modelcontextprotocol/server-github]", github["args"])
	}
}

func TestClaudeGenerateHookConfig_EventTranslation(t *testing.T) {
	c := &Claude{}
	hooks := map[string][]plugin.HookEntry{
		"before_tool":   {{Command: "cmd1"}},
		"after_tool":    {{Command: "cmd2"}},
		"before_prompt": {{Command: "cmd3"}},
		"on_stop":       {{Command: "cmd4"}},
	}

	result := c.GenerateHookConfig(hooks)
	data := result[filepath.Join(".claude", "settings.json")]

	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}

	hooksObj := settings["hooks"].(map[string]any)
	expectedEvents := []string{"PreToolUse", "PostToolUse", "UserPromptSubmit", "Stop"}
	for _, event := range expectedEvents {
		if _, ok := hooksObj[event]; !ok {
			t.Errorf("missing event %s", event)
		}
	}
}
