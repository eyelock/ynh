package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/eyelock/ynh/internal/migration"
	"github.com/eyelock/ynh/internal/plugin"
)

// cmdMigrate runs the format migration chain against a directory.
// Idempotent — the chain no-ops when no migrator applies.
//
// The command itself knows nothing about specific migrations. Adding or
// removing a migrator in internal/migration/ changes what this command
// handles automatically. Storage relocation is intentionally excluded;
// ynh itself triggers that when installing or relocating harnesses.
//
// Supports -r/--recursive to walk a tree and migrate every directory that
// any registered migrator applies to.
func cmdMigrate(args []string) error {
	recursive := false
	var target string

	for _, a := range args {
		switch a {
		case "-r", "--recursive":
			recursive = true
		case "-h", "--help":
			printMigrateUsage()
			return nil
		default:
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

	dirs := []string{target}
	if recursive {
		dirs = findMigratableDirs(target, chain)
		if len(dirs) == 0 {
			fmt.Printf("Nothing to migrate under %s\n", target)
			return nil
		}
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

func printMigrateUsage() {
	fmt.Println(`ynd migrate - run the format migration chain

Runs every registered format migrator against the target directory.
Migrators decide whether they apply based on the directory contents,
so the command works for any format transition handled by the chain.

Usage:
  ynd migrate [path]              Migrate a single directory (default: .)
  ynd migrate -r [path]           Recursively migrate all matching dirs under path

Options:
  -r, --recursive                 Walk the tree and migrate every matching dir
  -h, --help                      Show this help`)
}
