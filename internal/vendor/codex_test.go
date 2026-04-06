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

func TestCodexGenerateHookConfig_TwoLevelFormat(t *testing.T) {
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

	// Check PreToolUse (two-level: matcher and command at same level)
	preToolUse, ok := hooksObj["PreToolUse"].([]any)
	if !ok {
		t.Fatal("expected PreToolUse array")
	}
	if len(preToolUse) != 2 {
		t.Fatalf("PreToolUse entries = %d, want 2", len(preToolUse))
	}

	// First entry should have matcher
	entry0 := preToolUse[0].(map[string]any)
	if entry0["matcher"] != "Bash" {
		t.Errorf("matcher = %v, want Bash", entry0["matcher"])
	}
	if entry0["command"] != "echo before bash" {
		t.Errorf("command = %v, want 'echo before bash'", entry0["command"])
	}

	// Second entry should have no matcher
	entry1 := preToolUse[1].(map[string]any)
	if _, hasMatcher := entry1["matcher"]; hasMatcher {
		t.Error("second entry should not have matcher (empty omitted)")
	}

	// Should NOT have nested "hooks" array (that's Claude's format)
	if _, hasNestedHooks := entry0["hooks"]; hasNestedHooks {
		t.Error("Codex format should not have nested 'hooks' array")
	}
	// Should NOT have "type" field (that's Claude's format)
	if _, hasType := entry0["type"]; hasType {
		t.Error("Codex format should not have 'type' field")
	}

	// Check Stop
	stop, ok := hooksObj["Stop"].([]any)
	if !ok {
		t.Fatal("expected Stop array")
	}
	if len(stop) != 1 {
		t.Fatalf("Stop entries = %d, want 1", len(stop))
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
