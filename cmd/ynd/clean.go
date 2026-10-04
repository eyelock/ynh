package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/marketplace"
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
//
// One kind of git working copy is allowed through: the repository `ynd
// marketplace build` itself initialises in its output directory, which it can
// prove from the marker it wrote there. Cleaning that does not delete the
// directory; it empties it and keeps `.git` (and the marker), so the rebuild
// commits on top of the previous one and the history survives. Which entries
// to keep is decided by keepOnClean, not here.
func cleanOutputDir(dir string, skipConfirm bool) error {
	// shown is the path as the user gave it, made absolute: it is what every
	// message names, so /tmp/x stays /tmp/x on macOS rather than turning into
	// /private/tmp/x. abs is what the checks decide on and what is deleted.
	shown, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolving --clean target %q: %w", dir, err)
	}
	abs := shown
	// Resolve symlinks so a link pointing at $HOME cannot walk past the checks
	// below. A path that does not exist yet has nothing to resolve and nothing
	// to delete, so failure here is not fatal.
	if resolved, rErr := filepath.EvalSymlinks(abs); rErr == nil {
		abs = resolved
	}

	if reason := refuseToClean(abs); reason != "" {
		return fmt.Errorf("--clean refuses to delete %s: %s", shown, reason)
	}

	entries, err := os.ReadDir(abs)
	if os.IsNotExist(err) {
		return nil // nothing to clean
	}
	if err != nil {
		return fmt.Errorf("reading --clean target %s: %w", shown, err)
	}

	keep := keepOnClean(abs)
	var doomed []string
	for _, e := range entries {
		if !keep[e.Name()] {
			doomed = append(doomed, e.Name())
		}
	}
	if len(doomed) == 0 {
		return nil // empty: removing and recreating it changes nothing
	}

	if !skipConfirm {
		if len(keep) == 0 {
			fmt.Printf("--clean will permanently delete %s and its %d %s.\n",
				shown, len(doomed), pluralWord(len(doomed), "entry", "entries"))
		} else {
			fmt.Printf("--clean will permanently delete %d %s from %s, keeping its git history.\n",
				len(doomed), pluralWord(len(doomed), "entry", "entries"), shown)
		}
		// Choices are ordered so the *first* is the refusing one: promptAction
		// returns choices[0] on empty input or EOF, so a prompt whose first
		// choice was "y" would delete the directory whenever stdin is a pipe
		// rather than a terminal. A prompt labelled [y/N] that returns y on EOF
		// is a lie to the operator.
		if promptAction("Delete it? [y/N] ", "n", "y") != "y" {
			return declined("--clean declined. %s was not deleted.", shown)
		}
	}

	if len(keep) == 0 {
		if err := os.RemoveAll(abs); err != nil {
			return fmt.Errorf("cleaning output dir: %w", err)
		}
		return nil
	}
	for _, name := range doomed {
		if err := os.RemoveAll(filepath.Join(abs, name)); err != nil {
			return fmt.Errorf("cleaning output dir: %w", err)
		}
	}
	return nil
}

// keepOnClean returns the entries of abs that --clean must leave in place, or
// nil when the whole directory goes. A repository ynd created keeps its `.git`
// and the marker that proves it is ynd's, so the next build commits on top of
// the last one. Like refuseToClean it is a pure predicate: it reads, decides,
// and touches nothing, so the function that deletes has no decisions to make.
func keepOnClean(abs string) map[string]bool {
	if !marketplace.OwnsRepo(abs) {
		return nil
	}
	return map[string]bool{".git": true, marketplace.MarkerFile: true}
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
	// check that catches the realistic accident, an output path typed one
	// directory too high. The one exception is a repository ynd itself
	// initialised for a marketplace build, which it proves from the marker it
	// wrote there (marketplace.OwnsRepo, read-only). Even then the directory is
	// only emptied, never deleted; see keepOnClean.
	if fi, err := os.Stat(filepath.Join(abs, ".git")); err == nil && (fi.IsDir() || fi.Mode().IsRegular()) {
		if marketplace.OwnsRepo(abs) {
			return ""
		}
		return "it is a git working copy (not created by ynd)"
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
