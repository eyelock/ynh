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

	args := buildClaudeArgs(configPath, "", []string{"--model", "opus"})

	expected := []string{
		"claude",
		"--plugin-dir", claudeDir,
		"--add-dir", configPath,
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

	args := buildClaudeArgs(configPath, "", nil)

	expected := []string{
		"claude",
		"--plugin-dir", claudeDir,
		"--add-dir", configPath,
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

	args := buildClaudeArgs(configPath, "", nil)

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
	args := buildClaudeArgs(configPath, "", extra)

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

	args := buildClaudeArgs(configPath, "", []string{"-p", "fix the bug"})

	foundPlugin := false
	foundAddDir := false
	foundPrompt := false
	for i, arg := range args {
		if arg == "--plugin-dir" {
			foundPlugin = true
		}
		if arg == "--add-dir" {
			foundAddDir = true
		}
		if arg == "-p" && i+1 < len(args) && args[i+1] == "fix the bug" {
			foundPrompt = true
		}
	}
	if !foundPlugin {
		t.Error("missing --plugin-dir")
	}
	if !foundAddDir {
		t.Error("missing --add-dir")
	}
	if !foundPrompt {
		t.Error("missing -p prompt")
	}
}

func TestBuildClaudeArgs_InitialPromptBeforeAddDir(t *testing.T) {
	configPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(configPath, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	args := buildClaudeArgs(configPath, "do the thing", nil)

	// Prompt must appear before --add-dir
	promptIdx := -1
	addDirIdx := -1
	for i, arg := range args {
		if arg == "do the thing" {
			promptIdx = i
		}
		if arg == "--add-dir" {
			addDirIdx = i
		}
	}
	if promptIdx == -1 {
		t.Fatal("initial prompt not found in args")
	}
	if addDirIdx == -1 {
		t.Fatal("--add-dir not found in args")
	}
	if promptIdx > addDirIdx {
		t.Errorf("prompt at index %d comes after --add-dir at index %d; --add-dir suppresses positional args that follow it", promptIdx, addDirIdx)
	}
}

func TestClaudeGenerateHookConfig_NilHooks(t *testing.T) {
	c := &Claude{}
	result, err := c.GenerateHookConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Error("expected nil for nil hooks")
	}
}

func TestClaudeGenerateHookConfig_EmptyHooks(t *testing.T) {
	c := &Claude{}
	result, err := c.GenerateHookConfig(map[string][]plugin.HookEntry{})
	if err != nil {
		t.Fatal(err)
	}
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

	result, err := c.GenerateHookConfig(hooks)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	data, ok := result[filepath.Join(".claude", "hooks", "hooks.json")]
	if !ok {
		t.Fatal("expected .claude/hooks/hooks.json key")
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

func TestAnchorHookCommand(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"relative dot-slash", "./tools/hooks/x.sh", "$CLAUDE_PROJECT_DIR/tools/hooks/x.sh"},
		{"relative with args", "./tools/hooks/x.sh --flag a", "$CLAUDE_PROJECT_DIR/tools/hooks/x.sh --flag a"},
		{"absolute path untouched", "/usr/local/bin/x.sh", "/usr/local/bin/x.sh"},
		{"already anchored untouched", "$CLAUDE_PROJECT_DIR/x.sh", "$CLAUDE_PROJECT_DIR/x.sh"},
		{"path-style command untouched", "make build", "make build"},
		{"bare relative untouched", "tools/hooks/x.sh", "tools/hooks/x.sh"},
		{"parent relative untouched", "../x.sh", "../x.sh"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := anchorHookCommand(tt.in); got != tt.want {
				t.Errorf("anchorHookCommand(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestClaudeGenerateHookConfig_AnchorsRelativePaths(t *testing.T) {
	c := &Claude{}
	hooks := map[string][]plugin.HookEntry{
		"before_tool": {
			{Matcher: "Bash", Command: "./tools/hooks/guard.sh"},
		},
		"on_stop": {
			{Command: "make check"},
		},
	}

	result, err := c.GenerateHookConfig(hooks)
	if err != nil {
		t.Fatal(err)
	}
	data := result[filepath.Join(".claude", "hooks", "hooks.json")]

	var settings struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if got := settings.Hooks["PreToolUse"][0].Hooks[0].Command; got != "$CLAUDE_PROJECT_DIR/tools/hooks/guard.sh" {
		t.Errorf("relative command not anchored: got %q", got)
	}
	if got := settings.Hooks["Stop"][0].Hooks[0].Command; got != "make check" {
		t.Errorf("path-style command should be untouched: got %q", got)
	}
}

func TestClaudeGenerateMCPConfig_NilServers(t *testing.T) {
	c := &Claude{}
	result, err := c.GenerateMCPConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Error("expected nil for nil servers")
	}
}

func TestClaudeGenerateMCPConfig_EmptyServers(t *testing.T) {
	c := &Claude{}
	result, err := c.GenerateMCPConfig(map[string]plugin.MCPServer{})
	if err != nil {
		t.Fatal(err)
	}
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

	result, err := c.GenerateMCPConfig(servers)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	data, ok := result[filepath.Join(".claude", ".mcp.json")]
	if !ok {
		t.Fatal("expected .claude/.mcp.json key")
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
		"before_tool":      {{Command: "cmd1"}},
		"after_tool":       {{Command: "cmd2"}},
		"before_prompt":    {{Command: "cmd3"}},
		"on_stop":          {{Command: "cmd4"}},
		"on_session_start": {{Command: "cmd5"}},
	}

	result, err := c.GenerateHookConfig(hooks)
	if err != nil {
		t.Fatal(err)
	}
	data := result[filepath.Join(".claude", "hooks", "hooks.json")]

	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}

	hooksObj := settings["hooks"].(map[string]any)
	expectedEvents := []string{"PreToolUse", "PostToolUse", "UserPromptSubmit", "Stop", "SessionStart"}
	for _, event := range expectedEvents {
		if _, ok := hooksObj[event]; !ok {
			t.Errorf("missing event %s", event)
		}
	}
}

func TestClaudeApplyRuntimeInstructions(t *testing.T) {
	c := &Claude{}
	args, err := c.ApplyRuntimeInstructions(t.TempDir(), "PR #22 in eyelock/assistants")
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 2 {
		t.Fatalf("args = %v, want 2 elements", args)
	}
	if args[0] != "--append-system-prompt" {
		t.Errorf("args[0] = %q, want --append-system-prompt", args[0])
	}
	if args[1] != "PR #22 in eyelock/assistants" {
		t.Errorf("args[1] = %q, want PR #22 in eyelock/assistants", args[1])
	}
}
