package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/marketplace"
	"github.com/eyelock/ynh/internal/vendor"
)

func cmdMarketplace(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ynd marketplace build [config-file] [flags]")
	}

	switch args[0] {
	case "build":
		return cmdMarketplaceBuild(args[1:])
	case "-h", "--help", "help":
		return errHelp
	default:
		return fmt.Errorf("unknown marketplace subcommand: %s\nusage: ynd marketplace build [config-file] [flags]", args[0])
	}
}

func cmdMarketplaceBuild(args []string) error {
	var (
		outputDir  string
		vendors    string
		clean      bool
		configFile string
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

	// Default config file
	if configFile == "" {
		configFile = "marketplace.json"
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
			if trimmed == "codex" {
				continue // silently skip codex for marketplace
			}
			if _, err := vendor.Get(trimmed); err != nil {
				return err
			}
		}
	}

	// Handle --clean
	if clean {
		if err := os.RemoveAll(outputDir); err != nil {
			return fmt.Errorf("cleaning output dir: %w", err)
		}
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("creating output dir: %w", err)
	}

	// Load global config for remote source checking
	globalCfg, err := config.Load()
	if err != nil {
		globalCfg = &config.Config{}
	}

	configDir, err := filepath.Abs(filepath.Dir(configFile))
	if err != nil {
		return err
	}

	err = marketplace.Build(cfg, marketplace.BuildOptions{
		ConfigDir: configDir,
		OutputDir: outputDir,
		Vendors:   vendorList,
		Config:    globalCfg,
	})
	if err != nil {
		return err
	}

	fmt.Printf("Marketplace built → %s (%d plugins)\n", outputDir, len(cfg.Harnesses))
	return nil
}
