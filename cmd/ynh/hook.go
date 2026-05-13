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

func cmdHookTo(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ynh hook <add|remove>")
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
	if len(positional) != 3 {
		return fmt.Errorf("usage: ynh hook add <harness> <event> <command> [--matcher <pattern>]")
	}
	harnessRef, event, command := positional[0], positional[1], positional[2]

	dir, _, err := harness.ResolveEditTarget(harnessRef)
	if err != nil {
		return err
	}
	if err := harness.AddHook(dir, event, command, opts); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "Added hook (event %s)\n", event)
	return nil
}

func cmdHookRemove(args []string, stdout io.Writer) error {
	var positional []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			return fmt.Errorf("unknown flag: %s", a)
		}
		positional = append(positional, a)
	}
	if len(positional) != 3 {
		return fmt.Errorf("usage: ynh hook remove <harness> <event> <index>")
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
	if err := harness.RemoveHook(dir, event, index); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "Removed hook %d (event %s)\n", index, event)
	return nil
}
