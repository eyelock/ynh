package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/parentmcp"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/vendor"
)

func TestParentMCPEntryIsOnlyForADelegateSession(t *testing.T) {
	t.Setenv(delegationRecordEnv, "")
	if e, err := parentMCPEntry(); e != nil || err != nil {
		t.Fatalf("a run that is not a delegate's got %v, %v", e, err)
	}
}

func TestParentMCPEntryNamesTheRecordAndGoesInEveryVendorsConfig(t *testing.T) {
	t.Setenv(delegationRecordEnv, "/home/delegations/p/f/dlg-1.json")
	t.Setenv(parentAskMaxWaitEnv, "")
	e, err := parentMCPEntry()
	if err != nil || e == nil {
		t.Fatalf("entry = %v, %v", e, err)
	}
	if e.Transport() != plugin.MCPTypeStdio || strings.Join(e.Args, " ") != "parent-mcp --record /home/delegations/p/f/dlg-1.json" {
		t.Errorf("entry = %+v", e)
	}
	if e.Env["YNH_HOME"] != config.HomeDir() {
		t.Errorf("YNH_HOME = %q: a vendor may hand its MCP servers a trimmed environment", e.Env["YNH_HOME"])
	}
	for _, name := range []string{"claude", "codex", "copilot", "cursor", "opencode"} {
		a, err := vendor.Get(name)
		if err != nil {
			t.Fatal(err)
		}
		files, err := a.GenerateMCPConfig(map[string]plugin.MCPServer{parentmcp.ServerName: *e})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var all strings.Builder
		for _, c := range files {
			all.Write(c)
		}
		for _, want := range []string{parentmcp.ServerName, "parent-mcp", "dlg-1.json"} {
			if !strings.Contains(all.String(), want) {
				t.Errorf("%s: the MCP config lacks %q", name, want)
			}
		}
	}
	t.Setenv(parentAskMaxWaitEnv, "20")
	e, _ = parentMCPEntry()
	if got := strings.Join(e.Args, " "); !strings.HasSuffix(got, "--max-wait 20") {
		t.Errorf("args = %s", got)
	}
	t.Setenv(parentAskMaxWaitEnv, "soon")
	if _, err := parentMCPEntry(); err == nil {
		t.Error("a wait that is not a number should be refused")
	}
}

func TestParentMCPRefusesBadArguments(t *testing.T) {
	for _, args := range [][]string{{}, {"--record"}, {"--record", "x", "--max-wait", "0"}, {"--nope", "x"}} {
		if err := cmdParentMCP(args); err == nil {
			t.Errorf("%v: want an error", args)
		}
	}
}

func TestClaudeOwnMCPAllowRule(t *testing.T) {
	if got := claudeOwnMCPAllowRule("h", nil); got != "" {
		t.Errorf("no servers: got %q, want none", got)
	}
	got := claudeOwnMCPAllowRule("my.h", []string{"probe", "b c"})
	want := "--allowedTools=mcp__plugin_my_h_probe,mcp__plugin_my_h_b_c"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMCPServerNamesReadsTheRunDirsFile(t *testing.T) {
	dir := t.TempDir()
	if got := mcpServerNames(dir); got != nil {
		t.Errorf("no file: got %v", got)
	}
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(`{"mcpServers":{"b":{},"a":{}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := mcpServerNames(dir); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("got %v, want [a b]", got)
	}
}

func TestClaudeDelegateMCPAllowRuleOnlyForADelegatesOneShotSession(t *testing.T) {
	servers := []string{"probe"}
	want := "--allowedTools=mcp__plugin_h_probe"
	if got := claudeDelegateMCPAllowRule("claude", true, true, false, "h", servers); got != want {
		t.Errorf("delegate one-shot: got %q, want %q", got, want)
	}
	for name, got := range map[string]string{
		"a harness run directly (no delegation record)": claudeDelegateMCPAllowRule("claude", false, true, false, "h", servers),
		"an interactive delegate run":                   claudeDelegateMCPAllowRule("claude", true, false, false, "h", servers),
		"mcp_manual_approval":                           claudeDelegateMCPAllowRule("claude", true, true, true, "h", servers),
		"another vendor":                                claudeDelegateMCPAllowRule("codex", true, true, false, "h", servers),
	} {
		if got != "" {
			t.Errorf("%s: got %q, want no rule", name, got)
		}
	}
}
