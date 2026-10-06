// `ynh uninstall`: remove installed harnesses and their launchers.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/namespace"
)

// uninstallTarget is an installed harness that has been resolved but not yet
// removed. Resolution is separated from removal so that every name on the
// command line is checked before anything is deleted: a typo in one name
// removes nothing.
type uninstallTarget struct {
	ref      string
	bareName string
	// ptr is set for pointer-shaped installs (ynh fork, ynh install <dir>).
	// Nil means a tree-shaped install living at dir.
	ptr *harness.Pointer
	dir string
}

func cmdUninstall(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: ynh uninstall <name> [<name>...]")
	}

	// A name given twice is one uninstall, not two.
	refs := make([]string, 0, len(args))
	seen := make(map[string]bool, len(args))
	for _, ref := range args {
		if !seen[ref] {
			seen[ref] = true
			refs = append(refs, ref)
		}
	}

	// Phase 1: resolve every name. Nothing is removed until all resolve.
	targets := make([]uninstallTarget, 0, len(refs))
	var failed []error
	for _, ref := range refs {
		target, err := resolveUninstallTarget(ref)
		if err != nil {
			failed = append(failed, err)
			continue
		}
		targets = append(targets, target)
	}
	if len(failed) > 0 {
		return uninstallFailure(failed, len(refs), ", nothing was removed")
	}

	// Phase 2: remove. A failure here does not stop the remaining names;
	// each outcome is reported and the command exits non-zero if any failed.
	for _, target := range targets {
		if err := target.remove(); err != nil {
			failed = append(failed, err)
		}
	}
	return uninstallFailure(failed, len(refs), "")
}

// uninstallFailure turns the per-name errors into the command's return
// value. With a single name the error is returned as-is, so the one-name
// output is unchanged. With several, each failure is printed to stderr and a
// summary is returned for main to print.
func uninstallFailure(failed []error, total int, suffix string) error {
	if len(failed) == 0 {
		return nil
	}
	if total == 1 {
		return failed[0]
	}
	for _, err := range failed {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}
	return fmt.Errorf("%d of %d harnesses could not be uninstalled%s", len(failed), total, suffix)
}

// resolveUninstallTarget finds the install ref names without touching it.
//
// Pointer-shaped installs are checked first, before attempting to load the
// manifest. Removing a pointer is a metadata operation; it must succeed even
// when the pointed-to source tree is missing; that is the exact case where
// users most need to uninstall.
//
// Resolution mirrors LoadByID: try schema-2 (id-keyed) first, then fall back
// to schema-1 (name-keyed) for "local/<name>" canonical IDs.
func resolveUninstallTarget(ref string) (uninstallTarget, error) {
	ptr, err := harness.LoadPointerByID(ref)
	if err != nil {
		return uninstallTarget{}, fmt.Errorf("checking pointer: %w", err)
	}
	if ptr == nil {
		if name, ok := strings.CutPrefix(ref, "local/"); ok {
			ptr, err = harness.LoadPointer(name)
			if err != nil {
				return uninstallTarget{}, fmt.Errorf("checking pointer: %w", err)
			}
		}
	}
	if ptr != nil {
		return uninstallTarget{ref: ref, bareName: ptr.Name, ptr: ptr}, nil
	}

	// Tree-shaped install: resolve the on-disk directory (may be flat or
	// namespaced) via the manifest.
	p, err := harness.LoadQualified(ref)
	if err != nil {
		return uninstallTarget{}, fmt.Errorf("harness %q is not installed", ref)
	}
	return uninstallTarget{ref: ref, bareName: p.Name, dir: p.Dir}, nil
}

// remove deletes the resolved install and whatever shared resources (launcher,
// run dirs, sources entry) no surviving install still claims, then prints the
// confirmation.
func (t uninstallTarget) remove() error {
	var pointerSource string
	if t.ptr != nil {
		pointerSource = t.ptr.Source
		// Remove both schemas; RemovePointer* silently no-ops on missing files.
		if err := harness.RemovePointer(t.bareName); err != nil {
			return fmt.Errorf("removing pointer: %w", err)
		}
		if err := harness.RemovePointerByID(t.ref); err != nil {
			return fmt.Errorf("removing id-keyed pointer: %w", err)
		}
	} else {
		if err := os.RemoveAll(t.dir); err != nil {
			return fmt.Errorf("removing harness: %w", err)
		}
	}

	// Run dirs are keyed by canonical id ("local--foo"), so the removed
	// install's run dirs are exclusively its own, so remove them outright.
	// Both id forms in uninstalledIDs may have dirs; removing a missing one
	// is a no-op.
	uninstalledIDs := map[string]bool{t.ref: true}
	if t.ptr != nil {
		// Pointer registrations are listed under "local/<name>" regardless
		// of the ref form used to remove them.
		uninstalledIDs["local/"+t.bareName] = true
	}
	for id := range uninstalledIDs {
		_ = os.RemoveAll(filepath.Join(config.RunDir(), namespace.IDToFSName(id)))
	}

	// The launcher (~/.ynh/bin/<name>), legacy bare-name run path
	// (~/.ynh/run/<name>) and sources entry are keyed by bare name and
	// therefore shared across installs whose canonical ids differ only in
	// namespace (e.g. "local/foo" vs "github.com/org/repo/foo"). Remove
	// them only when no other install still claims the name; otherwise
	// leave them for the survivor, repointing the launcher and the legacy
	// run alias if they targeted the install just removed.
	var launcherNote string
	survivors, surErr := bareNameSurvivors(t.bareName, uninstalledIDs)
	switch {
	case surErr != nil:
		// Can't tell whether the name is still claimed, so leave the shared
		// resources in place rather than risk orphaning a surviving install.
		fmt.Fprintf(os.Stderr, "warning: could not check for other installs named %q: %v\n", t.bareName, surErr)
		fmt.Fprintf(os.Stderr, "  launcher, run alias and sources entry left in place\n")
	case len(survivors) == 0:
		// Remove launcher script
		launcherPath := filepath.Join(config.BinDir(), t.bareName)
		_ = os.Remove(launcherPath) // ignore error if launcher doesn't exist

		// Remove legacy bare-name run path (pre-re-key real dir or alias
		// symlink; RemoveAll removes a symlink without following it)
		runDir := filepath.Join(config.RunDir(), t.bareName)
		_ = os.RemoveAll(runDir) // ignore error if not present

		// Remove matching sources entry if present. Save only when one was
		// removed: Load returns defaults for a missing file, and saving those
		// would create a config.json nobody asked for (#490).
		if cfg, err := config.Load(); err == nil {
			remaining := make([]config.Source, 0, len(cfg.Sources))
			for _, s := range cfg.Sources {
				if s.Name != t.bareName {
					remaining = append(remaining, s)
				}
			}
			if len(remaining) != len(cfg.Sources) {
				cfg.Sources = remaining
				if err := cfg.Save(); err != nil {
					fmt.Fprintf(os.Stderr, "warning: could not update config after uninstall: %v\n", err)
				}
			}
		}
	default:
		launcherNote = repointLauncher(t.bareName, survivors)
		repointLegacyRunAlias(t.bareName, uninstalledIDs, survivors)
	}

	fmt.Printf("Uninstalled harness %q\n", t.bareName)
	if launcherNote != "" {
		fmt.Printf("  %s\n", launcherNote)
	}
	if pointerSource != "" && sourceTreeExists(pointerSource) {
		fmt.Printf("  Source tree left in place: %s\n", pointerSource)
	}
	return nil
}

// sourceTreeExists reports whether path is a directory, following a symlink.
// Uninstall reports a pointer's source tree as left in place only when there
// is one: the common reason to uninstall a fork is that its tree was already
// deleted, and naming a missing path as "left in place" describes something
// that is not there (#533). A regular file at the path is not a source tree
// either. It only reads.
func sourceTreeExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}
