// `ynh installed <name>` surfaces the recorded install provenance for a
// harness — what file was installed from where, at what time, and (for
// forks) the upstream provenance. The on-disk shape lives at
// ~/.ynh/harnesses/<id-fsname>/.ynh-plugin/installed.json (for tree-form
// installs) or at the pointer file (for local/source installs); this
// command unifies the read so consumers don't have to know the topology.
//
// Tracked as a real CLI command, not an internal-tier file, because
// external consumers already read installed.json directly.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
)

// installedEnvelope wraps the install record with the wire-protocol
// capabilities + ynh release version. Mirrors infoEnvelope / listEnvelope.
type installedEnvelope struct {
	Capabilities string `json:"capabilities"`
	YnhVersion   string `json:"ynh_version"`
	ID           string `json:"id"`
	// Installed is the same shape as the on-disk .ynh-plugin/installed.json,
	// so a consumer can compare the live CLI response against the file it
	// also reads directly. The field uses json.RawMessage to pass the
	// internal/plugin.InstalledJSON marshalled bytes through unmodified.
	Installed json.RawMessage `json:"installed"`
}

func cmdInstalled(args []string) error {
	return cmdInstalledTo(args, os.Stdout, os.Stderr)
}

func cmdInstalledTo(args []string, stdout, stderr io.Writer) error {
	structured := detectJSONFormat(args)

	format := "text"
	var name string
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
			if name != "" {
				return cliError(stderr, structured, errCodeInvalidInput,
					fmt.Sprintf("unexpected argument: %s", args[i]))
			}
			name = args[i]
		}
		i++
	}

	if name == "" {
		return cliError(stderr, structured, errCodeInvalidInput,
			"usage: ynh installed <harness-id> [--format json]")
	}

	h, err := harness.LoadByID(name)
	if err != nil {
		return cliError(stderr, structured, errCodeNotFound,
			fmt.Sprintf("harness %q is not installed", name))
	}
	ins, err := harness.LoadInstalledRecord(name, h)
	if err != nil {
		return cliError(stderr, structured, errCodeIOError,
			fmt.Sprintf("reading install record: %v", err))
	}
	if ins == nil {
		return cliError(stderr, structured, errCodeNotFound,
			fmt.Sprintf("no install record for harness %q (pre-migration install?)", name))
	}

	switch format {
	case "text":
		return printInstalledText(stdout, name, ins)
	case "json":
		return printInstalledJSON(stdout, name, ins)
	default:
		return cliError(stderr, structured, errCodeInvalidInput,
			fmt.Sprintf("invalid --format value %q (want text or json)", format))
	}
}

func printInstalledText(w io.Writer, id string, ins interface{}) error {
	_, _ = fmt.Fprintf(w, "id:           %s\n", id)
	insBytes, err := json.MarshalIndent(ins, "", "  ")
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(w, "installed:\n%s\n", string(insBytes))
	return nil
}

func printInstalledJSON(w io.Writer, id string, ins interface{}) error {
	insBytes, err := json.Marshal(ins)
	if err != nil {
		return fmt.Errorf("encoding installed record: %w", err)
	}
	env := installedEnvelope{
		Capabilities: config.CapabilitiesVersion,
		YnhVersion:   config.Version,
		ID:           id,
		Installed:    insBytes,
	}
	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding installed envelope: %w", err)
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}
