package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/resolver"
)

func cmdInclude(args []string) error {
	return cmdIncludeTo(args, os.Stdout)
}

func cmdIncludeTo(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ynh include <add|remove|update>")
	}
	switch args[0] {
	case "add":
		return cmdIncludeAdd(args[1:], stdout)
	case "remove":
		return cmdIncludeRemove(args[1:], stdout)
	case "update":
		return cmdIncludeUpdate(args[1:], stdout)
	default:
		return fmt.Errorf("unknown include subcommand: %s\nUsage: ynh include <add|remove|update>", args[0])
	}
}

func cmdIncludeAdd(args []string, stdout io.Writer) error {
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
		case "--pick":
			if i+1 >= len(args) {
				return fmt.Errorf("--pick requires a value")
			}
			i++
			opts.Pick = splitPick(args[i])
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

	if len(positional) != 2 {
		return fmt.Errorf("usage: ynh include add <harness> <url> [--path <subdir>] [--pick <items>] [--ref <ref>] [--replace]")
	}

	harnessRef, url := positional[0], positional[1]

	dir, installed, err := harness.ResolveEditTarget(harnessRef)
	if err != nil {
		return err
	}

	// Fetch before writing so pick validation happens before any mutation.
	needFetch := installed || len(opts.Pick) > 0
	if needFetch {
		gs := harness.GitSource{Git: url, Ref: opts.Ref, Path: opts.Path}
		basePath, _, fetchErr := resolver.ResolveGitSource(gs)
		if fetchErr != nil {
			return fmt.Errorf("fetching include: %w", fetchErr)
		}
		if len(opts.Pick) > 0 {
			if err := harness.ValidatePicks(basePath, opts.Pick); err != nil {
				return err
			}
		}
	}

	if err := harness.AddInclude(dir, url, opts); err != nil {
		return err
	}

	action := "Added"
	if opts.Replace {
		action = "Replaced"
	}
	msg := fmt.Sprintf("%s include %q", action, url)
	if opts.Path != "" {
		msg += fmt.Sprintf(" (path: %q)", opts.Path)
	}
	_, _ = fmt.Fprintln(stdout, msg)
	return nil
}

func cmdIncludeRemove(args []string, stdout io.Writer) error {
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

	if len(positional) != 2 {
		return fmt.Errorf("usage: ynh include remove <harness> <url> [--path <subdir>]")
	}

	harnessRef, url := positional[0], positional[1]

	dir, _, err := harness.ResolveEditTarget(harnessRef)
	if err != nil {
		return err
	}

	if err := harness.RemoveInclude(dir, url, opts); err != nil {
		return err
	}

	msg := fmt.Sprintf("Removed include %q", url)
	if opts.Path != "" {
		msg += fmt.Sprintf(" (path: %q)", opts.Path)
	}
	_, _ = fmt.Fprintln(stdout, msg)
	return nil
}

func cmdIncludeUpdate(args []string, stdout io.Writer) error {
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
		case "--pick":
			if i+1 >= len(args) {
				return fmt.Errorf("--pick requires a value")
			}
			i++
			opts.Pick = splitPick(args[i])
			opts.SetPick = true
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
		return fmt.Errorf("usage: ynh include update <harness> <url> [--from-path <subdir>] [--path <subdir>] [--pick <items>] [--ref <ref>]")
	}

	if opts.NewPath == nil && !opts.SetPick && opts.Ref == nil {
		return fmt.Errorf("ynh include update: at least one of --path, --pick, or --ref must be specified")
	}

	harnessRef, url := positional[0], positional[1]

	dir, installed, err := harness.ResolveEditTarget(harnessRef)
	if err != nil {
		return err
	}

	// Compute the final include state to know what to fetch/validate.
	finalInc, err := harness.FindUpdateTarget(dir, url, opts)
	if err != nil {
		return err
	}

	needFetch := installed || (opts.SetPick && len(opts.Pick) > 0)
	if needFetch {
		gs := harness.GitSource{Git: url, Ref: finalInc.Ref, Path: finalInc.Path}
		basePath, _, fetchErr := resolver.ResolveGitSource(gs)
		if fetchErr != nil {
			return fmt.Errorf("fetching include: %w", fetchErr)
		}
		if opts.SetPick && len(opts.Pick) > 0 {
			if err := harness.ValidatePicks(basePath, opts.Pick); err != nil {
				return err
			}
		}
	}

	if err := harness.UpdateInclude(dir, url, opts); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(stdout, "Updated include %q\n", url)
	return nil
}

// splitPick splits a comma-separated pick list, trimming whitespace.
func splitPick(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
