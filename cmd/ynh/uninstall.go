package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
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

	launcherPath := filepath.Join(config.BinDir(), bareName)
	_ = os.Remove(launcherPath)

	runDir := filepath.Join(config.RunDir(), bareName)
	_ = os.RemoveAll(runDir)

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

	fmt.Printf("Uninstalled harness %q\n", bareName)
	if pointerSource != "" {
		fmt.Printf("  Source tree left in place: %s\n", pointerSource)
	}
	return nil
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
