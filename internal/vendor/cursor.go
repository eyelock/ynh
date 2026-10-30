package vendor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
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
	for _, w := range CursorPrintModeWarnings(configPath) {
		fmt.Fprintf(os.Stderr, "  warning: %s\n", w)
	}
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
//
// A prompt is refused: Cursor's CLI reference lists --resume, --continue and
// the positional prompt separately and documents no combination of them
// (cursor.com/docs/cli/reference/parameters).
func (c *Cursor) LaunchResume(configPath, sessionID, prompt string, interactive bool, extraArgs []string) error {
	if prompt != "" {
		return ErrResumePromptNotSupported
	}
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

// cursorHookEventMap maps canonical event names to Cursor hook events
// (cursor.com/docs/hooks lists 18 agent events plus Tab and app-lifecycle
// ones). before_tool and after_tool map to preToolUse and postToolUse, which
// fire for any tool (Shell, Read, Write, Grep, Delete, Task, MCP), so they
// mean what they mean on Claude Code and Codex. They used to map to
// beforeShellExecution and afterFileEdit, which cover shell commands and file
// edits only. sessionEnd is tied to the IDE composer session: Cursor does not
// run it for cloud agents, and the docs do not say whether the CLI does.
var cursorHookEventMap = map[string]string{
	"before_tool":      "preToolUse",
	"after_tool":       "postToolUse",
	"before_prompt":    "beforeSubmitPrompt",
	"on_stop":          "stop",
	"on_session_start": "sessionStart",
	"on_session_end":   "sessionEnd",
}

// cursorUnsupportedHookEvents names the canonical events Cursor cannot
// express, each with the reason. Empty: every canonical event maps.
var cursorUnsupportedHookEvents = map[string]string{}

// Per-script timeout, in seconds, written as "timeout" (cursor.com/docs/hooks,
// "Command format": per-script options include "timeout (seconds, default is
// platform-dependent)"). The page documents no maximum.
var cursorHookTimeoutField = "timeout"
var cursorHookTimeoutUnsupported = ""

var cursorHookTimeoutCaps = map[string]int{}

// cursorMatcherEvents are the native events whose matcher Cursor documents for
// a canonical hook: preToolUse and postToolUse match the tool type (Shell,
// Read, Write, Grep, Delete, Task, MCP:<tool>) as a regex. Cursor documents no
// matcher for sessionStart or sessionEnd, and stop, beforeSubmitPrompt match a
// fixed word (Stop, UserPromptSubmit) so a canonical matcher is meaningless.
var cursorMatcherEvents = map[string]bool{"preToolUse": true, "postToolUse": true}

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

	// Cursor flat format: { "hooks": { "preToolUse": [ { "command": "...", "matcher": "Shell" } ] } }
	type cursorHookEntry struct {
		Command string `json:"command"`
		Matcher string `json:"matcher,omitempty"`
		Timeout int    `json:"timeout,omitempty"`
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
			e := cursorHookEntry{Command: anchor(entry.Command), Timeout: hookTimeoutValue("cursor", event, entry.Timeout)}
			if cursorMatcherEvents[cursorEvent] {
				e.Matcher = entry.Matcher
			}
			hookEntries = append(hookEntries, e)
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
// requires: renamed from .md to .mdc with Cursor's frontmatter. Plain .md
// files under .cursor/rules are silently ignored by Cursor. Other artifact
// types pass through unchanged.
func (c *Cursor) TransformArtifact(artifactType, name string, data []byte) (string, []byte) {
	if artifactType != "rules" || !strings.HasSuffix(name, ".md") {
		return name, data
	}
	stem := strings.TrimSuffix(name, ".md")
	return stem + ".mdc", cursorRule(stem, data)
}

// cursorRule renders a rule as an .mdc file with exactly one frontmatter
// block (#532). Cursor reads three fields, description, globs and
// alwaysApply (cursor.com/docs/context/rules), so a source rule's own
// frontmatter is merged into them rather than left in the body as a second
// block:
//
//   - description: the source's, else the humanised file name.
//   - globs: the source's globs, else its paths (Claude Code's rule scoping),
//     comma-joined the way Cursor writes them. A globs scalar is already
//     Cursor's form and is kept verbatim.
//   - alwaysApply: the source's when it is a valid boolean; otherwise false
//     for a rule with globs, which Cursor would ignore under alwaysApply:
//     true, and true for one without.
//
// Every other source field (name, for one) is dropped: Cursor does not read
// it, and the .mdc is a rendering for Cursor alone. A source with no closed
// frontmatter block is all body.
func cursorRule(stem string, data []byte) []byte {
	description := humanizeRuleName(stem)
	var globs, paths []string
	alwaysApply, explicit := true, false
	body := data

	if lines, rest, ok := splitFrontmatter(data); ok {
		body = bytes.TrimLeft(rest, "\r\n")
		for _, e := range frontmatterEntries(lines) {
			switch e.key {
			case "description":
				if d := strings.Join(strings.Fields(e.scalar()), " "); d != "" {
					description = d
				}
			case "globs":
				globs = e.list()
			case "paths":
				paths = e.list()
			case "alwaysApply":
				if v, err := strconv.ParseBool(e.scalar()); err == nil {
					alwaysApply, explicit = v, true
				}
			}
		}
	}
	if len(globs) == 0 {
		globs = paths
	}
	if len(globs) > 0 && !explicit {
		alwaysApply = false
	}

	var b bytes.Buffer
	b.WriteString("---\n")
	fmt.Fprintf(&b, "description: %s\n", description)
	if len(globs) > 0 {
		fmt.Fprintf(&b, "globs: %s\n", strings.Join(globs, ","))
	}
	fmt.Fprintf(&b, "alwaysApply: %t\n", alwaysApply)
	b.WriteString("---\n\n")
	b.Write(body)
	return b.Bytes()
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

// Workspace folders: Cursor's CLI has no flag that adds a directory. Its
// documented global options (cursor.com/docs/cli/reference/parameters) hold
// --workspace <path>, which names the one workspace directory, and
// `sandbox run --allow-paths`, which belongs to a different command. Neither
// grants an extra folder to an agent session, so a requested folder is
// refused with a warning rather than dropped.
var cursorWorkspaceFlag = ""
var cursorWorkspaceUnsupported = "the Cursor CLI has no flag that adds a directory to a session (--workspace names the single workspace)"
