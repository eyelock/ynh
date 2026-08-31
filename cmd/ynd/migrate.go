package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/migration"
	"github.com/eyelock/ynh/internal/plugin"
)

// cmdMigrate runs the format migration chain against a directory tree.
// Idempotent — the chain no-ops when no migrator applies.
//
// The command itself knows nothing about specific migrations. Adding or
// removing a migrator in internal/migration/ changes what this command
// handles automatically. Storage relocation is intentionally excluded;
// ynh itself triggers that when installing or relocating harnesses.
func cmdMigrate(args []string) error {
	var target string
	dryRun := false
	yes := false
	explicitTarget := false

	for _, a := range args {
		switch a {
		case "-h", "--help":
			printMigrateUsage()
			return nil
		case "--dry-run", "-n":
			dryRun = true
		case "-y", "--yes":
			yes = true
		default:
			if strings.HasPrefix(a, "-") {
				return fmt.Errorf("unknown flag: %s", a)
			}
			if target != "" {
				return fmt.Errorf("unexpected argument %q", a)
			}
			target = a
			explicitTarget = true
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

	abs, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolving %q: %w", target, err)
	}
	// Hard refusal, before anything is read. These ignore -y: skipping a
	// question is not a licence to rewrite a home directory.
	if reason := refuseToMigrate(abs, explicitTarget); reason != "" {
		return fmt.Errorf("refusing to migrate %s: %s", abs, reason)
	}

	chain := migration.FormatChain()

	dirs := findMigratableDirs(target, chain)
	if len(dirs) == 0 {
		fmt.Println("Nothing to migrate.")
		return nil
	}

	// Say what will change before changing it. This command deletes files, and
	// it previously reported what it had done only after doing it (#349).
	fmt.Printf("%d director(ies) would be migrated under %s:\n", len(dirs), abs)
	for _, d := range dirs {
		fmt.Printf("  %s\n", d)
	}
	if dryRun {
		fmt.Println("\nDry run: nothing was changed.")
		return nil
	}
	if !yes && !skipConfirmEnv() {
		// promptAction returns choices[0] on EOF, so the refusing answer is
		// first: a piped stdin must not mean yes.
		if promptAction("\nMigrate these directories? [y/N] ", "n", "y") != "y" {
			fmt.Println("Aborted.")
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
		if d.Name() == plugin.PluginDir || skipDuringMigrate[d.Name()] {
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

Runs every registered format migrator against the target directory tree.
Migrators decide whether they apply based on the directory contents,
so the command works for any format transition handled by the chain.

Usage:
  ynd migrate [path]              Migrate all matching dirs under path (default: .)

Options:
  -h, --help                      Show this help
Flags:
  -n, --dry-run          List what would change and exit without changing it
  -y, --yes              Skip the confirmation prompt (also $YNH_YES, or CI)

Refuses outright, ignoring -y, for a filesystem root or your home directory,
and for a git working copy when no path is given. Skips .git, node_modules
and vendor.`)
}

// skipDuringMigrate are directories a format migration has no business
// entering. Nothing ynh wrote lives in them, and a registry.json inside
// node_modules belongs to somebody else (#348, #349).
var skipDuringMigrate = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
}

// refuseToMigrate reports why a target must not be swept, or "" when it may be.
//
// A pure predicate: it reads nothing it could damage and is kept separate from
// the code that migrates, per .claude/rules/destructive-operations.md.
//
// The git-working-copy refusal applies only when no target was given. Pointing
// this at a repository deliberately is legitimate; defaulting into one because
// of the directory you happened to be in is how this repo's own fixtures were
// deleted.
func refuseToMigrate(abs string, explicitTarget bool) string {
	if filepath.Dir(abs) == abs {
		return "it is a filesystem root"
	}
	if home, err := os.UserHomeDir(); err == nil && sameDir(abs, home) {
		return "it is your home directory"
	}
	if !explicitTarget {
		if fi, err := os.Stat(filepath.Join(abs, ".git")); err == nil && (fi.IsDir() || fi.Mode().IsRegular()) {
			return "it is a git working copy and no target was given; pass the path explicitly if you mean it"
		}
	}
	return ""
}
