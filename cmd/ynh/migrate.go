package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/migration"
)

func cmdMigrate(args []string) error {
	return cmdMigrateTo(args, os.Stdout, os.Stderr)
}

func cmdMigrateTo(args []string, stdout, stderr io.Writer) error {
	structured := detectJSONFormat(args)

	format := "text"
	dryRun := false
	skipBroken := false

	i := 0
	for i < len(args) {
		switch args[i] {
		case "--format":
			if i+1 >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--format requires a value")
			}
			i++
			format = args[i]
		case "--dry-run":
			dryRun = true
		case "--skip-broken":
			skipBroken = true
		default:
			return cliError(stderr, structured, errCodeInvalidInput,
				fmt.Sprintf("unknown flag: %s", args[i]))
		}
		i++
	}

	if format != "text" && format != "json" {
		return cliError(stderr, structured, errCodeInvalidInput,
			fmt.Sprintf("invalid --format value %q (want text or json)", format))
	}

	home := config.HomeDir()
	currentSchema := migration.ReadSchemaVersion(home)

	if currentSchema >= migration.CurrentSchemaVersion {
		return printMigrateNoOp(stdout, format, currentSchema)
	}

	opts := migration.MigrateOpts{DryRun: dryRun, SkipBroken: skipBroken}
	combined := &migration.Manifest{SchemaVersion: migration.CurrentSchemaVersion}
	if currentSchema < 2 {
		m, err := migration.MigrateToSchema2(home, opts)
		if err != nil {
			return cliError(stderr, structured, errCodeIOError, err.Error())
		}
		combined.Entries = append(combined.Entries, m.Entries...)
		combined.Quarantined = append(combined.Quarantined, m.Quarantined...)
		combined.MigratedAt = m.MigratedAt
	}
	if currentSchema < 3 {
		m, err := migration.MigrateToSchema3(home, opts)
		if err != nil {
			return cliError(stderr, structured, errCodeIOError, err.Error())
		}
		combined.Entries = append(combined.Entries, m.Entries...)
		combined.Quarantined = append(combined.Quarantined, m.Quarantined...)
		if combined.MigratedAt == "" {
			combined.MigratedAt = m.MigratedAt
		}
	}

	return printMigrateResult(stdout, format, dryRun, combined)
}

func printMigrateNoOp(stdout io.Writer, format string, schemaVersion int) error {
	if format == "json" {
		return writeJSON(stdout, map[string]any{
			"schema_version": schemaVersion,
			"action":         "noop",
			"message":        "ynh home is already at the current schema version",
		})
	}
	_, err := fmt.Fprintf(stdout, "ynh home is already at schema version %d — nothing to migrate.\n", schemaVersion)
	return err
}

func printMigrateResult(stdout io.Writer, format string, dryRun bool, m *migration.Manifest) error {
	if format == "json" {
		payload := map[string]any{
			"schema_version": m.SchemaVersion,
			"migrated_at":    m.MigratedAt,
			"dry_run":        dryRun,
			"entries":        m.Entries,
			"quarantined":    m.Quarantined,
		}
		return writeJSON(stdout, payload)
	}
	verb := "migrated"
	if dryRun {
		verb = "would migrate"
	}
	_, _ = fmt.Fprintf(stdout, "Schema → %d: %s %d entries", m.SchemaVersion, verb, len(m.Entries))
	if len(m.Quarantined) > 0 {
		_, _ = fmt.Fprintf(stdout, " (%d quarantined)", len(m.Quarantined))
	}
	_, _ = fmt.Fprintln(stdout)
	for _, e := range m.Entries {
		_, _ = fmt.Fprintf(stdout, "  %s  %s → %s\n", e.Kind, e.OldID, e.NewID)
	}
	for _, q := range m.Quarantined {
		_, _ = fmt.Fprintf(stdout, "  quarantine  %s  (%s)\n", q.OriginalPath, q.Reason)
	}
	return nil
}

func writeJSON(w io.Writer, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}

// needsAutoMigrate reports whether the auto-migration gate should run
// before dispatching this command. A short list of commands must remain
// callable on a legacy home: migrate itself (else recursion), version /
// help (no home access), and paths (used by tooling to inspect the home
// before any migration decision).
func needsAutoMigrate(cmd string) bool {
	switch cmd {
	case "migrate", "quarantine", "version", "--version", "help", "--help", "-h", "paths":
		return false
	}
	return true
}

// autoMigrate runs schema-2 migration if the home is at schema 1.
// Returns nil if the home is already at schema 2 (no-op) or if migration
// completes successfully. Returns a wrapped error with a recovery hint
// when migration fails — by default migration aborts on the first broken
// entry; the user must opt into --skip-broken via `ynh migrate
// --skip-broken` explicitly.
func autoMigrate() error {
	home := config.HomeDir()
	// Fresh homes (no ~/.ynh dir at all) need no migration — there's
	// nothing on disk to convert. The first command that creates the
	// home (e.g. `ynh init`, `ynh install`) does so via config.EnsureDirs;
	// migration runs on subsequent invocations once content exists.
	if _, err := os.Stat(home); os.IsNotExist(err) {
		return nil
	}
	current := migration.ReadSchemaVersion(home)
	if current >= migration.CurrentSchemaVersion {
		return nil
	}
	// Fail-loud default: any broken entry aborts. The user runs
	// `ynh migrate --skip-broken` themselves to quarantine and continue.
	opts := migration.MigrateOpts{}
	if current < 2 {
		if _, err := migration.MigrateToSchema2(home, opts); err != nil {
			return fmt.Errorf("auto-migration to schema 2 failed: %w", err)
		}
	}
	if migration.ReadSchemaVersion(home) < 3 {
		if _, err := migration.MigrateToSchema3(home, opts); err != nil {
			return fmt.Errorf("auto-migration to schema 3 failed: %w", err)
		}
	}
	return nil
}
