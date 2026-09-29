package harness

import (
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

func TestAddMCP_TypeAndCwd(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddMCP(dir, "legacy", MCPAddOptions{Type: plugin.MCPTypeSSE, URL: "https://x/sse"}); err != nil {
		t.Fatal(err)
	}
	if err := AddMCP(dir, "local", MCPAddOptions{Command: "./bin/srv", Cwd: "./data"}); err != nil {
		t.Fatal(err)
	}
	h, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s := h.MCPServers["legacy"]; s.Type != plugin.MCPTypeSSE || s.Transport() != plugin.MCPTypeSSE {
		t.Errorf("legacy = %+v", s)
	}
	if s := h.MCPServers["local"]; s.Cwd != "./data" || s.Type != "" {
		t.Errorf("local = %+v, want cwd kept and no type written when not given", s)
	}
}

func TestAddMCP_TypeDisagreesWithFields(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	err := AddMCP(dir, "bad", MCPAddOptions{Type: plugin.MCPTypeStdio, URL: "https://x"})
	if err == nil || !strings.Contains(err.Error(), "type stdio requires command") {
		t.Errorf("err = %v", err)
	}
	err = AddMCP(dir, "bad", MCPAddOptions{Type: "local", Command: "x"})
	if err == nil || !strings.Contains(err.Error(), "unknown type") {
		t.Errorf("err = %v", err)
	}
}

func TestUpdateMCP_TypeAndCwd(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddMCP(dir, "s", MCPAddOptions{URL: "https://x/mcp"}); err != nil {
		t.Fatal(err)
	}
	sse := plugin.MCPTypeSSE
	if err := UpdateMCP(dir, "s", MCPUpdateOptions{Type: &sse}); err != nil {
		t.Fatal(err)
	}
	h, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if h.MCPServers["s"].Type != sse {
		t.Errorf("type = %q, want sse", h.MCPServers["s"].Type)
	}

	// A type that no longer matches the fields is refused after update.
	stdio := plugin.MCPTypeStdio
	if err := UpdateMCP(dir, "s", MCPUpdateOptions{Type: &stdio}); err == nil || !strings.Contains(err.Error(), "after update") {
		t.Errorf("err = %v, want post-update validation failure", err)
	}

	cwd := "./work"
	empty := ""
	if err := AddMCP(dir, "l", MCPAddOptions{Command: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := UpdateMCP(dir, "l", MCPUpdateOptions{Cwd: &cwd}); err != nil {
		t.Fatal(err)
	}
	if h, _ = LoadDir(dir); h.MCPServers["l"].Cwd != cwd {
		t.Errorf("cwd = %q", h.MCPServers["l"].Cwd)
	}
	if err := UpdateMCP(dir, "l", MCPUpdateOptions{Cwd: &empty}); err != nil {
		t.Fatal(err)
	}
	if h, _ = LoadDir(dir); h.MCPServers["l"].Cwd != "" {
		t.Errorf("cwd = %q, want cleared by --cwd \"\"", h.MCPServers["l"].Cwd)
	}
}

func TestProfileMCP_TypeAndCwd(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddProfile(dir, "p"); err != nil {
		t.Fatal(err)
	}
	if err := AddProfileMCP(dir, "p", "s", ProfileMCPAddOptions{Type: plugin.MCPTypeSSE, URL: "https://x/sse"}); err != nil {
		t.Fatal(err)
	}
	if err := AddProfileMCP(dir, "p", "bad", ProfileMCPAddOptions{Type: plugin.MCPTypeSSE, Command: "x"}); err == nil {
		t.Error("expected type/field disagreement to be refused")
	}
	stdio := plugin.MCPTypeStdio
	if err := UpdateProfileMCP(dir, "p", "s", MCPUpdateOptions{Type: &stdio}); err == nil || !strings.Contains(err.Error(), "after update") {
		t.Errorf("err = %v", err)
	}
	cwd := "${PLUGIN_DATA}/cache"
	cmd := "x"
	url := ""
	if err := UpdateProfileMCP(dir, "p", "s", MCPUpdateOptions{Type: &stdio, Command: &cmd, URL: &url, Cwd: &cwd}); err != nil {
		t.Fatal(err)
	}
	h, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := h.Profiles["p"].MCPServers["s"]
	if s == nil || s.Type != stdio || s.Cwd != cwd || s.URL != "" {
		t.Errorf("profile server = %+v", s)
	}
}
