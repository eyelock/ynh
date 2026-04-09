package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/exporter"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/resolver"
	"github.com/eyelock/ynh/internal/vendor"
)

func cmdExport(args []string) error {
	var (
		outputDir   string
		vendors     string
		subPath     string
		profileName string
		focusName   string
		clean       bool
		merged      bool
		source      string
	)

	// Parse flags
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
		case "--path":
			if i+1 >= len(args) {
				return fmt.Errorf("--path requires a value")
			}
			i++
			subPath = args[i]
		case "--profile":
			if i+1 >= len(args) {
				return fmt.Errorf("--profile requires a value")
			}
			i++
			profileName = args[i]
		case "--focus":
			if i+1 >= len(args) {
				return fmt.Errorf("--focus requires a value")
			}
			i++
			focusName = args[i]
		case "--clean":
			clean = true
		case "--merged":
			merged = true
		case "--harness":
			if i+1 >= len(args) {
				return fmt.Errorf("--harness requires a value")
			}
			i++
			source = args[i]
		case "-h", "--help":
			return errHelp
		default:
			if strings.HasPrefix(args[i], "-") {
				return fmt.Errorf("unknown flag: %s", args[i])
			}
			if source != "" {
				return fmt.Errorf("unexpected argument: %s", args[i])
			}
			source = args[i]
		}
		i++
	}

	// Resolve source: --harness flag > YNH_HARNESS > positional > error
	if source == "" {
		source = resolveHarnessEnv()
	}
	if source == "" {
		return fmt.Errorf("usage: ynd export <harness-dir|git-url> [--harness dir] [flags]")
	}

	// Resolve vendor from env var if no flag
	if vendors == "" {
		vendors = resolveVendorEnv()
	}

	// Resolve source to local path
	srcDir, err := resolveSource(source)
	if err != nil {
		return err
	}

	// Apply --path scoping
	if subPath != "" {
		srcDir = filepath.Join(srcDir, subPath)
		if _, err := os.Stat(srcDir); os.IsNotExist(err) {
			return fmt.Errorf("path %q not found in source", subPath)
		}
	}

	// Determine output directory
	if outputDir == "" {
		pj, err := plugin.LoadHarnessJSON(srcDir)
		if err != nil {
			return fmt.Errorf("loading .harness.json for name: %w", err)
		}
		outputDir = filepath.Join(".", "dist", pj.Name)
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

	// Parse vendor list
	var vendorList []string
	if vendors != "" {
		vendorList = strings.Split(vendors, ",")
		for _, v := range vendorList {
			if _, err := vendor.Get(strings.TrimSpace(v)); err != nil {
				return err
			}
		}
	}

	// Load config for remote source checking
	cfg, err := config.Load()
	if err != nil {
		cfg = &config.Config{}
	}

	mode := exporter.ModePerVendor
	if merged {
		mode = exporter.ModeMerged
	}

	// Resolve focus from flag or env var
	if focusName == "" {
		focusName = os.Getenv("YNH_FOCUS")
	}
	if focusName != "" && profileName != "" {
		return fmt.Errorf("cannot use --focus and --profile together")
	}

	// Resolve profile from flag or env var
	if profileName == "" {
		profileName = os.Getenv("YNH_PROFILE")
	}
	if focusName != "" && profileName != "" {
		return fmt.Errorf("cannot use --focus and --profile together (focus includes a profile)")
	}

	// Resolve focus → profile
	if focusName != "" {
		h, _, loadErr := loadHarnessForPreview(srcDir)
		if loadErr != nil {
			return fmt.Errorf("loading harness for focus resolution: %w", loadErr)
		}
		focus, ok := h.Focuses[focusName]
		if !ok {
			return fmt.Errorf("focus %q not defined in harness", focusName)
		}
		if focus.Profile != "" {
			profileName = focus.Profile
		}
	}

	results, err := exporter.Export(exporter.ExportOptions{
		SourceDir: srcDir,
		OutputDir: outputDir,
		Vendors:   vendorList,
		Mode:      mode,
		Config:    cfg,
		Profile:   profileName,
	})
	if err != nil {
		return err
	}

	// Print results
	for _, r := range results {
		fmt.Printf("Exported for %s → %s (%d skills, %d agents)\n", r.Vendor, r.OutputDir, r.Skills, r.Agents)
		for _, w := range r.Warnings {
			fmt.Printf("  warning: %s\n", w)
		}
	}

	return nil
}

// resolveSource determines if source is a local path or Git URL and returns
// the local directory path. For Git URLs, it resolves via the shared cache.
func resolveSource(source string) (string, error) {
	// Local path
	if strings.HasPrefix(source, ".") || strings.HasPrefix(source, "/") {
		abs, err := filepath.Abs(source)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(abs); os.IsNotExist(err) {
			return "", fmt.Errorf("source path not found: %s", abs)
		}
		return abs, nil
	}

	// Check if it exists as a local path anyway
	if _, err := os.Stat(source); err == nil {
		abs, err := filepath.Abs(source)
		if err != nil {
			return "", err
		}
		return abs, nil
	}

	// Git URL — resolve via cache
	result, err := resolver.EnsureRepo(source, "")
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", source, err)
	}
	return result.Path, nil
}
