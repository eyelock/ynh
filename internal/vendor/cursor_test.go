package vendor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

func TestCursorGenerateHookConfig_NilHooks(t *testing.T) {
	c := &Cursor{}
	result, err := c.GenerateHookConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Error("expected nil for nil hooks")
	}
}

func TestCursorGenerateHookConfig_FlatFormat(t *testing.T) {
	c := &Cursor{}
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

	data, ok := result[filepath.Join(".cursor", "hooks.json")]
	if !ok {
		t.Fatal("expected .cursor/hooks.json key")
	}

	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Check version field
	version, ok := config["version"].(float64)
	if !ok || version != 1 {
		t.Errorf("version = %v, want 1", config["version"])
	}

	hooksObj, ok := config["hooks"].(map[string]any)
	if !ok {
		t.Fatal("expected hooks object")
	}

	// Check beforeShellExecution (flat, no nesting, matcher ignored)
	beforeShell, ok := hooksObj["beforeShellExecution"].([]any)
	if !ok {
		t.Fatal("expected beforeShellExecution array")
	}
	if len(beforeShell) != 2 {
		t.Fatalf("beforeShellExecution entries = %d, want 2", len(beforeShell))
	}

	// Verify matcher field is NOT present in output
	entry0 := beforeShell[0].(map[string]any)
	if _, hasMatcher := entry0["matcher"]; hasMatcher {
		t.Error("Cursor hooks should not include matcher field")
	}
	if entry0["command"] != "echo before bash" {
		t.Errorf("command = %v, want 'echo before bash'", entry0["command"])
	}

	// Check stop
	stop, ok := hooksObj["stop"].([]any)
	if !ok {
		t.Fatal("expected stop array")
	}
	if len(stop) != 1 {
		t.Fatalf("stop entries = %d, want 1", len(stop))
	}
}

func TestCursorGenerateMCPConfig_NilServers(t *testing.T) {
	c := &Cursor{}
	result, err := c.GenerateMCPConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Error("expected nil for nil servers")
	}
}

func TestCursorGenerateMCPConfig_Format(t *testing.T) {
	c := &Cursor{}
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

	data, ok := result[filepath.Join(".cursor", "mcp.json")]
	if !ok {
		t.Fatal("expected .cursor/mcp.json key")
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

func TestCursorGenerateHookConfig_EventTranslation(t *testing.T) {
	c := &Cursor{}
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
	data := result[filepath.Join(".cursor", "hooks.json")]

	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}

	hooksObj := config["hooks"].(map[string]any)
	expectedEvents := []string{"beforeShellExecution", "afterFileEdit", "beforeSubmitPrompt", "stop"}
	for _, event := range expectedEvents {
		if _, ok := hooksObj[event]; !ok {
			t.Errorf("missing event %s", event)
		}
	}
}

func TestCursorApplyRuntimeInstructions(t *testing.T) {
	runDir := t.TempDir()
	existing := "harness instructions"
	if err := os.WriteFile(filepath.Join(runDir, ".cursorrules"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	c := &Cursor{}
	args, err := c.ApplyRuntimeInstructions(runDir, "PR #22 in eyelock/assistants")
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 0 {
		t.Errorf("args = %v, want empty (Cursor uses file injection)", args)
	}

	data, err := os.ReadFile(filepath.Join(runDir, ".cursorrules"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.HasPrefix(content, existing) {
		t.Errorf(".cursorrules does not start with original content")
	}
	if !strings.Contains(content, "PR #22 in eyelock/assistants") {
		t.Errorf(".cursorrules missing injected instructions: %q", content)
	}
}

func TestCursorApplyRuntimeInstructions_NoExistingFile(t *testing.T) {
	runDir := t.TempDir()

	c := &Cursor{}
	args, err := c.ApplyRuntimeInstructions(runDir, "PR #22")
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 0 {
		t.Errorf("args = %v, want empty", args)
	}

	data, err := os.ReadFile(filepath.Join(runDir, ".cursorrules"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "PR #22") {
		t.Errorf(".cursorrules missing injected instructions")
	}
}
