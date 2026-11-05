package agent

import (
	"os"
	"path/filepath"
	"testing"

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
