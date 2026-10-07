package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/exporter"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/migration"
	"github.com/eyelock/ynh/internal/namespace"
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
		skipConfirm bool
		merged      bool
		format      string
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
		case "-y", "--yes":
			skipConfirm = true
		case "--merged":
			merged = true
		case "--format":
			if i+1 >= len(args) {
				return fmt.Errorf("--format requires a value")
			}
			i++
			format = args[i]
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

	// The layout is decided before anything else is read, so a bad
	// combination is refused with nothing created (#451).
	mode := exporter.ModePerVendor
	if merged {
		mode = exporter.ModeMerged
	}
	switch format {
	case "", "vendor":
	case "agent-plugin":
		if merged {
			return fmt.Errorf("--merged and --format agent-plugin are different layouts; choose one")
		}
		mode = exporter.ModeAgentPlugin
	default:
		return fmt.Errorf("unknown --format %q (vendor, agent-plugin)", format)
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

	// Determine output directory. The format chain never rewrites srcDir: it
	// refuses a legacy manifest with the fix (#406).
	if outputDir == "" {
		if _, err := migration.FormatChain().Run(srcDir); err != nil {
			return err
		}
		p, err := harness.LoadDir(srcDir)
		if err != nil {
			return fmt.Errorf("loading harness for name: %w", err)
		}
		outputDir = filepath.Join(".", "dist", p.Name)
	}

	// Load config for remote source checking
	cfg, err := config.Load()
	if err != nil {
		cfg = &config.Config{}
	}

	results, err := exporter.Export(exporter.ExportOptions{
		SourceDir: srcDir,
		OutputDir: outputDir,
		Vendors:   vendorList,
		Mode:      mode,
		Config:    cfg,
		Profile:   profileName,
		// Nothing above writes. Export creates the output, and runs --clean,
		// only once the source has loaded, so a refused export leaves -o
		// exactly as it found it (#451).
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

	// Print results
	for _, r := range results {
		if r.Vendor == exporter.AgentPluginVendor {
			fmt.Printf("Exported Agent Plugin → %s (%d skills, %d agents)\n", r.OutputDir, r.Skills, r.Agents)
		} else {
			fmt.Printf("Exported for %s → %s (%d skills, %d agents)\n", r.Vendor, r.OutputDir, r.Skills, r.Agents)
		}
		for _, w := range r.Warnings {
			fmt.Printf("  warning: %s\n", w)
		}
	}

	return nil
}

// resolveSource determines whether source is a local path, an installed
// harness canonical id, or a Git URL, and returns the local directory
// path. Canonical ids (e.g. "local/<name>" or "github.com/<org>/<repo>/<name>")
// are resolved against the user's installed harnesses via harness.LoadByID,
// so consumers driving ynd off `ynh ls --format json` ids work without
// converting them back to filesystem paths. Bare names and other invalid
// ref shapes fall through to the Git-URL path, preserving prior behaviour.
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

	// Installed harness id (canonical ref). When the input lexically looks
	// like a canonical id, try the installed-harness lookup first so
	// commands like `ynd compose local/foo` resolve to the on-disk install
	// instead of attempting a git clone. We only attempt this when the
	// lookup succeeds; on miss we fall through to the git-URL path, since
	// a canonical-shaped string that isn't installed is still a valid
	// upstream reference to clone (e.g. github.com/org/repo/sub).
	if namespace.Classify(source) == namespace.RefID {
		if p, err := harness.LoadByID(source); err == nil {
			return p.Dir, nil
		}
	}

	// Git URL — resolve via cache
	result, err := resolver.EnsureRepo(source, "")
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", source, err)
	}
	return result.Path, nil
}
