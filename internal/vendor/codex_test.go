package vendor

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

func TestCodexGenerateHookConfig_NilHooks(t *testing.T) {
	c := &Codex{}
	result, err := c.GenerateHookConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Error("expected nil for nil hooks")
	}
}

func TestCodexGenerateHookConfig_ThreeLevelFormat(t *testing.T) {
	c := &Codex{}
	hooks := map[string][]plugin.HookEntry{
		"before_tool": {
			{Matcher: "Bash", Command: "echo before bash"},
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

	data, ok := result[filepath.Join(".codex", "hooks.json")]
	if !ok {
		t.Fatal("expected .codex/hooks.json key")
	}

	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	hooksObj, ok := config["hooks"].(map[string]any)
	if !ok {
		t.Fatal("expected hooks object")
	}

	// Check PreToolUse (three-level: matcher group → hooks array)
	preToolUse, ok := hooksObj["PreToolUse"].([]any)
	if !ok {
		t.Fatal("expected PreToolUse array")
	}
	// Two matcher groups: "Bash" and "" (no matcher)
	if len(preToolUse) != 2 {
		t.Fatalf("PreToolUse groups = %d, want 2", len(preToolUse))
	}

	// First group should have matcher "Bash"
	group0 := preToolUse[0].(map[string]any)
	if group0["matcher"] != "Bash" {
		t.Errorf("matcher = %v, want Bash", group0["matcher"])
	}

	// Should have nested "hooks" array with type: "command"
	innerHooks0, ok := group0["hooks"].([]any)
	if !ok {
		t.Fatal("expected nested hooks array in first group")
	}
	if len(innerHooks0) != 1 {
		t.Fatalf("inner hooks = %d, want 1", len(innerHooks0))
	}
	inner0 := innerHooks0[0].(map[string]any)
	if inner0["type"] != "command" {
		t.Errorf("type = %v, want command", inner0["type"])
	}
	if inner0["command"] != "echo before bash" {
		t.Errorf("command = %v, want 'echo before bash'", inner0["command"])
	}

	// Second group should have no matcher
	group1 := preToolUse[1].(map[string]any)
	if _, hasMatcher := group1["matcher"]; hasMatcher {
		t.Error("second group should not have matcher (empty omitted)")
	}

	// Check Stop
	stop, ok := hooksObj["Stop"].([]any)
	if !ok {
		t.Fatal("expected Stop array")
	}
	if len(stop) != 1 {
		t.Fatalf("Stop groups = %d, want 1", len(stop))
	}
	stopGroup := stop[0].(map[string]any)
	stopInner, ok := stopGroup["hooks"].([]any)
	if !ok {
		t.Fatal("expected nested hooks array in Stop group")
	}
	if len(stopInner) != 1 {
		t.Fatalf("Stop inner hooks = %d, want 1", len(stopInner))
	}
}

func TestCodexGenerateMCPConfig_NilServers(t *testing.T) {
	c := &Codex{}
	result, err := c.GenerateMCPConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Error("expected nil for nil servers")
	}
}

func TestCodexGenerateMCPConfig_StdioServer(t *testing.T) {
	c := &Codex{}
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
}

func TestCodexGenerateMCPConfig_HTTPServer(t *testing.T) {
	c := &Codex{}
	servers := map[string]plugin.MCPServer{
		"api": {
			URL: "https://api.example.com/mcp",
		},
	}

	result, err := c.GenerateMCPConfig(servers)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	data := result[".mcp.json"]
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	mcpServers := config["mcpServers"].(map[string]any)
	api := mcpServers["api"].(map[string]any)
	if api["url"] != "https://api.example.com/mcp" {
		t.Errorf("url = %v, want https://api.example.com/mcp", api["url"])
	}
}

func TestCodexGenerateMCPConfig_WithHeaders(t *testing.T) {
	c := &Codex{}
	servers := map[string]plugin.MCPServer{
		"api": {
			URL:     "https://api.example.com/mcp",
			Headers: map[string]string{"Authorization": "Bearer ${API_KEY}"},
		},
	}

	result, err := c.GenerateMCPConfig(servers)
	if err != nil {
		t.Fatal(err)
	}
	data := result[".mcp.json"]
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	mcpServers := config["mcpServers"].(map[string]any)
	api := mcpServers["api"].(map[string]any)
	headers := api["headers"].(map[string]any)
	if headers["Authorization"] != "Bearer ${API_KEY}" {
		t.Errorf("Authorization = %v", headers["Authorization"])
	}
}

func TestCodexGenerateHookConfig_EventTranslation(t *testing.T) {
	c := &Codex{}
	hooks := map[string][]plugin.HookEntry{
		"before_tool":   {{Command: "cmd1"}},
		"after_tool":    {{Command: "cmd2"}},
		"before_prompt": {{Command: "cmd3"}},
		"on_stop":       {{Command: "cmd4"}},
	}

	result, err := c.GenerateHookConfig(hooks)
	if err != nil {
		t.Fatal(err)
	}
	data := result[filepath.Join(".codex", "hooks.json")]

	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}

	hooksObj := config["hooks"].(map[string]any)
	expectedEvents := []string{"PreToolUse", "PostToolUse", "UserPromptSubmit", "Stop"}
	for _, event := range expectedEvents {
		if _, ok := hooksObj[event]; !ok {
			t.Errorf("missing event %s", event)
		}
	}
}
