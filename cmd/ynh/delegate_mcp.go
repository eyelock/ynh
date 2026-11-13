package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/eyelock/ynh/internal/a2aserve"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/delegatemcp"
	"github.com/eyelock/ynh/internal/plugin"
)

// delegateMCPServerName is the name of the MCP server `ynh run` adds to a
// parent that has A2A delegates. The vendors prefix a server's tools with it
// (mcp__ynh-delegates__delegate_to_<name> on Claude Code), which is what an
// allow rule names (docs/delegate-sessions.md, "Reaching a delegate over MCP").
const delegateMCPServerName = "ynh-delegates"

// delegateMaxWaitEnv sets, in seconds, the longest a delegate tool call may be
// held (delegatemcp.DefaultMaxWait when unset).
const delegateMaxWaitEnv = "YNH_DELEGATE_MAX_WAIT"

// cmdDelegateMCP is `ynh delegate-mcp`, the MCP server a vendor starts over
// stdio for a parent with A2A delegates. It is not for people to run:
//
//	ynh delegate-mcp --parent <id> --folder <dir> [--max-wait <seconds>]
//
// It reaches each delegate through the endpoint its environment names
// (YNH_DELEGATES, YNH_DELEGATE_<NAME>_CARD and _AUTH, which `ynh run` writes
// into the server's entry in the MCP config).
func cmdDelegateMCP(args []string) error {
	var parent, folder string
	maxWait := time.Duration(0)
	for i := 0; i < len(args); i++ {
		next := func() (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s requires a value", args[i])
			}
			i++
			return args[i], nil
		}
		var err error
		var v string
		switch args[i] {
		case "--parent":
			parent, err = next()
		case "--folder":
			folder, err = next()
		case "--max-wait":
			if v, err = next(); err == nil {
				var n int
				if n, err = strconv.Atoi(v); err == nil && n > 0 {
					maxWait = time.Duration(n) * time.Second
				}
			}
		default:
			err = fmt.Errorf("unknown flag %s\nusage: ynh delegate-mcp --parent <id> --folder <dir> [--max-wait <seconds>]", args[i])
		}
		if err != nil {
			return err
		}
	}
	if parent == "" || folder == "" {
		return fmt.Errorf("usage: ynh delegate-mcp --parent <id> --folder <dir> [--max-wait <seconds>]")
	}
	ds, err := delegatesFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return delegatemcp.New(delegatemcp.Config{
		Parent: parent, Folder: folder, Delegates: ds, MaxWait: maxWait, Version: config.Version,
	}).Serve(ctx, os.Stdin, os.Stdout)
}

// delegatesFromEnv reads the endpoints a run put in the environment.
func delegatesFromEnv(get func(string) string) ([]delegatemcp.Delegate, error) {
	var out []delegatemcp.Delegate
	for _, name := range strings.Split(get("YNH_DELEGATES"), ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		k := a2aserve.EnvName(name)
		card, auth := get("YNH_DELEGATE_"+k+"_CARD"), get("YNH_DELEGATE_"+k+"_AUTH")
		if card == "" || auth == "" {
			return nil, fmt.Errorf("delegate %s: YNH_DELEGATE_%s_CARD and YNH_DELEGATE_%s_AUTH must both be set", name, k, k)
		}
		out = append(out, delegatemcp.Delegate{Name: name, Card: card, Auth: auth})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("YNH_DELEGATES names no delegate: this server is started by `ynh run` for a harness with A2A delegates")
	}
	return out, nil
}

// delegateMCPEntry is the server `ynh run` adds to the parent's MCP config
// for its open endpoints: ynh itself, with the endpoints in its environment.
// It is nil when there are none. maxWait comes from YNH_DELEGATE_MAX_WAIT.
func delegateMCPEntry(eps []*a2aserve.Endpoint, parentID, folder string) (*plugin.MCPServer, error) {
	if len(eps) == 0 {
		return nil, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("finding the ynh binary: %w", err)
	}
	all, err := a2aserve.Env(eps)
	if err != nil {
		return nil, err
	}
	// The server finds the delegation records under ynh's home, and a vendor
	// may give its MCP servers a trimmed environment (Codex does), so the home
	// goes in the entry too.
	env := map[string]string{"YNH_HOME": config.HomeDir()}
	for _, kv := range all {
		k, v, _ := strings.Cut(kv, "=")
		if k == "YNH_DELEGATES" || strings.HasPrefix(k, "YNH_DELEGATE_") {
			env[k] = v
		}
	}
	args := []string{"delegate-mcp", "--parent", parentID, "--folder", folder}
	if v := os.Getenv(delegateMaxWaitEnv); v != "" {
		if n, err := strconv.Atoi(v); err != nil || n <= 0 {
			return nil, fmt.Errorf("%s=%q: want a whole number of seconds above 0", delegateMaxWaitEnv, v)
		}
		args = append(args, "--max-wait", v)
	}
	return &plugin.MCPServer{Type: plugin.MCPTypeStdio, Command: exe, Args: args, Env: env}, nil
}
