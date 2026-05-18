package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/vendor"
)

func cmdVendors(args []string) error {
	return cmdVendorsTo(args, os.Stdout, os.Stderr)
}

// vendorEntry is the JSON shape for a single vendor in `ynh vendors --format json`.
type vendorEntry struct {
	Name                  string `json:"name"`
	DisplayName           string `json:"display_name"`
	CLI                   string `json:"cli"`
	ConfigDir             string `json:"config_dir"`
	Available             bool   `json:"available"`
	SupportsInitialPrompt bool   `json:"supports_initial_prompt"`
}

func cmdVendorsTo(args []string, stdout, stderr io.Writer) error {
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
		return printVendorsText(stdout)
	case "json":
		return printVendorsJSON(stdout)
	default:
		return cliError(stderr, structured, errCodeInvalidInput,
			fmt.Sprintf("invalid --format value %q (want text or json)", format))
	}
}

func printVendorsText(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tDISPLAY NAME\tCLI\tCONFIG DIR\tAVAILABLE")

	for _, name := range vendor.Available() {
		adapter, err := vendor.Get(name)
		if err != nil {
			return fmt.Errorf("loading vendor %s: %w", name, err)
		}
		available := "false"
		if _, err := exec.LookPath(adapter.CLIName()); err == nil {
			available = "true"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			adapter.Name(), adapter.DisplayName(), adapter.CLIName(), adapter.ConfigDir(), available)
	}

	return tw.Flush()
}

// initialPrompter is an optional capability interface for vendors that support
// starting an interactive session with an initial prompt pre-loaded.
type initialPrompter interface {
	SupportsInitialPrompt() bool
}

func printVendorsJSON(w io.Writer) error {
	entries := make([]vendorEntry, 0, len(vendor.Available()))
	for _, name := range vendor.Available() {
		adapter, err := vendor.Get(name)
		if err != nil {
			return fmt.Errorf("loading vendor %s: %w", name, err)
		}
		_, lookErr := exec.LookPath(adapter.CLIName())
		supportsIP := false
		if ip, ok := adapter.(initialPrompter); ok {
			supportsIP = ip.SupportsInitialPrompt()
		}
		entries = append(entries, vendorEntry{
			Name:                  adapter.Name(),
			DisplayName:           adapter.DisplayName(),
			CLI:                   adapter.CLIName(),
			ConfigDir:             adapter.ConfigDir(),
			Available:             lookErr == nil,
			SupportsInitialPrompt: supportsIP,
		})
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding vendors: %w", err)
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}

// resolveVendor picks the vendor: CLI flag > YNH_VENDOR env > harness default > global config.
func resolveVendor(flag string, p *harness.Harness) (string, error) {
	if flag != "" {
		return flag, nil
	}
	if v := os.Getenv("YNH_VENDOR"); v != "" {
		return v, nil
	}
	if p.DefaultVendor != "" {
		return p.DefaultVendor, nil
	}

	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	if cfg.DefaultVendor != "" {
		return cfg.DefaultVendor, nil
	}

	return "", fmt.Errorf("no vendor specified (use -v flag, YNH_VENDOR env var, harness default_vendor, or global config)")
}
