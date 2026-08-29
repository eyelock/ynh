package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// cleanOutputDir implements --clean for the commands that generate into a
// directory the user names.
//
// It was a bare os.RemoveAll on that path, with no prompt and no guard, in both
// `ynd export` and `ynd marketplace`. The path comes straight from -o, so
// `ynd export -o ~/Documents --clean` deleted ~/Documents. A typo in a flag is
// not consent to delete a directory, and a build tool has no business being the
// most destructive command on the machine.
//
// Two layers, because they answer different questions:
//
//   - Some paths are never a build output, whatever the user says. Those are
//     refused outright by refuseToClean — -y is consent to skip a question, not
//     a licence to delete a home directory or a source repository.
//   - Anything else that exists and is non-empty asks first, unless the caller
//     passed -y, YNH_YES, or is running in CI. That matches how `ynd compress`
//     and `ynd inspect` already gate their destructive steps.
func cleanOutputDir(dir string, skipConfirm bool) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolving --clean target %q: %w", dir, err)
	}
	// Resolve symlinks so a link pointing at $HOME cannot walk past the checks
	// below. A path that does not exist yet has nothing to resolve and nothing
	// to delete, so failure here is not fatal.
	if resolved, rErr := filepath.EvalSymlinks(abs); rErr == nil {
		abs = resolved
	}

	if reason := refuseToClean(abs); reason != "" {
		return fmt.Errorf("--clean refuses to delete %s: %s", abs, reason)
	}

	entries, err := os.ReadDir(abs)
	if os.IsNotExist(err) {
		return nil // nothing to clean
	}
	if err != nil {
		return fmt.Errorf("reading --clean target %s: %w", abs, err)
	}
	if len(entries) == 0 {
		return nil // empty: removing and recreating it changes nothing
	}

	if !skipConfirm {
		fmt.Printf("--clean will permanently delete %s and its %d %s.\n",
			abs, len(entries), pluralWord(len(entries), "entry", "entries"))
		// Choices are ordered so the *first* is the refusing one: promptAction
		// returns choices[0] on empty input or EOF, so a prompt whose first
		// choice was "y" would delete the directory whenever stdin is a pipe
		// rather than a terminal. A prompt labelled [y/N] that returns y on EOF
		// is a lie to the operator.
		if promptAction("Delete it? [y/N] ", "n", "y") != "y" {
			return fmt.Errorf("--clean declined")
		}
	}

	if err := os.RemoveAll(abs); err != nil {
		return fmt.Errorf("cleaning output dir: %w", err)
	}
	return nil
}

// refuseToClean returns why a path must never be deleted, or "" if it may be.
//
// These are not "are you sure" cases. There is no invocation of a harness build
// tool for which deleting the filesystem root, the user's home, the directory
// the command was run from, or a git working copy is the intent.
//
// It is a pure predicate on purpose. It decides about dangerous paths without
// touching them, so those paths never have to be handed to the function that
// deletes — including in its own tests. See
// .claude/rules/destructive-operations.md; that rule exists because a mutation
// test which removed this guard and then ran the tests destroyed a developer's
// home directory.
func refuseToClean(abs string) string {
	if filepath.Dir(abs) == abs {
		return "it is a filesystem root"
	}
	if home, err := os.UserHomeDir(); err == nil && sameDir(abs, home) {
		return "it is your home directory"
	}
	if cwd, err := os.Getwd(); err == nil {
		if sameDir(abs, cwd) {
			return "it is the current directory"
		}
		if dirContains(abs, cwd) {
			return "the current directory is inside it"
		}
	}
	// A .git means this is somebody's source, not a build output. It is the
	// check that catches the realistic accident — an output path typed one
	// directory too high.
	if fi, err := os.Stat(filepath.Join(abs, ".git")); err == nil && (fi.IsDir() || fi.Mode().IsRegular()) {
		return "it is a git working copy"
	}
	return ""
}

// sameDir compares two paths after resolving symlinks, so /tmp and
// /private/tmp on macOS are recognised as the same directory.
func sameDir(a, b string) bool {
	if ra, err := filepath.EvalSymlinks(a); err == nil {
		a = ra
	}
	if rb, err := filepath.EvalSymlinks(b); err == nil {
		b = rb
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// dirContains reports whether child is inside parent.
func dirContains(parent, child string) bool {
	if rp, err := filepath.EvalSymlinks(parent); err == nil {
		parent = rp
	}
	if rc, err := filepath.EvalSymlinks(child); err == nil {
		child = rc
	}
	parent = filepath.Clean(parent) + string(filepath.Separator)
	child = filepath.Clean(child) + string(filepath.Separator)
	return strings.HasPrefix(child, parent)
}

// pluralWord picks the right form so counted output reads as English.
func pluralWord(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
