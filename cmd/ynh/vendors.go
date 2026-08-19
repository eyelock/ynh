package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/eyelock/ynh/internal/backend"
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
	entries, err := vendorEntries()
	if err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tDISPLAY NAME\tCLI\tCONFIG DIR\tAVAILABLE")

	for _, e := range entries {
		available := "false"
		if e.Available {
			available = "true"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			e.Name, e.DisplayName, e.CLI, e.ConfigDir, available)
	}

	return tw.Flush()
}

// initialPrompter is an optional capability interface for vendors that support
// starting an interactive session with an initial prompt pre-loaded.
type initialPrompter interface {
	SupportsInitialPrompt() bool
}

// vendorEntries lists the registered vendor adapters, plus one extra row per
// (backend, vendor) pair declared in ~/.ynh/config.json's "backends" map.
// Backend rows use the same "<backend>/<vendor>" spec accepted by -v (see
// backend.ParseSpec) as their name — discoverable without guessing at
// installed models, which config deliberately doesn't store (the model is
// supplied live in the -v string, not pinned in config).
func vendorEntries() ([]vendorEntry, error) {
	entries := make([]vendorEntry, 0, len(vendor.Available()))
	for _, name := range vendor.Available() {
		adapter, err := vendor.Get(name)
		if err != nil {
			return nil, fmt.Errorf("loading vendor %s: %w", name, err)
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

	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}

	var backendNames []string
	for name := range cfg.Backends {
		backendNames = append(backendNames, name)
	}
	sort.Strings(backendNames)

	for _, backendName := range backendNames {
		var vendorNames []string
		for name := range cfg.Backends[backendName].Vendors {
			vendorNames = append(vendorNames, name)
		}
		sort.Strings(vendorNames)

		// Query the backend for installed models, if it declares a type ynh
		// knows how to ask (currently just "ollama"). This is best-effort:
		// an unreachable server or unrecognized type just falls back to a
		// bare "<backend>/<vendor>" row instead of failing the listing.
		models, _ := backend.ListModels(cfg, backendName)

		for _, vn := range vendorNames {
			adapter, err := vendor.Get(vn)
			if err != nil {
				// Config names a vendor ynh doesn't know; skip it rather
				// than failing the whole listing over one bad entry.
				continue
			}
			_, lookErr := exec.LookPath(adapter.CLIName())
			supportsIP := false
			if ip, ok := adapter.(initialPrompter); ok {
				supportsIP = ip.SupportsInitialPrompt()
			}

			if len(models) == 0 {
				entries = append(entries, vendorEntry{
					Name:                  backendName + "/" + vn,
					DisplayName:           fmt.Sprintf("%s (%s)", adapter.DisplayName(), backendName),
					CLI:                   adapter.CLIName(),
					ConfigDir:             adapter.ConfigDir(),
					Available:             lookErr == nil,
					SupportsInitialPrompt: supportsIP,
				})
				continue
			}

			for _, model := range models {
				entries = append(entries, vendorEntry{
					Name:                  backendName + "/" + vn + "/" + model,
					DisplayName:           fmt.Sprintf("%s (%s · %s)", adapter.DisplayName(), backendName, model),
					CLI:                   adapter.CLIName(),
					ConfigDir:             adapter.ConfigDir(),
					Available:             lookErr == nil,
					SupportsInitialPrompt: supportsIP,
				})
			}
		}
	}

	return entries, nil
}

func printVendorsJSON(w io.Writer) error {
	entries, err := vendorEntries()
	if err != nil {
		return err
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
