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

// cursorPluginJSON is the Cursor plugin.json schema — identity fields only.
type cursorPluginJSON struct {
	Name        string             `json:"name"`
	Version     string             `json:"version"`
	Description string             `json:"description,omitempty"`
	Author      *plugin.AuthorInfo `json:"author,omitempty"`
	Keywords    []string           `json:"keywords,omitempty"`
}

func init() {
	Register(&Cursor{})
}

// Cursor implements the Adapter interface for Cursor Agent CLI.
// Uses .cursor/rules/ for rules and .cursorrules at project root.
type Cursor struct{}

func (c *Cursor) Name() string        { return "cursor" }
func (c *Cursor) DisplayName() string { return "Cursor" }
func (c *Cursor) CLIName() string     { return "agent" }

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

func (c *Cursor) GenerateHookConfig(hooks map[string][]plugin.HookEntry) (map[string][]byte, error) {
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
			hookEntries = append(hookEntries, cursorHookEntry{Command: entry.Command})
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

	// Same JSON shape and event names in both locations — only the path
	// differs: .cursor/hooks.json for project-level config (read by `ynh run`
	// staging), hooks/hooks.json at plugin root for plugin-format export
	// (cursor.com/docs/reference/plugins, "Define hooks in hooks/hooks.json").
	// There's no "is this a plugin export" flag threaded through Adapter, so
	// both are always emitted — the unused one is simply inert in the other
	// context.
	return map[string][]byte{
		filepath.Join(".cursor", "hooks.json"): data,
		filepath.Join("hooks", "hooks.json"):   data,
	}, nil
}

func (c *Cursor) GeneratePluginManifest(hj *plugin.HarnessJSON, outputDir string) (map[string][]byte, error) {
	pj := &cursorPluginJSON{
		Name:        hj.Name,
		Version:     hj.Version,
		Description: hj.Description,
		Author:      hj.Author,
		Keywords:    hj.Keywords,
	}
	data, err := json.MarshalIndent(pj, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshalling plugin.json: %w", err)
	}
	data = append(data, '\n')
	return map[string][]byte{
		filepath.Join(".cursor-plugin", "plugin.json"): data,
	}, nil
}

func (c *Cursor) ExportArtifactDirs() map[string]string { return nil }

func (c *Cursor) SupportsExportDelegates() bool { return true }

func (c *Cursor) MarketplaceManifestDir() string { return ".cursor-plugin" }

func (c *Cursor) GenerateMarketplaceIndex(cfg MarketplaceIndexConfig, plugins []MarketplacePluginInfo) ([]byte, error) {
	type indexPlugin struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		Version     string `json:"version,omitempty"`
		Source      string `json:"source"`
	}
	type indexOwner struct {
		Name  string `json:"name"`
		Email string `json:"email,omitempty"`
	}
	type indexJSON struct {
		Name        string        `json:"name"`
		Owner       indexOwner    `json:"owner"`
		Description string        `json:"description,omitempty"`
		Plugins     []indexPlugin `json:"plugins"`
	}

	idx := indexJSON{
		Name:        cfg.Name,
		Owner:       indexOwner{Name: cfg.OwnerName, Email: cfg.OwnerEmail},
		Description: cfg.Description,
	}
	for _, p := range plugins {
		idx.Plugins = append(idx.Plugins, indexPlugin{
			Name:        p.Name,
			Description: p.Description,
			Version:     p.Version,
			Source:      "./plugins/" + p.Name,
		})
	}

	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
	return data, nil
}

func (c *Cursor) GenerateMCPConfig(servers map[string]plugin.MCPServer) (map[string][]byte, error) {
	if len(servers) == 0 {
		return nil, nil
	}

	// Cursor uses "mcpServers" key — same structure as Claude. Written at two
	// locations: .cursor/mcp.json for project-level config (read by `ynh run`
	// staging) and mcp.json (no dot) at plugin root for plugin-format export
	// (cursor.com/docs/reference/plugins). Both are the same content; there's
	// no "is this a plugin export" flag threaded through Adapter, so both are
	// always emitted — the unused one is simply inert in the other context.
	config := map[string]any{
		"mcpServers": servers,
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshalling MCP config: %w", err)
	}
	data = append(data, '\n')

	return map[string][]byte{
		filepath.Join(".cursor", "mcp.json"): data,
		"mcp.json":                           data,
	}, nil
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
	agentBin, err := exec.LookPath("agent")
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
