package marketplace

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/exporter"
	"github.com/eyelock/ynh/internal/migration"
	"github.com/eyelock/ynh/internal/pathutil"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/resolver"
	"github.com/eyelock/ynh/internal/vendor"
)

// pluginInfo holds resolved metadata for a plugin in the marketplace.
type pluginInfo struct {
	Name        string
	Description string
	Version     string
}

// marketplaceJSON is the Claude/Cursor marketplace index format — used for test unmarshalling.
type marketplaceJSON struct {
	Name        string              `json:"name"`
	Owner       MarketplaceOwner    `json:"owner"`
	Description string              `json:"description,omitempty"`
	Plugins     []marketplacePlugin `json:"plugins"`
}

// marketplacePlugin is one entry in the Claude/Cursor marketplace index.
type marketplacePlugin struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Version     string `json:"version,omitempty"`
	Source      string `json:"source"`
}

// codexMarketplaceJSON is the Codex marketplace index format — used for test unmarshalling.
type codexMarketplaceJSON struct {
	Name      string                    `json:"name"`
	Interface codexMarketplaceInterface `json:"interface"`
	Plugins   []codexMarketplacePlugin  `json:"plugins"`
}

type codexMarketplaceInterface struct {
	DisplayName string `json:"displayName"`
}

type codexMarketplacePlugin struct {
	Name     string                 `json:"name"`
	Source   codexMarketplaceSource `json:"source"`
	Policy   codexMarketplacePolicy `json:"policy,omitempty"`
	Category string                 `json:"category,omitempty"`
}

type codexMarketplaceSource struct {
	Source string `json:"source"`
	Path   string `json:"path"`
}

type codexMarketplacePolicy struct {
	Installation   string `json:"installation,omitempty"`
	Authentication string `json:"authentication,omitempty"`
}

// MarketplaceConfig is ynh's build config for marketplace generation.
type MarketplaceConfig struct {
	Name        string             `json:"name"`
	Owner       MarketplaceOwner   `json:"owner"`
	Description string             `json:"description,omitempty"`
	Harnesses   []MarketplaceEntry `json:"harnesses"`
}

// MarketplaceOwner identifies the marketplace publisher.
type MarketplaceOwner struct {
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
}

// MarketplaceEntry describes one harness or plugin in the marketplace.
type MarketplaceEntry struct {
	Type        string `json:"type"`                  // "harness" or "plugin"
	Source      string `json:"source"`                // local path or git URL
	Description string `json:"description,omitempty"` // override plugin.json description
	Version     string `json:"version,omitempty"`     // override plugin.json version
	Path        string `json:"path,omitempty"`        // monorepo subdir
}

// isLocalSource reports whether a source string refers to a local filesystem path.
func isLocalSource(source string) bool {
	return strings.HasPrefix(source, "/") || strings.HasPrefix(source, ".")
}

// validateRemoteSource checks that a non-local source looks like a valid Git URL.
// Accepts HTTPS/HTTP/SSH URLs and host/org/repo shorthand (e.g. github.com/org/repo).
func validateRemoteSource(source string) error {
	if strings.HasPrefix(source, "https://") || strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "git@") {
		return nil
	}
	// Shorthand must include at least host/org/repo (3 slash-separated components).
	parts := strings.SplitN(source, "/", 3)
	if len(parts) < 3 || parts[1] == "" || parts[2] == "" {
		return fmt.Errorf("remote source %q must be a full URL (https://...) or host/org/repo shorthand (e.g. github.com/org/repo)", source)
	}
	return nil
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
	if len(cfg.Harnesses) == 0 {
		return nil, fmt.Errorf("marketplace config: at least one harness is required")
	}

	for i, e := range cfg.Harnesses {
		if e.Type != "harness" && e.Type != "plugin" {
			return nil, fmt.Errorf("marketplace config: entry %d: type must be \"harness\" or \"plugin\", got %q", i, e.Type)
		}
		if e.Source == "" {
			return nil, fmt.Errorf("marketplace config: entry %d: source is required", i)
		}
		if !isLocalSource(e.Source) {
			if err := validateRemoteSource(e.Source); err != nil {
				return nil, fmt.Errorf("marketplace config: entry %d: %w", i, err)
			}
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
	// Vendors lists target vendors (default: claude, cursor, codex, copilot).
	Vendors []string
	// Config provides remote source checking.
	Config *config.Config
	// BeforeWrite, when set, runs once every entry has been checked and
	// before anything is written. The CLI runs --clean here, so a refused
	// build does not empty the output directory first.
	BeforeWrite func() error
}

// Build generates a vendor-native marketplace directory from a marketplace config.
func Build(cfg *MarketplaceConfig, opts BuildOptions) error {
	vendors := opts.Vendors
	if len(vendors) == 0 {
		vendors = []string{"claude", "cursor", "codex", "copilot"}
	}

	// Read and check every entry before writing anything, so a refused entry
	// (a legacy manifest, a missing source) leaves OutputDir as it was rather
	// than creating it, or a plugins/ directory inside it (#451).
	type preparedEntry struct {
		kind   string
		srcDir string
		info   pluginInfo
	}
	var prepared []preparedEntry
	for _, entry := range cfg.Harnesses {
		srcDir, err := resolveEntrySource(entry.Source, opts.ConfigDir)
		if err != nil {
			return fmt.Errorf("entry %q: %w", entry.Source, err)
		}

		// Apply monorepo subdir
		if entry.Path != "" {
			if err := pathutil.CheckSubpath(entry.Path); err != nil {
				return fmt.Errorf("entry %q: invalid path: %w", entry.Source, err)
			}
			srcDir = filepath.Join(srcDir, entry.Path)
		}

		var info pluginInfo

		switch entry.Type {
		case "harness":
			if _, err := migration.FormatChain().Run(srcDir); err != nil {
				return fmt.Errorf("entry %q: %w", entry.Source, err)
			}
			hj, err := plugin.LoadPluginJSON(srcDir)
			if err != nil {
				return fmt.Errorf("entry %q: %w", entry.Source, err)
			}
			info = pluginInfo{Name: hj.Name, Description: hj.Description, Version: hj.Version}
		case "plugin":
			if _, err := migration.FormatChain().Run(srcDir); err != nil {
				return fmt.Errorf("entry %q: %w", entry.Source, err)
			}
			pi, err := loadPluginManifest(srcDir)
			if err != nil {
				return fmt.Errorf("entry %q: %w", entry.Source, err)
			}
			info = pi
		}

		prepared = append(prepared, preparedEntry{kind: entry.Type, srcDir: srcDir, info: info})
	}

	if opts.BeforeWrite != nil {
		if err := opts.BeforeWrite(); err != nil {
			return err
		}
	}

	pluginsDir := filepath.Join(opts.OutputDir, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		return fmt.Errorf("creating plugins dir: %w", err)
	}

	var pluginInfos []pluginInfo
	for i, p := range prepared {
		entry := cfg.Harnesses[i]
		info := p.info
		pluginOutputDir := filepath.Join(pluginsDir, info.Name)

		switch p.kind {
		case "harness":
			if err := buildHarnessEntry(p.srcDir, pluginOutputDir, vendors, opts.Config); err != nil {
				return fmt.Errorf("harness %q: %w", info.Name, err)
			}
		case "plugin":
			if err := buildPluginEntry(p.srcDir, pluginOutputDir, vendors); err != nil {
				return fmt.Errorf("plugin %q: %w", info.Name, err)
			}
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

	// Claude Code requires a working tree for relative plugin source paths to
	// resolve during /plugin install, so the output is always a git repository.
	// Three cases:
	//
	//   - Not a repo yet: initialise one, mark it as ours, commit the build.
	//   - A repo ynd created earlier (the marker proves it): commit this build
	//     on top, so a rebuild keeps the history rather than losing it.
	//   - Somebody else's repo: leave their history alone, as before.
	switch {
	case !isGitRepo(opts.OutputDir):
		if err := initGitRepo(opts.OutputDir); err != nil {
			return fmt.Errorf("initializing git repo: %w", err)
		}
	case OwnsRepo(opts.OutputDir):
		if err := commitBuild(opts.OutputDir); err != nil {
			return fmt.Errorf("committing build: %w", err)
		}
	}

	return nil
}

// buildHarnessEntry exports a harness using ModeMerged into the plugin output dir.
func buildHarnessEntry(srcDir, outputDir string, vendors []string, cfg *config.Config) error {
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
// generating missing vendor manifests. Build has already refused a manifest
// ynh no longer reads, against the source rather than the copy (#406).
func buildPluginEntry(srcDir, outputDir string, vendors []string) error {
	if err := assembler.CopyDir(srcDir, outputDir); err != nil {
		return fmt.Errorf("copying plugin: %w", err)
	}

	// Load metadata for manifest generation, falling back to the
	// vendor-native .claude-plugin/plugin.json format for standalone plugins.
	var hj *plugin.HarnessJSON
	var err error
	if plugin.IsPluginDir(outputDir) {
		hj, err = plugin.LoadPluginJSON(outputDir)
	}
	if hj == nil {
		pi, piErr := loadPluginManifest(outputDir)
		if piErr != nil {
			return fmt.Errorf("no .agents/harness/plugin.json or .claude-plugin/plugin.json found: %w", piErr)
		}
		hj = &plugin.HarnessJSON{
			Name:        pi.Name,
			Description: pi.Description,
			Version:     pi.Version,
		}
	}
	if err != nil {
		return err
	}

	// Generate missing vendor manifests
	for _, v := range vendors {
		adapter, err := vendor.Get(v)
		if err != nil {
			continue
		}
		manifestFiles, err := adapter.GeneratePluginManifest(hj, outputDir)
		if err != nil {
			return fmt.Errorf("generating %s manifest: %w", v, err)
		}
		for relPath, data := range manifestFiles {
			absPath := filepath.Join(outputDir, relPath)
			// Only generate if missing
			if _, statErr := os.Stat(absPath); os.IsNotExist(statErr) {
				if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
					return err
				}
				if err := os.WriteFile(absPath, data, 0o644); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// loadPluginManifest reads name, description, and version from a vendor-native
// plugin's .claude-plugin/plugin.json. Used for plugin entries that don't have harness.json.
func loadPluginManifest(dir string) (pluginInfo, error) {
	path := filepath.Join(dir, ".claude-plugin", "plugin.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return pluginInfo{}, fmt.Errorf("reading %s: %w", path, err)
	}
	var manifest struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Version     string `json:"version"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return pluginInfo{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	if manifest.Name == "" {
		return pluginInfo{}, fmt.Errorf("%s: name is required", path)
	}
	return pluginInfo{
		Name:        manifest.Name,
		Description: manifest.Description,
		Version:     manifest.Version,
	}, nil
}

// resolveEntrySource resolves an entry source to a local directory path.
// Local paths (starting with / or .) are resolved relative to configDir.
// Remote sources (GitHub shorthand, HTTPS, SSH) are cloned and cached via the resolver.
func resolveEntrySource(source, configDir string) (string, error) {
	if isLocalSource(source) {
		if filepath.IsAbs(source) {
			return source, nil
		}
		return filepath.Join(configDir, source), nil
	}
	result, err := resolver.EnsureRepo(source, "")
	if err != nil {
		return "", fmt.Errorf("cloning %q: %w", source, err)
	}
	return result.Path, nil
}

// MarkerFile is written at the root of every repository ynd initialises for a
// marketplace build, and committed with it. It is how ynd later tells its own
// build output from somebody's source tree: `--clean` may empty a repository
// that carries the marker and whose root commit is ours, and refuses every
// other git working copy. An unrelated repository has no reason to contain it.
const MarkerFile = ".ynd-marketplace"

// markerContent names the tool and a format number, so the file explains
// itself to anyone who finds it and can be told apart from a later revision.
const markerContent = "ynd marketplace 1\n"

// commitMessage is the subject of every commit ynd makes in a marketplace
// repository. OwnsRepo checks the root commit against it.
const commitMessage = "ynd marketplace build"

// isGitRepo checks whether dir is the root of a Git repository.
func isGitRepo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// OwnsRepo reports whether the git repository at dir was created by ynd. Both
// proofs must hold: the marker file is present with the expected content, and
// the repository's root commit is the one initGitRepo makes. A marker copied
// into a real repository by hand does not make its history ours.
//
// It is read-only. `ynd --clean` consults it to decide whether a working copy
// may be emptied, so it must never modify the directory it is asked about.
func OwnsRepo(dir string) bool {
	content, err := os.ReadFile(filepath.Join(dir, MarkerFile))
	if err != nil || !strings.HasPrefix(string(content), "ynd marketplace") {
		return false
	}
	if fi, err := os.Stat(filepath.Join(dir, ".git")); err != nil || !fi.IsDir() {
		return false // a .git file is a worktree or submodule: never ours
	}
	cmd := exec.Command("git", "log", "--max-parents=0", "--format=%s", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	for _, subject := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if subject == commitMessage {
			return true
		}
	}
	return false
}

// initGitRepo initializes a new Git repo in dir, marks it as ynd's own, and
// commits all content.
func initGitRepo(dir string) error {
	if err := runGit(dir, "init"); err != nil {
		return err
	}
	return commitBuild(dir)
}

// commitBuild writes the marker and commits everything in dir, if anything
// changed. Uses -c flags for identity so it works in environments without a
// global git config (e.g. CI).
func commitBuild(dir string) error {
	if err := os.WriteFile(filepath.Join(dir, MarkerFile), []byte(markerContent), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", MarkerFile, err)
	}
	if err := runGit(dir, "add", "-A"); err != nil {
		return err
	}
	status := exec.Command("git", "status", "--porcelain")
	status.Dir = dir
	out, err := status.Output()
	if err != nil {
		return fmt.Errorf("git status: %w", err)
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		return nil // an identical rebuild has nothing to commit
	}
	return runGit(dir, "-c", "user.name=ynd", "-c", "user.email=ynd@localhost", "commit", "-m", commitMessage)
}

func runGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, out)
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
