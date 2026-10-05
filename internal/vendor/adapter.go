package vendor

import (
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/eyelock/ynh/internal/plugin"
)

// ErrUnknownVendor is returned when a vendor name is not registered.
var ErrUnknownVendor = errors.New("unknown vendor")

// ErrInitialPromptNotSupported is returned by LaunchWithInitialPrompt on
// vendors whose CLI has no mechanism to pass an initial message into an
// interactive session.
var ErrInitialPromptNotSupported = errors.New("vendor does not support interactive sessions with an initial prompt")

// ErrResumeNotSupported is returned by LaunchResume on vendors whose CLI has
// no mechanism to continue a previous interactive session.
var ErrResumeNotSupported = errors.New("vendor does not support resuming sessions")

// ErrNoResumableSession is returned by ResolveLastSession when the vendor's
// session store was readable but holds nothing for the given directory.
// Callers treat this as "launch cold", not as a failure: there is genuinely no
// prior session, so the vendor's continue-last form would find nothing either
// (or worse, for Copilot, would resume some other directory's session).
var ErrNoResumableSession = errors.New("no resumable session found")

// ErrSessionLookupUnavailable is returned by ResolveLastSession on vendors that
// cannot identify a session id locally at all — Codex (store is sqlite, which
// ynh will not take a driver for) and Cursor (chats are server-side).
//
// Distinct from ErrNoResumableSession because it says nothing about whether a
// session exists: these vendors can still resume via their continue-last form,
// so callers should resume with an empty id rather than launching cold.
var ErrSessionLookupUnavailable = errors.New("vendor cannot resolve session ids locally")

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

	// LaunchWithInitialPrompt starts an interactive session with the prompt
	// pre-populated as the first user message. Unlike LaunchNonInteractive,
	// the session continues after the LLM responds. Vendors that cannot
	// support this return ErrInitialPromptNotSupported.
	LaunchWithInitialPrompt(configPath string, prompt string, extraArgs []string) error

	// SupportsResume reports whether this vendor's CLI can continue a previous
	// interactive session at all. Drives `supports_resume` in
	// `ynh vendors --format json`.
	//
	// Note this is true for every current vendor, so it says nothing about
	// whether a *specific* session can be pinned — that additionally requires a
	// readable local session store, which only Claude and Copilot have.
	SupportsResume() bool

	// ResolveLastSession returns the id of the most recent session the vendor
	// recorded for cwd. It returns ErrNoResumableSession when the store was
	// readable but empty for cwd, and ErrSessionLookupUnavailable when this
	// vendor cannot resolve ids locally at all — the two lead callers to
	// different fallbacks, so keep them distinct.
	// notBefore, when non-zero, ignores sessions last touched before it.
	//
	// Implementations read the vendor's own on-disk session store. They must
	// never parse terminal output: the resume banner a CLI prints on exit is
	// undocumented UI that changes without notice, and the same identifier is
	// already on disk.
	ResolveLastSession(cwd string, notBefore time.Time) (string, error)

	// LaunchResume starts an interactive session continuing sessionID. An empty
	// sessionID means "whatever the vendor considers most recent".
	//
	// Implementations MUST NOT emit a bare resume flag. On every current vendor
	// a bare --resume opens an interactive session *picker* rather than
	// resuming the latest, which would hang an unattended relaunch waiting for
	// a keypress. Emit an explicit id, or the vendor's continue-last form
	// (claude --continue, codex resume --last, cursor --continue) — never
	// neither.
	LaunchResume(configPath string, sessionID string, extraArgs []string) error

	// GenerateSystemPrompt produces vendor-native instruction files from the
	// harness instructions content. Returns a map of relative file paths to
	// file contents. Always includes AGENTS.md (cross-vendor); vendors add
	// their own files as needed (e.g. CLAUDE.md, .cursorrules).
	GenerateSystemPrompt(content []byte) map[string][]byte

	// ApplyRuntimeInstructions injects per-invocation context into the vendor's
	// instructions pipeline. For CLI-flag vendors (Claude, Codex) it returns
	// extra args to append to the launch call; for file-based vendors (Cursor)
	// it writes directly to the run dir and returns nil args.
	// text is guaranteed non-empty by the caller.
	ApplyRuntimeInstructions(runDir, text string) ([]string, error)

	// GenerateHookConfig translates canonical hook declarations to vendor-native
	// hook configuration files. Returns a map of relative file paths to file contents.
	// Returns nil if hooks is nil or empty.
	GenerateHookConfig(hooks map[string][]plugin.HookEntry) (map[string][]byte, error)

	// GenerateMCPConfig translates MCP server declarations to vendor-native
	// MCP configuration files for the assembled session layout (`ynh run`,
	// `ynd preview`, the agent loop). Returns a map of relative file paths to
	// file contents. Returns nil if servers is nil or empty. A vendor whose
	// plugin reads a different path also implements GeneratePluginMCPConfig,
	// which the exporter prefers (see exporter.PluginMCPGenerator).
	GenerateMCPConfig(servers map[string]plugin.MCPServer) (map[string][]byte, error)

	// GeneratePluginManifest produces vendor-native plugin manifest files
	// (e.g. .claude-plugin/plugin.json). Returns a map of relative file paths
	// to file contents. The outputDir is needed by some vendors to detect
	// existing content (e.g. Codex checks for skills/ and .mcp.json), never to
	// guess the layout. A vendor whose run-dir layout differs from its export
	// layout (Copilot) writes the run-dir manifest here and also implements
	// GenerateExportPluginManifest, which the exporter prefers (see
	// exporter.ExportManifestGenerator). Returns nil if the vendor has no
	// manifest format.
	GeneratePluginManifest(hj *plugin.HarnessJSON, outputDir string) (map[string][]byte, error)

	// PluginManifestDir returns the directory GeneratePluginManifest writes
	// plugin.json into (e.g. ".claude-plugin", ".codex-plugin"). A vendor
	// whose run-dir layout nests its plugin under ConfigDir (Copilot) writes
	// it at ConfigDir()/PluginManifestDir() there, and at PluginManifestDir()
	// in an export. Not the marketplace index
	// directory: Codex keeps its index at .agents/plugins, apart from its
	// manifest. Returns empty string if the vendor has no manifest format.
	PluginManifestDir() string

	// ExportArtifactDirs returns the artifact directory mapping for export.
	// Some vendors support a subset of artifact types in their plugin format
	// (e.g. Codex only supports skills). Returns nil to use ArtifactDirs().
	ExportArtifactDirs() map[string]string

	// SupportsExportDelegates reports whether this vendor supports delegate
	// harnesses in exported plugins. Codex does not support delegates.
	SupportsExportDelegates() bool

	// MarketplaceManifestDir returns the directory name for marketplace index
	// files (e.g. ".claude-plugin", ".agents/plugins"). It can differ from
	// PluginManifestDir, so it never locates a plugin manifest. Returns empty
	// string if the vendor has no marketplace system.
	MarketplaceManifestDir() string

	// GenerateMarketplaceIndex produces vendor-native marketplace index content.
	// Returns nil if the vendor has no marketplace system.
	GenerateMarketplaceIndex(cfg MarketplaceIndexConfig, plugins []MarketplacePluginInfo) ([]byte, error)

	// AgentPluginLayout describes where this vendor's client-specific
	// components sit inside a portable Agent Plugins package
	// (https://agent-plugins.org, §8). The portable core, skills/ and
	// mcp.json, is the same for every vendor and is not described here.
	AgentPluginLayout() AgentPluginLayout
}

// AgentPluginLayout is a vendor's answer to "what do you read from an Agent
// Plugins package beyond the portable core, and where". The specification
// leaves agents, rules, commands and hooks to each client, under a
// reverse-domain namespace the client documents. A client that has not
// adopted the format at all is reached through its own legacy manifest and
// layout at the plugin root instead, which the specification's migration
// guide calls a compatibility package.
//
// Every path is relative to the plugin root, slash-separated.
type AgentPluginLayout struct {
	// LoadsFormat is false for a client that does not read root plugin.json
	// as an Agent Plugins manifest. Such a client gets its own
	// GeneratePluginManifest output and GenerateSystemPrompt files at the
	// root, alongside the portable ones.
	LoadsFormat bool

	// Namespace is the reverse-domain identifier the client has published
	// for its extension data and directory, or "" when it has none.
	Namespace string

	// ArtifactDir is where the vendor's non-portable artifacts (agents,
	// rules, commands, per ExportArtifactDirs) and delegate agents go: "."
	// for the plugin root, the namespace directory, or "" when the client
	// cannot receive them from this package at all.
	ArtifactDir string

	// Hooks is the file the vendor's plugin hook config is written to
	// (GeneratePluginHookConfig where the vendor has one, so commands are
	// anchored at the plugin root), or "" when the client does not load
	// hooks from this package.
	Hooks string

	// HooksExtension, when true, records the Hooks path under
	// extensions.<Namespace>.hooks in the portable manifest, for a client
	// whose manifest pointer replaces its default hook discovery.
	HooksExtension bool

	// MCP is the file the vendor's plugin MCP config is written to
	// (GeneratePluginMCPConfig where the vendor has one), or "" when the
	// client reads the portable mcp.json.
	MCP string
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
		return nil, fmt.Errorf("%w %q (available: %v)", ErrUnknownVendor, name, Available())
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

// pluginHookFile is where a plugin package carries one vendor's hooks: a file
// under hooks/ named for the vendor, which that vendor's manifest names in its
// "hooks" field. Never the shared default hooks/hooks.json: Claude Code loads
// that file from a plugin root even when the manifest names another, and
// Copilot reads it by default, so in a package several vendors share it would
// hand one vendor's format to another (#469). The same layout is used for a
// single-vendor export, so every plugin ynh writes follows one rule.
func pluginHookFile(vendorName string) string {
	return filepath.Join("hooks", vendorName+".json")
}

// pluginHookPointer returns the manifest "hooks" value naming vendorName's
// plugin hook file, or "" when outputDir does not carry one.
func pluginHookPointer(outputDir, vendorName string) string {
	return pluginFilePointer(outputDir, pluginHookFile(vendorName))
}

// pluginFilePointer returns the manifest value naming rel, a file relative to
// the plugin root outputDir, in the "./"-prefixed form plugin loaders require,
// or "" when the file is not there. A plugin loader rejects a component path
// that does not exist, so a manifest names a file only when it is present.
func pluginFilePointer(outputDir, rel string) string {
	if !fileExists(filepath.Join(outputDir, rel)) {
		return ""
	}
	return "./" + filepath.ToSlash(rel)
}

// pluginRootCommand returns a hook command rewriter for a plugin: a leading
// "./" names a script shipped inside the plugin, so it is anchored to the
// vendor's plugin-root variable, quoted so an install path with spaces stays
// one word (#483). A hook runs in the agent's working directory, not the
// plugin root, so the bare "./" would look in the user's project. Commands
// that are absolute, already anchored or PATH-style are left unchanged. The
// exporter copies each such script into the plugin.
func pluginRootCommand(rootVar string) func(string) string {
	return func(cmd string) string {
		if rest, ok := strings.CutPrefix(cmd, "./"); ok {
			return `"${` + rootVar + `}"/` + rest
		}
		return cmd
	}
}

// keepHookCommand leaves a hook command exactly as the harness wrote it.
func keepHookCommand(cmd string) string { return cmd }

// Available returns all registered vendor names, sorted alphabetically. The
// registry is a map, so every list of vendors shown to a user comes from here:
// ranging over the map directly would change the order on every call (#520).
func Available() []string {
	return slices.Sorted(maps.Keys(registry))
}
