// `ynd migrate-manifest` walks a directory tree and converts every legacy
// harness/registry manifest it finds to the current format. Idempotent —
// runs every registered format migrator in internal/migration/. Adding or
// removing a migrator there changes what this command handles automatically.
//
// Renamed from `ynd migrate` to disambiguate from `ynh migrate`, which
// migrates the ynh home directory schema rather than harness source trees.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/migration"
	"github.com/eyelock/ynh/internal/plugin"
)

func cmdMigrateManifest(args []string) error {
	var target string

	for _, a := range args {
		switch a {
		case "-h", "--help":
			printMigrateManifestUsage()
			return nil
		default:
			if strings.HasPrefix(a, "-") {
				return fmt.Errorf("unknown flag: %s", a)
			}
			if target != "" {
				return fmt.Errorf("unexpected argument %q", a)
			}
			target = a
		}
	}

	if target == "" {
		target = "."
	}

	info, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("target %q: %w", target, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("target %q is not a directory", target)
	}

	chain := migration.FormatChain()

	dirs := findMigratableDirs(target, chain)
	if len(dirs) == 0 {
		fmt.Println("Nothing to migrate.")
		return nil
	}

	migrated := 0
	for _, dir := range dirs {
		applied, err := chain.Run(dir)
		if err != nil {
			return fmt.Errorf("migrating %s: %w", dir, err)
		}
		if len(applied) > 0 {
			fmt.Printf("Migrated %s\n", dir)
			for _, d := range applied {
				fmt.Printf("  %s\n", d)
			}
			migrated++
		}
	}

	if migrated == 0 {
		fmt.Println("Nothing to migrate.")
	} else {
		fmt.Printf("Migrated %d director(ies).\n", migrated)
	}
	return nil
}

// findMigratableDirs walks root and returns every directory where at least
// one migrator in chain applies. The walker never enters .ynh-plugin/ subdirs
// (migrator targets are the parent harness/registry dir).
func findMigratableDirs(root string, chain migration.Chain) []string {
	var dirs []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if d.Name() == plugin.PluginDir {
			return filepath.SkipDir
		}
		for _, m := range chain {
			if m.Applies(path) {
				dirs = append(dirs, path)
				return nil
			}
		}
		return nil
	})
	return dirs
}

func printMigrateManifestUsage() {
	fmt.Println(`ynd migrate-manifest - convert legacy harness manifests to the current format

Runs every registered format migrator against the target directory tree.
Migrators decide whether they apply based on the directory contents,
so the command works for any format transition handled by the chain.

Usage:
  ynd migrate-manifest [path]     Migrate all matching dirs under path (default: .)

Options:
  -h, --help                      Show this help

Compared with 'ynh migrate', which upgrades the ~/.ynh home schema,
'ynd migrate-manifest' operates on harness *source* directories.`)
}
