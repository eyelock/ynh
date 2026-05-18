package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/eyelock/ynh/internal/harness"
)

// cmdMCP dispatches `ynh mcp add|remove|update`. Each subcommand accepts a
// `--profile <name>` flag that scopes the change to a profile overlay
// (formerly `ynh profile mcp ...`). `--null` is only valid with --profile
// (it stamps a profile-overlay null to remove an inherited server).
//
// `update --clear <field>` replaces the four legacy flags
// `--clear-args|--clear-env|--clear-headers` (and is repeatable).
func cmdMCP(args []string) error {
	return cmdMCPTo(args, os.Stdout)
}

func cmdMCPTo(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ynh mcp <add|remove|update> [--profile <p>]")
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
	var command, url string
	var argsList []string
	var env, headers map[string]string
	var null bool
	var profileName string
	var positional []string

	addEnv := func(k, v string) {
		if env == nil {
			env = make(map[string]string)
		}
		env[k] = v
	}
	addHeader := func(k, v string) {
		if headers == nil {
			headers = make(map[string]string)
		}
		headers[k] = v
	}

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--command":
			if i+1 >= len(args) {
				return fmt.Errorf("--command requires a value")
			}
			i++
			command = args[i]
		case "--arg":
			if i+1 >= len(args) {
				return fmt.Errorf("--arg requires a value")
			}
			i++
			argsList = append(argsList, args[i])
		case "--env":
			if i+1 >= len(args) {
				return fmt.Errorf("--env requires a value")
			}
			i++
			k, v, err := parseKV(args[i])
			if err != nil {
				return fmt.Errorf("--env: %w", err)
			}
			addEnv(k, v)
		case "--url":
			if i+1 >= len(args) {
				return fmt.Errorf("--url requires a value")
			}
			i++
			url = args[i]
		case "--header":
			if i+1 >= len(args) {
				return fmt.Errorf("--header requires a value")
			}
			i++
			k, v, err := parseKV(args[i])
			if err != nil {
				return fmt.Errorf("--header: %w", err)
			}
			addHeader(k, v)
		case "--null":
			null = true
		case "--profile":
			if i+1 >= len(args) {
				return fmt.Errorf("--profile requires a value")
			}
			i++
			profileName = args[i]
		default:
			if strings.HasPrefix(args[i], "-") {
				return fmt.Errorf("unknown flag: %s", args[i])
			}
			positional = append(positional, args[i])
		}
	}
	if len(positional) != 2 {
		return fmt.Errorf("usage: ynh mcp add <harness> <name> [--command <cmd> | --url <url> | --null] [--arg <v>...] [--env K=V...] [--header K=V...] [--profile <p>]")
	}
	if null && profileName == "" {
		return fmt.Errorf("--null is only valid with --profile (it stamps a profile overlay null to remove an inherited server)")
	}

	harnessRef, serverName := positional[0], positional[1]

	dir, _, err := harness.ResolveEditTarget(harnessRef)
	if err != nil {
		return err
	}

	if profileName != "" {
		opts := harness.ProfileMCPAddOptions{
			Command: command,
			Args:    argsList,
			Env:     env,
			URL:     url,
			Headers: headers,
			Null:    null,
		}
		if err := harness.AddProfileMCP(dir, profileName, serverName, opts); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "Added mcp server %q to profile %q\n", serverName, profileName)
		return nil
	}

	opts := harness.MCPAddOptions{
		Command: command,
		Args:    argsList,
		Env:     env,
		URL:     url,
		Headers: headers,
	}
	if err := harness.AddMCP(dir, serverName, opts); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "Added mcp server %q\n", serverName)
	return nil
}

func cmdMCPRemove(args []string, stdout io.Writer) error {
	var profileName string
	var positional []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--profile":
			if i+1 >= len(args) {
				return fmt.Errorf("--profile requires a value")
			}
			i++
			profileName = args[i]
		default:
			if strings.HasPrefix(args[i], "-") {
				return fmt.Errorf("unknown flag: %s", args[i])
			}
			positional = append(positional, args[i])
		}
	}
	if len(positional) != 2 {
		return fmt.Errorf("usage: ynh mcp remove <harness> <name> [--profile <p>]")
	}
	harnessRef, serverName := positional[0], positional[1]

	dir, _, err := harness.ResolveEditTarget(harnessRef)
	if err != nil {
		return err
	}
	if profileName != "" {
		if err := harness.RemoveProfileMCP(dir, profileName, serverName); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "Removed mcp server %q from profile %q\n", serverName, profileName)
		return nil
	}
	if err := harness.RemoveMCP(dir, serverName); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "Removed mcp server %q\n", serverName)
	return nil
}

// mcpClearTargets translates `--clear <field>` (repeatable) into the boolean
// Set* flags that internal/harness.MCPUpdateOptions exposes. Accepted fields:
// args, env, headers. Errors on unknown fields so typos fail loud.
func mcpClearTargets(field string, opts *harness.MCPUpdateOptions) error {
	switch field {
	case "args":
		opts.SetArgs = true
	case "env":
		opts.SetEnv = true
	case "headers":
		opts.SetHeaders = true
	default:
		return fmt.Errorf("--clear %q: unknown field (want one of args, env, headers)", field)
	}
	return nil
}

func cmdMCPUpdate(args []string, stdout io.Writer) error {
	var opts harness.MCPUpdateOptions
	var profileName string
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
		case "--clear":
			if i+1 >= len(args) {
				return fmt.Errorf("--clear requires a value (one of args, env, headers)")
			}
			i++
			if err := mcpClearTargets(args[i], &opts); err != nil {
				return err
			}
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
		case "--profile":
			if i+1 >= len(args) {
				return fmt.Errorf("--profile requires a value")
			}
			i++
			profileName = args[i]
		default:
			if strings.HasPrefix(args[i], "-") {
				return fmt.Errorf("unknown flag: %s", args[i])
			}
			positional = append(positional, args[i])
		}
	}
	if len(positional) != 2 {
		return fmt.Errorf("usage: ynh mcp update <harness> <name> [--command <cmd>] [--url <url>] [--arg <v>...] [--env K=V...] [--header K=V...] [--clear args|env|headers] [--profile <p>]")
	}
	harnessRef, serverName := positional[0], positional[1]

	dir, _, err := harness.ResolveEditTarget(harnessRef)
	if err != nil {
		return err
	}
	if profileName != "" {
		if err := harness.UpdateProfileMCP(dir, profileName, serverName, opts); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "Updated mcp server %q in profile %q\n", serverName, profileName)
		return nil
	}
	if err := harness.UpdateMCP(dir, serverName, opts); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "Updated mcp server %q\n", serverName)
	return nil
}
