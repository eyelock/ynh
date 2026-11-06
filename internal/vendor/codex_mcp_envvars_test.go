package vendor

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeEnvRunDir(t *testing.T, doc string, names []string) string {
	t.Helper()
	runDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(runDir, ".mcp.json"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteCodexEnvPassthrough(runDir, names); err != nil {
		t.Fatal(err)
	}
	return runDir
}

func hasArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

// Codex gives a stdio server only a default environment, so the harness's
// env_passthrough names must be listed in env_vars or a ynm server opens the
// user's real ~/.ynm instead of the run's YNM_HOME (ynh #723).
func TestCodexMCP_ForwardsEnvPassthroughToStdioServers(t *testing.T) {
	runDir := writeEnvRunDir(t, `{"mcpServers":{
"ynm":{"command":"ynm","args":["serve"]},
"wrapped":{"command":"py","env":{"YNM_USER":"x","K":"v"}},
"remote":{"type":"http","url":"https://x/mcp"}}}`, []string{"YNM_USER", "YNM_HOME"})
	got := CodexMCP(runDir)
	if !hasArg(got.Args, `mcp_servers.ynm.env_vars=["YNM_HOME", "YNM_USER"]`) {
		t.Errorf("ynm server not given env_vars:\n%v", got.Args)
	}
	// A variable the server's own env sets is not also forwarded.
	if !hasArg(got.Args, `mcp_servers.wrapped.env_vars=["YNM_HOME"]`) {
		t.Errorf("wrapped server env_vars wrong:\n%v", got.Args)
	}
	for _, a := range got.Args {
		if strings.HasPrefix(a, "mcp_servers.remote.env_vars") {
			t.Errorf("a remote server takes no env_vars: %s", a)
		}
	}
}

func TestCodexMCP_NoEnvPassthroughNoEnvVars(t *testing.T) {
	runDir := writeEnvRunDir(t, `{"mcpServers":{"ynm":{"command":"ynm"}}}`, nil)
	for _, a := range CodexMCP(runDir).Args {
		if strings.Contains(a, "env_vars") {
			t.Errorf("unexpected %s", a)
		}
	}
}

// Every launch path takes its MCP overrides from the same place.
func TestCodexLaunchArgs_ForwardEnvPassthrough(t *testing.T) {
	runDir := writeEnvRunDir(t, `{"mcpServers":{"ynm":{"command":"ynm"}}}`, []string{"YNM_HOME"})
	want := `mcp_servers.ynm.env_vars=["YNM_HOME"]`
	resume, err := codexResumeArgs("", "go", false, nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string][]string{
		"interactive": codexInteractiveArgs(runDir, nil, io.Discard),
		"exec":        codexExecArgs(runDir, "go", nil, io.Discard),
		"resume":      withCodexRunArgs(resume, codexAgentArgsWarned(runDir, io.Discard)),
	}
	for name, args := range paths {
		if !hasArg(args, want) {
			t.Errorf("%s args lack %s:\n%v", name, want, args)
		}
	}
}
