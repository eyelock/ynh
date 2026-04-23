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
// Consumers like TermQ call `ynd version --format json` (or `ynh version
// --format json`) to gate their feature surface on ynh's contract capability.
func cmdVersion(args []string) error {
	return cmdVersionTo(args, os.Stdout, os.Stderr)
}

func cmdVersionTo(args []string, stdout, _ io.Writer) error {
	format := "text"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--format":
			if i+1 >= len(args) {
				return fmt.Errorf("--format requires a value")
			}
			i++
			format = args[i]
		default:
			return fmt.Errorf("unknown flag: %s", args[i])
		}
	}

	switch format {
	case "text":
		_, _ = fmt.Fprintln(stdout, config.Version)
		return nil
	case "json":
		payload := struct {
			Version      string `json:"version"`
			Capabilities string `json:"capabilities"`
		}{
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
		return fmt.Errorf("invalid --format value %q (want text or json)", format)
	}
}
