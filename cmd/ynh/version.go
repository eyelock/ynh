package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/eyelock/ynh/internal/config"
)

// cmdVersion prints the release version and, with --format json, a structured
// envelope that also carries the wire-contract CapabilitiesVersion.
//
// Consumers like TermQ call `ynh version --format json` to gate their feature
// surface on ynh's contract capability, decoupling the gate from the release
// tag (dev builds report the same capability string as the release they
// branched from).
func cmdVersion(args []string) error {
	return cmdVersionTo(args, os.Stdout, os.Stderr)
}

func cmdVersionTo(args []string, stdout, stderr io.Writer) error {
	format := "text"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--format":
			if i+1 >= len(args) {
				return cliError(stderr, true, errCodeInvalidInput, "--format requires a value")
			}
			i++
			format = args[i]
		default:
			return cliError(stderr, true, errCodeInvalidInput, fmt.Sprintf("unknown flag: %s", args[i]))
		}
	}

	switch format {
	case "text":
		_, _ = fmt.Fprintln(stdout, config.Version)
		return nil
	case "json":
		payload := versionPayload{
			Version:      config.Version,
			Capabilities: config.CapabilitiesVersion,
		}
		data, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return fmt.Errorf("encoding version payload: %w", err)
		}
		_, _ = fmt.Fprintln(stdout, string(data))
		return nil
	default:
		return cliError(stderr, true, errCodeInvalidInput,
			fmt.Sprintf("invalid --format value %q (want text or json)", format))
	}
}

// versionPayload is the JSON shape of `ynh version --format json`.
// Keep field names stable — external consumers (TermQ) decode this.
type versionPayload struct {
	Version      string `json:"version"`
	Capabilities string `json:"capabilities"`
}
