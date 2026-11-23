package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/vendor"
)

// A worker's codex turn must forward the harness's env_passthrough to its MCP
// servers, or a ynm server opens the user's real ~/.ynm (ynh #723).
func TestBuildCodexArgs_ForwardsEnvPassthrough(t *testing.T) {
	runDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(runDir, ".mcp.json"), []byte(`{"mcpServers":{"ynm":{"command":"ynm"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := vendor.WriteCodexEnvPassthrough(runDir, []string{"YNM_HOME"}); err != nil {
		t.Fatal(err)
	}
	args := buildCodexArgs(StartOptions{ConfigPath: runDir}, "")
	want := `mcp_servers.ynm.env_vars=["YNM_HOME"]`
	for _, a := range args {
		if a == want {
			return
		}
	}
	t.Errorf("args lack %s:\n%v", want, args)
}

func TestBuildCodexArgsHooks(t *testing.T) {
	runDir := t.TempDir()
	files, err := (&vendor.Codex{}).GenerateHookConfig(map[string][]plugin.HookEntry{
		"on_session_start": {{Command: "echo s"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for rel, data := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(runDir, rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(runDir, rel), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	has := func(args []string) bool {
		return strings.Contains(strings.Join(args, "\n"), "hooks.SessionStart=")
	}
	if !has(buildCodexArgs(StartOptions{ConfigPath: runDir, TrustedHooks: true}, "")) {
		t.Error("a trusted harness's hooks are not passed")
	}
	if has(buildCodexArgs(StartOptions{ConfigPath: runDir}, "")) {
		t.Error("an untrusted harness's hooks are passed")
	}
	if !has(buildCodexArgs(StartOptions{ConfigPath: runDir, TrustedHooks: true}, "thread-1")) {
		t.Error("a resumed turn does not pass the hooks")
	}
}
