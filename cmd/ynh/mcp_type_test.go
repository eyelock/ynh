package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

func TestCmdMCPAdd_TypeAndCwd(t *testing.T) {
	dir := t.TempDir()
	writeMCPTestHarness(t, dir, "h")
	var buf bytes.Buffer
	if err := cmdMCPTo([]string{"add", dir, "legacy", "--url", "https://x/sse", "--type", "sse"}, &buf); err != nil {
		t.Fatal(err)
	}
	if err := cmdMCPTo([]string{"add", dir, "local", "--command", "./bin/srv", "--cwd", "${PLUGIN_ROOT}"}, &buf); err != nil {
		t.Fatal(err)
	}
	srvs := loadTestMCPServers(t, dir)
	if srvs["legacy"].Type != "sse" {
		t.Errorf("legacy = %+v", srvs["legacy"])
	}
	if srvs["local"].Cwd != "${PLUGIN_ROOT}" {
		t.Errorf("local = %+v", srvs["local"])
	}
}

func TestCmdMCPAdd_TypeRejectedWhenItDisagrees(t *testing.T) {
	dir := t.TempDir()
	writeMCPTestHarness(t, dir, "h")
	err := cmdMCPTo([]string{"add", dir, "s", "--command", "x", "--type", "streamable-http"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "requires url") {
		t.Errorf("err = %v", err)
	}
	for _, flag := range []string{"--type", "--cwd"} {
		if err := cmdMCPTo([]string{"add", dir, "s", flag}, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "requires a value") {
			t.Errorf("%s without value: err = %v", flag, err)
		}
	}
}

func TestCmdMCPUpdate_TypeAndCwd(t *testing.T) {
	dir := t.TempDir()
	writeMCPTestHarness(t, dir, "h")
	if err := cmdMCPTo([]string{"add", dir, "s", "--url", "https://x/mcp"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if err := cmdMCPTo([]string{"update", dir, "s", "--type", "sse"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if srvs := loadTestMCPServers(t, dir); srvs["s"].Type != "sse" {
		t.Errorf("s = %+v", srvs["s"])
	}
	if err := cmdMCPTo([]string{"add", dir, "l", "--command", "x"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if err := cmdMCPTo([]string{"update", dir, "l", "--cwd", "./work"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if srvs := loadTestMCPServers(t, dir); srvs["l"].Cwd != "./work" {
		t.Errorf("l = %+v", srvs["l"])
	}
}

func TestCmdProfileMCP_TypeAndCwd(t *testing.T) {
	dir := t.TempDir()
	writeMCPTestHarness(t, dir, "h")
	if err := cmdProfileTo([]string{"add", dir, "p"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if err := cmdProfileTo([]string{"mcp", "add", dir, "p", "s", "--command", "x", "--type", "stdio", "--cwd", "${PLUGIN_DATA}"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if err := cmdProfileTo([]string{"mcp", "update", dir, "p", "s", "--cwd", "./other"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	hj, err := plugin.LoadPluginJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := hj.Profiles["p"].MCPServers["s"]
	if s == nil || s.Type != "stdio" || s.Cwd != "./other" {
		t.Errorf("profile server = %+v", s)
	}
}
