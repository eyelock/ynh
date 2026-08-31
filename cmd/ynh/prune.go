// `ynh prune` — remove orphaned symlink installations and stale run directories.
//
// This deletes. It is kept apart from status.go, which only reads, so the two
// are not edited as though they carry the same risk.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/namespace"
	"github.com/eyelock/ynh/internal/symlink"
)

func cmdPrune() error {
	log, err := symlink.LoadLog()
	if err != nil {
		return err
	}

	orphans := log.Prune()
	for _, inst := range orphans {
		fmt.Printf("Removing orphaned installation: %s (%s) in %s\n", inst.Harness, inst.Vendor, inst.Project)
	}

	if len(orphans) > 0 {
		log.RemoveOrphans(orphans)
		if err := log.Save(); err != nil {
			return err
		}
	}

	// Scan for orphan pointer files: pointer exists but its source tree is
	// gone. The user owned the source — they likely deleted it without
	// uninstalling first. Removing the pointer is a metadata operation, so
	// we can do it without consent prompts.
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
			fmt.Printf("Removed orphan pointer: %s (source missing: %s)\n", e.Name, e.Dir)
			orphanPointers++
		}
	}

	// Build a set of installed harness names so the launcher / run-dir
	// scanners can decide "stale" by membership rather than per-entry
	// Load lookups. Schema 2's id-keyed install layout means
	// harness.Load(<bare-name>) no longer resolves an install whose
	// canonical id is e.g. "local/<name>" — using ListAll names directly
	// is both correct under schema 2 and faster than N Load calls.
	//
	// Membership covers both name forms: bare names (launchers, legacy run
	// dirs, and the bare-name run alias — load-bearing for project symlinks
	// planted before the id-keyed re-key) and id fs-names (run dirs keyed
	// by canonical id, e.g. "local--foo").
	installedNames := map[string]bool{}
	if installs, err := harness.ListAll(); err == nil {
		for _, e := range installs {
			installedNames[e.Name] = true
			installedNames[namespace.IDToFSName(canonicalIDForEntry(e))] = true
		}
	}

	// Scan for stale launcher scripts in ~/.ynh/bin/
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
			fmt.Printf("Removed stale launcher: %s\n", launcherPath)
			staleLaunchers++
		}
	}

	// Scan for stale run directories in ~/.ynh/run/
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
			fmt.Printf("Removed stale run dir: %s\n", staleRun)
			staleRuns++
		}
	}

	if len(orphans) == 0 && orphanPointers == 0 && staleLaunchers == 0 && staleRuns == 0 {
		fmt.Println("No orphaned installations found.")
	}

	return nil
}
