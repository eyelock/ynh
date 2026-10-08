package main

import (
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/eyelock/ynh/internal/agentplugin"
	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/resolver"
	"github.com/eyelock/ynh/internal/vendor"
)

func cmdPreview(args []string) error {
	var (
		vendorName string
		outputDir  string
		profiles   []string
		focusName  string
		source     string
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
			profiles = append(profiles, args[i])
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
	sel, err := resolveSelection(srcDir, profiles, focusName)
	if err != nil {
		return err
	}

	// Assemble into temp dir
	tmpDir, report, err := assembleForVendorSources(srcDir, vendorName, sel)
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
	printMCPSources(os.Stdout, report.mcp)
	printDelegateMCP(os.Stdout, report.delegate)
	printIncludedSelectables(os.Stdout, report.included)
	printIncludedHooks(os.Stdout, report.included)

	return nil
}

// printIncludedHooks lists the hooks that came from included harnesses, each
// with the include it came from, and those that did not because the include
// does not say "hooks": true. The not-active ones are what the warning on
// stderr is about.
func printIncludedHooks(w io.Writer, included []resolver.ResolveResult) {
	hs, err := assembler.ComposeHooks(nil, included)
	if err != nil {
		return
	}
	for _, list := range []struct {
		title   string
		origins []assembler.HookOrigin
	}{
		{"Hooks from included harnesses:", hs.FromIncludes},
		{"Hooks from included harnesses, not active (add \"hooks\": true to the include):", hs.NotActive},
	} {
		if len(list.origins) == 0 {
			continue
		}
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, list.title)
		for _, o := range list.origins {
			_, _ = fmt.Fprintf(w, "  %s (from %s)\n", o.Event, o.Source)
		}
	}
}

// printMCPSources lists the MCP servers that came from an included harness,
// each with where it came from. Servers the root declares are not listed: the
// MCP file already shows them, and there is nothing to attribute.
func printMCPSources(w io.Writer, sources []harness.MCPProvenance) {
	var lines []string
	for _, s := range sources {
		if s.Source != harness.MCPSourceRoot {
			lines = append(lines, fmt.Sprintf("  %s (from %s)", s.Server, s.Source))
		}
	}
	if len(lines) == 0 {
		return
	}
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "MCP servers from included harnesses:")
	for _, l := range lines {
		_, _ = fmt.Fprintln(w, l)
	}
}

// printDelegateMCP lists the MCP servers each delegate declares, with where
// the delegate got them. They are not the session's servers: the vendor
// connects them to the delegate's subagent alone.
func printDelegateMCP(w io.Writer, servers []assembler.DelegateMCP) {
	if len(servers) == 0 {
		return
	}
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "MCP servers of delegates:")
	for _, s := range servers {
		source := "its own"
		if s.Source != harness.MCPSourceRoot {
			source = "from " + s.Source
		}
		_, _ = fmt.Fprintf(w, "  %s: %s (%s)\n", s.Delegate, s.Server, source)
	}
}

// printIncludedSelectables lists the focuses and profiles of the included
// harnesses under the namespaced names --focus and --profile take.
func printIncludedSelectables(w io.Writer, included []resolver.ResolveResult) {
	var focuses, profiles []string
	for _, r := range included {
		if r.Harness == nil {
			continue
		}
		for _, name := range slices.Sorted(maps.Keys(r.Harness.Focuses)) {
			focuses = append(focuses, r.Namespace+":"+name)
		}
		for _, name := range slices.Sorted(maps.Keys(r.Harness.Profiles)) {
			profiles = append(profiles, r.Namespace+":"+name)
		}
	}
	for _, list := range []struct {
		title string
		names []string
	}{
		{"Focuses from included harnesses:", focuses},
		{"Profiles from included harnesses:", profiles},
	} {
		if len(list.names) == 0 {
			continue
		}
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, list.title)
		for _, n := range list.names {
			_, _ = fmt.Fprintf(w, "  %s\n", n)
		}
	}
}

// previewReport is what assembling a preview learned besides the files: where
// each MCP server came from, and the harnesses the includes resolved to.
type previewReport struct {
	mcp      []harness.MCPProvenance
	delegate []assembler.DelegateMCP
	included []resolver.ResolveResult
}

// assembleForVendor is assembleForVendorSources without the report.
func assembleForVendor(srcDir string, vendorName string, sel harness.Selection) (string, error) {
	dir, _, err := assembleForVendorSources(srcDir, vendorName, sel)
	return dir, err
}

// assembleForVendorSources loads a harness from srcDir and assembles vendor-native
// output into a temp directory. Returns the temp dir path (caller must clean up).
// It also returns where each MCP server came from and the included harnesses.
func assembleForVendorSources(srcDir string, vendorName string, sel harness.Selection) (string, previewReport, error) {
	adapter, err := vendor.Get(vendorName)
	if err != nil {
		return "", previewReport{}, err
	}

	// Load harness — handle bare AGENTS.md by working on a temp copy
	h, workDir, err := loadHarnessForPreview(srcDir)
	if err != nil {
		return "", previewReport{}, fmt.Errorf("loading harness: %w", err)
	}
	if workDir != "" {
		defer func() { _ = os.RemoveAll(workDir) }()
		srcDir = workDir
	}

	// Apply profile if specified
	if sel.Profile != "" {
		h, err = harness.ResolveProfile(h, sel.Profile)
		if err != nil {
			return "", previewReport{}, err
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
			if err := cfg.CheckSource(del.Git, h.Dir); err != nil {
				return "", previewReport{}, fmt.Errorf("delegate %q: %w", del.Git, err)
			}
		}
	}

	// Resolve includes
	resolved, _, err := resolver.ResolveSelected(h, cfg, sel)
	if err != nil {
		return "", previewReport{}, fmt.Errorf("resolving includes: %w", err)
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
		return "", previewReport{}, fmt.Errorf("creating temp dir: %w", err)
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
		return "", previewReport{}, fmt.Errorf("assembling: %w", err)
	}

	// Assemble delegates
	delegateMCP, err := assembler.AssembleDelegates(tmpDir, adapter, h.DelegatesTo, h.Dir, assembler.DelegateOptions{Config: cfg})
	if err != nil {
		return "", previewReport{}, fmt.Errorf("assembling delegates: %w", err)
	}

	// Generate hook config, and copy in the scripts those hooks run
	hookWarnings, err := assembler.WriteComposedSessionHooks(tmpDir, adapter, h, resolved)
	if err != nil {
		return "", previewReport{}, err
	}
	for _, w := range hookWarnings {
		fmt.Fprintf(os.Stderr, "  warning: %s\n", w)
	}

	// Generate MCP config: the harness's own servers and those of its included
	// harnesses, composed. Same client-side resolution as ynh run, against the
	// same data directory, so the preview shows what a run would write.
	// Preview does not create the directory: it launches nothing.
	servers, mcpSources, expErr := harness.ComposeMCPServers(h, resolver.IncludedHarnesses(resolved), harness.PluginDataDir(h), os.LookupEnv)
	if expErr != nil {
		return "", previewReport{}, expErr
	}
	if len(servers) > 0 {
		mcpFiles, err := adapter.GenerateMCPConfig(servers)
		if err != nil {
			return "", previewReport{}, fmt.Errorf("generating MCP config: %w", err)
		}
		if err := writeGeneratedFiles(tmpDir, mcpFiles); err != nil {
			return "", previewReport{}, fmt.Errorf("writing MCP config: %w", err)
		}
	}

	// Generate vendor plugin manifest (after hooks/MCP so path pointers are accurate)
	// The real manifest, not a synthesized one. h.Version is populated and
	// correct here; hardcoding "0.0.0" made this path disagree with
	// `ynd export` and with what actually ships, for the same harness.
	pj := &plugin.HarnessJSON{
		Name:        h.Name,
		Version:     h.Version,
		Description: h.Description,
		Author:      h.Author,
		Keywords:    h.Keywords,
	}
	manifestFiles, err := adapter.GeneratePluginManifest(pj, tmpDir)
	if err != nil {
		return "", previewReport{}, fmt.Errorf("writing plugin manifest: %w", err)
	}
	if err := writeGeneratedFiles(tmpDir, manifestFiles); err != nil {
		return "", previewReport{}, fmt.Errorf("writing plugin manifest: %w", err)
	}

	success = true
	return tmpDir, previewReport{mcp: mcpSources, delegate: delegateMCP, included: resolved}, nil
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
// and synthesizes plugin.json there. Returns (harness, tempDir, error).
// If tempDir is non-empty, the caller must clean it up.
func loadHarnessForPreview(dir string) (*harness.Harness, string, error) {
	format, err := harness.DetectFormat(dir)
	if err != nil {
		return nil, "", err
	}
	switch format {
	case "plugin", agentplugin.Format:
		h, err := harness.LoadDir(dir)
		return h, "", err
	case "legacy":
		return nil, "", fmt.Errorf("legacy format detected in %q. Migrate to .agents/harness/plugin.json", dir)
	}

	// No manifest: synthesize from AGENTS.md / instructions.md if present
	if assembler.FindInstructionsFile(dir) == "" {
		return nil, "", fmt.Errorf("directory %q has no harness manifest or AGENTS.md", dir)
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
