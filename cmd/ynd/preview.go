package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/resolver"
	"github.com/eyelock/ynh/internal/vendor"
)

func cmdPreview(args []string) error {
	var (
		vendorName  string
		outputDir   string
		profileName string
		focusName   string
		source      string
	)

	// Parse flags
	i := 0
	for i < len(args) {
		switch args[i] {
		case "-v", "--vendor":
			if i+1 >= len(args) {
				return fmt.Errorf("%s requires a value", args[i])
			}
			i++
			vendorName = args[i]
		case "-o", "--output":
			if i+1 >= len(args) {
				return fmt.Errorf("%s requires a value", args[i])
			}
			i++
			outputDir = args[i]
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
		return fmt.Errorf("usage: ynd preview <harness-dir> [--harness dir] [-v vendor] [-o output-dir] [--profile name]")
	}

	// Resolve vendor: -v flag > YNH_VENDOR > default
	vendorName = resolveVendorDefault(vendorName)

	// Resolve source to local path
	srcDir, err := resolveSource(source)
	if err != nil {
		return err
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

	// Resolve focus → profile (must load harness first to look up focus entry)
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

	// Assemble into temp dir
	tmpDir, err := assembleForVendor(srcDir, vendorName, profileName)
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Output
	if outputDir != "" {
		// Copy tmpDir to outputDir
		if err := os.MkdirAll(outputDir, 0o755); err != nil {
			return fmt.Errorf("creating output dir: %w", err)
		}
		if err := assembler.CopyDir(tmpDir, outputDir); err != nil {
			return fmt.Errorf("copying to output: %w", err)
		}
		fmt.Printf("Preview written to %s\n", outputDir)
	} else {
		// Print tree with file contents to stdout
		if err := printTree(tmpDir, ""); err != nil {
			return fmt.Errorf("printing tree: %w", err)
		}
	}

	return nil
}

// assembleForVendor loads a harness from srcDir and assembles vendor-native
// output into a temp directory. Returns the temp dir path (caller must clean up).
func assembleForVendor(srcDir string, vendorName string, profileName string) (string, error) {
	adapter, err := vendor.Get(vendorName)
	if err != nil {
		return "", err
	}

	// Load harness — handle bare AGENTS.md by working on a temp copy
	h, workDir, err := loadHarnessForPreview(srcDir)
	if err != nil {
		return "", fmt.Errorf("loading harness: %w", err)
	}
	if workDir != "" {
		defer func() { _ = os.RemoveAll(workDir) }()
		srcDir = workDir
	}

	// Apply profile if specified
	if profileName != "" {
		h, err = harness.ResolveProfile(h, profileName)
		if err != nil {
			return "", err
		}
	}

	// Load config for remote source checking
	cfg, err := config.Load()
	if err != nil {
		cfg = &config.Config{}
	}

	// Check remote sources for delegates
	if cfg != nil {
		for _, del := range h.DelegatesTo {
			if err := cfg.CheckRemoteSource(del.Git); err != nil {
				return "", fmt.Errorf("delegate %q: %w", del.Git, err)
			}
		}
	}

	// Resolve includes
	resolved, err := resolver.Resolve(h, cfg)
	if err != nil {
		return "", fmt.Errorf("resolving includes: %w", err)
	}

	// Build content list
	var content []resolver.ResolvedContent
	for _, r := range resolved {
		content = append(content, r.Content)
	}
	content = append(content, resolver.ResolvedContent{
		BasePath: srcDir,
	})

	// Create temp dir for assembly
	tmpDir, err := os.MkdirTemp("", "ynd-preview-*")
	if err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}

	// Clean up on failure
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(tmpDir)
		}
	}()

	// Assemble artifacts
	if err := assembler.AssembleTo(tmpDir, adapter, content); err != nil {
		return "", fmt.Errorf("assembling: %w", err)
	}

	// Assemble delegates
	if err := assembler.AssembleDelegates(tmpDir, adapter, h.DelegatesTo); err != nil {
		return "", fmt.Errorf("assembling delegates: %w", err)
	}

	// Generate hook config
	if len(h.Hooks) > 0 {
		hookFiles, err := adapter.GenerateHookConfig(h.Hooks)
		if err != nil {
			return "", fmt.Errorf("generating hook config: %w", err)
		}
		if err := writeGeneratedFiles(tmpDir, hookFiles); err != nil {
			return "", fmt.Errorf("writing hook config: %w", err)
		}
	}

	// Generate MCP config
	if len(h.MCPServers) > 0 {
		mcpFiles, err := adapter.GenerateMCPConfig(h.MCPServers)
		if err != nil {
			return "", fmt.Errorf("generating MCP config: %w", err)
		}
		if err := writeGeneratedFiles(tmpDir, mcpFiles); err != nil {
			return "", fmt.Errorf("writing MCP config: %w", err)
		}
	}

	// Generate vendor plugin manifest (after hooks/MCP so path pointers are accurate)
	pj := &plugin.HarnessJSON{Name: h.Name, Version: "0.0.0", Description: h.Description}
	manifestFiles, err := adapter.GeneratePluginManifest(pj, tmpDir)
	if err != nil {
		return "", fmt.Errorf("writing plugin manifest: %w", err)
	}
	if err := writeGeneratedFiles(tmpDir, manifestFiles); err != nil {
		return "", fmt.Errorf("writing plugin manifest: %w", err)
	}

	success = true
	return tmpDir, nil
}

// writeGeneratedFiles writes a map of relative paths to file contents into baseDir.
func writeGeneratedFiles(baseDir string, files map[string][]byte) error {
	for relPath, data := range files {
		absPath := filepath.Join(baseDir, relPath)
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(absPath, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// loadHarnessForPreview loads a harness without mutating the source directory.
// If the source is a bare AGENTS.md directory, it copies to a temp dir first
// and synthesizes harness.json there. Returns (harness, tempDir, error).
// If tempDir is non-empty, the caller must clean it up.
func loadHarnessForPreview(dir string) (*harness.Harness, string, error) {
	switch harness.DetectFormat(dir) {
	case "plugin", "harness":
		h, err := harness.LoadDir(dir)
		return h, "", err
	case "legacy":
		return nil, "", fmt.Errorf("legacy format detected in %q. Migrate to .ynh-plugin/plugin.json", dir)
	}

	// Check for bare AGENTS.md or instructions.md
	hasInstructions := assembler.FindInstructionsFile(dir) != ""
	if !hasInstructions {
		return nil, "", fmt.Errorf("directory %q has no .ynh-plugin/plugin.json, .harness.json, or AGENTS.md", dir)
	}

	// Copy to temp dir to avoid mutating source
	tmpDir, err := os.MkdirTemp("", "ynd-synth-*")
	if err != nil {
		return nil, "", fmt.Errorf("creating temp dir: %w", err)
	}

	// Clean up on failure
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(tmpDir)
		}
	}()

	if err := assembler.CopyDir(dir, tmpDir); err != nil {
		return nil, "", fmt.Errorf("copying source: %w", err)
	}

	// Synthesize minimal plugin.json
	name := filepath.Base(dir)
	hj := &plugin.HarnessJSON{
		Name:    name,
		Version: "0.0.0",
	}
	if err := plugin.SavePluginJSON(tmpDir, hj); err != nil {
		return nil, "", fmt.Errorf("writing synthesized plugin.json: %w", err)
	}

	h, err := harness.LoadDir(tmpDir)
	if err != nil {
		return nil, "", err
	}

	success = true
	return h, tmpDir, nil
}

// printTree walks a directory and prints a formatted tree with file contents.
func printTree(root string, prefix string) error {
	return printTreeDir(root, root, prefix)
}

func printTreeDir(root string, dir string, prefix string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	// Sort: directories first, then files
	var dirs, files []fs.DirEntry
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e)
		} else {
			files = append(files, e)
		}
	}

	// Print directories first
	for _, d := range dirs {
		fmt.Printf("%s%s/\n", prefix, d.Name())
		if err := printTreeDir(root, filepath.Join(dir, d.Name()), prefix+"  "); err != nil {
			return err
		}
	}

	// Print files with content
	for _, f := range files {
		fmt.Printf("%s%s\n", prefix, f.Name())
		filePath := filepath.Join(dir, f.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}

		contentPrefix := prefix + "  "
		lines := strings.Split(string(data), "\n")
		// Remove trailing empty line from Split
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}

		maxLines := 100
		if len(lines) <= maxLines {
			for _, line := range lines {
				fmt.Printf("%s%s\n", contentPrefix, line)
			}
		} else {
			for _, line := range lines[:maxLines] {
				fmt.Printf("%s%s\n", contentPrefix, line)
			}
			fmt.Printf("%s[... %d more lines]\n", contentPrefix, len(lines)-maxLines)
		}
	}

	return nil
}
