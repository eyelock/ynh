package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/resolver"
)

func cmdProfile(args []string) error {
	return cmdProfileTo(args, os.Stdout)
}

func cmdProfileTo(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ynh profile <add|remove|hook|mcp|include>")
	}
	switch args[0] {
	case "add":
		return cmdProfileAdd(args[1:], stdout)
	case "remove":
		return cmdProfileRemove(args[1:], stdout)
	case "hook":
		return cmdProfileHook(args[1:], stdout)
	case "mcp":
		return cmdProfileMCP(args[1:], stdout)
	case "include":
		return cmdProfileInclude(args[1:], stdout)
	default:
		return fmt.Errorf("unknown profile subcommand: %s\nUsage: ynh profile <add|remove|hook|mcp|include>", args[0])
	}
}

// ---- profile add/remove ---------------------------------------------

func cmdProfileAdd(args []string, stdout io.Writer) error {
	var positional []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			return fmt.Errorf("unknown flag: %s", a)
		}
		positional = append(positional, a)
	}
	if len(positional) != 2 {
		return fmt.Errorf("usage: ynh profile add <harness> <name>")
	}
	harnessRef, name := positional[0], positional[1]

	dir, _, err := harness.ResolveEditTarget(harnessRef)
	if err != nil {
		return err
	}
	if err := harness.AddProfile(dir, name); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "Added profile %q\n", name)
	return nil
}

func cmdProfileRemove(args []string, stdout io.Writer) error {
	var positional []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			return fmt.Errorf("unknown flag: %s", a)
		}
		positional = append(positional, a)
	}
	if len(positional) != 2 {
		return fmt.Errorf("usage: ynh profile remove <harness> <name>")
	}
	harnessRef, name := positional[0], positional[1]

	dir, _, err := harness.ResolveEditTarget(harnessRef)
	if err != nil {
		return err
	}
	if err := harness.RemoveProfile(dir, name); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "Removed profile %q\n", name)
	return nil
}

// ---- profile hook ---------------------------------------------------

func cmdProfileHook(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ynh profile hook <add|remove>")
	}
	switch args[0] {
	case "add":
		return cmdProfileHookAdd(args[1:], stdout)
	case "remove":
		return cmdProfileHookRemove(args[1:], stdout)
	default:
		return fmt.Errorf("unknown profile hook subcommand: %s\nUsage: ynh profile hook <add|remove>", args[0])
	}
}

func cmdProfileHookAdd(args []string, stdout io.Writer) error {
	var opts harness.ProfileHookAddOptions
	var positional []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--matcher":
			if i+1 >= len(args) {
				return fmt.Errorf("--matcher requires a value")
			}
			i++
			opts.Matcher = args[i]
		default:
			if strings.HasPrefix(args[i], "-") {
				return fmt.Errorf("unknown flag: %s", args[i])
			}
			positional = append(positional, args[i])
		}
	}
	if len(positional) != 4 {
		return fmt.Errorf("usage: ynh profile hook add <harness> <profile> <event> <command> [--matcher <pattern>]")
	}
	harnessRef, profileName, event, command := positional[0], positional[1], positional[2], positional[3]

	dir, _, err := harness.ResolveEditTarget(harnessRef)
	if err != nil {
		return err
	}
	if err := harness.AddProfileHook(dir, profileName, event, command, opts); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "Added hook to profile %q (event %s)\n", profileName, event)
	return nil
}

func cmdProfileHookRemove(args []string, stdout io.Writer) error {
	var positional []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			return fmt.Errorf("unknown flag: %s", a)
		}
		positional = append(positional, a)
	}
	if len(positional) != 4 {
		return fmt.Errorf("usage: ynh profile hook remove <harness> <profile> <event> <index>")
	}
	harnessRef, profileName, event, idxStr := positional[0], positional[1], positional[2], positional[3]
	index, err := strconv.Atoi(idxStr)
	if err != nil {
		return fmt.Errorf("hook index must be an integer: %s", idxStr)
	}

	dir, _, rErr := harness.ResolveEditTarget(harnessRef)
	if rErr != nil {
		return rErr
	}
	if err := harness.RemoveProfileHook(dir, profileName, event, index); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "Removed hook %d from profile %q (event %s)\n", index, profileName, event)
	return nil
}

// ---- profile mcp ----------------------------------------------------

func cmdProfileMCP(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ynh profile mcp <add|remove|update>")
	}
	switch args[0] {
	case "add":
		return cmdProfileMCPAdd(args[1:], stdout)
	case "remove":
		return cmdProfileMCPRemove(args[1:], stdout)
	case "update":
		return cmdProfileMCPUpdate(args[1:], stdout)
	default:
		return fmt.Errorf("unknown profile mcp subcommand: %s\nUsage: ynh profile mcp <add|remove|update>", args[0])
	}
}

// parseKV splits a K=V flag value; errors if no '='.
func parseKV(s string) (string, string, error) {
	idx := strings.Index(s, "=")
	if idx <= 0 {
		return "", "", fmt.Errorf("expected K=V form, got %q", s)
	}
	return s[:idx], s[idx+1:], nil
}

func cmdProfileMCPAdd(args []string, stdout io.Writer) error {
	var opts harness.ProfileMCPAddOptions
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
		case "--null":
			opts.Null = true
		default:
			if strings.HasPrefix(args[i], "-") {
				return fmt.Errorf("unknown flag: %s", args[i])
			}
			positional = append(positional, args[i])
		}
	}
	if len(positional) != 3 {
		return fmt.Errorf("usage: ynh profile mcp add <harness> <profile> <name> [--command <cmd> | --url <url> | --null] [--arg <v>...] [--env K=V...] [--header K=V...]")
	}
	harnessRef, profileName, serverName := positional[0], positional[1], positional[2]

	dir, _, err := harness.ResolveEditTarget(harnessRef)
	if err != nil {
		return err
	}
	if err := harness.AddProfileMCP(dir, profileName, serverName, opts); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "Added mcp server %q to profile %q\n", serverName, profileName)
	return nil
}

func cmdProfileMCPRemove(args []string, stdout io.Writer) error {
	var positional []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			return fmt.Errorf("unknown flag: %s", a)
		}
		positional = append(positional, a)
	}
	if len(positional) != 3 {
		return fmt.Errorf("usage: ynh profile mcp remove <harness> <profile> <name>")
	}
	harnessRef, profileName, serverName := positional[0], positional[1], positional[2]

	dir, _, err := harness.ResolveEditTarget(harnessRef)
	if err != nil {
		return err
	}
	if err := harness.RemoveProfileMCP(dir, profileName, serverName); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "Removed mcp server %q from profile %q\n", serverName, profileName)
	return nil
}

func cmdProfileMCPUpdate(args []string, stdout io.Writer) error {
	var opts harness.ProfileMCPUpdateOptions
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
	if len(positional) != 3 {
		return fmt.Errorf("usage: ynh profile mcp update <harness> <profile> <name> [--command <cmd>] [--url <url>] [--arg <v>...] [--env K=V...] [--header K=V...] [--clear-args|--clear-env|--clear-headers]")
	}
	harnessRef, profileName, serverName := positional[0], positional[1], positional[2]

	dir, _, err := harness.ResolveEditTarget(harnessRef)
	if err != nil {
		return err
	}
	if err := harness.UpdateProfileMCP(dir, profileName, serverName, opts); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "Updated mcp server %q in profile %q\n", serverName, profileName)
	return nil
}

// ---- profile include ------------------------------------------------

func cmdProfileInclude(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ynh profile include <add|remove|update>")
	}
	switch args[0] {
	case "add":
		return cmdProfileIncludeAdd(args[1:], stdout)
	case "remove":
		return cmdProfileIncludeRemove(args[1:], stdout)
	case "update":
		return cmdProfileIncludeUpdate(args[1:], stdout)
	default:
		return fmt.Errorf("unknown profile include subcommand: %s\nUsage: ynh profile include <add|remove|update>", args[0])
	}
}

func cmdProfileIncludeAdd(args []string, stdout io.Writer) error {
	var opts harness.AddOptions
	var positional []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--path":
			if i+1 >= len(args) {
				return fmt.Errorf("--path requires a value")
			}
			i++
			opts.Path = args[i]
		case "--ref":
			if i+1 >= len(args) {
				return fmt.Errorf("--ref requires a value")
			}
			i++
			opts.Ref = args[i]
		case "--replace":
			opts.Replace = true
		default:
			if strings.HasPrefix(args[i], "-") {
				return fmt.Errorf("unknown flag: %s", args[i])
			}
			positional = append(positional, args[i])
		}
	}
	if len(positional) != 3 {
		return fmt.Errorf("usage: ynh profile include add <harness> <profile> <url> [--path <subdir>] [--ref <ref>] [--replace]")
	}
	harnessRef, profileName, url := positional[0], positional[1], positional[2]

	dir, installed, err := harness.ResolveEditTarget(harnessRef)
	if err != nil {
		return err
	}

	if installed {
		gs := harness.GitSource{Git: url, Ref: opts.Ref, Path: opts.Path}
		if _, _, fetchErr := resolver.ResolveGitSource(gs); fetchErr != nil {
			return fmt.Errorf("fetching include: %w", fetchErr)
		}
	}

	if err := harness.AddProfileInclude(dir, profileName, url, opts); err != nil {
		return err
	}
	action := "Added"
	if opts.Replace {
		action = "Replaced"
	}
	_, _ = fmt.Fprintf(stdout, "%s include %q in profile %q\n", action, url, profileName)
	return nil
}

func cmdProfileIncludeRemove(args []string, stdout io.Writer) error {
	var opts harness.RemoveOptions
	var positional []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--path":
			if i+1 >= len(args) {
				return fmt.Errorf("--path requires a value")
			}
			i++
			opts.Path = args[i]
		default:
			if strings.HasPrefix(args[i], "-") {
				return fmt.Errorf("unknown flag: %s", args[i])
			}
			positional = append(positional, args[i])
		}
	}
	if len(positional) != 3 {
		return fmt.Errorf("usage: ynh profile include remove <harness> <profile> <url> [--path <subdir>]")
	}
	harnessRef, profileName, url := positional[0], positional[1], positional[2]

	dir, _, err := harness.ResolveEditTarget(harnessRef)
	if err != nil {
		return err
	}
	if err := harness.RemoveProfileInclude(dir, profileName, url, opts); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "Removed include %q from profile %q\n", url, profileName)
	return nil
}

func cmdProfileIncludeUpdate(args []string, stdout io.Writer) error {
	var opts harness.UpdateOptions
	var positional []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--from-path":
			if i+1 >= len(args) {
				return fmt.Errorf("--from-path requires a value")
			}
			i++
			opts.FromPath = args[i]
		case "--path":
			if i+1 >= len(args) {
				return fmt.Errorf("--path requires a value")
			}
			i++
			v := args[i]
			opts.NewPath = &v
		case "--ref":
			if i+1 >= len(args) {
				return fmt.Errorf("--ref requires a value")
			}
			i++
			v := args[i]
			opts.Ref = &v
		default:
			if strings.HasPrefix(args[i], "-") {
				return fmt.Errorf("unknown flag: %s", args[i])
			}
			positional = append(positional, args[i])
		}
	}
	if len(positional) != 3 {
		return fmt.Errorf("usage: ynh profile include update <harness> <profile> <url> [--from-path <subdir>] [--path <subdir>] [--ref <ref>]")
	}
	if opts.NewPath == nil && opts.Ref == nil {
		return fmt.Errorf("ynh profile include update: at least one of --path or --ref must be specified")
	}
	harnessRef, profileName, url := positional[0], positional[1], positional[2]

	dir, installed, err := harness.ResolveEditTarget(harnessRef)
	if err != nil {
		return err
	}

	if installed {
		finalInc, ferr := harness.FindProfileIncludeUpdateTarget(dir, profileName, url, opts)
		if ferr != nil {
			return ferr
		}
		gs := harness.GitSource{Git: url, Ref: finalInc.Ref, Path: finalInc.Path}
		if _, _, fetchErr := resolver.ResolveGitSource(gs); fetchErr != nil {
			return fmt.Errorf("fetching include: %w", fetchErr)
		}
	}

	if err := harness.UpdateProfileInclude(dir, profileName, url, opts); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "Updated include %q in profile %q\n", url, profileName)
	return nil
}
