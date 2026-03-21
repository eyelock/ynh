package marketplace

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/exporter"
	"github.com/eyelock/ynh/internal/plugin"
)

// MarketplaceConfig is ynh's build config for marketplace generation.
type MarketplaceConfig struct {
	Name        string             `json:"name"`
	Owner       MarketplaceOwner   `json:"owner"`
	Description string             `json:"description,omitempty"`
	Entries     []MarketplaceEntry `json:"entries"`
}

// MarketplaceOwner identifies the marketplace publisher.
type MarketplaceOwner struct {
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
}

// MarketplaceEntry describes one persona or plugin in the marketplace.
type MarketplaceEntry struct {
	Type        string `json:"type"`                  // "persona" or "plugin"
	Source      string `json:"source"`                // local path or git URL
	Description string `json:"description,omitempty"` // override plugin.json description
	Version     string `json:"version,omitempty"`     // override plugin.json version
	Path        string `json:"path,omitempty"`        // monorepo subdir
}

// LoadConfig reads and parses a marketplace.json file.
func LoadConfig(path string) (*MarketplaceConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading marketplace config: %w", err)
	}

	var cfg MarketplaceConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing marketplace config: %w", err)
	}

	if cfg.Name == "" {
		return nil, fmt.Errorf("marketplace config: name is required")
	}
	if cfg.Owner.Name == "" {
		return nil, fmt.Errorf("marketplace config: owner.name is required")
	}
	if len(cfg.Entries) == 0 {
		return nil, fmt.Errorf("marketplace config: at least one entry is required")
	}

	for i, e := range cfg.Entries {
		if e.Type != "persona" && e.Type != "plugin" {
			return nil, fmt.Errorf("marketplace config: entry %d: type must be \"persona\" or \"plugin\", got %q", i, e.Type)
		}
		if e.Source == "" {
			return nil, fmt.Errorf("marketplace config: entry %d: source is required", i)
		}
	}

	return &cfg, nil
}

// BuildOptions configures a marketplace build.
type BuildOptions struct {
	// ConfigDir is the directory containing marketplace.json (for resolving relative paths).
	ConfigDir string
	// OutputDir is where to write the marketplace output.
	OutputDir string
	// Vendors lists target vendors (default: claude, cursor).
	Vendors []string
	// Config provides remote source checking.
	Config *config.Config
}

// Build generates a vendor-native marketplace directory from a marketplace config.
func Build(cfg *MarketplaceConfig, opts BuildOptions) error {
	vendors := opts.Vendors
	if len(vendors) == 0 {
		vendors = []string{"claude", "cursor"}
	}

	// Filter out codex — no marketplace system
	var filtered []string
	for _, v := range vendors {
		if v != "codex" {
			filtered = append(filtered, v)
		}
	}
	vendors = filtered

	pluginsDir := filepath.Join(opts.OutputDir, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		return fmt.Errorf("creating plugins dir: %w", err)
	}

	// Process each entry
	var pluginInfos []pluginInfo
	for _, entry := range cfg.Entries {
		srcDir := resolveEntrySource(entry.Source, opts.ConfigDir)

		// Apply monorepo subdir
		if entry.Path != "" {
			srcDir = filepath.Join(srcDir, entry.Path)
		}

		pj, err := plugin.LoadPluginJSON(srcDir)
		if err != nil {
			return fmt.Errorf("entry %q: %w", entry.Source, err)
		}

		pluginOutputDir := filepath.Join(pluginsDir, pj.Name)

		switch entry.Type {
		case "persona":
			if err := buildPersonaEntry(srcDir, pluginOutputDir, vendors, opts.Config); err != nil {
				return fmt.Errorf("persona %q: %w", pj.Name, err)
			}
		case "plugin":
			if err := buildPluginEntry(srcDir, pluginOutputDir, vendors); err != nil {
				return fmt.Errorf("plugin %q: %w", pj.Name, err)
			}
		}

		info := pluginInfo{
			Name:        pj.Name,
			Description: pj.Description,
			Version:     pj.Version,
		}
		// Apply overrides from marketplace config
		if entry.Description != "" {
			info.Description = entry.Description
		}
		if entry.Version != "" {
			info.Version = entry.Version
		}
		pluginInfos = append(pluginInfos, info)
	}

	// Generate marketplace indexes for each vendor
	for _, v := range vendors {
		if err := GenerateIndex(cfg, pluginInfos, opts.OutputDir, v); err != nil {
			return fmt.Errorf("generating %s index: %w", v, err)
		}
	}

	// Generate README.md
	if err := generateReadme(cfg, pluginInfos, opts.OutputDir); err != nil {
		return fmt.Errorf("generating README: %w", err)
	}

	// Initialize as Git repo if needed — Claude Code requires a working tree
	// for relative plugin source paths to resolve during /plugin install.
	if !isGitRepo(opts.OutputDir) {
		if err := initGitRepo(opts.OutputDir); err != nil {
			return fmt.Errorf("initializing git repo: %w", err)
		}
	}

	return nil
}

// buildPersonaEntry exports a persona using ModeMerged into the plugin output dir.
func buildPersonaEntry(srcDir, outputDir string, vendors []string, cfg *config.Config) error {
	_, err := exporter.Export(exporter.ExportOptions{
		SourceDir: srcDir,
		OutputDir: outputDir,
		Vendors:   vendors,
		Mode:      exporter.ModeMerged,
		Config:    cfg,
	})
	return err
}

// buildPluginEntry copies a self-contained plugin directory as-is,
// generating missing vendor manifests.
func buildPluginEntry(srcDir, outputDir string, vendors []string) error {
	if err := assembler.CopyDir(srcDir, outputDir); err != nil {
		return fmt.Errorf("copying plugin: %w", err)
	}

	// Load plugin.json for manifest generation
	pj, err := plugin.LoadPluginJSON(outputDir)
	if err != nil {
		return fmt.Errorf("loading copied plugin.json: %w", err)
	}

	// Generate missing vendor manifests
	for _, v := range vendors {
		switch v {
		case "claude":
			manifestDir := filepath.Join(outputDir, ".claude-plugin")
			if _, err := os.Stat(filepath.Join(manifestDir, "plugin.json")); os.IsNotExist(err) {
				if err := exporter.GenerateClaudeManifest(pj, outputDir); err != nil {
					return err
				}
			}
		case "cursor":
			manifestDir := filepath.Join(outputDir, ".cursor-plugin")
			if _, err := os.Stat(filepath.Join(manifestDir, "plugin.json")); os.IsNotExist(err) {
				if err := exporter.GenerateCursorManifest(pj, outputDir); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// resolveEntrySource resolves an entry source path relative to the config directory.
func resolveEntrySource(source, configDir string) string {
	if filepath.IsAbs(source) {
		return source
	}
	return filepath.Join(configDir, source)
}

// isGitRepo checks whether dir is the root of a Git repository.
func isGitRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// initGitRepo initializes a new Git repo in dir and commits all content.
func initGitRepo(dir string) error {
	for _, args := range [][]string{
		{"init"},
		{"add", "."},
		{"commit", "-m", "ynd marketplace build"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git %s: %w\n%s", args[0], err, out)
		}
	}
	return nil
}

func generateReadme(cfg *MarketplaceConfig, plugins []pluginInfo, outputDir string) error {
	var content string
	content += fmt.Sprintf("# %s\n\n", cfg.Name)
	if cfg.Description != "" {
		content += cfg.Description + "\n\n"
	}
	content += "## Plugins\n\n"
	content += "| Name | Description | Version |\n"
	content += "|------|-------------|--------|\n"
	for _, p := range plugins {
		version := p.Version
		if version == "" {
			version = "-"
		}
		content += fmt.Sprintf("| %s | %s | %s |\n", p.Name, p.Description, version)
	}
	content += "\n"

	return os.WriteFile(filepath.Join(outputDir, "README.md"), []byte(content), 0o644)
}
