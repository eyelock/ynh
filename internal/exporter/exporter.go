package exporter

import (
	"fmt"
	"os"
	"path/filepath"

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

// ExportMode controls the output layout.
type ExportMode int

const (
	// ModePerVendor creates separate dirs: output/claude/, output/cursor/, output/codex/
	ModePerVendor ExportMode = iota
	// ModeMerged creates a single dir with dual manifests (for marketplace builds)
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
			if err := opts.Config.CheckRemoteSource(del.Git); err != nil {
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

	// Copy artifacts once (using standard artifact dirs)
	artifactDirs := vendor.DefaultArtifactDirs()
	if err := copyContent(outputDir, content, artifactDirs); err != nil {
		return nil, err
	}

	// Count artifacts
	skills := countDir(filepath.Join(outputDir, "skills"))
	agents := countDir(filepath.Join(outputDir, "agents"))

	// Generate manifests and instructions for each vendor
	var results []ExportResult

	for _, v := range vendors {
		adapter, err := vendor.Get(v)
		if err != nil {
			continue
		}
		manifestFiles, err := adapter.GeneratePluginManifest(pj, outputDir)
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
		if err := ExportDelegates(outputDir, p.DelegatesTo); err != nil {
			return nil, fmt.Errorf("exporting delegates: %w", err)
		}
		// Recount agents after delegate generation
		agents = countDir(filepath.Join(outputDir, "agents"))
	}

	// Hook config for each vendor
	if len(p.Hooks) > 0 {
		for _, v := range vendors {
			adapter, err := vendor.Get(v)
			if err != nil {
				continue
			}
			if err := writeHookConfig(outputDir, adapter, p.Hooks); err != nil {
				return nil, fmt.Errorf("writing hook config for %s: %w", v, err)
			}
		}
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
	if err := copyContent(outputDir, content, artifactDirs); err != nil {
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
		if err := ExportDelegates(outputDir, p.DelegatesTo); err != nil {
			return result, fmt.Errorf("exporting delegates: %w", err)
		}
	}

	// Hook config
	if len(p.Hooks) > 0 {
		if err := writeHookConfig(outputDir, adapter, p.Hooks); err != nil {
			return result, fmt.Errorf("writing hook config: %w", err)
		}
	}

	// MCP config
	if len(p.MCPServers) > 0 {
		if err := writeMCPConfig(outputDir, adapter, p.MCPServers); err != nil {
			return result, fmt.Errorf("writing MCP config: %w", err)
		}
	}

	// Manifest — generated after content (MCP, skills) so path pointers are accurate
	manifestFiles, err := adapter.GeneratePluginManifest(pj, outputDir)
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

// writeMCPConfig generates vendor-native MCP config files and writes them to the output directory.
func writeMCPConfig(outputDir string, adapter VendorExporter, servers map[string]plugin.MCPServer) error {
	mcpFiles, err := adapter.GenerateMCPConfig(servers)
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

// writeHookConfig generates vendor-native hook config files and writes them to the output directory.
func writeHookConfig(outputDir string, adapter VendorExporter, hooks map[string][]plugin.HookEntry) error {
	hookFiles, err := adapter.GenerateHookConfig(hooks)
	if err != nil {
		return fmt.Errorf("generating hook config: %w", err)
	}
	for relPath, content := range hookFiles {
		absPath := filepath.Join(outputDir, relPath)
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			return fmt.Errorf("creating hook config dir: %w", err)
		}
		if err := os.WriteFile(absPath, content, 0o644); err != nil {
			return fmt.Errorf("writing hook config %s: %w", relPath, err)
		}
	}
	return nil
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
func copyContent(targetBaseDir string, content []resolver.ResolvedContent, artifactDirs map[string]string) error {
	for _, rc := range content {
		if len(rc.Paths) == 0 {
			if err := assembler.CopyAllArtifacts(rc.BasePath, targetBaseDir, artifactDirs); err != nil {
				return err
			}
		} else {
			for _, picked := range rc.Paths {
				if err := assembler.CopyPicked(rc.BasePath, picked, targetBaseDir, artifactDirs); err != nil {
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
