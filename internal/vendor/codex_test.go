package vendor

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

func TestCodexGenerateHookConfig_NilHooks(t *testing.T) {
	c := &Codex{}
	result := c.GenerateHookConfig(nil)
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

	result := c.GenerateHookConfig(hooks)
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
	result := c.GenerateMCPConfig(nil)
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

	result := c.GenerateMCPConfig(servers)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	data, ok := result[filepath.Join(".codex", "config.toml")]
	if !ok {
		t.Fatal("expected .codex/config.toml key")
	}

	toml := string(data)
	if !strings.Contains(toml, "[mcp_servers.github]") {
		t.Error("expected [mcp_servers.github] section")
	}
	if !strings.Contains(toml, `command = "npx"`) {
		t.Error("expected command = \"npx\"")
	}
	if !strings.Contains(toml, `args = ["-y", "@modelcontextprotocol/server-github"]`) {
		t.Error("expected args array")
	}
	if !strings.Contains(toml, "[mcp_servers.github.env]") {
		t.Error("expected [mcp_servers.github.env] section")
	}
	if !strings.Contains(toml, `GITHUB_TOKEN = "${GITHUB_TOKEN}"`) {
		t.Error("expected GITHUB_TOKEN env var")
	}
}

func TestCodexGenerateMCPConfig_HTTPServer(t *testing.T) {
	c := &Codex{}
	servers := map[string]plugin.MCPServer{
		"api": {
			URL: "https://api.example.com/mcp",
		},
	}

	result := c.GenerateMCPConfig(servers)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	data := result[filepath.Join(".codex", "config.toml")]
	toml := string(data)

	if !strings.Contains(toml, "[mcp_servers.api]") {
		t.Error("expected [mcp_servers.api] section")
	}
	if !strings.Contains(toml, `url = "https://api.example.com/mcp"`) {
		t.Error("expected url value")
	}
	// Should NOT have command
	if strings.Contains(toml, "command") {
		t.Error("HTTP server should not have command")
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

	result := c.GenerateMCPConfig(servers)
	data := result[filepath.Join(".codex", "config.toml")]
	toml := string(data)

	if !strings.Contains(toml, "[mcp_servers.api.headers]") {
		t.Error("expected headers section")
	}
	if !strings.Contains(toml, `Authorization = "Bearer ${API_KEY}"`) {
		t.Error("expected Authorization header")
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

	result := c.GenerateHookConfig(hooks)
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
