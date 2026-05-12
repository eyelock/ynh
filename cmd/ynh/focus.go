package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/eyelock/ynh/internal/harness"
)

func cmdFocus(args []string) error {
	return cmdFocusTo(args, os.Stdout)
}

func cmdFocusTo(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ynh focus <add|remove|update>")
	}
	switch args[0] {
	case "add":
		return cmdFocusAdd(args[1:], stdout)
	case "remove":
		return cmdFocusRemove(args[1:], stdout)
	case "update":
		return cmdFocusUpdate(args[1:], stdout)
	default:
		return fmt.Errorf("unknown focus subcommand: %s\nUsage: ynh focus <add|remove|update>", args[0])
	}
}

func cmdFocusAdd(args []string, stdout io.Writer) error {
	var opts harness.FocusAddOptions
	var positional []string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--profile":
			if i+1 >= len(args) {
				return fmt.Errorf("--profile requires a value")
			}
			i++
			opts.Profile = args[i]
		default:
			if strings.HasPrefix(args[i], "-") {
				return fmt.Errorf("unknown flag: %s", args[i])
			}
			positional = append(positional, args[i])
		}
	}

	if len(positional) != 3 {
		return fmt.Errorf("usage: ynh focus add <harness> <name> <prompt> [--profile <name>]")
	}

	harnessRef, name, prompt := positional[0], positional[1], positional[2]

	dir, _, err := harness.ResolveEditTarget(harnessRef)
	if err != nil {
		return err
	}

	if err := harness.AddFocus(dir, name, prompt, opts); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(stdout, "Added focus %q\n", name)
	return nil
}

func cmdFocusRemove(args []string, stdout io.Writer) error {
	var positional []string
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "-") {
			return fmt.Errorf("unknown flag: %s", args[i])
		}
		positional = append(positional, args[i])
	}

	if len(positional) != 2 {
		return fmt.Errorf("usage: ynh focus remove <harness> <name>")
	}

	harnessRef, name := positional[0], positional[1]

	dir, _, err := harness.ResolveEditTarget(harnessRef)
	if err != nil {
		return err
	}

	if err := harness.RemoveFocus(dir, name); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(stdout, "Removed focus %q\n", name)
	return nil
}

func cmdFocusUpdate(args []string, stdout io.Writer) error {
	var opts harness.FocusUpdateOptions
	var positional []string
	var clearProfile bool

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--prompt":
			if i+1 >= len(args) {
				return fmt.Errorf("--prompt requires a value")
			}
			i++
			v := args[i]
			opts.Prompt = &v
		case "--profile":
			if i+1 >= len(args) {
				return fmt.Errorf("--profile requires a value")
			}
			i++
			v := args[i]
			opts.Profile = &v
		case "--clear-profile":
			clearProfile = true
		default:
			if strings.HasPrefix(args[i], "-") {
				return fmt.Errorf("unknown flag: %s", args[i])
			}
			positional = append(positional, args[i])
		}
	}

	if clearProfile {
		if opts.Profile != nil {
			return fmt.Errorf("--profile and --clear-profile are mutually exclusive")
		}
		empty := ""
		opts.Profile = &empty
	}

	if len(positional) != 2 {
		return fmt.Errorf("usage: ynh focus update <harness> <name> [--prompt <text>] [--profile <name>] [--clear-profile]")
	}

	if opts.Prompt == nil && opts.Profile == nil {
		return fmt.Errorf("ynh focus update: at least one of --prompt, --profile, or --clear-profile must be specified")
	}

	harnessRef, name := positional[0], positional[1]

	dir, _, err := harness.ResolveEditTarget(harnessRef)
	if err != nil {
		return err
	}

	if err := harness.UpdateFocus(dir, name, opts); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(stdout, "Updated focus %q\n", name)
	return nil
}
