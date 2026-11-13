package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/parentmcp"
	"github.com/eyelock/ynh/internal/plugin"
)

// delegationRecordEnv names the delegation record of a delegate session: the
// delegate launcher sets it, and its presence is what makes a `ynh run` the
// run of a delegate.
const delegationRecordEnv = "YNH_DELEGATION_RECORD"

// parentAskMaxWaitEnv sets, in seconds, the longest one ask_parent call may be
// held (parentmcp.DefaultMaxWait when unset).
const parentAskMaxWaitEnv = "YNH_PARENT_ASK_MAX_WAIT"

// cmdParentMCP is `ynh parent-mcp`, the MCP server a vendor starts over stdio
// for a delegate session, with the one tool ask_parent. It is not for people
// to run:
//
//	ynh parent-mcp --record <path> [--max-wait <seconds>]
func cmdParentMCP(args []string) error {
	var record string
	maxWait := time.Duration(0)
	for i := 0; i < len(args); i++ {
		if i+1 >= len(args) {
			return fmt.Errorf("%s requires a value\nusage: ynh parent-mcp --record <path> [--max-wait <seconds>]", args[i])
		}
		v := args[i+1]
		switch args[i] {
		case "--record":
			record = v
		case "--max-wait":
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				return fmt.Errorf("--max-wait: want a whole number of seconds above 0")
			}
			maxWait = time.Duration(n) * time.Second
		default:
			return fmt.Errorf("unknown flag %s\nusage: ynh parent-mcp --record <path> [--max-wait <seconds>]", args[i])
		}
		i++
	}
	if record == "" {
		return fmt.Errorf("usage: ynh parent-mcp --record <path> [--max-wait <seconds>]")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return parentmcp.New(parentmcp.Config{Record: record, MaxWait: maxWait, Version: config.Version}).Serve(ctx, os.Stdin, os.Stdout)
}

// parentMCPEntry is the server `ynh run` adds to a delegate session's MCP
// config, so the delegate can ask its parent a question. It is nil when this
// run is not a delegate's (no YNH_DELEGATION_RECORD).
func parentMCPEntry() (*plugin.MCPServer, error) {
	record := os.Getenv(delegationRecordEnv)
	if record == "" {
		return nil, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("finding the ynh binary: %w", err)
	}
	args := []string{"parent-mcp", "--record", record}
	if v := os.Getenv(parentAskMaxWaitEnv); v != "" {
		if n, err := strconv.Atoi(v); err != nil || n <= 0 {
			return nil, fmt.Errorf("%s=%q: want a whole number of seconds above 0", parentAskMaxWaitEnv, v)
		}
		args = append(args, "--max-wait", v)
	}
	// A vendor may give its MCP servers a trimmed environment (Codex does), so
	// the home, where the record's folder is, goes in the entry.
	return &plugin.MCPServer{Type: plugin.MCPTypeStdio, Command: exe, Args: args, Env: map[string]string{"YNH_HOME": config.HomeDir()}}, nil
}

// parentAllowRule is the Claude Code flag that allows ask_parent, for the
// delegate harness called name. Claude Code names a plugin's MCP server
// tools mcp__plugin_<harness>_<server>__<tool>; the rule names the server, so
// it allows the one tool. The value is joined to the flag, so the variadic
// flag takes nothing after it.
func parentAllowRule(name string) string {
	return "--allowedTools=mcp__plugin_" + name + "_" + parentmcp.ServerName
}
