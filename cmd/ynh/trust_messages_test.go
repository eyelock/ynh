package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

const mcpOnlyHarness = `{"name":"mcponly","version":"0.1.0","default_vendor":"claude",` +
	`"mcp_servers":{"srv":{"command":"echo","args":["hi"]}}}`

// unreadableIncludeFolder is a folder harness with a remote include that is
// not in the cache and cannot be fetched.
func unreadableIncludeFolder(t *testing.T) string {
	t.Helper()
	t.Setenv("YNH_HOME", t.TempDir())
	dir := t.TempDir()
	manifest := `{"name":"unread","version":"0.1.0","includes":[{"git":"https://example.invalid/none.git"}]}`
	if err := writePluginJSONFile(dir, []byte(manifest)); err != nil {
		t.Fatal(err)
	}
	return dir
}

// Every acceptance route says "Accepted" for the act of accepting (ynh #1003).
func TestTrustMessages_AcceptSaysAccepted(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	id := installListTestHarness(t, home, "hooked", hookedHarness)
	var out bytes.Buffer
	if err := cmdTrustTo([]string{"accept", id}, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "Accepted local/hooked (accept): 1 command(s)") {
		t.Errorf("accept output = %q", out.String())
	}
	if strings.Contains(out.String(), "Recorded") {
		t.Errorf("accept still says Recorded: %q", out.String())
	}
}

// A refused acceptance says what was refused, not that it refuses to run.
func TestTrustMessages_RefusedAcceptDoesNotSayRun(t *testing.T) {
	dir := unreadableIncludeFolder(t)
	err := cmdTrustTo([]string{"accept", dir}, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("want a refusal")
	}
	if !strings.Contains(err.Error(), "refusing to accept unread") || strings.Contains(err.Error(), "refusing to run") {
		t.Errorf("err = %v", err)
	}
}

// ynh check and ynh run give the same exit class and error kind for the same
// untrusted folder: untrusted, exit 1, never not_found.
func TestTrustMessages_CheckAndRunAgreeOnUnreadableInclude(t *testing.T) {
	dir := unreadableIncludeFolder(t)

	var stdout, stderr bytes.Buffer
	cerr := cmdCheck([]string{dir, "--format", "json", "--cwd", t.TempDir()}, &stdout, &stderr)
	if cerr == nil || errors.Is(cerr, errCheckExec) || errors.Is(cerr, errCheckBlocked) {
		t.Fatalf("check err = %v, want a plain refusal (exit 1)", cerr)
	}
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(lastLine(stderr.String()), &env); err != nil {
		t.Fatalf("check output not JSON: %v\n%s", err, stderr.String())
	}
	if env.Error.Code != errCodeUntrusted {
		t.Errorf("check code = %q, want %q", env.Error.Code, errCodeUntrusted)
	}

	t.Setenv("PATH", t.TempDir())
	rerr := cmdRun([]string{dir, "-v", "claude", "--", "hi"})
	if !isUntrusted(rerr) {
		t.Fatalf("run err = %v, want untrusted", rerr)
	}
	if env.Error.Message != rerr.Error() {
		t.Errorf("messages differ:\ncheck %q\nrun   %q", env.Error.Message, rerr)
	}
}

// A legacy-digest refusal names the kinds the harness declares, not all three:
// an MCP server is never called a sensor.
func TestTrustMessages_LegacyNoteNamesTheExecutablesKind(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	id := installTrustedTestHarness(t, home, "mcponly", mcpOnlyHarness)
	installListTestHarness(t, home, "mcponly", strings.Replace(mcpOnlyHarness, `"hi"`, `"changed"`, 1))
	store, err := loadTrustStore()
	if err != nil {
		t.Fatal(err)
	}
	rec := store.Harnesses[id]
	rec.Scheme = 2
	store.Harnesses[id] = rec
	if err := saveTrustStore(store); err != nil {
		t.Fatal(err)
	}
	gerr := trustGate(id, loadInstalled(t, id), io.Discard)
	if !isUntrusted(gerr) {
		t.Fatalf("err = %v", gerr)
	}
	msg := gerr.Error()
	if !strings.Contains(msg, "scripts that\nMCP servers run") || strings.Contains(msg, "sensors") {
		t.Errorf("refusal should name only MCP servers:\n%s", msg)
	}
	if !strings.Contains(msg, "MCP server srv: echo") {
		t.Errorf("refusal should list the server by its kind:\n%s", msg)
	}
}

func TestKindsOf(t *testing.T) {
	cases := []struct {
		s, h, m int
		want    string
	}{
		{0, 0, 0, ""}, {0, 0, 1, "MCP servers"}, {1, 0, 0, "sensors"},
		{0, 1, 1, "hooks and MCP servers"}, {2, 1, 1, "hooks, MCP servers and sensors"},
	}
	for _, c := range cases {
		if got := kindsOf(c.s, c.h, c.m); got != c.want {
			t.Errorf("kindsOf(%d,%d,%d) = %q, want %q", c.s, c.h, c.m, got, c.want)
		}
	}
}

// lastLine is the final non-empty line of s: the JSON envelope, after any
// notice a folder run prints first.
func lastLine(s string) []byte {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return []byte(lines[len(lines)-1])
}

// A refusal about an include that cannot be read does not call what it
// brings "sensors": it may be a hook or an MCP server.
func TestTrustMessages_UnreadableIncludeDoesNotSaySensors(t *testing.T) {
	dir := unreadableIncludeFolder(t)
	err := cmdRun([]string{dir, "-v", "claude", "--", "hi"})
	if !isUntrusted(err) || strings.Contains(err.Error(), "sensors") || !strings.Contains(err.Error(), "what it executes cannot be read") {
		t.Fatalf("err = %v", err)
	}
}
