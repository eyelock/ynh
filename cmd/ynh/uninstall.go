// `ynh uninstall` — remove an installed harness and its launcher.

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

func cmdUninstall(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: ynh uninstall <harness-name>")
	}

	ref := args[0]

	// Pointer-shaped install: take the pointer path first, before attempting
	// to load the manifest. Removing a pointer is a metadata operation; it
	// must succeed even when the pointed-to source tree is missing — that's
	// the exact case where users most need to uninstall.
	//
	// Resolution mirrors LoadByID: try schema-2 (id-keyed) first, then fall
	// back to schema-1 (name-keyed) for "local/<name>" canonical IDs.
	var bareName, pointerSource string
	ptr, ptrErr := harness.LoadPointerByID(ref)
	if ptrErr != nil {
		return fmt.Errorf("checking pointer: %w", ptrErr)
	}
	if ptr == nil {
		if name, ok := strings.CutPrefix(ref, "local/"); ok {
			var err error
			ptr, err = harness.LoadPointer(name)
			if err != nil {
				return fmt.Errorf("checking pointer: %w", err)
			}
		}
	}
	if ptr != nil {
		bareName = ptr.Name
		pointerSource = ptr.Source
		// Remove both schemas — RemovePointer* silently no-ops on missing files.
		if err := harness.RemovePointer(bareName); err != nil {
			return fmt.Errorf("removing pointer: %w", err)
		}
		if err := harness.RemovePointerByID(ref); err != nil {
			return fmt.Errorf("removing id-keyed pointer: %w", err)
		}
	} else {
		// Tree-shaped install: resolve the on-disk directory (may be flat or
		// namespaced) via the manifest, then remove the directory.
		p, err := harness.LoadQualified(ref)
		if err != nil {
			return fmt.Errorf("harness %q is not installed", ref)
		}
		bareName = p.Name
		if err := os.RemoveAll(p.Dir); err != nil {
			return fmt.Errorf("removing harness: %w", err)
		}
	}

	// Run dirs are keyed by canonical id ("local--foo"), so the removed
	// install's run dirs are exclusively its own — remove them outright.
	// Both id forms in uninstalledIDs may have dirs; removing a missing one
	// is a no-op.
	uninstalledIDs := map[string]bool{ref: true}
	if ptr != nil {
		// Pointer registrations are listed under "local/<name>" regardless
		// of the ref form used to remove them.
		uninstalledIDs["local/"+bareName] = true
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
	survivors, surErr := bareNameSurvivors(bareName, uninstalledIDs)
	switch {
	case surErr != nil:
		// Can't tell whether the name is still claimed — leave the shared
		// resources in place rather than risk orphaning a surviving install.
		fmt.Fprintf(os.Stderr, "warning: could not check for other installs named %q: %v\n", bareName, surErr)
		fmt.Fprintf(os.Stderr, "  launcher, run alias and sources entry left in place\n")
	case len(survivors) == 0:
		// Remove launcher script
		launcherPath := filepath.Join(config.BinDir(), bareName)
		_ = os.Remove(launcherPath) // ignore error if launcher doesn't exist

		// Remove legacy bare-name run path (pre-re-key real dir or alias
		// symlink — RemoveAll removes a symlink without following it)
		runDir := filepath.Join(config.RunDir(), bareName)
		_ = os.RemoveAll(runDir) // ignore error if not present

		// Remove matching sources entry if present
		if cfg, err := config.Load(); err == nil {
			remaining := make([]config.Source, 0, len(cfg.Sources))
			for _, s := range cfg.Sources {
				if s.Name != bareName {
					remaining = append(remaining, s)
				}
			}
			cfg.Sources = remaining
			if err := cfg.Save(); err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not update config after uninstall: %v\n", err)
			}
		}
	default:
		launcherNote = repointLauncher(bareName, survivors)
		repointLegacyRunAlias(bareName, uninstalledIDs, survivors)
	}

	fmt.Printf("Uninstalled harness %q\n", bareName)
	if launcherNote != "" {
		fmt.Printf("  %s\n", launcherNote)
	}
	if pointerSource != "" {
		fmt.Printf("  Source tree left in place: %s\n", pointerSource)
	}
	return nil
}
