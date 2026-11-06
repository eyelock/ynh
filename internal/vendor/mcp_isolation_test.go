package vendor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// stubMCPList puts empty executables named bins on PATH and makes the listing
// return out (or err) without running anything.
func stubMCPList(t *testing.T, out string, err error, bins ...string) *[][]string {
	t.Helper()
	dir := t.TempDir()
	for _, b := range bins {
		if e := os.WriteFile(filepath.Join(dir, b), []byte("#!/bin/sh\n"), 0o755); e != nil {
			t.Fatal(e)
		}
	}
	t.Setenv("PATH", dir)
	var calls [][]string
	old := mcpListRunner
	mcpListRunner = func(bin string, args []string, env []string, dir string) ([]byte, error) {
		calls = append(calls, args)
		return []byte(out), err
	}
	t.Cleanup(func() { mcpListRunner = old })
	return &calls
}

func writeIsoFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCodexIsolateMCP_TurnsOffEveryServerTheHarnessDoesNotCompose(t *testing.T) {
	calls := stubMCPList(t, `[{"name":"usersrv"},{"name":"harnesssrv"},{"name":"has.dot"}]`, nil, "codex")
	dir := t.TempDir()
	writeIsoFile(t, filepath.Join(dir, ".mcp.json"), `{"mcpServers":{"harnesssrv":{"command":"x"}}}`)

	args, warning := (&Codex{}).IsolateMCP(dir)

	want := []string{"-c", "mcp_servers.usersrv.enabled=false"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args = %v, want %v", args, want)
	}
	if !strings.Contains(warning, "has.dot") || strings.Contains(warning, "\n") {
		t.Errorf("warning = %q, want a one-line note naming has.dot", warning)
	}
	if len(*calls) != 1 || !reflect.DeepEqual((*calls)[0], []string{"mcp", "list", "--json"}) {
		t.Errorf("listing calls = %v", *calls)
	}
}

func TestCodexIsolateMCP_ListingFailsWarnsAndAddsNothing(t *testing.T) {
	stubMCPList(t, "", os.ErrInvalid, "codex")
	args, warning := (&Codex{}).IsolateMCP(t.TempDir())
	if len(args) != 0 || !strings.Contains(warning, "could not be applied on Codex") {
		t.Errorf("args = %v, warning = %q", args, warning)
	}
}

func TestCopilotIsolateMCP_DisablesListedServersAndBuiltins(t *testing.T) {
	stubMCPList(t, "banner line\n"+`{"mcpServers":{"usersrv":{},"computer-use":{},"harnesssrv":{}}}`, nil, "copilot")
	dir := t.TempDir()
	writeIsoFile(t, filepath.Join(dir, ".copilot", ".mcp.json"), `{"mcpServers":{"harnesssrv":{}}}`)

	args, warning := (&Copilot{}).IsolateMCP(dir)

	want := []string{"--disable-builtin-mcps", "--disable-mcp-server", "computer-use", "--disable-mcp-server", "usersrv"}
	if !reflect.DeepEqual(args, want) {
		t.Errorf("args = %v, want %v", args, want)
	}
	if warning != "" {
		t.Errorf("warning = %q, want none", warning)
	}
}

func TestCopilotIsolateMCP_ListingFailsFallsBackToBuiltinsOnly(t *testing.T) {
	stubMCPList(t, "", os.ErrInvalid, "copilot")
	args, warning := (&Copilot{}).IsolateMCP(t.TempDir())
	if !reflect.DeepEqual(args, []string{"--disable-builtin-mcps"}) {
		t.Errorf("args = %v", args)
	}
	if !strings.Contains(warning, "partial on Copilot") || strings.Contains(warning, "\n") {
		t.Errorf("warning = %q", warning)
	}
}

func TestOpenCodeIsolateMCPEnv_DisablesACopyOfEachOtherServer(t *testing.T) {
	stubMCPList(t, `[{"type":"document","path":"/g","info":{"mcp":{"servers":{
		"usersrv":{"type":"local","command":["a","tok-in-arg"],"environment":{"T":"***"}},
		"remsrv":{"type":"remote","url":"https://x.example/mcp?k=secret","headers":{"Authorization":"***"}},
		"harnesssrv":{"type":"local","command":["h"]}}}}},
		{"type":"directory","path":"/d"}]`, nil, "opencode")
	dir := t.TempDir()
	writeIsoFile(t, filepath.Join(dir, ".opencode", "opencode.json"), `{"mcp":{"servers":{"harnesssrv":{"type":"local","command":["h"]}}}}`)
	t.Setenv("OPENCODE_CONFIG_CONTENT", `{"model":"x/y"}`)

	env, warning := (&OpenCode{}).IsolateMCPEnv(dir)
	if warning != "" || len(env) != 1 || !strings.HasPrefix(env[0], "OPENCODE_CONFIG_CONTENT=") {
		t.Fatalf("env = %v, warning = %q", env, warning)
	}
	var doc struct {
		Model string `json:"model"`
		MCP   struct {
			Servers map[string]map[string]any `json:"servers"`
		} `json:"mcp"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(env[0], "OPENCODE_CONFIG_CONTENT=")), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Model != "x/y" {
		t.Errorf("the caller's own OPENCODE_CONFIG_CONTENT was not kept: %+v", doc)
	}
	if _, ok := doc.MCP.Servers["harnesssrv"]; ok {
		t.Error("the harness's own server must not be touched")
	}
	u := doc.MCP.Servers["usersrv"]
	if u["disabled"] != true || u["type"] != "local" || u["command"] == nil {
		t.Errorf("usersrv = %v, want a local placeholder with disabled:true", u)
	}
	r := doc.MCP.Servers["remsrv"]
	if r["disabled"] != true || r["type"] != "remote" || r["url"] != "https://disabled.invalid" {
		t.Errorf("remsrv = %v, want a remote placeholder with disabled:true", r)
	}
	// Nothing of the user's own servers, credentials included, is copied.
	for _, leak := range []string{"tok-in-arg", "secret", "Authorization", "environment", "headers"} {
		if strings.Contains(env[0], leak) {
			t.Errorf("OPENCODE_CONFIG_CONTENT carries %q: %s", leak, env[0])
		}
	}
}

func TestOpenCodeIsolateMCPEnv_BadExistingContentWarns(t *testing.T) {
	stubMCPList(t, `[]`, nil, "opencode")
	t.Setenv("OPENCODE_CONFIG_CONTENT", "not json")
	env, warning := (&OpenCode{}).IsolateMCPEnv(t.TempDir())
	if len(env) != 0 || !strings.Contains(warning, "could not be applied on OpenCode") {
		t.Errorf("env = %v, warning = %q", env, warning)
	}
}

func TestCursorIsolateMCP_IsNotSupportedAndSaysSo(t *testing.T) {
	args, warning := (&Cursor{}).IsolateMCP(t.TempDir())
	if len(args) != 0 || !strings.Contains(warning, "not supported") || strings.Contains(warning, "\n") {
		t.Errorf("args = %v, warning = %q", args, warning)
	}
}
