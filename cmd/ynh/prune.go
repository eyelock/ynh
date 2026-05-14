package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/symlink"
)

// runPrune sweeps orphan installations, pointer files, launcher scripts, and
// run directories. Invoked from `ynh status --prune`. quiet=true suppresses
// the per-entry text logging (used in structured-output mode).
func runPrune(stdout io.Writer, quiet bool) error {
	log, err := symlink.LoadLog()
	if err != nil {
		return err
	}

	orphans := log.Prune()
	for _, inst := range orphans {
		if !quiet {
			_, _ = fmt.Fprintf(stdout, "Removing orphaned installation: %s (%s) in %s\n",
				inst.Harness, inst.Vendor, inst.Project)
		}
	}

	if len(orphans) > 0 {
		log.RemoveOrphans(orphans)
		if err := log.Save(); err != nil {
			return err
		}
	}

	// Orphan pointer sweep: pointer exists but its source tree is gone.
	orphanPointers := 0
	if pointers, err := harness.ListPointers(); err == nil {
		for _, e := range pointers {
			if _, err := os.Stat(e.Dir); err == nil {
				continue
			} else if !os.IsNotExist(err) {
				continue
			}
			if err := harness.RemovePointer(e.Name); err != nil {
				fmt.Fprintf(os.Stderr, "warning: removing pointer %q: %v\n", e.Name, err)
				continue
			}
			if !quiet {
				_, _ = fmt.Fprintf(stdout, "Removed orphan pointer: %s (source missing: %s)\n", e.Name, e.Dir)
			}
			orphanPointers++
		}
	}

	installedNames := map[string]bool{}
	if installs, err := harness.ListAll(); err == nil {
		for _, e := range installs {
			installedNames[e.Name] = true
		}
	}

	staleLaunchers := 0
	binDir := config.BinDir()
	entries, err := os.ReadDir(binDir)
	if err == nil {
		for _, entry := range entries {
			name := entry.Name()
			if name == "ynh" || name == "ynd" {
				continue
			}
			if installedNames[name] {
				continue
			}
			launcherPath := filepath.Join(binDir, name)
			data, err := os.ReadFile(launcherPath)
			if err != nil {
				continue
			}
			if !strings.Contains(string(data), "exec ynh run") {
				continue
			}
			_ = os.Remove(launcherPath)
			if !quiet {
				_, _ = fmt.Fprintf(stdout, "Removed stale launcher: %s\n", launcherPath)
			}
			staleLaunchers++
		}
	}

	staleRuns := 0
	runDir := config.RunDir()
	runEntries, err := os.ReadDir(runDir)
	if err == nil {
		for _, entry := range runEntries {
			name := entry.Name()
			if installedNames[name] {
				continue
			}
			staleRun := filepath.Join(runDir, name)
			_ = os.RemoveAll(staleRun)
			if !quiet {
				_, _ = fmt.Fprintf(stdout, "Removed stale run dir: %s\n", staleRun)
			}
			staleRuns++
		}
	}

	if !quiet && len(orphans) == 0 && orphanPointers == 0 && staleLaunchers == 0 && staleRuns == 0 {
		_, _ = fmt.Fprintln(stdout, "No orphaned installations found.")
	}

	return nil
}
