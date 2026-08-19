package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/eyelock/ynh/internal/harness"
)

// cmdProfile dispatches `ynh profile add|remove`. The hook/mcp/include
// sub-trees that used to live here were folded into top-level
// `ynh hook|mcp|include` with a `--profile <name>` flag — the wire surface
// is now identical for harness-level and profile-level edits.
func cmdProfile(args []string) error {
	return cmdProfileTo(args, os.Stdout)
}

func cmdProfileTo(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ynh profile <add|remove>")
	}
	switch args[0] {
	case "add":
		return cmdProfileAdd(args[1:], stdout)
	case "remove":
		return cmdProfileRemove(args[1:], stdout)
	default:
		return fmt.Errorf("unknown profile subcommand: %s\nUsage: ynh profile <add|remove>", args[0])
	}
}

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

// parseKV splits a K=V flag value; errors if no '='. Shared by mcp.go.
func parseKV(s string) (string, string, error) {
	idx := strings.Index(s, "=")
	if idx <= 0 {
		return "", "", fmt.Errorf("expected K=V form, got %q", s)
	}
	return s[:idx], s[idx+1:], nil
}
