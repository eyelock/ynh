package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/marketplace"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/vendor"
	"github.com/eyelock/ynh/internal/warn"
)

func cmdMarketplace(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ynd marketplace build [config-file] [flags]")
	}

	switch args[0] {
	case "build":
		return cmdMarketplaceBuild(args[1:], os.Stderr)
	case "-h", "--help", "help":
		return errHelp
	default:
		return fmt.Errorf("unknown marketplace subcommand: %s\nusage: ynd marketplace build [config-file] [flags]", args[0])
	}
}

// cmdMarketplaceBuild builds a marketplace and writes each entry's export
// warnings to stderr.
func cmdMarketplaceBuild(args []string, stderr io.Writer) error {
	var (
		outputDir   string
		vendors     string
		clean       bool
		skipConfirm bool
		configFile  string
		format      string
	)

	i := 0
	for i < len(args) {
		switch args[i] {
		case "-o", "--output":
			if i+1 >= len(args) {
				return fmt.Errorf("%s requires a value", args[i])
			}
			i++
			outputDir = args[i]
		case "-v", "--vendor":
			if i+1 >= len(args) {
				return fmt.Errorf("%s requires a value", args[i])
			}
			i++
			vendors = args[i]
		case "--clean":
			clean = true
		case "-y", "--yes":
			skipConfirm = true
		case "--format":
			if i+1 >= len(args) {
				return fmt.Errorf("--format requires a value")
			}
			i++
			format = args[i]
		case "-h", "--help":
			return errHelp
		default:
			if strings.HasPrefix(args[i], "-") {
				return fmt.Errorf("unknown flag: %s", args[i])
			}
			if configFile != "" {
				return fmt.Errorf("unexpected argument: %s", args[i])
			}
			configFile = args[i]
		}
		i++
	}

	// The registry index is also the build input (ynh #395): with no argument
	// the manifest directory's index is read when there is one, and an explicit
	// path to a registry index works too. Either writes each vendor's index in
	// place and builds no content. Anything else is the old build config.
	if configFile == "" && plugin.IsRegistryDir(".") {
		configFile = plugin.MarketplacePath(".")
	}
	if configFile != "" && plugin.InManifestDir(configFile) {
		if clean || format != "" {
			return fmt.Errorf("--clean and --format apply to a build that makes content; a registry index is written in place, beside its repository root's vendor directories")
		}
		return buildFromRegistryIndex(configFile, outputDir, vendors, stderr)
	}
	// Default config file
	if configFile == "" {
		configFile = "marketplace.json"
	}
	var agentPlugin bool
	switch format {
	case "", "vendor":
	case "agent-plugin":
		agentPlugin = true
	default:
		return fmt.Errorf("unknown --format %q (vendor, agent-plugin)", format)
	}

	// Load marketplace config
	cfg, err := marketplace.LoadConfig(configFile)
	if err != nil {
		return err
	}

	// Default output dir
	if outputDir == "" {
		outputDir = filepath.Join(".", "dist")
	}

	// Resolve vendor from env var if no flag
	if vendors == "" {
		vendors = resolveVendorEnv()
	}

	// Parse vendor list
	var vendorList []string
	if vendors != "" {
		vendorList = strings.Split(vendors, ",")
		for _, v := range vendorList {
			trimmed := strings.TrimSpace(v)
			if _, err := vendor.Get(trimmed); err != nil {
				return err
			}
		}
	}

	// Load global config for remote source checking. config.Load already
	// returns an empty config for an absent file, so an error here means the
	// file exists and is malformed, worth failing on immediately rather than
	// surfacing several steps later as a confusing resolution failure.
	globalCfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading global config: %w", err)
	}

	configDir, err := filepath.Abs(filepath.Dir(configFile))
	if err != nil {
		return err
	}

	warnings, err := marketplace.Build(cfg, marketplace.BuildOptions{
		ConfigDir:   configDir,
		OutputDir:   outputDir,
		Vendors:     vendorList,
		Config:      globalCfg,
		AgentPlugin: agentPlugin,
		// --clean runs only once Build has checked every entry. Nothing
		// before Build writes, and Build creates the output only after this,
		// so a refused build leaves -o exactly as it found it (#451).
		BeforeWrite: func() error {
			if !clean {
				return nil
			}
			return cleanOutputDir(outputDir, skipConfirm || skipConfirmEnv())
		},
	})
	if err != nil {
		return err
	}

	warn.Print(stderr, warnings...)
	fmt.Printf("Marketplace built → %s (%d plugins)\n", outputDir, len(cfg.Harnesses))
	return nil
}

// buildFromRegistryIndex writes each vendor's marketplace index from the
// registry index at path, in place under the repository root (the folder that
// holds the manifest directory) unless -o names another.
func buildFromRegistryIndex(path, outputDir, vendors string, stderr io.Writer) error {
	idx, err := marketplace.LoadRegistryIndex(path)
	if err != nil {
		return err
	}
	if outputDir == "" {
		outputDir = plugin.HarnessRoot(path)
	}
	if vendors == "" {
		vendors = resolveVendorEnv()
	}
	var vendorList []string
	if vendors != "" {
		for _, v := range strings.Split(vendors, ",") {
			v = strings.TrimSpace(v)
			if _, err := vendor.Get(v); err != nil {
				return err
			}
			vendorList = append(vendorList, v)
		}
	}
	written, err := marketplace.BuildFromRegistryIndex(idx, marketplace.RegistryBuildOptions{OutputDir: outputDir, Vendors: vendorList})
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stderr, "Wrote %d vendor index(es) from %s under %s:\n", len(written), path, outputDir)
	for _, w := range written {
		_, _ = fmt.Fprintf(stderr, "  %s\n", w)
	}
	return nil
}
