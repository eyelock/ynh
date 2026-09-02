package main

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
)

// Filtering discovered files through git's ignore rules.
//
// `.gitignore` here deliberately excludes `.claude/plans/`, `.claude/sessions/`
// and `.claude/**/*.local.*` so a contributor can keep local planning notes
// beside the harness. The discovery walk did not know that, so `ynd lint`
// reported findings in those notes — and because `make check` runs
// `make check-artifacts` runs `ynd lint .claude`, **a contributor's private
// notes could fail the project's own gate**. That happened: a local defects
// file with two consecutive blank lines broke `make check` until it was
// reformatted, for no reason connected to the change being made.
//
// `ynd fmt` is the worse half. It shares this discovery and *rewrites*, so it
// silently reformatted files the user had told git to leave alone.
//
// The rule is asymmetric on purpose: not touching a file someone asked git to
// ignore is a small loss, while editing one is a surprise. So ignored paths are
// skipped by default, everywhere discovery happens.

// filterIgnored removes paths that git ignores.
//
// Outside a work tree — a harness in a plain directory, a temp dir in a test —
// there are no ignore rules to consult and every path is returned unchanged.
// The same is true if git is not installed: this narrows what the tools touch,
// so failing open is the safe direction, and a missing git must not stop
// `ynd lint` working.
//
// Ignore rules are asked of git rather than reimplemented. Negation patterns,
// nested `.gitignore` files, `.git/info/exclude` and the user's global
// excludesfile all compose in ways that are easy to get subtly wrong, and a
// wrong answer here means either failing on files that should be skipped or
// rewriting files that should not be touched.
func filterIgnored(paths []string) []string {
	if len(paths) == 0 {
		return paths
	}
	ignored := ignoredSet(paths)
	if len(ignored) == 0 {
		return paths
	}
	out := paths[:0:0]
	for _, p := range paths {
		if abs, err := filepath.Abs(p); err == nil && ignored[abs] {
			continue
		}
		out = append(out, p)
	}
	return out
}

// ignoredSet returns the absolute paths git considers ignored, as a set.
//
// One `git check-ignore` call for the whole batch rather than one per file:
// discovery routinely produces hundreds of paths, and a subprocess each would
// dominate the runtime of a command expected to feel instant.
func ignoredSet(paths []string) map[string]bool {
	dir := workTreeFor(paths)
	if dir == "" {
		return nil
	}

	// -z on both sides: paths may contain spaces, and on the way back git
	// would otherwise quote and escape them.
	cmd := exec.Command("git", "-C", dir, "check-ignore", "--stdin", "-z")
	cmd.Stdin = strings.NewReader(strings.Join(absAll(paths), "\x00") + "\x00")
	var out bytes.Buffer
	cmd.Stdout = &out
	// check-ignore exits 1 when nothing matched, which is not an error here —
	// it is the common case in a repository with no ignored artifacts.
	_ = cmd.Run()

	set := map[string]bool{}
	for _, p := range strings.Split(out.String(), "\x00") {
		if p == "" {
			continue
		}
		if abs, err := filepath.Abs(p); err == nil {
			set[abs] = true
		}
	}
	return set
}

// workTreeFor finds the git work tree the discovered paths live in, or "" when
// they are not in one. Derived from the first path rather than the process's
// working directory, because `ynd lint <dir>` may be pointed anywhere.
func workTreeFor(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	start := filepath.Dir(paths[0])
	if abs, err := filepath.Abs(start); err == nil {
		start = abs
	}
	cmd := exec.Command("git", "-C", start, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func absAll(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if abs, err := filepath.Abs(p); err == nil {
			out = append(out, abs)
			continue
		}
		out = append(out, p)
	}
	return out
}
