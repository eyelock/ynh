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

// Every Codex turn carries the isolation found in Start: a turn's own codex
// exec process gets the -c overrides before its positional tail.
func TestCodexSession_EveryTurnCarriesTheIsolation(t *testing.T) {
	fixtures, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}
	records := codexStub(t, fixtures)
	sess, err := (&CodexBackend{}).Start(context.Background(), StartOptions{WorktreeDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sess.Close() }()
	sess.(*codexSession).isolation = []string{"-c", "mcp_servers.usersrv.enabled=false"}

	for _, msg := range []string{"first", "second"} {
		if err := sess.Send(msg); err != nil {
			t.Fatal(err)
		}
		if _, err := sess.Next(); err != nil {
			t.Fatalf("Next: %v", err)
		}
	}
	const thread = "0199a213-81c0-7800-8aa1-bbab2a035a53"
	for i, want := range [][]string{
		{"exec", "--json", "-c", "mcp_servers.usersrv.enabled=false", "-"},
		{"exec", "resume", thread, "--json", "-c", "mcp_servers.usersrv.enabled=false", "-"},
	} {
		if got := readLines(t, filepath.Join(records, "args"+string(rune('0'+i)))); !reflect.DeepEqual(got, want) {
			t.Errorf("turn %d args = %q, want %q", i+1, got, want)
		}
	}
}

// Every OpenCode turn carries the isolation found in Start: a turn's own
// opencode run process gets OPENCODE_CONFIG_CONTENT in its environment.
func TestOpenCodeSession_EveryTurnCarriesTheIsolation(t *testing.T) {
	out := readOpenCodeFixture(t, "opencode-tool-turn.jsonl")
	dir := t.TempDir()
	record := filepath.Join(dir, "config-content")
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 'opencode v2.0.27'; exit 0; fi\n" +
		"printf '%s\\n' \"${OPENCODE_CONFIG_CONTENT-unset}\" >> " + record + "\n" +
		"cat <<'OUTEOF'\n" + out + "\nOUTEOF\n"
	if err := os.WriteFile(filepath.Join(dir, "opencode"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	shadowVendorCLIs(t, dir)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("OPENCODE_CONFIG_CONTENT", "")
	_ = os.Unsetenv("OPENCODE_CONFIG_CONTENT")

	sess, err := (&OpenCodeBackend{}).Start(context.Background(), StartOptions{WorktreeDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	const content = `{"mcp":{"projsrv":{"command":["false"],"disabled":true,"type":"local"}}}`
	sess.(*openCodeSession).isolationEnv = []string{"OPENCODE_CONFIG_CONTENT=" + content}

	for _, msg := range []string{"first", "second"} {
		if err := sess.Send(msg); err != nil {
			t.Fatal(err)
		}
		if _, err := sess.Next(); err != nil {
			t.Fatalf("Next: %v", err)
		}
	}
	if got := readLines(t, record); !reflect.DeepEqual(got, []string{content, content}) {
		t.Errorf("OPENCODE_CONFIG_CONTENT per turn = %q, want %q twice", got, content)
	}
}
