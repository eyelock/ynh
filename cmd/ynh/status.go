// `ynh status` — which projects have symlink installations, and whether each
// still points at something.

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/eyelock/ynh/internal/symlink"
	"github.com/eyelock/ynh/internal/vendor"
)

func cmdStatus(args []string) error {
	return cmdStatusTo(args, os.Stdout, os.Stderr)
}

// statusInstallation is the JSON shape for a single symlink installation
// in `ynh status --format json`. Mirrors symlink.Installation; redeclared
// here so the wire contract is owned by cmd/ynh and not coupled to the
// internal package's struct evolution.
type statusInstallation struct {
	Harness   string                `json:"harness"`
	Vendor    string                `json:"vendor"`
	Project   string                `json:"project"`
	Timestamp string                `json:"timestamp"`
	Symlinks  []vendor.SymlinkEntry `json:"symlinks"`
}

func cmdStatusTo(args []string, stdout, stderr io.Writer) error {
	structured := detectJSONFormat(args)

	format := "text"
	i := 0
	for i < len(args) {
		switch args[i] {
		case "--format":
			if i+1 >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--format requires a value")
			}
			i++
			format = args[i]
		default:
			if strings.HasPrefix(args[i], "-") {
				return cliError(stderr, structured, errCodeInvalidInput,
					fmt.Sprintf("unknown flag: %s", args[i]))
			}
			return cliError(stderr, structured, errCodeInvalidInput,
				fmt.Sprintf("unexpected argument: %s", args[i]))
		}
		i++
	}

	switch format {
	case "text":
		return printStatusText(stdout)
	case "json":
		return printStatusJSON(stdout)
	default:
		return cliError(stderr, structured, errCodeInvalidInput,
			fmt.Sprintf("invalid --format value %q (want text or json)", format))
	}
}

func printStatusText(w io.Writer) error {
	log, err := symlink.LoadLog()
	if err != nil {
		return err
	}
	if len(log.Installations) == 0 {
		_, _ = fmt.Fprintln(w, "No symlink installations found.")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "HARNESS\tVENDOR\tPROJECT\tSYMLINKS")
	for _, inst := range log.Installations {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%d\n", inst.Harness, inst.Vendor, inst.Project, len(inst.Symlinks))
	}
	return tw.Flush()
}

func printStatusJSON(w io.Writer) error {
	log, err := symlink.LoadLog()
	if err != nil {
		return err
	}
	entries := make([]statusInstallation, 0, len(log.Installations))
	for _, inst := range log.Installations {
		entries = append(entries, statusInstallation{
			Harness:   inst.Harness,
			Vendor:    inst.Vendor,
			Project:   inst.Project,
			Timestamp: inst.Timestamp.UTC().Format(time.RFC3339),
			Symlinks:  inst.Symlinks,
		})
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding status: %w", err)
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}

// symlinkIntact returns true if at least one symlink from the installation
// still exists on disk AND resolves to a live target. Returns false if all
// symlinks are missing (e.g. the user deleted the vendor config directory)
// or dangling (e.g. the run dir they point into was removed or re-keyed) —
// either way the installation needs re-planting against the current run dir.
func symlinkIntact(inst *symlink.Installation) bool {
	for _, entry := range inst.Symlinks {
		info, err := os.Lstat(entry.Link)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		// Stat follows the link: an error here means the target is gone —
		// a dangling link is not intact.
		if _, err := os.Stat(entry.Link); err == nil {
			return true
		}
	}
	return false
}
