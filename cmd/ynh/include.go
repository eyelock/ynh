package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
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
	var forceGit bool

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
		case "--as":
			if i+1 >= len(args) {
				return fmt.Errorf("--as requires a value")
			}
			i++
			opts.As = args[i]
		case "--replace":
			opts.Replace = true
		case "--git":
			forceGit = true
		default:
			if strings.HasPrefix(args[i], "-") {
				return fmt.Errorf("unknown flag: %s", args[i])
			}
			positional = append(positional, args[i])
		}
	}

	if len(positional) != 2 {
		return fmt.Errorf("usage: ynh include add <harness> <url|path> [--path <subdir>] [--pick <items>] [--ref <ref>] [--as <alias>] [--git] [--replace]")
	}

	harnessRef, url := positional[0], positional[1]

	// Before any fetch: a bad alias is a usage error, not a network one.
	if err := harness.ValidateIncludeAlias(opts.As); err != nil {
		return err
	}

	dir, installed, err := harness.ResolveEditTarget(harnessRef)
	if err != nil {
		return err
	}

	// A filesystem path is a local include, recorded as such; a Git URL is
	// fetched. --git keeps a path a Git source (a repository on disk).
	stored, local, err := prepareLocalInclude(url, dir, forceGit, opts)
	if err != nil {
		return err
	}
	if local {
		url, opts.Local = stored, true
	} else {
		// Fetch before writing so pick validation happens before any mutation.
		needFetch := installed || len(opts.Pick) > 0
		if needFetch {
			gs := harness.GitSource{Git: url, Ref: opts.Ref, Path: opts.Path}
			basePath, _, fetchErr := resolver.ResolveGitSource(gs, dir)
			if fetchErr != nil {
				return fmt.Errorf("fetching include: %w", fetchErr)
			}
			if len(opts.Pick) > 0 {
				if err := harness.ValidatePicks(basePath, opts.Pick); err != nil {
					return err
				}
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
	if opts.As != "" {
		msg += fmt.Sprintf(" as %q", opts.As)
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
	url = includeArgAsStored(dir, url)

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

	url = includeArgAsStored(dir, url)

	// Compute the final include state to know what to fetch/validate.
	finalInc, err := harness.FindUpdateTarget(dir, url, opts)
	if err != nil {
		return err
	}

	// A local include is read from disk: nothing to fetch, and picks are
	// checked against the directory.
	if finalInc.Local != "" {
		if opts.SetPick && len(opts.Pick) > 0 {
			base := finalInc.Local
			if !filepath.IsAbs(base) {
				base = filepath.Join(dir, base)
			}
			if err := harness.ValidatePicks(filepath.Join(base, finalInc.Path), opts.Pick); err != nil {
				return err
			}
		}
		if err := harness.UpdateInclude(dir, url, opts); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(stdout, "Updated include %q\n", url)
		return nil
	}

	needFetch := installed || (opts.SetPick && len(opts.Pick) > 0)
	if needFetch {
		gs := harness.GitSource{Git: url, Ref: finalInc.Ref, Path: finalInc.Path}
		basePath, _, fetchErr := resolver.ResolveGitSource(gs, dir)
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

// looksLikeLocalPath reports whether an include argument names a place on the
// filesystem rather than a Git URL. An explicit path (absolute, ./, ../, ~/,
// or . and ..) always does; anything else does only when it is not
// URL-shaped and is a directory that exists, so github.com/org/repo is never
// mistaken for a path.
func looksLikeLocalPath(arg, harnessDir string) bool {
	switch {
	case arg == "." || arg == "..",
		strings.HasPrefix(arg, "/"), strings.HasPrefix(arg, "./"),
		strings.HasPrefix(arg, "../"), strings.HasPrefix(arg, "~/"):
		return true
	case strings.Contains(arg, "://"), strings.Contains(arg, "@"):
		return false
	}
	for _, base := range []string{"", harnessDir} {
		p := arg
		if base != "" {
			p = filepath.Join(base, arg)
		}
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

// localIncludeForm returns how a local path is recorded in the manifest and
// the absolute directory it names. A bare relative path that exists under the
// harness is taken as relative to the harness, as the manifest reads it; any
// other relative path is relative to where the command ran. A path inside the
// harness is recorded relative to it, so the harness stays portable; one
// outside is recorded absolute, which the source allow-list governs.
func localIncludeForm(arg, harnessDir string) (stored, abs string, err error) {
	p := arg
	if arg == "~" || strings.HasPrefix(arg, "~/") {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", "", fmt.Errorf("local include %q: %w", arg, herr)
		}
		p = filepath.Join(home, strings.TrimPrefix(arg, "~"))
	}
	if !filepath.IsAbs(p) {
		base := ""
		if !strings.HasPrefix(arg, ".") {
			if _, statErr := os.Stat(filepath.Join(harnessDir, p)); statErr == nil {
				base = harnessDir
			}
		}
		if base == "" {
			cwd, cerr := os.Getwd()
			if cerr != nil {
				return "", "", fmt.Errorf("local include %q: %w", arg, cerr)
			}
			base = cwd
		}
		p = filepath.Join(base, p)
	}
	abs = filepath.Clean(p)
	// /tmp is /private/tmp on macOS: compare real paths, or an include inside
	// the harness reads as outside it. The path is recorded as typed, so
	// `include remove` and --replace find it by the same spelling.
	real, root := abs, harnessDir
	if r, err := filepath.EvalSymlinks(abs); err == nil {
		real = r
	}
	if r, err := filepath.Abs(root); err == nil {
		root = r
	}
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	if rel, err := filepath.Rel(root, real); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel), abs, nil
	}
	return abs, abs, nil
}

// includeArgAsStored maps the source named on the command line to the one the
// manifest holds. An argument the manifest already lists is used as written;
// a path is otherwise put in the form `include add` would have recorded.
func includeArgAsStored(dir, arg string) string {
	sources, err := harness.IncludeSources(dir)
	return storedIncludeArg(dir, arg, sources, err)
}

// storedIncludeArg is includeArgAsStored given the manifest's sources.
func storedIncludeArg(dir, arg string, sources []string, srcErr error) string {
	if srcErr == nil && slices.Contains(sources, arg) {
		return arg
	}
	if looksLikeLocalPath(arg, dir) {
		if stored, _, err := localIncludeForm(arg, dir); err == nil {
			return stored
		}
	}
	return arg
}

// prepareLocalInclude decides how `include add` records arg. For a filesystem
// path it validates the directory and picks, and returns the form to store with
// local true; for a Git URL (or with forceGit) it returns arg unchanged and
// local false, leaving the fetch to the caller.
func prepareLocalInclude(arg, dir string, forceGit bool, opts harness.AddOptions) (stored string, local bool, err error) {
	if forceGit || !looksLikeLocalPath(arg, dir) {
		return arg, false, nil
	}
	stored, abs, err := localIncludeForm(arg, dir)
	if err != nil {
		return "", false, err
	}
	if opts.Ref != "" {
		return "", false, fmt.Errorf("--ref applies to Git includes only; %q is a local path (use --git to treat it as a repository)", arg)
	}
	base := abs
	if opts.Path != "" {
		base = filepath.Join(abs, opts.Path)
	}
	if info, statErr := os.Stat(base); statErr != nil || !info.IsDir() {
		return "", false, fmt.Errorf("local include %q: %s is not a directory", arg, base)
	}
	if len(opts.Pick) > 0 {
		if err := harness.ValidatePicks(base, opts.Pick); err != nil {
			return "", false, err
		}
	}
	return stored, true, nil
}
