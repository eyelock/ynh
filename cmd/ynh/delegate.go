package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/resolver"
)

func cmdDelegate(args []string) error {
	return cmdDelegateTo(args, os.Stdout)
}

func cmdDelegateTo(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ynh delegate <add|remove|update>")
	}
	switch args[0] {
	case "add":
		return cmdDelegateAdd(args[1:], stdout)
	case "remove":
		return cmdDelegateRemove(args[1:], stdout)
	case "update":
		return cmdDelegateUpdate(args[1:], stdout)
	default:
		return fmt.Errorf("unknown delegate subcommand: %s\nUsage: ynh delegate <add|remove|update>", args[0])
	}
}

func cmdDelegateAdd(args []string, stdout io.Writer) error {
	var opts harness.DelegateAddOptions
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
		default:
			if strings.HasPrefix(args[i], "-") {
				return fmt.Errorf("unknown flag: %s", args[i])
			}
			positional = append(positional, args[i])
		}
	}

	if len(positional) != 2 {
		return fmt.Errorf("usage: ynh delegate add <harness> <url> [--ref <ref>] [--path <subdir>]")
	}

	harnessRef, url := positional[0], positional[1]

	dir, installed, err := harness.ResolveEditTarget(harnessRef)
	if err != nil {
		return err
	}

	if installed {
		gs := harness.GitSource{Git: url, Ref: opts.Ref}
		if _, _, fetchErr := resolver.ResolveGitSource(gs); fetchErr != nil {
			return fmt.Errorf("fetching delegate: %w", fetchErr)
		}
	}

	if err := harness.AddDelegate(dir, url, opts); err != nil {
		return err
	}

	msg := fmt.Sprintf("Added delegate %q", url)
	if opts.Path != "" {
		msg += fmt.Sprintf(" (path: %q)", opts.Path)
	}
	_, _ = fmt.Fprintln(stdout, msg)
	return nil
}

func cmdDelegateRemove(args []string, stdout io.Writer) error {
	var opts harness.DelegateRemoveOptions
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

	if len(positional) != 2 {
		return fmt.Errorf("usage: ynh delegate remove <harness> <url> [--path <subdir>]")
	}

	harnessRef, url := positional[0], positional[1]

	dir, _, err := harness.ResolveEditTarget(harnessRef)
	if err != nil {
		return err
	}

	if err := harness.RemoveDelegate(dir, url, opts); err != nil {
		return err
	}

	msg := fmt.Sprintf("Removed delegate %q", url)
	if opts.Path != "" {
		msg += fmt.Sprintf(" (path: %q)", opts.Path)
	}
	_, _ = fmt.Fprintln(stdout, msg)
	return nil
}

func cmdDelegateUpdate(args []string, stdout io.Writer) error {
	var opts harness.DelegateUpdateOptions
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

	if len(positional) != 2 {
		return fmt.Errorf("usage: ynh delegate update <harness> <url> [--from-path <subdir>] [--path <subdir>] [--ref <ref>]")
	}

	if opts.NewPath == nil && opts.Ref == nil {
		return fmt.Errorf("ynh delegate update: at least one of --path or --ref must be specified")
	}

	harnessRef, url := positional[0], positional[1]

	dir, installed, err := harness.ResolveEditTarget(harnessRef)
	if err != nil {
		return err
	}

	if installed {
		finalDel, findErr := harness.FindDelegateUpdateTarget(dir, url, opts)
		if findErr != nil {
			return findErr
		}
		gs := harness.GitSource{Git: url, Ref: finalDel.Ref}
		if _, _, fetchErr := resolver.ResolveGitSource(gs); fetchErr != nil {
			return fmt.Errorf("fetching delegate: %w", fetchErr)
		}
	}

	if err := harness.UpdateDelegate(dir, url, opts); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(stdout, "Updated delegate %q\n", url)
	return nil
}
