package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

func writeMCPTestHarness(t *testing.T, dir, name string) {
	t.Helper()
	hj := &plugin.HarnessJSON{Name: name, Version: "0.1.0"}
	if err := plugin.SavePluginJSON(dir, hj); err != nil {
		t.Fatal(err)
	}
}

func loadTestMCPServers(t *testing.T, dir string) map[string]plugin.MCPServer {
	t.Helper()
	hj, err := plugin.LoadPluginJSON(dir)
	if err != nil {
		t.Fatalf("LoadPluginJSON: %v", err)
	}
	return hj.MCPServers
}

func TestCmdMCP_NoArgs(t *testing.T) {
	err := cmdMCP([]string{})
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Errorf("expected usage error, got: %v", err)
	}
}

func TestCmdMCP_UnknownSubcommand(t *testing.T) {
	err := cmdMCP([]string{"bogus"})
	if err == nil || !strings.Contains(err.Error(), "unknown mcp subcommand") {
		t.Errorf("expected unknown subcommand error, got: %v", err)
	}
}

func TestCmdMCPAdd_Command(t *testing.T) {
	dir := t.TempDir()
	writeMCPTestHarness(t, dir, "h")

	var buf bytes.Buffer
	if err := cmdMCPTo([]string{
		"add", dir, "github",
		"--command", "gh", "--arg", "mcp", "--env", "TOK=abc",
	}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	srvs := loadTestMCPServers(t, dir)
	srv, ok := srvs["github"]
	if !ok || srv.Command != "gh" || len(srv.Args) != 1 || srv.Env["TOK"] != "abc" {
		t.Errorf("expected gh mcp server, got %+v", srv)
	}
}

func TestCmdMCPAdd_URL(t *testing.T) {
	dir := t.TempDir()
	writeMCPTestHarness(t, dir, "h")

	var buf bytes.Buffer
	if err := cmdMCPTo([]string{
		"add", dir, "api",
		"--url", "https://example.com", "--header", "Authorization=Bearer xyz",
	}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	srv := loadTestMCPServers(t, dir)["api"]
	if srv.URL != "https://example.com" || srv.Headers["Authorization"] != "Bearer xyz" {
		t.Errorf("expected url-based server, got %+v", srv)
	}
}

func TestCmdMCPAdd_BothCommandAndURL(t *testing.T) {
	dir := t.TempDir()
	writeMCPTestHarness(t, dir, "h")

	var buf bytes.Buffer
	err := cmdMCPTo([]string{
		"add", dir, "x", "--command", "c", "--url", "u",
	}, &buf)
	if err == nil || !strings.Contains(err.Error(), "cannot have both") {
		t.Errorf("expected both-error, got: %v", err)
	}
}

func TestCmdMCPAdd_Neither(t *testing.T) {
	dir := t.TempDir()
	writeMCPTestHarness(t, dir, "h")

	var buf bytes.Buffer
	err := cmdMCPTo([]string{"add", dir, "x"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "requires --command or --url") {
		t.Errorf("expected requires-flag error, got: %v", err)
	}
}

func TestCmdMCPAdd_NoNullSupport(t *testing.T) {
	// Harness-level (top-level) entries can't be null — there's no
	// inheritance source to suppress, so --null is only meaningful with
	// --profile. Verify the top-level rejection still fires under the
	// collapsed surface.
	dir := t.TempDir()
	writeMCPTestHarness(t, dir, "h")

	var buf bytes.Buffer
	err := cmdMCPTo([]string{"add", dir, "x", "--null"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "--null is only valid with --profile") {
		t.Errorf("expected --null-requires-profile error, got: %v", err)
	}
}

func TestCmdMCPAdd_Duplicate(t *testing.T) {
	dir := t.TempDir()
	writeMCPTestHarness(t, dir, "h")

	var buf bytes.Buffer
	_ = cmdMCPTo([]string{"add", dir, "x", "--command", "c"}, &buf)
	err := cmdMCPTo([]string{"add", dir, "x", "--command", "c2"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "already present") {
		t.Errorf("expected duplicate error, got: %v", err)
	}
}

func TestCmdMCPRemove_Basic(t *testing.T) {
	dir := t.TempDir()
	writeMCPTestHarness(t, dir, "h")

	var buf bytes.Buffer
	_ = cmdMCPTo([]string{"add", dir, "x", "--command", "c"}, &buf)

	if err := cmdMCPTo([]string{"remove", dir, "x"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := loadTestMCPServers(t, dir)["x"]; ok {
		t.Errorf("expected server removed")
	}
}

func TestCmdMCPRemove_NotFound(t *testing.T) {
	dir := t.TempDir()
	writeMCPTestHarness(t, dir, "h")

	var buf bytes.Buffer
	err := cmdMCPTo([]string{"remove", dir, "x"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not-found error, got: %v", err)
	}
}

func TestCmdMCPUpdate_Basic(t *testing.T) {
	dir := t.TempDir()
	writeMCPTestHarness(t, dir, "h")

	var buf bytes.Buffer
	_ = cmdMCPTo([]string{"add", dir, "x", "--command", "old"}, &buf)

	if err := cmdMCPTo([]string{"update", dir, "x", "--command", "new"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if loadTestMCPServers(t, dir)["x"].Command != "new" {
		t.Errorf("expected command=new, got %+v", loadTestMCPServers(t, dir)["x"])
	}
}

func TestCmdMCPUpdate_NoFlags(t *testing.T) {
	dir := t.TempDir()
	writeMCPTestHarness(t, dir, "h")

	var buf bytes.Buffer
	_ = cmdMCPTo([]string{"add", dir, "x", "--command", "c"}, &buf)

	err := cmdMCPTo([]string{"update", dir, "x"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "at least one") {
		t.Errorf("expected at-least-one error, got: %v", err)
	}
}

func TestCmdMCPUpdate_SwitchCommandToURL(t *testing.T) {
	// command → url switch requires clearing command and setting url, but
	// since update applies pointer fields, --command "" + --url x should
	// produce a valid url-only server.
	dir := t.TempDir()
	writeMCPTestHarness(t, dir, "h")

	var buf bytes.Buffer
	_ = cmdMCPTo([]string{"add", dir, "x", "--command", "c"}, &buf)

	if err := cmdMCPTo([]string{
		"update", dir, "x", "--command", "", "--url", "https://example",
	}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	srv := loadTestMCPServers(t, dir)["x"]
	if srv.Command != "" || srv.URL != "https://example" {
		t.Errorf("expected url-only after switch, got %+v", srv)
	}
}

func TestCmdMCPUpdate_NotFound(t *testing.T) {
	dir := t.TempDir()
	writeMCPTestHarness(t, dir, "h")

	var buf bytes.Buffer
	err := cmdMCPTo([]string{"update", dir, "x", "--command", "c"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not-found error, got: %v", err)
	}
}
