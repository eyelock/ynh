package exporter

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/resolver"
	"github.com/eyelock/ynh/internal/vendor"
)

// VendorExporter describes the vendor capabilities that the exporter needs.
// Consumers define their own narrow interface rather than depending on the
// full vendor.Adapter.
type VendorExporter interface {
	// ArtifactDirs maps artifact types to their directory names.
	ArtifactDirs() map[string]string
	// ExportArtifactDirs returns restricted artifact dirs for export, or nil to use ArtifactDirs().
	ExportArtifactDirs() map[string]string
	// SupportsExportDelegates reports whether this vendor supports delegates in exports.
	SupportsExportDelegates() bool
	// GenerateSystemPrompt produces vendor-native instruction files.
	GenerateSystemPrompt(content []byte) map[string][]byte
	// GeneratePluginManifest produces vendor-native plugin manifest files.
	GeneratePluginManifest(hj *plugin.HarnessJSON, outputDir string) (map[string][]byte, error)
	// GenerateHookConfig translates canonical hooks to vendor-native config.
	GenerateHookConfig(hooks map[string][]plugin.HookEntry) (map[string][]byte, error)
	// GenerateMCPConfig translates MCP servers to vendor-native config.
	GenerateMCPConfig(servers map[string]plugin.MCPServer) (map[string][]byte, error)
}

// PluginHookGenerator is implemented by a vendor whose plugin package carries
// hooks at a different path from the one its project sessions read. Claude,
// Codex and Cursor all do: a session reads .claude/hooks/hooks.json,
// .codex/hooks.json or .cursor/hooks.json, while a plugin carries
// hooks/<vendor>.json, named by the vendor's manifest "hooks" field. An export
// is a plugin, so it uses this path when the vendor offers one, and
// GenerateHookConfig otherwise.
type PluginHookGenerator interface {
	GeneratePluginHookConfig(hooks map[string][]plugin.HookEntry) (map[string][]byte, error)
}

// PluginMCPGenerator is implemented by a vendor whose plugin package carries
// MCP servers at a different path from the one its project sessions read.
// Cursor: a project reads .cursor/mcp.json, a plugin mcp.json (#470). Copilot:
// a run dir carries .copilot/.mcp.json, a plugin .github/mcp.json (#471). An
// export is a plugin, so it uses this path when the vendor offers one, and
// GenerateMCPConfig otherwise.
type PluginMCPGenerator interface {
	GeneratePluginMCPConfig(servers map[string]plugin.MCPServer) (map[string][]byte, error)
}

// ExportManifestGenerator is implemented by a vendor whose run-dir layout puts
// its manifest somewhere other than the plugin root. Copilot is the case: a
// run dir nests the plugin under .copilot/, an export does not. An export
// asks for its own layout explicitly rather than having the vendor guess it
// from the files present (#471).
type ExportManifestGenerator interface {
	GenerateExportPluginManifest(hj *plugin.HarnessJSON, outputDir string) (map[string][]byte, error)
}

// PluginManifest returns a vendor's manifest files for an exported plugin
// rooted at outputDir: the export layout when the vendor distinguishes one,
// GeneratePluginManifest otherwise. Marketplace builds use it too.
func PluginManifest(adapter VendorExporter, hj *plugin.HarnessJSON, outputDir string) (map[string][]byte, error) {
	if eg, ok := adapter.(ExportManifestGenerator); ok {
		return eg.GenerateExportPluginManifest(hj, outputDir)
	}
	return adapter.GeneratePluginManifest(hj, outputDir)
}

// ExportMode controls the output layout.
type ExportMode int

const (
	// ModePerVendor creates separate dirs: output/claude/, output/cursor/, output/codex/
	ModePerVendor ExportMode = iota
	// ModeMerged creates a single dir with every selected vendor's manifest,
	// Codex included (for marketplace builds)
	ModeMerged
)

// ExportOptions configures an export operation.
type ExportOptions struct {
	// SourceDir is the harness source directory — always a local path.
	// For remote sources, the CLI resolves (clone + --path scoping) before calling Export.
	SourceDir string
	// OutputDir is where to write exported plugin(s).
	OutputDir string
	// Vendors lists target vendors (empty = all registered).
	Vendors []string
	// Mode controls per-vendor vs merged output.
	Mode ExportMode
	// Config provides remote source checking for includes and delegates.
	Config *config.Config
	// Profile selects a named configuration variant. Empty means no profile.
	Profile string
	// BeforeWrite, when set, runs once the source has loaded and its includes
	// have resolved, before anything is written. The CLI runs --clean here, so
	// a refused export does not empty the output directory first.
	BeforeWrite func() error
}

// ExportResult describes the output for one vendor.
type ExportResult struct {
	Vendor    string
	OutputDir string
	Skills    int
	Agents    int
	Warnings  []string
}

// Export produces vendor-native plugin directories from a harness source.
func Export(opts ExportOptions) ([]ExportResult, error) {
	// Load harness
	p, err := harness.LoadDir(opts.SourceDir)
	if err != nil {
		return nil, fmt.Errorf("loading harness: %w", err)
	}

	// Apply profile if specified
	if opts.Profile != "" {
		p, err = harness.ResolveProfile(p, opts.Profile)
		if err != nil {
			return nil, err
		}
	}

	// harness.LoadDir above ran the migration chain, so the manifest is at the new path.
	hj, err := plugin.LoadPluginJSON(opts.SourceDir)
	if err != nil {
		return nil, fmt.Errorf("loading plugin.json: %w", err)
	}

	// Check remote sources for all delegates
	if opts.Config != nil {
		for _, del := range p.DelegatesTo {
			if err := opts.Config.CheckSource(del.Git, p.Dir); err != nil {
				return nil, fmt.Errorf("delegate %q: %w", del.Git, err)
			}
		}
	}

	// Resolve all remote includes
	resolved, err := resolver.Resolve(p, opts.Config)
	if err != nil {
		return nil, fmt.Errorf("resolving includes: %w", err)
	}

	// Extract ResolvedContent for assembly
	var content []resolver.ResolvedContent
	for _, r := range resolved {
		content = append(content, r.Content)
	}

	// Add SourceDir as local content (harness's own embedded artifacts)
	content = append(content, resolver.ResolvedContent{
		BasePath: opts.SourceDir,
	})

	// Discover instructions.md (last one wins)
	instructionsPath := DiscoverInstructions(content)

	// Determine target vendors
	vendors := opts.Vendors
	if len(vendors) == 0 {
		vendors = vendor.Available()
	}

	if opts.BeforeWrite != nil {
		if err := opts.BeforeWrite(); err != nil {
			return nil, err
		}
	}

	if opts.Mode == ModeMerged {
		return exportMerged(opts, hj, p, content, instructionsPath, vendors)
	}
	return exportPerVendor(opts, hj, p, content, instructionsPath, vendors)
}

func exportPerVendor(opts ExportOptions, pj *plugin.HarnessJSON, p *harness.Harness, content []resolver.ResolvedContent, instructionsPath string, vendors []string) ([]ExportResult, error) {
	var results []ExportResult

	for _, v := range vendors {
		vendorDir := filepath.Join(opts.OutputDir, v)

		// Clean and recreate vendor subdir
		if err := os.RemoveAll(vendorDir); err != nil {
			return nil, fmt.Errorf("cleaning %s output: %w", v, err)
		}
		if err := os.MkdirAll(vendorDir, 0o755); err != nil {
			return nil, fmt.Errorf("creating %s output: %w", v, err)
		}

		result, err := exportForVendor(v, vendorDir, pj, p, content, instructionsPath)
		if err != nil {
			return nil, fmt.Errorf("exporting for %s: %w", v, err)
		}
		results = append(results, result)
	}

	return results, nil
}

func exportMerged(opts ExportOptions, pj *plugin.HarnessJSON, p *harness.Harness, content []resolver.ResolvedContent, instructionsPath string, vendors []string) ([]ExportResult, error) {
	outputDir := opts.OutputDir

	// Clean and recreate output dir
	if err := os.RemoveAll(outputDir); err != nil {
		return nil, fmt.Errorf("cleaning output: %w", err)
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating output: %w", err)
	}

	// Copy artifacts once (using standard artifact dirs), shared across all
	// vendors in this merged output — no per-vendor transform applies here,
	// since a rename for one vendor would corrupt the shared copy for others.
	artifactDirs := vendor.DefaultArtifactDirs()
	if err := copyContent(outputDir, content, artifactDirs, nil); err != nil {
		return nil, err
	}

	// Count artifacts
	skills := countDir(filepath.Join(outputDir, "skills"))
	agents := countDir(filepath.Join(outputDir, "agents"))

	// Hook config for each vendor, before the manifests: each vendor's
	// manifest names its hooks file only when the file is there. The scripts
	// those hooks run are copied once, since the vendors share the root.
	var warnings []string
	if len(p.Hooks) > 0 {
		wroteHooks := false
		for _, v := range vendors {
			adapter, err := vendor.Get(v)
			if err != nil {
				continue
			}
			wrote, err := writeHookConfig(outputDir, adapter, p.Hooks)
			if err != nil {
				return nil, fmt.Errorf("writing hook config for %s: %w", v, err)
			}
			wroteHooks = wroteHooks || wrote
		}
		if wroteHooks {
			w, err := copyHookScripts(p.Dir, outputDir, p.Hooks)
			if err != nil {
				return nil, err
			}
			warnings = w
		}
	}

	// Generate manifests and instructions for each vendor
	var results []ExportResult

	for _, v := range vendors {
		adapter, err := vendor.Get(v)
		if err != nil {
			continue
		}
		manifestFiles, err := PluginManifest(adapter, pj, outputDir)
		if err != nil {
			return nil, fmt.Errorf("generating %s manifest: %w", v, err)
		}
		if err := writeGeneratedFiles(outputDir, manifestFiles); err != nil {
			return nil, fmt.Errorf("writing %s manifest: %w", v, err)
		}
	}

	// Instructions
	if instructionsPath != "" {
		if err := WriteMergedSystemPrompt(instructionsPath, outputDir, vendors); err != nil {
			return nil, err
		}
	}

	// Delegates (Claude/Cursor only in merged mode)
	if len(p.DelegatesTo) > 0 {
		if err := ExportDelegates(outputDir, p.DelegatesTo, p.Dir); err != nil {
			return nil, fmt.Errorf("exporting delegates: %w", err)
		}
		// Recount agents after delegate generation
		agents = countDir(filepath.Join(outputDir, "agents"))
	}

	// MCP config for each vendor
	if len(p.MCPServers) > 0 {
		for _, v := range vendors {
			adapter, err := vendor.Get(v)
			if err != nil {
				continue
			}
			if err := writeMCPConfig(outputDir, adapter, p.MCPServers); err != nil {
				return nil, fmt.Errorf("writing MCP config for %s: %w", v, err)
			}
		}
	}

	results = append(results, ExportResult{
		Vendor:    "merged",
		OutputDir: outputDir,
		Skills:    skills,
		Agents:    agents,
		Warnings:  warnings,
	})

	return results, nil
}

func exportForVendor(vendorName string, outputDir string, pj *plugin.HarnessJSON, p *harness.Harness, content []resolver.ResolvedContent, instructionsPath string) (ExportResult, error) {
	result := ExportResult{
		Vendor:    vendorName,
		OutputDir: outputDir,
	}

	adapter, err := vendor.Get(vendorName)
	if err != nil {
		return result, err
	}

	// Determine artifact dirs — some vendors restrict what they support
	artifactDirs := adapter.ExportArtifactDirs()
	if artifactDirs == nil {
		artifactDirs = adapter.ArtifactDirs()
	}
	var transform assembler.ArtifactTransform
	if t, ok := adapter.(assembler.ArtifactTransformer); ok {
		transform = t.TransformArtifact
	}
	if err := copyContent(outputDir, content, artifactDirs, transform); err != nil {
		return result, err
	}

	// Warn about skipped artifact types when export uses a restricted set
	if exportDirs := adapter.ExportArtifactDirs(); exportDirs != nil {
		allDirs := adapter.ArtifactDirs()
		skippedCounts := map[string]int{}
		for artifactType := range allDirs {
			if _, ok := exportDirs[artifactType]; ok {
				continue
			}
			for _, rc := range content {
				skippedCounts[artifactType] += countDir(filepath.Join(rc.BasePath, artifactType))
			}
		}
		var parts []string
		for _, artifactType := range []string{"agents", "rules", "commands"} {
			if n := skippedCounts[artifactType]; n > 0 {
				parts = append(parts, fmt.Sprintf("%d %s", n, artifactType))
			}
		}
		if len(parts) > 0 {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: skipping %s (not supported)", vendorName, joinParts(parts)))
		}
		if len(p.DelegatesTo) > 0 && !adapter.SupportsExportDelegates() {
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s: skipping %d delegates (not supported)", vendorName, len(p.DelegatesTo)))
		}
	}

	// Instructions
	if instructionsPath != "" {
		if err := WriteSystemPrompt(instructionsPath, outputDir, adapter); err != nil {
			return result, err
		}
	}

	// Delegates
	if len(p.DelegatesTo) > 0 && adapter.SupportsExportDelegates() {
		if err := ExportDelegates(outputDir, p.DelegatesTo, p.Dir); err != nil {
			return result, fmt.Errorf("exporting delegates: %w", err)
		}
	}

	// Hook config, and the scripts its hooks run
	if len(p.Hooks) > 0 {
		wrote, err := writeHookConfig(outputDir, adapter, p.Hooks)
		if err != nil {
			return result, fmt.Errorf("writing hook config: %w", err)
		}
		if wrote {
			warnings, err := copyHookScripts(p.Dir, outputDir, p.Hooks)
			if err != nil {
				return result, err
			}
			result.Warnings = append(result.Warnings, warnings...)
		}
	}

	// MCP config
	if len(p.MCPServers) > 0 {
		if err := writeMCPConfig(outputDir, adapter, p.MCPServers); err != nil {
			return result, fmt.Errorf("writing MCP config: %w", err)
		}
	}

	// Manifest — generated after content (MCP, skills) so path pointers are accurate
	manifestFiles, err := PluginManifest(adapter, pj, outputDir)
	if err != nil {
		return result, fmt.Errorf("generating manifest: %w", err)
	}
	if err := writeGeneratedFiles(outputDir, manifestFiles); err != nil {
		return result, fmt.Errorf("writing manifest: %w", err)
	}

	result.Skills = countDir(filepath.Join(outputDir, "skills"))
	result.Agents = countDir(filepath.Join(outputDir, "agents"))
	return result, nil
}

// writeMCPConfig generates the vendor's plugin MCP config and writes it to the output directory.
//
// Note what this deliberately does not do: it does not expand ${VAR}
// references in MCP env values. Assembly for a local run resolves them
// (see plugin.ExpandMCPEnv), because the config is about to be used by this
// operator on this machine. An export is a distributable artifact, and
// resolving there would bake whoever ran the export's credentials into a
// bundle meant to be shared. References stay literal so the consumer resolves
// them from their own environment.
func writeMCPConfig(outputDir string, adapter VendorExporter, servers map[string]plugin.MCPServer) error {
	generate := adapter.GenerateMCPConfig
	if pg, ok := adapter.(PluginMCPGenerator); ok {
		generate = pg.GeneratePluginMCPConfig
	}
	mcpFiles, err := generate(servers)
	if err != nil {
		return fmt.Errorf("generating MCP config: %w", err)
	}
	for relPath, content := range mcpFiles {
		absPath := filepath.Join(outputDir, relPath)
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			return fmt.Errorf("creating MCP config dir: %w", err)
		}
		if err := os.WriteFile(absPath, content, 0o644); err != nil {
			return fmt.Errorf("writing MCP config %s: %w", relPath, err)
		}
	}
	return nil
}

// writeHookConfig generates the vendor's plugin hook config and writes it to
// the output directory. It reports whether it wrote any file: a vendor that
// emits no hooks (Copilot) needs none of the scripts they run.
func writeHookConfig(outputDir string, adapter VendorExporter, hooks map[string][]plugin.HookEntry) (bool, error) {
	generate := adapter.GenerateHookConfig
	if pg, ok := adapter.(PluginHookGenerator); ok {
		generate = pg.GeneratePluginHookConfig
	}
	hookFiles, err := generate(hooks)
	if err != nil {
		return false, fmt.Errorf("generating hook config: %w", err)
	}
	for relPath, content := range hookFiles {
		absPath := filepath.Join(outputDir, relPath)
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			return false, fmt.Errorf("creating hook config dir: %w", err)
		}
		if err := os.WriteFile(absPath, content, 0o644); err != nil {
			return false, fmt.Errorf("writing hook config %s: %w", relPath, err)
		}
	}
	return len(hookFiles) > 0, nil
}

// copyHookScripts copies into a plugin each script its hooks run by a "./"
// path. A plugin hook file anchors such a command to the vendor's plugin-root
// variable (#483), so the script must ship in the plugin: it is copied from
// the same path in the harness directory, keeping its mode. A "./" script that
// is not a regular file in the harness, or that climbs out of it, cannot ship,
// and comes back as a warning rather than an error, because the export is
// still a valid plugin and the hook may be meant for a tree the harness does
// not own. Other commands (absolute, variable-anchored, PATH-style) are not
// scripts the harness ships and are left alone.
func copyHookScripts(harnessDir, outputDir string, hooks map[string][]plugin.HookEntry) ([]string, error) {
	events := make([]string, 0, len(hooks))
	for event := range hooks {
		events = append(events, event)
	}
	sort.Strings(events)

	var warnings []string
	seen := map[string]bool{}
	for _, event := range events {
		for _, entry := range hooks[event] {
			script, ok := hookScript(entry.Command)
			if !ok || seen[script] {
				continue
			}
			seen[script] = true
			rel := filepath.Clean(filepath.FromSlash(strings.TrimPrefix(script, "./")))
			if !filepath.IsLocal(rel) {
				warnings = append(warnings, fmt.Sprintf("hook script %s is outside the harness, so the plugin cannot carry it", script))
				continue
			}
			src := filepath.Join(harnessDir, rel)
			if info, err := os.Lstat(src); err != nil || !info.Mode().IsRegular() {
				warnings = append(warnings, fmt.Sprintf("hook script %s is not a file in the harness, so the plugin does not carry it", script))
				continue
			}
			if err := assembler.CopyFile(src, filepath.Join(outputDir, rel)); err != nil {
				return nil, fmt.Errorf("copying hook script %s: %w", script, err)
			}
		}
	}
	return warnings, nil
}

// hookScript returns the script a hook command runs when it names one by a
// "./" path, the form a plugin hook file anchors to the plugin root: the
// command's first word.
func hookScript(cmd string) (string, bool) {
	fields := strings.Fields(cmd)
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "./") {
		return "", false
	}
	return fields[0], true
}

// writeGeneratedFiles writes a map of relative paths to file contents into baseDir.
func writeGeneratedFiles(baseDir string, files map[string][]byte) error {
	for relPath, data := range files {
		absPath := filepath.Join(baseDir, relPath)
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(absPath, data, 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", relPath, err)
		}
	}
	return nil
}

// copyContent copies resolved content into the target directory using the given artifact dirs mapping.
func copyContent(targetBaseDir string, content []resolver.ResolvedContent, artifactDirs map[string]string, transform assembler.ArtifactTransform) error {
	for _, rc := range content {
		if len(rc.Paths) == 0 {
			if err := assembler.CopyAllArtifacts(rc.BasePath, targetBaseDir, artifactDirs, transform); err != nil {
				return err
			}
		} else {
			for _, picked := range rc.Paths {
				if err := assembler.CopyPicked(rc.BasePath, picked, targetBaseDir, artifactDirs, transform); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// countDir counts immediate children of a directory. Returns 0 if dir doesn't exist.
func countDir(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	return len(entries)
}

// joinParts joins string parts with commas and "and" for the last element.
func joinParts(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " and " + parts[1]
	default:
		result := ""
		for i, p := range parts {
			if i == len(parts)-1 {
				result += "and " + p
			} else {
				result += p + ", "
			}
		}
		return result
	}
}
