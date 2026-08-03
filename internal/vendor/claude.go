package vendor

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/eyelock/ynh/internal/plugin"
)

// claudePluginJSON is the Claude Code plugin.json schema — identity fields only.
type claudePluginJSON struct {
	Name        string             `json:"name"`
	Version     string             `json:"version"`
	Description string             `json:"description,omitempty"`
	Author      *plugin.AuthorInfo `json:"author,omitempty"`
	Keywords    []string           `json:"keywords,omitempty"`
}

func init() {
	Register(&Claude{})
}

// Claude implements the Adapter interface for Claude Code CLI.
type Claude struct{}

func (c *Claude) Name() string        { return "claude" }
func (c *Claude) DisplayName() string { return "Claude Code" }
func (c *Claude) CLIName() string     { return "claude" }

func (c *Claude) ConfigDir() string {
	return ".claude"
}

func (c *Claude) InstructionsFile() string { return "CLAUDE.md" }

func (c *Claude) ArtifactDirs() map[string]string { return DefaultArtifactDirs() }

func (c *Claude) GenerateSystemPrompt(content []byte) map[string][]byte {
	// AGENTS.md: cross-vendor instructions (read by Codex, Cursor, Copilot, etc.)
	// CLAUDE.md: @-import of AGENTS.md (Claude doesn't read AGENTS.md natively)
	// See: https://code.claude.com/docs/en/memory
	return map[string][]byte{
		"AGENTS.md": content,
		"CLAUDE.md": []byte("@AGENTS.md\n"),
	}
}

func (c *Claude) NeedsSymlinks() bool { return false }

func (c *Claude) Install(stagingDir string, projectDir string) ([]SymlinkEntry, error) {
	return nil, nil
}

func (c *Claude) Clean(entries []SymlinkEntry) error {
	return nil
}

func (c *Claude) LaunchInteractive(configPath string, extraArgs []string) error {
	return launchClaude(configPath, "", extraArgs)
}

func (c *Claude) LaunchNonInteractive(configPath string, prompt string, extraArgs []string) error {
	args := append([]string{"-p", prompt}, extraArgs...)
	return launchClaude(configPath, "", args)
}

func (c *Claude) LaunchWithInitialPrompt(configPath, prompt string, extraArgs []string) error {
	return launchClaude(configPath, prompt, extraArgs)
}

func (c *Claude) SupportsInitialPrompt() bool { return true }

func (c *Claude) SupportsResume() bool { return true }

// ResolveLastSession reads Claude's own session store. Claude writes one
// <session-uuid>.jsonl per conversation into
// ~/.claude/projects/<slugified-cwd>/, so the newest file's stem is the id
// `--resume` wants.
//
// This lookup is only correct because Claude is launched via syscall.Exec and
// therefore inherits ynh's cwd — the project directory. (Codex and Cursor set
// cmd.Dir to the run dir, so a cwd-keyed lookup would be meaningless for them.)
func (c *Claude) ResolveLastSession(cwd string, notBefore time.Time) (string, error) {
	home, err := vendorHomeDir()
	if err != nil {
		return "", err
	}

	projectDir := filepath.Join(home, ".claude", "projects", claudeProjectSlug(cwd))
	entries, err := dirEntriesByModTimeDesc(projectDir)
	if err != nil {
		return "", err
	}

	candidates := make([]sessionCandidate, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".jsonl" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		candidates = append(candidates, sessionCandidate{
			id:      strings.TrimSuffix(e.Name(), ".jsonl"),
			modTime: info.ModTime(),
		})
	}
	return newestCandidate(candidates, notBefore)
}

// claudeProjectSlug converts an absolute path into the directory name Claude
// uses under ~/.claude/projects. Every "/" and "." becomes "-", so a leading
// slash yields a leading dash and "/foo/.worktrees" yields "-foo--worktrees".
// Verified against real directory names; other characters are left alone.
//
// The mapping is lossy (a path containing "-" is indistinguishable from one
// containing "/") so it is only ever computed forward from a known cwd. Never
// try to recover a path from a slug.
func claudeProjectSlug(dir string) string {
	return strings.NewReplacer("/", "-", ".", "-").Replace(dir)
}

// LaunchResume continues a prior Claude conversation. An empty sessionID falls
// back to --continue, which Claude documents as "the most recent conversation
// in the current directory" — directory-scoped, so it stays bound to this
// project. A bare --resume is never emitted: it opens the session picker.
func (c *Claude) LaunchResume(configPath, sessionID string, extraArgs []string) error {
	var resumeArgs []string
	if sessionID != "" {
		resumeArgs = []string{"--resume", sessionID}
	} else {
		resumeArgs = []string{"--continue"}
	}
	return launchClaude(configPath, "", append(resumeArgs, extraArgs...))
}

func (c *Claude) ApplyRuntimeInstructions(runDir, text string) ([]string, error) {
	return []string{"--append-system-prompt", text}, nil
}

// buildClaudeArgs constructs the argument list for the Claude Code CLI.
// initialPrompt, when non-empty, is placed first so it precedes --add-dir;
// --add-dir suppresses any positional arg that follows it.
func buildClaudeArgs(configPath string, initialPrompt string, extraArgs []string) []string {
	args := []string{"claude"}

	// Positional prompt must come before --add-dir; --add-dir suppresses any
	// positional arg that follows it in the args list.
	if initialPrompt != "" {
		args = append(args, initialPrompt)
	}

	// Load assembled artifacts (skills, agents, rules, commands) via --plugin-dir.
	// Also --add-dir to grant read access so Claude doesn't prompt for permission
	// when reading plugin files at runtime (the staging dir is outside the project).
	pluginDir := filepath.Join(configPath, ".claude")
	args = append(args, "--plugin-dir", pluginDir)
	args = append(args, "--add-dir", configPath)

	// Inject harness instructions if present.
	instructionsPath := filepath.Join(configPath, "CLAUDE.md")
	if data, err := os.ReadFile(instructionsPath); err == nil && len(data) > 0 {
		args = append(args, "--append-system-prompt", string(data))
	}

	args = append(args, extraArgs...)
	return args
}

// claudeHookEventMap maps canonical event names to Claude Code hook events.
var claudeHookEventMap = map[string]string{
	"before_tool":      "PreToolUse",
	"after_tool":       "PostToolUse",
	"before_prompt":    "UserPromptSubmit",
	"on_stop":          "Stop",
	"on_session_start": "SessionStart",
}

// anchorHookCommand rewrites a leading "./" in a hook command so it resolves
// from the project root via $CLAUDE_PROJECT_DIR, which Claude Code injects into
// the hook subprocess. Without this, a relative command breaks the moment the
// agent's working directory moves into a subdirectory — and a blocking guard
// hook then silently fails open. Commands that are absolute, already anchored
// to a variable, or PATH-style (no leading "./") are left unchanged.
func anchorHookCommand(cmd string) string {
	if strings.HasPrefix(cmd, "./") {
		return "$CLAUDE_PROJECT_DIR/" + cmd[2:]
	}
	return cmd
}

// ClaudeHookEvent returns the Claude-native event name for a canonical event
// (e.g. "after_tool" → "PostToolUse"), or ("", false) if the name is not a
// canonical event. It is the single source of the canonical→Claude mapping.
func ClaudeHookEvent(canonical string) (string, bool) {
	native, ok := claudeHookEventMap[canonical]
	return native, ok
}

func (c *Claude) GenerateHookConfig(hooks map[string][]plugin.HookEntry) (map[string][]byte, error) {
	if len(hooks) == 0 {
		return nil, nil
	}

	// Claude's three-level structure:
	// { "hooks": { "PreToolUse": [ { "matcher": "X", "hooks": [ { "type": "command", "command": "..." } ] } ] } }

	type claudeInnerHook struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	}
	type claudeHookGroup struct {
		Matcher string            `json:"matcher,omitempty"`
		Hooks   []claudeInnerHook `json:"hooks"`
	}

	allEvents := make(map[string][]claudeHookGroup)

	// Process events in sorted order for deterministic output
	var events []string
	for event := range hooks {
		events = append(events, event)
	}
	sort.Strings(events)

	for _, event := range events {
		entries := hooks[event]
		claudeEvent, ok := claudeHookEventMap[event]
		if !ok {
			continue
		}

		// Group entries by matcher
		type matcherGroup struct {
			matcher string
			cmds    []string
		}
		var groups []matcherGroup
		groupIdx := make(map[string]int)

		for _, entry := range entries {
			key := entry.Matcher
			if idx, exists := groupIdx[key]; exists {
				groups[idx].cmds = append(groups[idx].cmds, entry.Command)
			} else {
				groupIdx[key] = len(groups)
				groups = append(groups, matcherGroup{matcher: key, cmds: []string{entry.Command}})
			}
		}

		var hookGroups []claudeHookGroup
		for _, g := range groups {
			var inner []claudeInnerHook
			for _, cmd := range g.cmds {
				inner = append(inner, claudeInnerHook{Type: "command", Command: anchorHookCommand(cmd)})
			}
			hookGroups = append(hookGroups, claudeHookGroup{
				Matcher: g.matcher,
				Hooks:   inner,
			})
		}

		allEvents[claudeEvent] = hookGroups
	}

	if len(allEvents) == 0 {
		return nil, nil
	}

	settings := map[string]any{
		"hooks": allEvents,
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshalling hook config: %w", err)
	}
	data = append(data, '\n')

	// Write to hooks/hooks.json inside the plugin dir (.claude/).
	// Claude Code discovers hooks from plugins via hooks/hooks.json,
	// not from settings.json (which only supports the "agent" key in plugins).
	return map[string][]byte{
		filepath.Join(".claude", "hooks", "hooks.json"): data,
	}, nil
}

func (c *Claude) GeneratePluginManifest(hj *plugin.HarnessJSON, outputDir string) (map[string][]byte, error) {
	pj := &claudePluginJSON{
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
		filepath.Join(".claude-plugin", "plugin.json"): data,
	}, nil
}

func (c *Claude) ExportArtifactDirs() map[string]string { return nil }

func (c *Claude) SupportsExportDelegates() bool { return true }

func (c *Claude) MarketplaceManifestDir() string { return ".claude-plugin" }

func (c *Claude) GenerateMarketplaceIndex(cfg MarketplaceIndexConfig, plugins []MarketplacePluginInfo) ([]byte, error) {
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

func (c *Claude) GenerateMCPConfig(servers map[string]plugin.MCPServer) (map[string][]byte, error) {
	if len(servers) == 0 {
		return nil, nil
	}

	// Claude uses .mcp.json with "mcpServers" key — direct passthrough
	config := map[string]any{
		"mcpServers": servers,
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshalling MCP config: %w", err)
	}
	data = append(data, '\n')

	// Write inside the plugin dir (.claude/) so Claude Code discovers it
	// as a plugin-provided MCP server configuration.
	return map[string][]byte{
		filepath.Join(".claude", ".mcp.json"): data,
	}, nil
}

func launchClaude(configPath string, initialPrompt string, extraArgs []string) error {
	claudeBin, err := exec.LookPath("claude")
	if err != nil {
		return err
	}

	args := buildClaudeArgs(configPath, initialPrompt, extraArgs)
	return syscall.Exec(claudeBin, args, os.Environ())
}
