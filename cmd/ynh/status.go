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
// in `ynh status --format json`.
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
	prune := false
	i := 0
	for i < len(args) {
		switch args[i] {
		case "--format":
			if i+1 >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--format requires a value")
			}
			i++
			format = args[i]
		case "--prune":
			prune = true
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

	// --prune runs the orphan sweep and then falls through to the regular
	// status output (which now reflects the post-prune state). In structured
	// mode the post-prune snapshot is what consumers want anyway; the prune
	// log lines go to stdout in text mode only.
	if prune {
		if err := runPrune(stdout, format == "json"); err != nil {
			return cliError(stderr, structured, errCodeIOError, err.Error())
		}
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
