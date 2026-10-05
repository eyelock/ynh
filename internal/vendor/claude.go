package vendor

import (
	"encoding/json"
	"errors"
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

// claudePluginJSON is the Claude Code plugin.json schema: identity fields,
// plus pointers to the plugin's hooks and MCP files when it carries them.
type claudePluginJSON struct {
	Name        string             `json:"name"`
	Version     string             `json:"version"`
	Description string             `json:"description,omitempty"`
	Author      *plugin.AuthorInfo `json:"author,omitempty"`
	Keywords    []string           `json:"keywords,omitempty"`
	Hooks       string             `json:"hooks,omitempty"`
	MCPServers  string             `json:"mcpServers,omitempty"`
}

// claudePluginMCPFile is where an exported Claude plugin carries its MCP
// servers, named by the manifest's "mcpServers" field. A Claude plugin reads
// .mcp.json at its root and what that field names
// (code.claude.com/docs/en/plugins-reference); the root .mcp.json is Codex's
// in a merged package, so Claude gets a file of its own (#481).
var claudePluginMCPFile = filepath.Join("mcp", "claude.json")

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

	// Claude records the cwd it resolved, which is not necessarily the one ynh
	// was handed: os.Getwd honours $PWD, so a shell sitting in /tmp/x reports
	// that while Claude writes its store under -private-tmp-x. Try every form
	// the directory might have been recorded as.
	var candidates []sessionCandidate
	for _, dir := range dirCandidates(cwd) {
		projectDir := filepath.Join(home, ".claude", "projects", claudeProjectSlug(dir))
		entries, err := dirEntriesByModTimeDesc(projectDir)
		if err != nil {
			if errors.Is(err, ErrNoResumableSession) {
				continue
			}
			return "", err
		}

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
// agent's working directory moves into a subdirectory, and a blocking guard
// hook then silently fails open. Commands that are absolute, already anchored
// to a variable, or PATH-style (no leading "./") are left unchanged. It serves
// the project settings file (ClaudeSettingsHooks), where the hooks and their
// scripts belong to the project.
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

// GenerateHookConfig writes the session hook file, .claude/hooks/hooks.json.
// `ynh run` launches Claude with --plugin-dir pointed at the assembled .claude/
// directory, so this is hooks/hooks.json at that plugin's root, the default
// location Claude Code reads (code.claude.com/docs/en/plugins-reference). It
// serves `ynh run`, `ynd preview` and the agent loop. A "./" command names a
// script the harness ships: Claude loads a --plugin-dir plugin in place, so
// the command is anchored to ${CLAUDE_PLUGIN_ROOT}, which is that .claude/
// directory, and session assembly copies the script there (see
// SessionHookScriptDir, #495). An exported plugin uses
// GeneratePluginHookConfig instead.
func (c *Claude) GenerateHookConfig(hooks map[string][]plugin.HookEntry) (map[string][]byte, error) {
	data, err := claudeHookDocument(hooks, pluginRootCommand("CLAUDE_PLUGIN_ROOT"))
	if err != nil || data == nil {
		return nil, err
	}
	return map[string][]byte{filepath.Join(".claude", "hooks", "hooks.json"): data}, nil
}

// SessionHookScriptDir is where session assembly copies the scripts a session
// hook runs by a "./" path: the --plugin-dir plugin root, which
// ${CLAUDE_PLUGIN_ROOT} names in GenerateHookConfig's commands.
func (c *Claude) SessionHookScriptDir() string { return c.ConfigDir() }

// ClaudeSettingsHooks renders canonical hooks as the "hooks" document `ynh hook
// export` merges into a project's .claude/settings.json. A settings file
// belongs to the project, so a "./" command is anchored to $CLAUDE_PROJECT_DIR
// (see anchorHookCommand). It returns nil when no hook maps to a Claude event.
func ClaudeSettingsHooks(hooks map[string][]plugin.HookEntry) ([]byte, error) {
	return claudeHookDocument(hooks, anchorHookCommand)
}

// GeneratePluginHookConfig writes the plugin hook file, hooks/claude.json at
// the root of an exported plugin, which GeneratePluginManifest names in the
// manifest's "hooks" field. The exporter uses it for `ynd export` and
// marketplace packages (#468). The document is the session file's: a "./"
// command names a script shipped in the plugin and is anchored to
// ${CLAUDE_PLUGIN_ROOT} (#483).
func (c *Claude) GeneratePluginHookConfig(hooks map[string][]plugin.HookEntry) (map[string][]byte, error) {
	data, err := claudeHookDocument(hooks, pluginRootCommand("CLAUDE_PLUGIN_ROOT"))
	if err != nil || data == nil {
		return nil, err
	}
	return map[string][]byte{pluginHookFile(c.Name()): data}, nil
}

// claudeHookDocument renders canonical hooks in Claude Code's format, or nil
// when none of them maps to a Claude event. anchor rewrites each command for
// where the file is read.
func claudeHookDocument(hooks map[string][]plugin.HookEntry, anchor func(string) string) ([]byte, error) {
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
				inner = append(inner, claudeInnerHook{Type: "command", Command: anchor(cmd)})
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

	// Claude Code reads plugin hooks from a hooks file, not from a plugin's
	// settings.json (which only supports the "agent" key in plugins).
	return data, nil
}

func (c *Claude) GeneratePluginManifest(hj *plugin.HarnessJSON, outputDir string) (map[string][]byte, error) {
	data, err := claudePluginManifest(hj, outputDir)
	if err != nil {
		return nil, err
	}
	return map[string][]byte{
		filepath.Join(c.PluginManifestDir(), "plugin.json"): data,
	}, nil
}

// claudePluginManifest renders .claude-plugin/plugin.json. Copilot reads the
// same file, so both adapters render it here and a merged package gets the
// same bytes whichever vendor writes it last. The "hooks" field names Claude's
// plugin hook file when outputDir carries one; Claude Code also loads a
// default hooks/hooks.json, which ynh never writes into a plugin. The
// "mcpServers" field names Claude's plugin MCP file when outputDir carries
// one; Claude Code loads a root .mcp.json first and merges the named file
// over it. A session layout carries neither file, so its manifest names
// neither.
func claudePluginManifest(hj *plugin.HarnessJSON, outputDir string) ([]byte, error) {
	pj := &claudePluginJSON{
		Name:        hj.Name,
		Version:     hj.Version,
		Description: hj.Description,
		Author:      hj.Author,
		Keywords:    hj.Keywords,
		Hooks:       pluginHookPointer(outputDir, "claude"),
		MCPServers:  pluginFilePointer(outputDir, claudePluginMCPFile),
	}
	data, err := json.MarshalIndent(pj, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshalling plugin.json: %w", err)
	}
	return append(data, '\n'), nil
}

func (c *Claude) ExportArtifactDirs() map[string]string { return nil }

func (c *Claude) SupportsExportDelegates() bool { return true }

func (c *Claude) PluginManifestDir() string { return ".claude-plugin" }

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

// claudeMCPServer is Claude Code's .mcp.json entry. Type is Claude's own
// spelling: "http" for Streamable HTTP, "sse" for the legacy transport, and
// absent for stdio, which is what an entry without a type has always meant
// there. Claude Code rejects a url entry that carries no type
// (code.claude.com/docs/en/mcp), so the remote case is the one that matters.
type claudeMCPServer struct {
	Type    string            `json:"type,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Cwd     string            `json:"cwd,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// claudeMCPServers maps the canonical servers to Claude Code's .mcp.json
// shape. Codex reads the same shape (developers.openai.com/codex/mcp), so
// its adapter shares this.
func claudeMCPServers(servers map[string]plugin.MCPServer) map[string]claudeMCPServer {
	out := make(map[string]claudeMCPServer, len(servers))
	for name, s := range servers {
		cs := claudeMCPServer{
			Command: s.Command,
			Args:    s.Args,
			Env:     s.Env,
			Cwd:     s.Cwd,
			URL:     s.URL,
			Headers: s.Headers,
		}
		switch s.Transport() {
		case plugin.MCPTypeStreamableHTTP:
			cs.Type = "http"
		case plugin.MCPTypeSSE:
			cs.Type = "sse"
		}
		out[name] = cs
	}
	return out
}

// GenerateMCPConfig writes the session MCP file, .claude/.mcp.json. `ynh run`
// launches Claude with --plugin-dir pointed at the assembled .claude/
// directory, so this is .mcp.json at that plugin's root, the default location
// Claude Code reads. It serves `ynh run`, `ynd preview` and the agent loop.
// An exported plugin uses GeneratePluginMCPConfig instead.
func (c *Claude) GenerateMCPConfig(servers map[string]plugin.MCPServer) (map[string][]byte, error) {
	data, err := claudeMCPDocument(servers)
	if err != nil || data == nil {
		return nil, err
	}
	return map[string][]byte{filepath.Join(".claude", ".mcp.json"): data}, nil
}

// GeneratePluginMCPConfig writes the plugin MCP file, mcp/claude.json at the
// root of an exported plugin, which claudePluginManifest names in the
// manifest's "mcpServers" field. The exporter uses it for `ynd export` and
// marketplace packages (#481). The document is the same as the session file.
func (c *Claude) GeneratePluginMCPConfig(servers map[string]plugin.MCPServer) (map[string][]byte, error) {
	data, err := claudeMCPDocument(servers)
	if err != nil || data == nil {
		return nil, err
	}
	return map[string][]byte{claudePluginMCPFile: data}, nil
}

// claudeMCPDocument renders MCP servers under Claude's "mcpServers" key, in
// Claude's spelling, or nil when there are none.
func claudeMCPDocument(servers map[string]plugin.MCPServer) ([]byte, error) {
	if len(servers) == 0 {
		return nil, nil
	}
	data, err := json.MarshalIndent(map[string]any{"mcpServers": claudeMCPServers(servers)}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshalling MCP config: %w", err)
	}
	return append(data, '\n'), nil
}

func launchClaude(configPath string, initialPrompt string, extraArgs []string) error {
	claudeBin, err := exec.LookPath("claude")
	if err != nil {
		return err
	}

	args := buildClaudeArgs(configPath, initialPrompt, extraArgs)
	return syscall.Exec(claudeBin, args, os.Environ())
}
