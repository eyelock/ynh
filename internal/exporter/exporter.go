package exporter

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/persona"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/resolver"
	"github.com/eyelock/ynh/internal/vendor"
)

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
	// SourceDir is the persona source directory — always a local path.
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
}

// ExportResult describes the output for one vendor.
type ExportResult struct {
	Vendor    string
	OutputDir string
	Skills    int
	Agents    int
	Warnings  []string
}

// Export produces vendor-native plugin directories from a persona source.
func Export(opts ExportOptions) ([]ExportResult, error) {
	// Load persona
	p, err := persona.LoadPluginDir(opts.SourceDir)
	if err != nil {
		return nil, fmt.Errorf("loading persona: %w", err)
	}

	pj, err := plugin.LoadPluginJSON(opts.SourceDir)
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

	// Add SourceDir as local content (persona's own embedded artifacts)
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
		return exportMerged(opts, pj, p, content, instructionsPath, vendors)
	}
	return exportPerVendor(opts, pj, p, content, instructionsPath, vendors)
}

func exportPerVendor(opts ExportOptions, pj *plugin.PluginJSON, p *persona.Persona, content []resolver.ResolvedContent, instructionsPath string, vendors []string) ([]ExportResult, error) {
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

func exportMerged(opts ExportOptions, pj *plugin.PluginJSON, p *persona.Persona, content []resolver.ResolvedContent, instructionsPath string, vendors []string) ([]ExportResult, error) {
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
	var mergedWarnings []string

	for _, v := range vendors {
		switch v {
		case "claude":
			if err := GenerateClaudeManifest(pj, outputDir); err != nil {
				return nil, err
			}
		case "cursor":
			if err := GenerateCursorManifest(pj, outputDir); err != nil {
				return nil, err
			}
		case "codex":
			// Codex has no marketplace system — skip in merged mode
			mergedWarnings = append(mergedWarnings, "Codex: excluded from merged export (no marketplace system)")
			continue
		}
	}

	// Instructions
	if instructionsPath != "" {
		// Write AGENTS.md always
		if err := assembler.CopyFile(instructionsPath, filepath.Join(outputDir, "AGENTS.md")); err != nil {
			return nil, err
		}
		// Write .cursorrules if Cursor is a target
		for _, v := range vendors {
			if v == "cursor" {
				if err := assembler.CopyFile(instructionsPath, filepath.Join(outputDir, ".cursorrules")); err != nil {
					return nil, err
				}
				break
			}
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

	results = append(results, ExportResult{
		Vendor:    "merged",
		OutputDir: outputDir,
		Skills:    skills,
		Agents:    agents,
		Warnings:  mergedWarnings,
	})

	return results, nil
}

func exportForVendor(vendorName string, outputDir string, pj *plugin.PluginJSON, p *persona.Persona, content []resolver.ResolvedContent, instructionsPath string) (ExportResult, error) {
	result := ExportResult{
		Vendor:    vendorName,
		OutputDir: outputDir,
	}

	adapter, err := vendor.Get(vendorName)
	if err != nil {
		return result, err
	}

	if vendorName == "codex" {
		return exportCodex(outputDir, pj, p, content, instructionsPath)
	}

	// Claude and Cursor: standard plugin layout at the output root
	artifactDirs := adapter.ArtifactDirs()
	if err := copyContent(outputDir, content, artifactDirs); err != nil {
		return result, err
	}

	// Manifest
	switch vendorName {
	case "claude":
		if err := GenerateClaudeManifest(pj, outputDir); err != nil {
			return result, err
		}
	case "cursor":
		if err := GenerateCursorManifest(pj, outputDir); err != nil {
			return result, err
		}
	}

	// Instructions
	if instructionsPath != "" {
		vendorInstructionsFile := ""
		if vendorName == "cursor" {
			vendorInstructionsFile = adapter.InstructionsFile() // .cursorrules
		}
		if err := GenerateInstructions(instructionsPath, outputDir, vendorInstructionsFile); err != nil {
			return result, err
		}
	}

	// Delegates (Claude and Cursor only)
	if len(p.DelegatesTo) > 0 {
		if err := ExportDelegates(outputDir, p.DelegatesTo); err != nil {
			return result, fmt.Errorf("exporting delegates: %w", err)
		}
	}

	result.Skills = countDir(filepath.Join(outputDir, "skills"))
	result.Agents = countDir(filepath.Join(outputDir, "agents"))
	return result, nil
}

// exportCodex handles the Codex-specific layout: .agents/skills/ only.
// Codex has no plugin manifest, no agents/rules/commands support.
func exportCodex(outputDir string, _ *plugin.PluginJSON, p *persona.Persona, content []resolver.ResolvedContent, instructionsPath string) (ExportResult, error) {
	result := ExportResult{
		Vendor:    "codex",
		OutputDir: outputDir,
	}

	// Codex uses .agents/skills/ for skill discovery
	codexSkillsDir := filepath.Join(outputDir, ".agents", "skills")
	if err := os.MkdirAll(codexSkillsDir, 0o755); err != nil {
		return result, err
	}

	// Only copy skills — Codex has no loading mechanism for other artifact types
	codexArtifactDirs := map[string]string{
		"skills": filepath.Join(".agents", "skills"),
	}
	if err := copyContent(outputDir, content, codexArtifactDirs); err != nil {
		return result, err
	}

	// Count skipped artifacts for warnings
	skippedAgents := 0
	skippedRules := 0
	skippedCommands := 0
	for _, rc := range content {
		skippedAgents += countDir(filepath.Join(rc.BasePath, "agents"))
		skippedRules += countDir(filepath.Join(rc.BasePath, "rules"))
		skippedCommands += countDir(filepath.Join(rc.BasePath, "commands"))
	}
	if skippedAgents > 0 || skippedRules > 0 || skippedCommands > 0 {
		var parts []string
		if skippedAgents > 0 {
			parts = append(parts, fmt.Sprintf("%d agents", skippedAgents))
		}
		if skippedRules > 0 {
			parts = append(parts, fmt.Sprintf("%d rules", skippedRules))
		}
		if skippedCommands > 0 {
			parts = append(parts, fmt.Sprintf("%d commands", skippedCommands))
		}
		result.Warnings = append(result.Warnings, fmt.Sprintf("Codex: skipping %s (not supported)", joinParts(parts)))
	}
	if len(p.DelegatesTo) > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Codex: skipping %d delegates (not supported)", len(p.DelegatesTo)))
	}

	// Instructions — AGENTS.md only (Codex natively consumes it)
	if instructionsPath != "" {
		if err := GenerateInstructions(instructionsPath, outputDir, ""); err != nil {
			return result, err
		}
	}

	result.Skills = countDir(codexSkillsDir)
	return result, nil
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
