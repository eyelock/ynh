package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/inboxmcp"
	"github.com/eyelock/ynh/internal/plugin"
)

// cmdInboxMCP is `ynh inbox-mcp`, the MCP server a vendor starts over stdio
// for an interactive session, so a task delegated to its harness can be taken
// there. It is not for people to run:
//
//	ynh inbox-mcp --harness <run name> [--session <session id>]
//
// The session id is the run's own, which ynh run puts in YNH_SESSION_ID for the
// vendor and so for this server: the entry stays the same for every run of the
// harness, so a run's snapshot is still shared.
func cmdInboxMCP(args []string) error {
	var harness, session string
	for i := 0; i < len(args); i += 2 {
		if i+1 >= len(args) {
			return fmt.Errorf("%s requires a value\nusage: ynh inbox-mcp --harness <run name> --session <session id>", args[i])
		}
		switch args[i] {
		case "--harness":
			harness = args[i+1]
		case "--session":
			session = args[i+1]
		default:
			return fmt.Errorf("unknown flag %s\nusage: ynh inbox-mcp --harness <run name> --session <session id>", args[i])
		}
	}
	if session == "" {
		session = os.Getenv(inboxSessionEnv)
	}
	if harness == "" || session == "" {
		return fmt.Errorf("usage: ynh inbox-mcp --harness <run name> [--session <session id>] (the session id is read from %s when not given)", inboxSessionEnv)
	}
	cwd, _ := os.Getwd()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return inboxmcp.New(inboxmcp.Config{Harness: harness, Session: session, Cwd: cwd, Version: config.Version}).Serve(ctx, os.Stdin, os.Stdout)
}

// inboxSessionEnv carries the run's session id to the ynh-inbox server.
const inboxSessionEnv = "YNH_SESSION_ID"

// inboxVendors are the vendors whose interactive session can take a task:
// the launch-time allow rule for the ynh-inbox server is honoured with no
// prompt on each (spike ynh #907). OpenCode needs none but cannot be told a
// task is waiting, and Cursor has not been run, so both are routed headless.
var inboxVendors = map[string]bool{"claude": true, "codex": true, "copilot": true}

// inboxSession reports whether an interactive session of this vendor and
// harness can take tasks: the harness is installed (a delegation names an
// installed harness, so a run from a folder or an inline manifest has no
// mailbox anything can address), the vendor is supported and the harness has
// not asked for every MCP tool to be approved by hand (mcp_manual_approval),
// which would put the allow rule out of reach.
func inboxSession(vendorName string, interactive, installed, manualApproval bool) bool {
	return interactive && installed && !manualApproval && inboxVendors[vendorName]
}

// inboxMCPEntry is the server `ynh run` adds to an interactive session's MCP
// config. The home goes in the entry because a vendor may give its MCP
// servers a trimmed environment (Codex does).
func inboxMCPEntry(runName string) (*plugin.MCPServer, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("finding the ynh binary: %w", err)
	}
	return &plugin.MCPServer{Type: plugin.MCPTypeStdio, Command: exe,
		Args: []string{"inbox-mcp", "--harness", runName},
		Env:  map[string]string{"YNH_HOME": config.HomeDir()}}, nil
}

// inboxAllowArgs are the vendor arguments that allow the ynh-inbox tools with
// no prompt, written at launch (the spike's evidence, ynh #907): Claude Code
// names the plugin-prefixed server, Codex approves the server's tools, and
// Copilot CLI allows the server.
func inboxAllowArgs(vendorName, harnessName string) []string {
	switch vendorName {
	case "claude":
		return []string{"--allowedTools=mcp__plugin_" + claudeMCPName(harnessName) + "_" + claudeMCPName(inboxmcp.ServerName)}
	case "codex":
		return []string{"-c", "mcp_servers." + inboxmcp.ServerName + "." + "default_tools_approval_mode=\"approve\""}
	case "copilot":
		return []string{"--allow-tool=" + inboxmcp.ServerName}
	}
	return nil
}

// inboxEnvNames is the environment name Codex must pass to its MCP servers for
// the ynh-inbox server to learn its session: Codex trims their environment.
func inboxEnvNames(wired bool) []string {
	if !wired {
		return nil
	}
	return []string{inboxSessionEnv}
}
