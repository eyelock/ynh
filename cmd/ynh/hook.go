package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/eyelock/ynh/internal/harness"
)

func cmdHook(args []string) error {
	return cmdHookTo(args, os.Stdout)
}

// cmdHookTo dispatches `ynh hook add|remove`. Both subcommands accept a
// `--profile <name>` flag that scopes the hook to a profile overlay (the
// behaviour formerly served by `ynh profile hook ...`). Without --profile,
// the hook is registered at the harness top level.
func cmdHookTo(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ynh hook <add|remove> [--profile <p>]")
	}
	switch args[0] {
	case "add":
		return cmdHookAdd(args[1:], stdout)
	case "remove":
		return cmdHookRemove(args[1:], stdout)
	default:
		return fmt.Errorf("unknown hook subcommand: %s\nUsage: ynh hook <add|remove>", args[0])
	}
}

func cmdHookAdd(args []string, stdout io.Writer) error {
	var opts harness.HookAddOptions
	var profileName string
	var positional []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--matcher":
			if i+1 >= len(args) {
				return fmt.Errorf("--matcher requires a value")
			}
			i++
			opts.Matcher = args[i]
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
	if len(positional) != 3 {
		return fmt.Errorf("usage: ynh hook add <harness> <event> <command> [--matcher <pattern>] [--profile <name>]")
	}
	harnessRef, event, command := positional[0], positional[1], positional[2]

	dir, _, err := harness.ResolveEditTarget(harnessRef)
	if err != nil {
		return err
	}
	if profileName != "" {
		if err := harness.AddProfileHook(dir, profileName, event, command, opts); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "Added hook to profile %q (event %s)\n", profileName, event)
		return nil
	}
	if err := harness.AddHook(dir, event, command, opts); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "Added hook (event %s)\n", event)
	return nil
}

func cmdHookRemove(args []string, stdout io.Writer) error {
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
	if len(positional) != 3 {
		return fmt.Errorf("usage: ynh hook remove <harness> <event> <index> [--profile <name>]")
	}
	harnessRef, event, idxStr := positional[0], positional[1], positional[2]
	index, err := strconv.Atoi(idxStr)
	if err != nil {
		return fmt.Errorf("hook index must be an integer: %s", idxStr)
	}

	dir, _, rErr := harness.ResolveEditTarget(harnessRef)
	if rErr != nil {
		return rErr
	}
	if profileName != "" {
		if err := harness.RemoveProfileHook(dir, profileName, event, index); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "Removed hook %d from profile %q (event %s)\n", index, profileName, event)
		return nil
	}
	if err := harness.RemoveHook(dir, event, index); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "Removed hook %d (event %s)\n", index, event)
	return nil
}
