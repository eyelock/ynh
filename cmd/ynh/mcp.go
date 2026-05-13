package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/eyelock/ynh/internal/harness"
)

func cmdMCP(args []string) error {
	return cmdMCPTo(args, os.Stdout)
}

func cmdMCPTo(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ynh mcp <add|remove|update>")
	}
	switch args[0] {
	case "add":
		return cmdMCPAdd(args[1:], stdout)
	case "remove":
		return cmdMCPRemove(args[1:], stdout)
	case "update":
		return cmdMCPUpdate(args[1:], stdout)
	default:
		return fmt.Errorf("unknown mcp subcommand: %s\nUsage: ynh mcp <add|remove|update>", args[0])
	}
}

func cmdMCPAdd(args []string, stdout io.Writer) error {
	var opts harness.MCPAddOptions
	var positional []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--command":
			if i+1 >= len(args) {
				return fmt.Errorf("--command requires a value")
			}
			i++
			opts.Command = args[i]
		case "--arg":
			if i+1 >= len(args) {
				return fmt.Errorf("--arg requires a value")
			}
			i++
			opts.Args = append(opts.Args, args[i])
		case "--env":
			if i+1 >= len(args) {
				return fmt.Errorf("--env requires a value")
			}
			i++
			k, v, err := parseKV(args[i])
			if err != nil {
				return fmt.Errorf("--env: %w", err)
			}
			if opts.Env == nil {
				opts.Env = make(map[string]string)
			}
			opts.Env[k] = v
		case "--url":
			if i+1 >= len(args) {
				return fmt.Errorf("--url requires a value")
			}
			i++
			opts.URL = args[i]
		case "--header":
			if i+1 >= len(args) {
				return fmt.Errorf("--header requires a value")
			}
			i++
			k, v, err := parseKV(args[i])
			if err != nil {
				return fmt.Errorf("--header: %w", err)
			}
			if opts.Headers == nil {
				opts.Headers = make(map[string]string)
			}
			opts.Headers[k] = v
		default:
			if strings.HasPrefix(args[i], "-") {
				return fmt.Errorf("unknown flag: %s", args[i])
			}
			positional = append(positional, args[i])
		}
	}
	if len(positional) != 2 {
		return fmt.Errorf("usage: ynh mcp add <harness> <name> [--command <cmd> | --url <url>] [--arg <v>...] [--env K=V...] [--header K=V...]")
	}
	harnessRef, serverName := positional[0], positional[1]

	dir, _, err := harness.ResolveEditTarget(harnessRef)
	if err != nil {
		return err
	}
	if err := harness.AddMCP(dir, serverName, opts); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "Added mcp server %q\n", serverName)
	return nil
}

func cmdMCPRemove(args []string, stdout io.Writer) error {
	var positional []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			return fmt.Errorf("unknown flag: %s", a)
		}
		positional = append(positional, a)
	}
	if len(positional) != 2 {
		return fmt.Errorf("usage: ynh mcp remove <harness> <name>")
	}
	harnessRef, serverName := positional[0], positional[1]

	dir, _, err := harness.ResolveEditTarget(harnessRef)
	if err != nil {
		return err
	}
	if err := harness.RemoveMCP(dir, serverName); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "Removed mcp server %q\n", serverName)
	return nil
}

func cmdMCPUpdate(args []string, stdout io.Writer) error {
	var opts harness.MCPUpdateOptions
	var positional []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--command":
			if i+1 >= len(args) {
				return fmt.Errorf("--command requires a value")
			}
			i++
			v := args[i]
			opts.Command = &v
		case "--arg":
			if i+1 >= len(args) {
				return fmt.Errorf("--arg requires a value")
			}
			i++
			opts.Args = append(opts.Args, args[i])
			opts.SetArgs = true
		case "--clear-args":
			opts.SetArgs = true
		case "--env":
			if i+1 >= len(args) {
				return fmt.Errorf("--env requires a value")
			}
			i++
			k, v, err := parseKV(args[i])
			if err != nil {
				return fmt.Errorf("--env: %w", err)
			}
			if opts.Env == nil {
				opts.Env = make(map[string]string)
			}
			opts.Env[k] = v
			opts.SetEnv = true
		case "--clear-env":
			opts.SetEnv = true
		case "--url":
			if i+1 >= len(args) {
				return fmt.Errorf("--url requires a value")
			}
			i++
			v := args[i]
			opts.URL = &v
		case "--header":
			if i+1 >= len(args) {
				return fmt.Errorf("--header requires a value")
			}
			i++
			k, v, err := parseKV(args[i])
			if err != nil {
				return fmt.Errorf("--header: %w", err)
			}
			if opts.Headers == nil {
				opts.Headers = make(map[string]string)
			}
			opts.Headers[k] = v
			opts.SetHeaders = true
		case "--clear-headers":
			opts.SetHeaders = true
		default:
			if strings.HasPrefix(args[i], "-") {
				return fmt.Errorf("unknown flag: %s", args[i])
			}
			positional = append(positional, args[i])
		}
	}
	if len(positional) != 2 {
		return fmt.Errorf("usage: ynh mcp update <harness> <name> [--command <cmd>] [--url <url>] [--arg <v>...] [--env K=V...] [--header K=V...] [--clear-args|--clear-env|--clear-headers]")
	}
	harnessRef, serverName := positional[0], positional[1]

	dir, _, err := harness.ResolveEditTarget(harnessRef)
	if err != nil {
		return err
	}
	if err := harness.UpdateMCP(dir, serverName, opts); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "Updated mcp server %q\n", serverName)
	return nil
}
