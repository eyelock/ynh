package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/migration"
)

// cmdQuarantine dispatches `ynh quarantine list/restore/drop` to its handler.
//
// The quarantine directory at ~/.ynh/.quarantine/broken/ holds entries that
// migration could not convert (malformed installed.json, missing source URL,
// hand-edited launcher, etc.). Each entry's basename is the lookup key; the
// migration manifest at ~/.ynh/.migration-manifest.json carries the
// original-path and reason for surfaced context.
func cmdQuarantine(args []string) error {
	return cmdQuarantineTo(args, os.Stdout, os.Stderr)
}

func cmdQuarantineTo(args []string, stdout, stderr io.Writer) error {
	if len(args) < 1 {
		return cliError(stderr, false, errCodeInvalidInput,
			"usage: ynh quarantine <list|restore|drop> [args]")
	}
	switch args[0] {
	case "list":
		return cmdQuarantineList(args[1:], stdout, stderr)
	case "restore":
		return cmdQuarantineRestore(args[1:], stdout, stderr)
	case "drop":
		return cmdQuarantineDrop(args[1:], stdout, stderr)
	default:
		return cliError(stderr, false, errCodeInvalidInput,
			fmt.Sprintf("unknown quarantine subcommand: %s", args[0]))
	}
}

// quarantineEntry is the JSON shape for `ynh quarantine list --format json`.
// One per directory entry under ~/.ynh/.quarantine/broken/. Reason and
// OriginalPath come from the persisted migration manifest when available;
// they may be empty if the manifest is missing or the entry was added by a
// different migration run.
type quarantineEntry struct {
	// Name is the basename of the quarantined directory or file — the key
	// users pass to `ynh quarantine restore <name>` or `... drop <name>`.
	Name string `json:"name"`
	// Path is the absolute path to the entry inside the quarantine dir.
	Path string `json:"path"`
	// OriginalPath is where the entry lived before quarantine (from manifest).
	OriginalPath string `json:"original_path,omitempty"`
	// Reason is the migration error that caused the entry to be quarantined
	// (from manifest).
	Reason string `json:"reason,omitempty"`
}

func cmdQuarantineList(args []string, stdout, stderr io.Writer) error {
	structured := detectJSONFormat(args)
	format := "text"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--format":
			if i+1 >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--format requires a value")
			}
			i++
			format = args[i]
		case "--json":
			format = "json"
		default:
			return cliError(stderr, structured, errCodeInvalidInput,
				fmt.Sprintf("unknown flag: %s", args[i]))
		}
	}
	if format != "text" && format != "json" {
		return cliError(stderr, structured, errCodeInvalidInput,
			fmt.Sprintf("invalid --format value %q (want text or json)", format))
	}

	entries, err := scanQuarantine()
	if err != nil {
		return cliError(stderr, structured, errCodeIOError, err.Error())
	}

	if format == "json" {
		return writeJSON(stdout, entries)
	}

	if len(entries) == 0 {
		_, _ = fmt.Fprintln(stdout, "No quarantined entries.")
		return nil
	}
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tORIGINAL PATH\tREASON")
	for _, e := range entries {
		orig := e.OriginalPath
		if orig == "" {
			orig = "-"
		}
		reason := e.Reason
		if reason == "" {
			reason = "-"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", e.Name, orig, reason)
	}
	return tw.Flush()
}

// scanQuarantine reads ~/.ynh/.quarantine/broken/ and decorates each entry
// with manifest data when available. Returns an empty slice (not nil) when
// the quarantine dir is missing or empty.
func scanQuarantine() ([]quarantineEntry, error) {
	home := config.HomeDir()
	qDir := filepath.Join(home, migration.QuarantineDir, "broken")

	dirEntries, err := os.ReadDir(qDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []quarantineEntry{}, nil
		}
		return nil, fmt.Errorf("reading quarantine dir: %w", err)
	}

	// Best-effort manifest decoration. Manifest may be absent (no migration
	// has run on this home) or stale (entries restored/dropped since).
	manifest, _ := migration.ReadManifest(home)
	manifestByPath := map[string]migration.QuarantineEntry{}
	if manifest != nil {
		for _, q := range manifest.Quarantined {
			if q.Quarantined != "" {
				manifestByPath[q.Quarantined] = q
			}
		}
	}

	out := make([]quarantineEntry, 0, len(dirEntries))
	for _, de := range dirEntries {
		entryPath := filepath.Join(qDir, de.Name())
		e := quarantineEntry{
			Name: de.Name(),
			Path: entryPath,
		}
		if mq, ok := manifestByPath[entryPath]; ok {
			e.OriginalPath = mq.OriginalPath
			e.Reason = mq.Reason
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func cmdQuarantineRestore(args []string, stdout, stderr io.Writer) error {
	structured := detectJSONFormat(args)
	var name, toFlag string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--to":
			if i+1 >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--to requires a value")
			}
			i++
			toFlag = args[i]
		default:
			if strings.HasPrefix(args[i], "-") {
				return cliError(stderr, structured, errCodeInvalidInput,
					fmt.Sprintf("unknown flag: %s", args[i]))
			}
			if name != "" {
				return cliError(stderr, structured, errCodeInvalidInput,
					fmt.Sprintf("unexpected argument: %s", args[i]))
			}
			name = args[i]
		}
	}
	if name == "" {
		return cliError(stderr, structured, errCodeInvalidInput,
			"usage: ynh quarantine restore <name> [--to <path>]")
	}

	home := config.HomeDir()
	src := filepath.Join(home, migration.QuarantineDir, "broken", name)
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return cliError(stderr, structured, errCodeNotFound,
				fmt.Sprintf("no quarantined entry %q. Run 'ynh quarantine list' to see what's quarantined.", name))
		}
		return cliError(stderr, structured, errCodeIOError, err.Error())
	}

	dst := toFlag
	if dst == "" {
		// Default: restore to OriginalPath from manifest.
		entries, err := scanQuarantine()
		if err != nil {
			return cliError(stderr, structured, errCodeIOError, err.Error())
		}
		for _, e := range entries {
			if e.Name == name && e.OriginalPath != "" {
				dst = e.OriginalPath
				break
			}
		}
	}
	if dst == "" {
		return cliError(stderr, structured, errCodeInvalidInput,
			fmt.Sprintf("no manifest entry for %q to derive original path; pass --to <path> explicitly", name))
	}

	if _, err := os.Stat(dst); err == nil {
		return cliError(stderr, structured, errCodeIOError,
			fmt.Sprintf("destination %s already exists; remove it or pass --to <other-path>", dst))
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return cliError(stderr, structured, errCodeIOError, err.Error())
	}
	if err := os.Rename(src, dst); err != nil {
		return cliError(stderr, structured, errCodeIOError,
			fmt.Sprintf("restoring %s to %s: %v", name, dst, err))
	}
	_, _ = fmt.Fprintf(stdout, "Restored %s\n  to: %s\n", name, dst)
	return nil
}

func cmdQuarantineDrop(args []string, stdout, stderr io.Writer) error {
	structured := detectJSONFormat(args)
	if len(args) != 1 {
		return cliError(stderr, structured, errCodeInvalidInput,
			"usage: ynh quarantine drop <name>")
	}
	name := args[0]
	home := config.HomeDir()
	target := filepath.Join(home, migration.QuarantineDir, "broken", name)
	if _, err := os.Stat(target); err != nil {
		if os.IsNotExist(err) {
			return cliError(stderr, structured, errCodeNotFound,
				fmt.Sprintf("no quarantined entry %q", name))
		}
		return cliError(stderr, structured, errCodeIOError, err.Error())
	}
	if err := os.RemoveAll(target); err != nil {
		return cliError(stderr, structured, errCodeIOError,
			fmt.Sprintf("dropping %s: %v", name, err))
	}
	_, _ = fmt.Fprintf(stdout, "Dropped %s\n", name)
	return nil
}
