package vendor

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/eyelock/ynh/internal/plugin"
)

// cursorPluginJSON is the Cursor plugin.json schema: identity fields, plus a
// pointer to the plugin's hooks file when it carries one.
type cursorPluginJSON struct {
	Name        string             `json:"name"`
	Version     string             `json:"version"`
	Description string             `json:"description,omitempty"`
	Author      *plugin.AuthorInfo `json:"author,omitempty"`
	Keywords    []string           `json:"keywords,omitempty"`
	Hooks       string             `json:"hooks,omitempty"`
}

func init() {
	Register(&Cursor{})
}

// cursorCLI is Cursor's CLI, which is not the editor's "cursor" launcher:
// see CLIName in adapter.go. Cursor's docs install it as "agent" and name
// "cursor-agent" only as an alias kept for older scripts
// (cursor.com/docs/cli/installation; the 8 Jan 2026 release notes: "The new
// primary entrypoint is agent (cursor-agent still works as an alias)"), so an
// install that has the alias also has "agent", and one name is enough.
const cursorCLI = "agent"

// Cursor implements the Adapter interface for Cursor Agent CLI.
// Uses .cursor/rules/ for rules and .cursorrules at project root.
type Cursor struct{}

func (c *Cursor) Name() string        { return "cursor" }
func (c *Cursor) DisplayName() string { return "Cursor" }
func (c *Cursor) CLIName() string     { return cursorCLI }

func (c *Cursor) ConfigDir() string {
	return ".cursor"
}

func (c *Cursor) InstructionsFile() string { return ".cursorrules" }

func (c *Cursor) ArtifactDirs() map[string]string { return DefaultArtifactDirs() }

func (c *Cursor) GenerateSystemPrompt(content []byte) map[string][]byte {
	// AGENTS.md: cross-vendor format
	// .cursorrules: Cursor-native instructions
	return map[string][]byte{
		"AGENTS.md":    content,
		".cursorrules": content,
	}
}

func (c *Cursor) NeedsSymlinks() bool { return true }

func (c *Cursor) Install(stagingDir string, projectDir string) ([]SymlinkEntry, error) {
	return installSymlinks(stagingDir, projectDir, c.ConfigDir(), c.ArtifactDirs())
}

func (c *Cursor) Clean(entries []SymlinkEntry) error {
	return cleanSymlinks(entries)
}

func (c *Cursor) LaunchInteractive(configPath string, extraArgs []string) error {
	return launchCursor(configPath, extraArgs)
}

func (c *Cursor) LaunchNonInteractive(configPath string, prompt string, extraArgs []string) error {
	args := append([]string{"-p", prompt}, extraArgs...)
	return launchCursor(configPath, args)
}

func (c *Cursor) LaunchWithInitialPrompt(configPath, prompt string, extraArgs []string) error {
	// Positional arg without -p starts an interactive session with the prompt
	// pre-loaded as the first user message (documented: agent "query").
	args := append(extraArgs, prompt)
	return launchCursor(configPath, args)
}

func (c *Cursor) SupportsInitialPrompt() bool { return true }

func (c *Cursor) SupportsResume() bool { return true }

// ResolveLastSession always reports no resumable session: Cursor keeps no local
// session store to read. ~/.cursor/ holds configuration only and
// ~/.local/share/cursor-agent/ holds nothing but installed versions — chats
// appear to live server-side, reachable only through the CLI's own picker.
//
// Cursor can still resume (see LaunchResume); it just cannot be told *which*
// session from here unless a caller supplies an id from elsewhere.
func (c *Cursor) ResolveLastSession(cwd string, notBefore time.Time) (string, error) {
	return "", ErrSessionLookupUnavailable
}

// LaunchResume continues a prior Cursor chat. An empty sessionID uses
// --continue ("Continue previous session"). A bare --resume is never emitted:
// Cursor documents it as "Select a session to resume", i.e. a picker.
func (c *Cursor) LaunchResume(configPath, sessionID string, extraArgs []string) error {
	var resumeArgs []string
	if sessionID != "" {
		resumeArgs = []string{"--resume", sessionID}
	} else {
		resumeArgs = []string{"--continue"}
	}
	return launchCursor(configPath, append(resumeArgs, extraArgs...))
}

func (c *Cursor) ApplyRuntimeInstructions(runDir, text string) ([]string, error) {
	cursorrules := filepath.Join(runDir, ".cursorrules")
	f, err := os.OpenFile(cursorrules, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("opening .cursorrules: %w", err)
	}
	if _, err := fmt.Fprintf(f, "\n\n%s\n", text); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("writing runtime instructions: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("closing .cursorrules: %w", err)
	}
	return nil, nil
}

// cursorHookEventMap maps canonical event names to Cursor hook events.
// Cursor supports: beforeSubmitPrompt, beforeShellExecution, beforeMCPExecution,
// beforeReadFile, afterFileEdit, stop, sessionStart, and more (see
// cursor.com/docs/hooks for the full list). There is no afterShellExecution
// event mapped to before_tool/after_tool — see cursor.md reference doc.
var cursorHookEventMap = map[string]string{
	"before_tool":      "beforeShellExecution",
	"after_tool":       "afterFileEdit",
	"before_prompt":    "beforeSubmitPrompt",
	"on_stop":          "stop",
	"on_session_start": "sessionStart",
}

// GenerateHookConfig writes the project hook file, .cursor/hooks.json, the
// only project-level path Cursor reads (cursor.com/docs/hooks). It serves
// `ynh run`, `ynd preview` and the agent loop, which all launch Cursor in the
// assembled directory. A plugin reads a different path; see
// GeneratePluginHookConfig.
func (c *Cursor) GenerateHookConfig(hooks map[string][]plugin.HookEntry) (map[string][]byte, error) {
	data, err := cursorHookDocument(hooks, keepHookCommand)
	if err != nil || data == nil {
		return nil, err
	}
	return map[string][]byte{filepath.Join(".cursor", "hooks.json"): data}, nil
}

// GeneratePluginHookConfig writes the plugin hook file, hooks/cursor.json at
// the plugin root, which GeneratePluginManifest names in the manifest's
// "hooks" field. A Cursor plugin reads hooks/hooks.json by default, and a
// manifest "hooks" path replaces that discovery
// (cursor.com/docs/reference/plugins), so Cursor reads this file and nothing
// else. The exporter uses it for `ynd export` and marketplace packages. Each
// context reads exactly one of the two files (#454, #469). The document is the
// project file's except for "./" commands, which name a script shipped in the
// plugin and are anchored to ${CURSOR_PLUGIN_ROOT}, which Cursor expands in a
// plugin hook command (cursor.com/docs/reference/plugins, #483).
func (c *Cursor) GeneratePluginHookConfig(hooks map[string][]plugin.HookEntry) (map[string][]byte, error) {
	data, err := cursorHookDocument(hooks, pluginRootCommand("CURSOR_PLUGIN_ROOT"))
	if err != nil || data == nil {
		return nil, err
	}
	return map[string][]byte{pluginHookFile(c.Name()): data}, nil
}

// cursorHookDocument renders canonical hooks in Cursor's flat format, or nil
// when none of them maps to a Cursor event. anchor rewrites each command for
// where the file is read.
func cursorHookDocument(hooks map[string][]plugin.HookEntry, anchor func(string) string) ([]byte, error) {
	if len(hooks) == 0 {
		return nil, nil
	}

	// Cursor flat format: { "hooks": { "beforeShellExecution": [ { "command": "..." } ] } }
	type cursorHookEntry struct {
		Command string `json:"command"`
	}

	allEvents := make(map[string][]cursorHookEntry)

	var events []string
	for event := range hooks {
		events = append(events, event)
	}
	sort.Strings(events)

	for _, event := range events {
		entries := hooks[event]
		cursorEvent, ok := cursorHookEventMap[event]
		if !ok {
			continue
		}

		var hookEntries []cursorHookEntry
		for _, entry := range entries {
			hookEntries = append(hookEntries, cursorHookEntry{Command: anchor(entry.Command)})
		}

		allEvents[cursorEvent] = hookEntries
	}

	if len(allEvents) == 0 {
		return nil, nil
	}

	config := map[string]any{
		"version": 1,
		"hooks":   allEvents,
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshalling hook config: %w", err)
	}
	data = append(data, '\n')

	return data, nil
}

func (c *Cursor) GeneratePluginManifest(hj *plugin.HarnessJSON, outputDir string) (map[string][]byte, error) {
	pj := &cursorPluginJSON{
		Name:        hj.Name,
		Version:     hj.Version,
		Description: hj.Description,
		Author:      hj.Author,
		Keywords:    hj.Keywords,
		Hooks:       pluginHookPointer(outputDir, c.Name()),
	}
	data, err := json.MarshalIndent(pj, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshalling plugin.json: %w", err)
	}
	data = append(data, '\n')
	return map[string][]byte{
		filepath.Join(c.PluginManifestDir(), "plugin.json"): data,
	}, nil
}

func (c *Cursor) ExportArtifactDirs() map[string]string { return nil }

func (c *Cursor) SupportsExportDelegates() bool { return true }

func (c *Cursor) PluginManifestDir() string { return ".cursor-plugin" }

// AgentPluginLayout: Cursor loads the portable core and has published no
// extension namespace (cursor.com/docs/plugins), so rules, agents, commands
// and hooks cannot reach it through this package. Cursor also does not
// expand ${PLUGIN_ROOT} or ${PLUGIN_DATA} in mcp.json.
func (c *Cursor) AgentPluginLayout() AgentPluginLayout {
	return AgentPluginLayout{LoadsFormat: true}
}

func (c *Cursor) MarketplaceManifestDir() string { return ".cursor-plugin" }

// GenerateMarketplaceIndex writes Cursor's own index shape.
//
// This was a byte-for-byte copy of Claude's, which is how Cursor came to emit
// Claude's format. Two things differ, per `references/cursor.md:127` and the
// committed `.cursor-plugin/marketplace.json` that
// `scripts/marketplace-consistency.sh` already gates on:
//
//   - `description` nests under `metadata`, rather than sitting at the top level
//   - plugins carry no `version`
//
// The `source` field deliberately keeps `./plugins/<name>` rather than the bare
// `"plugin-name"` the reference shows. That is not a third divergence: the
// reference's example describes a marketplace whose plugins sit at its root —
// like this repository's own committed index, which uses "." and "./.claude" —
// while `ynd marketplace build` copies each plugin into `./plugins/<name>`.
// The source has to say where the plugin actually is.
func (c *Cursor) GenerateMarketplaceIndex(cfg MarketplaceIndexConfig, plugins []MarketplacePluginInfo) ([]byte, error) {
	type indexPlugin struct {
		Name        string `json:"name"`
		Source      string `json:"source"`
		Description string `json:"description,omitempty"`
	}
	type indexOwner struct {
		Name  string `json:"name"`
		Email string `json:"email,omitempty"`
	}
	type indexMetadata struct {
		Description string `json:"description,omitempty"`
	}
	type indexJSON struct {
		Name     string        `json:"name"`
		Owner    indexOwner    `json:"owner"`
		Metadata indexMetadata `json:"metadata"`
		Plugins  []indexPlugin `json:"plugins"`
	}

	idx := indexJSON{
		Name:     cfg.Name,
		Owner:    indexOwner{Name: cfg.OwnerName, Email: cfg.OwnerEmail},
		Metadata: indexMetadata{Description: cfg.Description},
	}
	for _, p := range plugins {
		idx.Plugins = append(idx.Plugins, indexPlugin{
			Name:        p.Name,
			Source:      "./plugins/" + p.Name,
			Description: p.Description,
		})
	}

	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
	return data, nil
}

// GenerateMCPConfig writes the project MCP file, .cursor/mcp.json, the only
// project-level path Cursor reads (cursor.com/docs/context/mcp). It serves
// `ynh run`, `ynd preview` and the agent loop, which all launch Cursor in the
// assembled directory. A plugin reads a different path; see
// GeneratePluginMCPConfig.
func (c *Cursor) GenerateMCPConfig(servers map[string]plugin.MCPServer) (map[string][]byte, error) {
	data, err := cursorMCPDocument(servers)
	if err != nil || data == nil {
		return nil, err
	}
	return map[string][]byte{filepath.Join(".cursor", "mcp.json"): data}, nil
}

// GeneratePluginMCPConfig writes the plugin MCP file, mcp.json (no dot) at the
// plugin root, which a Cursor plugin discovers automatically
// (cursor.com/docs/reference/plugins). The exporter uses it for `ynd export`
// and marketplace packages. The document is the same as the project file;
// only the path differs, and each context reads exactly one of them (#470).
func (c *Cursor) GeneratePluginMCPConfig(servers map[string]plugin.MCPServer) (map[string][]byte, error) {
	data, err := cursorMCPDocument(servers)
	if err != nil || data == nil {
		return nil, err
	}
	return map[string][]byte{"mcp.json": data}, nil
}

// cursorMCPDocument renders MCP servers under Cursor's "mcpServers" key, the
// same structure as Claude's, or nil when there are none.
func cursorMCPDocument(servers map[string]plugin.MCPServer) ([]byte, error) {
	if len(servers) == 0 {
		return nil, nil
	}
	// Cursor's mcp.json has no transport field: a command is stdio and a
	// url is auto-detected (cursor.com/docs/mcp), so the canonical type is
	// dropped rather than passed through as a key Cursor does not define.
	out := make(map[string]cursorMCPServer, len(servers))
	for name, s := range servers {
		out[name] = cursorMCPServer{
			Command: s.Command,
			Args:    s.Args,
			Env:     s.Env,
			Cwd:     s.Cwd,
			URL:     s.URL,
			Headers: s.Headers,
		}
	}
	data, err := json.MarshalIndent(map[string]any{"mcpServers": out}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshalling MCP config: %w", err)
	}
	return append(data, '\n'), nil
}

// cursorMCPServer is Cursor's mcp.json entry: the canonical fields minus
// the transport, which Cursor infers.
type cursorMCPServer struct {
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Cwd     string            `json:"cwd,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// TransformArtifact rewrites Cursor rule files to the .mdc format Cursor
// requires: renamed from .md to .mdc with injected frontmatter. Plain .md
// files under .cursor/rules are silently ignored by Cursor. Other artifact
// types pass through unchanged.
func (c *Cursor) TransformArtifact(artifactType, name string, data []byte) (string, []byte) {
	if artifactType != "rules" || !strings.HasSuffix(name, ".md") {
		return name, data
	}

	newName := strings.TrimSuffix(name, ".md") + ".mdc"
	description := humanizeRuleName(strings.TrimSuffix(name, ".md"))

	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "description: %s\n", description)
	b.WriteString("alwaysApply: true\n")
	b.WriteString("---\n\n")
	b.Write(data)

	return newName, []byte(b.String())
}

// humanizeRuleName turns a rule filename stem (e.g. "artifact-authoring")
// into a human-readable title (e.g. "Artifact Authoring").
func humanizeRuleName(stem string) string {
	words := strings.FieldsFunc(stem, func(r rune) bool { return r == '-' || r == '_' })
	for i, w := range words {
		if w == "" {
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

func launchCursor(configPath string, extraArgs []string) error {
	agentBin, err := exec.LookPath(cursorCLI)
	if err != nil {
		return err
	}

	// Cursor Agent has no --cwd or --plugin-dir flags.
	// Use symlink-based installation (--install) to integrate with projects.
	// Launch as child process so ynh stays alive for signal handling.
	cmd := exec.Command(agentBin, extraArgs...)
	cmd.Dir = configPath
	return runChildProcess(cmd)
}
