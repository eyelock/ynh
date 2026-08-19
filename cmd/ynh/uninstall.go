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
	// must succeed even when the pointed-to source tree is missing.
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
		if err := harness.RemovePointer(bareName); err != nil {
			return fmt.Errorf("removing pointer: %w", err)
		}
		if err := harness.RemovePointerByID(ref); err != nil {
			return fmt.Errorf("removing id-keyed pointer: %w", err)
		}
	} else {
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

// bareNameSurvivors returns the canonical ids of installed harnesses that
// still claim bareName after an uninstall. Runs post-removal, so the removed
// install is already absent from ListAll; uninstalledIDs additionally drops
// stale duplicate registrations of the just-removed canonical id (e.g. an
// unmigrated schema-1 flat tree shadowing the schema-2 dir that was removed).
// Results keep ListAll order (namespace, then name) so callers that pick one
// get a deterministic choice.
func bareNameSurvivors(bareName string, uninstalledIDs map[string]bool) ([]string, error) {
	entries, err := harness.ListAll()
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if e.Name != bareName {
			continue
		}
		if id := canonicalIDForEntry(e); !uninstalledIDs[id] {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// repointLauncher keeps the bare-name launcher usable for the installs that
// still claim the name after an uninstall. If the launcher already targets
// one of the survivors it is left untouched; otherwise it is regenerated for
// the first survivor. Returns a user-facing note for the uninstall summary,
// or "" when there is nothing to report (reserved names never get a
// launcher — see cmdInstall).
func repointLauncher(bareName string, survivors []string) string {
	if bareName == "ynh" {
		return ""
	}
	launcherPath := filepath.Join(config.BinDir(), bareName)
	if body, err := os.ReadFile(launcherPath); err == nil {
		for _, id := range survivors {
			if strings.Contains(string(body), fmt.Sprintf("ynh run %q", id)) {
				return fmt.Sprintf("Launcher kept: targets surviving install %s", id)
			}
		}
	}
	if err := generateLauncher(bareName, survivors[0]); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not regenerate launcher for %s: %v\n", survivors[0], err)
		return ""
	}
	return fmt.Sprintf("Launcher repointed to surviving install %s", survivors[0])
}

// repointLegacyRunAlias keeps the legacy bare-name run path (run/<name>,
// a symlink to an id-keyed run dir — see updateLegacyRunAlias) coherent
// after an uninstall with survivors. An alias targeting a removed install's
// run dir is repointed to the sole survivor, or removed when several
// survivors make the bare name ambiguous. A real directory (stale
// pre-re-key assembly) or an alias already targeting a survivor is left
// alone. Best-effort: failures leave a dangling alias, which run repairs.
func repointLegacyRunAlias(bareName string, uninstalledIDs map[string]bool, survivors []string) {
	alias := filepath.Join(config.RunDir(), bareName)
	target, err := os.Readlink(alias)
	if err != nil {
		return // no alias (or a real dir) — nothing to repoint
	}
	removed := false
	for id := range uninstalledIDs {
		if target == namespace.IDToFSName(id) {
			removed = true
			break
		}
	}
	if !removed {
		return
	}
	_ = os.Remove(alias)
	if len(survivors) == 1 {
		_ = os.Symlink(namespace.IDToFSName(survivors[0]), alias)
	}
}

// harnessHasRemoteSource reports whether the harness was installed from a
// git or registry source we can re-pull. Local installs and forks are
// excluded — those have no upstream to track.
func harnessHasRemoteSource(p *harness.Harness) bool {
	if p.InstalledFrom == nil {
		return false
	}
	if p.InstalledFrom.ForkedFrom != nil {
		return false
	}
	switch p.InstalledFrom.SourceType {
	case "git", "registry":
		return p.InstalledFrom.Source != ""
	}
	return false
}
