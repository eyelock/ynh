package vendor

import (
	"errors"
	"fmt"
	"sort"

	"github.com/eyelock/ynh/internal/plugin"
)

// ErrUnknownVendor is returned when a vendor name is not registered.
var ErrUnknownVendor = errors.New("unknown vendor")

// SymlinkEntry records a single symlink created during Install.
type SymlinkEntry struct {
	Target string `json:"target"` // Where the symlink points (staging dir)
	Link   string `json:"link"`   // Where the symlink lives (project dir)
}

// Adapter knows how to lay out config files and launch a session for a specific vendor.
type Adapter interface {
	// Name returns the vendor identifier (e.g. "claude", "codex", "cursor").
	Name() string

	// DisplayName returns the human-friendly name (e.g. "Claude Code", "OpenAI Codex").
	DisplayName() string

	// CLIName returns the CLI binary name (e.g. "claude", "codex", "agent").
	CLIName() string

	// ConfigDir returns the vendor's config directory name (e.g. ".claude").
	ConfigDir() string

	// ArtifactDirs maps artifact types to their directory names within the config dir.
	ArtifactDirs() map[string]string

	// InstructionsFile returns the filename for project-level instructions
	// (e.g. "CLAUDE.md", "codex.md", ".cursorrules"). The file is placed at
	// the project root, not inside the config directory.
	InstructionsFile() string

	// NeedsSymlinks returns true if this vendor requires symlink installation
	// into the project directory (Cursor, Codex). False for vendors that use
	// native plugin loading (Claude).
	NeedsSymlinks() bool

	// Install creates the necessary integration between the assembled config
	// directory and the target project. For symlink vendors, this creates
	// symlinks from the project's vendor config dir to the staging dir.
	// For Claude, this is a no-op.
	Install(stagingDir string, projectDir string) ([]SymlinkEntry, error)

	// Clean removes any integration artifacts created by Install.
	// Only removes symlinks that were created by ynh (verified via entries).
	Clean(entries []SymlinkEntry) error

	// LaunchInteractive starts an interactive session with the vendor CLI.
	// configPath is the assembled config directory.
	// extraArgs are passed through verbatim to the vendor CLI.
	LaunchInteractive(configPath string, extraArgs []string) error

	// LaunchNonInteractive runs a one-shot prompt.
	// extraArgs are passed through verbatim to the vendor CLI.
	LaunchNonInteractive(configPath string, prompt string, extraArgs []string) error

	// GenerateSystemPrompt produces vendor-native instruction files from the
	// harness instructions content. Returns a map of relative file paths to
	// file contents. Always includes AGENTS.md (cross-vendor); vendors add
	// their own files as needed (e.g. CLAUDE.md, .cursorrules).
	GenerateSystemPrompt(content []byte) map[string][]byte

	// GenerateHookConfig translates canonical hook declarations to vendor-native
	// hook configuration files. Returns a map of relative file paths to file contents.
	// Returns nil if hooks is nil or empty.
	GenerateHookConfig(hooks map[string][]plugin.HookEntry) (map[string][]byte, error)

	// GenerateMCPConfig translates MCP server declarations to vendor-native
	// MCP configuration files. Returns a map of relative file paths to file contents.
	// Returns nil if servers is nil or empty.
	GenerateMCPConfig(servers map[string]plugin.MCPServer) (map[string][]byte, error)

	// GeneratePluginManifest produces vendor-native plugin manifest files
	// (e.g. .claude-plugin/plugin.json). Returns a map of relative file paths
	// to file contents. The outputDir is needed by some vendors to detect
	// existing content (e.g. Codex checks for skills/ and .mcp.json).
	// Returns nil if the vendor has no manifest format.
	GeneratePluginManifest(hj *plugin.HarnessJSON, outputDir string) (map[string][]byte, error)

	// ExportArtifactDirs returns the artifact directory mapping for export.
	// Some vendors support a subset of artifact types in their plugin format
	// (e.g. Codex only supports skills). Returns nil to use ArtifactDirs().
	ExportArtifactDirs() map[string]string

	// SupportsExportDelegates reports whether this vendor supports delegate
	// harnesses in exported plugins. Codex does not support delegates.
	SupportsExportDelegates() bool

	// MarketplaceManifestDir returns the directory name for marketplace index
	// files (e.g. ".claude-plugin", ".agents/plugins"). Returns empty string
	// if the vendor has no marketplace system.
	MarketplaceManifestDir() string

	// GenerateMarketplaceIndex produces vendor-native marketplace index content.
	// Returns nil if the vendor has no marketplace system.
	GenerateMarketplaceIndex(cfg MarketplaceIndexConfig, plugins []MarketplacePluginInfo) ([]byte, error)
}

// MarketplaceIndexConfig holds marketplace identity for index generation.
type MarketplaceIndexConfig struct {
	Name        string
	Description string
	OwnerName   string
	OwnerEmail  string
}

// MarketplacePluginInfo holds resolved metadata for one plugin in the marketplace.
type MarketplacePluginInfo struct {
	Name        string
	Description string
	Version     string
}

// DefaultName is the fallback vendor when no vendor is specified.
const DefaultName = "claude"

var registry = map[string]Adapter{}

// Register adds a vendor adapter to the registry.
func Register(a Adapter) {
	registry[a.Name()] = a
}

// Get returns a vendor adapter by name.
func Get(name string) (Adapter, error) {
	a, ok := registry[name]
	if !ok {
		var available []string
		for k := range registry {
			available = append(available, k)
		}
		return nil, fmt.Errorf("%w %q (available: %v)", ErrUnknownVendor, name, available)
	}
	return a, nil
}

// DefaultArtifactDirs returns the standard artifact directory mapping
// shared by all current vendors.
func DefaultArtifactDirs() map[string]string {
	return map[string]string{
		"skills":   "skills",
		"agents":   "agents",
		"rules":    "rules",
		"commands": "commands",
	}
}

// Available returns all registered vendor names, sorted alphabetically.
func Available() []string {
	var names []string
	for k := range registry {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
