package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeBackend_Name(t *testing.T) {
	b := &ClaudeBackend{}
	if b.Name() != "claude" {
		t.Errorf("expected 'claude', got %q", b.Name())
	}
}

func TestBuildClaudeStreamArgs_Minimal(t *testing.T) {
	args := buildClaudeStreamArgs(StartOptions{})
	required := []string{"--input-format", "stream-json", "--output-format", "stream-json", "--print", "--verbose"}
	for _, r := range required {
		found := false
		for _, a := range args {
			if a == r {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("required arg %q not found in %v", r, args)
		}
	}
}

func TestBuildClaudeStreamArgs_Model(t *testing.T) {
	args := buildClaudeStreamArgs(StartOptions{Model: "claude-opus-4-7"})
	found := false
	for i, a := range args {
		if a == "--model" && i+1 < len(args) && args[i+1] == "claude-opus-4-7" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("--model flag not found in %v", args)
	}
}

func TestBuildClaudeStreamArgs_NoModelWhenEmpty(t *testing.T) {
	args := buildClaudeStreamArgs(StartOptions{})
	for _, a := range args {
		if a == "--model" {
			t.Error("--model should not appear when Model is empty")
		}
	}
}

func TestBuildClaudeStreamArgs_ConfigPath(t *testing.T) {
	// ConfigPath with no readable CLAUDE.md — should still add --plugin-dir and a read rule, never --add-dir (ynh #898).
	args := buildClaudeStreamArgs(StartOptions{ConfigPath: "/nonexistent/path"})
	hasPluginDir := false
	hasAddDir := false
	hasReadRule := false
	for _, a := range args {
		if a == "--plugin-dir" {
			hasPluginDir = true
		}
		if a == "--add-dir" {
			hasAddDir = true
		}
		if a == "--allowedTools=Read(//nonexistent/path/**)" {
			hasReadRule = true
		}
	}
	if !hasPluginDir {
		t.Error("expected --plugin-dir with ConfigPath set")
	}
	if hasAddDir {
		t.Error("--add-dir would make Claude Code ask to trust the run dir")
	}
	if !hasReadRule {
		t.Error("expected a read rule for ConfigPath")
	}
}

func TestBuildClaudeStreamArgs_Isolation(t *testing.T) {
	withServers := t.TempDir()
	if err := os.MkdirAll(filepath.Join(withServers, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	mcpPath := filepath.Join(withServers, ".claude", ".mcp.json")
	if err := os.WriteFile(mcpPath, []byte(`{"mcpServers":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	noServers := t.TempDir()

	tests := []struct {
		name       string
		dir        string
		isolated   bool
		wantStrict bool
		wantConfig string
	}{
		{"off", withServers, false, false, ""},
		{"on with servers", withServers, true, true, "--mcp-config=" + mcpPath},
		{"on without servers", noServers, true, true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := buildClaudeStreamArgs(StartOptions{ConfigPath: tt.dir, IsolatedMCP: tt.isolated})
			var strict, config string
			for _, a := range args {
				if a == "--strict-mcp-config" {
					strict = a
				}
				if strings.HasPrefix(a, "--mcp-config") {
					config = a
				}
			}
			if (strict != "") != tt.wantStrict {
				t.Errorf("--strict-mcp-config present = %v, want %v in %v", strict != "", tt.wantStrict, args)
			}
			if config != tt.wantConfig {
				t.Errorf("mcp-config = %q, want %q", config, tt.wantConfig)
			}
		})
	}
}

// A worker of a trusted harness has its own MCP servers' tools allowed, as a
// one-shot ynh run does (ynh #1006 T7), and nothing else: not ynh's own servers.
func TestBuildClaudeStreamArgs_OwnMCPAllowRule(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	mcp := `{"mcpServers":{"probe":{},"ynh-delegates":{},"ynh-parent":{}}}`
	if err := os.WriteFile(filepath.Join(dir, ".claude", ".mcp.json"), []byte(mcp), 0o644); err != nil {
		t.Fatal(err)
	}
	has := func(args []string, want string) bool {
		for _, a := range args {
			if a == want {
				return true
			}
		}
		return false
	}
	const rule = "--allowedTools=mcp__plugin_seat_probe"
	base := StartOptions{ConfigPath: dir, HarnessName: "seat"}

	trusted := base
	trusted.TrustedHarness = true
	if !has(buildClaudeStreamArgs(trusted), rule) {
		t.Errorf("trusted harness: no %s in %v", rule, buildClaudeStreamArgs(trusted))
	}
	if has(buildClaudeStreamArgs(base), rule) {
		t.Error("an untrusted harness had its MCP tools allowed")
	}
	manual := trusted
	manual.MCPManualApproval = true
	if has(buildClaudeStreamArgs(manual), rule) {
		t.Error("mcp_manual_approval did not withhold the rule")
	}
}
