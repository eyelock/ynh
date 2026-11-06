package agent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fakeVendorCLI puts an executable named bin on PATH that runs script.
func fakeVendorCLI(t *testing.T, bin, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, bin), []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestWithIsolationArgs_GoesBeforeThePositionalTail(t *testing.T) {
	got := withIsolationArgs([]string{"exec", "--json", "-"}, []string{"-c", "x=1"}, 1)
	if want := []string{"exec", "--json", "-c", "x=1", "-"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	got = withIsolationArgs([]string{"run", "--", "msg"}, nil, 2)
	if want := []string{"run", "--", "msg"}; !reflect.DeepEqual(got, want) {
		t.Errorf("no isolation: got %v, want %v", got, want)
	}
}

func TestCodexStart_IsolatedMCPTurnsOffTheOperatorsServers(t *testing.T) {
	fakeVendorCLI(t, "codex", `[ "$1 $2" = "mcp list" ] && echo '[{"name":"usersrv"}]'`)

	on, err := (&CodexBackend{}).Start(context.Background(), StartOptions{IsolatedMCP: true, WorktreeDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-c", "mcp_servers.usersrv.enabled=false"}
	if got := on.(*codexSession).isolation; !reflect.DeepEqual(got, want) {
		t.Errorf("isolated: isolation = %v, want %v", got, want)
	}

	off, err := (&CodexBackend{}).Start(context.Background(), StartOptions{WorktreeDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if got := off.(*codexSession).isolation; len(got) != 0 {
		t.Errorf("not isolated: isolation = %v, want none", got)
	}
}

func TestOpenCodeStart_IsolatedMCPSetsTheConfigContent(t *testing.T) {
	fakeVendorCLI(t, "opencode", `case "$*" in
*--version*) echo "opencode v2.2.3" ;;
*"debug config"*) echo '[{"type":"document","path":"/p","info":{"mcp":{"servers":{"projsrv":{"type":"local","command":["a"]}}}}}]' ;;
esac`)
	var warn bytes.Buffer

	on, err := (&OpenCodeBackend{}).Start(context.Background(), StartOptions{IsolatedMCP: true, WorktreeDir: t.TempDir(), Stderr: &warn})
	if err != nil {
		t.Fatal(err)
	}
	env := on.(*openCodeSession).isolationEnv
	if len(env) != 1 || !strings.HasPrefix(env[0], "OPENCODE_CONFIG_CONTENT=") || !strings.Contains(env[0], `"projsrv":{"command":["false"],"disabled":true,"type":"local"}`) {
		t.Errorf("isolationEnv = %v", env)
	}
	if warn.Len() != 0 {
		t.Errorf("unexpected warning: %q", warn.String())
	}

	off, err := (&OpenCodeBackend{}).Start(context.Background(), StartOptions{WorktreeDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if got := off.(*openCodeSession).isolationEnv; len(got) != 0 {
		t.Errorf("not isolated: isolationEnv = %v, want none", got)
	}
}
