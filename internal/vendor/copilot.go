package vendor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/eyelock/ynh/internal/plugin"
)

func init() {
	Register(&Copilot{})
}

// copilotCLI is GitHub Copilot CLI's binary: see CLIName in adapter.go.
const copilotCLI = "copilot"

// Copilot implements the Adapter interface for GitHub Copilot CLI.
//
// Launch strategy mirrors Claude: --plugin-dir for native plugin loading,
// no symlinks, syscall.Exec for a clean process replacement. Confirmed via
// `copilot help`: --plugin-dir <directory> ("Load a plugin from a local
// directory") is a direct analog of Claude's flag.
//
// Two confirmed gaps from Claude's model, both hand-tested against v1.0.75:
//   - Copilot has no --append-system-prompt-equivalent flag.
//   - A plugin loaded via --plugin-dir does NOT get its bundled AGENTS.md or
//     .mcp.json read (skills and agents DO load correctly; instructions and
//     MCP config do not). See launchCopilot for how this is worked around.
type Copilot struct{}

func (c *Copilot) Name() string        { return "copilot" }
func (c *Copilot) DisplayName() string { return "GitHub Copilot CLI" }
func (c *Copilot) CLIName() string     { return copilotCLI }

func (c *Copilot) ConfigDir() string {
	return ".copilot"
}

func (c *Copilot) InstructionsFile() string { return "AGENTS.md" }

func (c *Copilot) ArtifactDirs() map[string]string { return DefaultArtifactDirs() }

func (c *Copilot) GenerateSystemPrompt(content []byte) map[string][]byte {
	// AGENTS.md: Copilot natively reads this from a project (the
	// --no-custom-instructions flag exists to disable it). A plugin's bundled
	// AGENTS.md is not read: OBSERVED live on Copilot CLI 1.0.105, 2026-11-05,
	// with --plugin-dir (docs/vendor-parity.md L20, #722). Whether an
	// installed plugin reads it has not been run. No export calls this for
	// Copilot now: ExportCarriesInstructions is false, so the export writes no
	// AGENTS.md and warns. The method stays because the adapter interface
	// requires it.
	return map[string][]byte{
		"AGENTS.md": content,
	}
}

// FoldsRulesIntoInstructions: Copilot does not read the rules ynh stages in the
// config dir, so the assembler appends them to the instructions file it does
// read (assembler.RuleFolder).
func (c *Copilot) FoldsRulesIntoInstructions() bool { return true }

func (c *Copilot) NeedsSymlinks() bool { return false }

func (c *Copilot) Install(stagingDir string, projectDir string) ([]SymlinkEntry, error) {
	return nil, nil
}

func (c *Copilot) Clean(entries []SymlinkEntry) error {
	return nil
}

func (c *Copilot) LaunchInteractive(configPath string, extraArgs []string) error {
	return launchCopilot(configPath, "", extraArgs)
}

func (c *Copilot) LaunchNonInteractive(configPath string, prompt string, extraArgs []string) error {
	// --allow-all-tools is documented as required for non-interactive mode —
	// without it, a scripted run hangs on a permission prompt with no TTY to
	// answer it. Confirmed via `copilot help`.
	args := append([]string{"-p", prompt, "--allow-all-tools"}, extraArgs...)
	return launchCopilot(configPath, "", args)
}

func (c *Copilot) LaunchWithInitialPrompt(configPath, prompt string, extraArgs []string) error {
	return launchCopilot(configPath, prompt, extraArgs)
}

func (c *Copilot) SupportsInitialPrompt() bool { return true }

// ReplacesProcess is true: every Copilot launch ends in syscall.Exec.
func (c *Copilot) ReplacesProcess() bool { return true }

func (c *Copilot) SupportsResume() bool { return true }

// ResolveLastSession reads Copilot's own session store. Each session gets a
// ~/.copilot/session-state/<id>/workspace.yaml recording its id and cwd, so
// resolution is a newest-first directory walk that stops at the first entry
// matching cwd.
//
// Copilot also maintains ~/.copilot/session-store.db, a sqlite index carrying
// the same id→cwd mapping. The flat files are used instead because reading
// sqlite would mean taking a driver dependency, and ynh is deliberately
// standard-library-only.
//
// workspace.yaml is an internal file with no compatibility promise (shape
// confirmed against copilot 1.0.77). Every parse failure degrades to
// ErrNoResumableSession — a cold launch — rather than an error.
func (c *Copilot) ResolveLastSession(cwd string, notBefore time.Time) (string, error) {
	home, err := vendorHomeDir()
	if err != nil {
		return "", err
	}

	stateDir := filepath.Join(home, ".copilot", "session-state")
	entries, err := dirEntriesByModTimeDesc(stateDir)
	if err != nil {
		return "", err
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if !notBefore.IsZero() && info.ModTime().Before(notBefore) {
			// Entries are newest-first, so everything after this is older too.
			break
		}

		id, sessionCwd, err := readCopilotWorkspace(filepath.Join(stateDir, e.Name(), "workspace.yaml"))
		if err != nil || !sameDir(sessionCwd, cwd) {
			continue
		}
		if id == "" {
			// Fall back to the directory name, which is the id too.
			id = e.Name()
		}
		return id, nil
	}
	return "", ErrNoResumableSession
}

// readCopilotWorkspace extracts the id and cwd from a Copilot workspace.yaml.
// Hand-parsed rather than pulled through a YAML library: the file is flat, only
// two top-level scalars are needed, and ynh takes no external dependencies.
func readCopilotWorkspace(path string) (id string, cwd string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}

	for _, line := range strings.Split(string(data), "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found || strings.HasPrefix(strings.TrimSpace(key), "#") {
			continue
		}
		// Only top-level keys: anything indented belongs to a nested mapping.
		if key != strings.TrimLeft(key, " \t") {
			continue
		}
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		switch strings.TrimSpace(key) {
		case "id":
			id = value
		case "cwd":
			cwd = value
		}
	}
	if cwd == "" {
		return "", "", fmt.Errorf("no cwd recorded in %s", path)
	}
	return id, cwd, nil
}

// LaunchResume continues a prior Copilot session.
//
// An empty sessionID uses --continue, but that is a deliberate last resort:
// Copilot documents --continue as "the most recent session" with no directory
// qualifier, so it can resume a session belonging to an entirely different
// worktree. Prefer an explicit id. A bare --resume is never emitted — Copilot's
// own help describes it as "using session picker".
//
// A prompt is delivered to the resumed session: one-shot as `-p <prompt>`
// with --allow-all-tools, as a fresh one-shot run is, and interactive as
// `-i <prompt>`, which opens the session with it as the first message. The
// one-shot form was observed to answer from the earlier session (Copilot CLI
// 1.0.105, 2026-11-05): `copilot --resume=<id> -p "<prompt>"`.
func (c *Copilot) LaunchResume(configPath, sessionID, prompt string, interactive bool, extraArgs []string) error {
	initial, args := copilotResumeArgs(sessionID, prompt, interactive, extraArgs)
	return launchCopilot(configPath, initial, args)
}

// copilotResumeArgs is a resume's launch inputs: the interactive prompt, which
// launchCopilot places as -i, and the arguments after it.
func copilotResumeArgs(sessionID, prompt string, interactive bool, extraArgs []string) (string, []string) {
	var args []string
	if sessionID != "" {
		args = []string{"--resume=" + sessionID}
	} else {
		args = []string{"--continue"}
	}
	initial := ""
	if prompt != "" {
		if interactive {
			initial = prompt
		} else {
			args = append(args, "-p", prompt, "--allow-all-tools")
		}
	}
	return initial, append(args, extraArgs...)
}

// IsolateMCP keeps the user's configured servers out of a launch. Copilot lists
// every server it would load (`copilot mcp list --json`: the user's, the
// workspace's, installed plugins' and the built-ins) and takes
// --disable-mcp-server <name> per launch, so each listed server the harness
// does not compose is turned off, and --disable-builtin-mcps drops the
// built-ins as a group. The harness's own servers arrive by
// --additional-mcp-config and are not in that listing. Verified live on
// Copilot CLI 1.0.106 (docs/vendor-parity.md, L6). If the listing cannot be
// read, only the built-ins are dropped and ynh says so.
func (c *Copilot) IsolateMCP(configPath string) ([]string, string) {
	bin, err := exec.LookPath(c.CLIName())
	if err != nil {
		return []string{"--disable-builtin-mcps"}, fmt.Sprintf("MCP isolation is partial on Copilot (%v): built-in servers are disabled, but servers in ~/.copilot/mcp-config.json still load", err)
	}
	listed, err := copilotMCPListNames(bin, nil, "")
	if err != nil {
		return []string{"--disable-builtin-mcps"}, fmt.Sprintf("MCP isolation is partial on Copilot (%v): built-in servers are disabled, but servers in ~/.copilot/mcp-config.json still load", err)
	}
	return copilotIsolationArgs(listed, harnessMCPNames(filepath.Join(configPath, ".copilot", ".mcp.json"), false)), ""
}

// ApplyRuntimeInstructions appends per-invocation text to the assembled
// AGENTS.md in runDir. buildCopilotArgs reads that same file later in this
// invocation and projects it into the project's own instructions file, so
// the runtime overlay and the harness's base instructions arrive together.
func (c *Copilot) ApplyRuntimeInstructions(runDir, text string) ([]string, error) {
	agentsPath := filepath.Join(runDir, "AGENTS.md")
	f, err := os.OpenFile(agentsPath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("opening AGENTS.md: %w", err)
	}
	if _, err := fmt.Fprintf(f, "\n\n%s\n", text); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("writing runtime instructions: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("closing AGENTS.md: %w", err)
	}
	return nil, nil
}

// copilotHookEventMap maps canonical event names to Copilot hook events, in the
// PascalCase spelling of the VS Code compatible format (SessionStart,
// UserPromptSubmit, PreToolUse, PostToolUse, Stop, SessionEnd), per
// docs.github.com/en/copilot/reference/hooks-configuration. Copilot also
// accepts camelCase native names (sessionStart, userPromptSubmitted,
// preToolUse, postToolUse, agentStop, sessionEnd). PascalCase is used because
// the page says a PascalCase PreToolUse takes Claude-style matchers: "*" or an
// empty value matches all tools, and "Edit|Write" matches the Claude tool
// names (runtime edit maps to Edit, bash to Bash, view to Read). Canonical
// matchers are written in those names, so they keep their meaning. The
// payload is also Claude's snake_case one (hook_event_name, session_id).
// CONFIRMED live (Copilot CLI 1.0.105, 2026-10-30): a Bash matcher fires on both
// PreToolUse and PostToolUse, and an Edit matcher does not fire for a shell
// call; tool_name is Bash in the payload.
var copilotHookEventMap = map[string]string{
	"before_tool":      "PreToolUse",
	"after_tool":       "PostToolUse",
	"before_prompt":    "UserPromptSubmit",
	"on_stop":          "Stop",
	"on_session_start": "SessionStart",
	"on_session_end":   "SessionEnd",
}

// How on_session_start output reaches the model: CONFIRMED live (Copilot
// CLI 1.0.105, 2026-10-30): plain stdout does NOT reach the model;
// {"additionalContext": "..."} does
// (docs.github.com/en/copilot/reference/hooks-configuration: "Only
// additionalContext is consumed for sessionStart"). ynh wraps the hook.
var copilotSessionStartContext = "json-field:additionalContext"

// copilotUnsupportedHookEvents names the canonical events Copilot cannot
// express, each with the reason. Empty: every canonical event maps.
var copilotUnsupportedHookEvents = map[string]string{}

// Per-hook timeout, in seconds, written as "timeoutSec"
// (docs.github.com/en/copilot/reference/hooks-configuration: "Timeout in
// seconds. Default: 30"; "timeout" is an alias used only when timeoutSec is
// absent). The page documents no maximum. CONFIRMED live (Copilot CLI
// 1.0.105, 2026-10-30): timeoutSec 1 killed a 3 s hook, 10 let it finish.
var copilotHookTimeoutField = "timeoutSec"
var copilotHookTimeoutUnsupported = ""

var copilotHookTimeoutCaps = map[string]int{}

// copilotMatcherEvents are the native events whose matcher Copilot documents:
// the tool events match the tool name. The page documents no matcher for
// session or prompt events, so ynd does not write one there.
var copilotMatcherEvents = map[string]bool{"PreToolUse": true, "PostToolUse": true}

// copilotPluginRootVar is the variable a "./" hook command is anchored to. The
// CLI plugin reference documents PLUGIN_ROOT, set in the environment of stdio
// MCP servers and expanded in their args, env and cwd, and CLAUDE_PLUGIN_ROOT
// and COPILOT_PLUGIN_ROOT as aliases in agent mcp-servers frontmatter. It does
// not say whether a hook command sees any of them. CONFIRMED live (Copilot CLI 1.0.105, 2026-10-30):
// all three are set to the plugin directory for a hook, and
// "${PLUGIN_ROOT}"/... expands (#497). A hook script also runs with the plugin
// directory as its working directory; the project is the payload's cwd.
const copilotPluginRootVar = "PLUGIN_ROOT"

// copilotSessionHookFile is where `ynh run`, `ynd preview` and the agent loop
// put Copilot's hooks: hooks/hooks.json at the root of the .copilot/ directory
// buildCopilotArgs passes to --plugin-dir. A plugin reads hooks.json or
// hooks/hooks.json from its installation directory
// (docs.github.com/en/copilot/reference/hooks-configuration, "Plugins").
var copilotSessionHookFile = filepath.Join(".copilot", "hooks", "hooks.json")

// copilotPluginHookFile is where an exported plugin carries Copilot's hooks:
// hooks.json at the plugin root, one of the two default names Copilot reads.
// It is deliberately not hooks/hooks.json, which Claude Code, Cursor and Codex
// all read by default and would take for their own format (#469). Claude,
// Cursor and Codex read no root hooks.json.
const copilotPluginHookFile = "hooks.json"

// GenerateHookConfig writes the session hook file, .copilot/hooks/hooks.json.
// `ynh run` launches Copilot with --plugin-dir pointed at the assembled
// .copilot/ directory, so this is the plugin's own hooks/hooks.json. A "./"
// command names a script the harness ships and is anchored to
// "${PLUGIN_ROOT}", with session assembly copying the script into .copilot/
// (see SessionHookScriptDir). An exported plugin uses GeneratePluginHookConfig.
//
// Earlier ynh returned nothing here: a hand test against v1.0.75 saw a
// preToolUse hook in .github/hooks/ never fire in an untrusted folder. The
// current page says "Repository and inherited-agent hooks require workspace
// admission", and says nothing of plugin hooks. So ynh does not write
// .github/hooks/ into the project, and rests on the plugin route. CONFIRMED
// live (Copilot CLI 1.0.105, 2026-10-30): all six events fire from this file via --plugin-dir in
// an UNTRUSTED folder, so plugin hooks need no folder trust. The repository
// .github/hooks/ route was not retested. See
// .claude/skills/vendor-adapters/references/copilot.md.
func (c *Copilot) GenerateHookConfig(hooks map[string][]plugin.HookEntry) (map[string][]byte, error) {
	data, err := copilotHookDocument(hooks, pluginRootCommand(copilotPluginRootVar))
	if err != nil || data == nil {
		return nil, err
	}
	files := map[string][]byte{copilotSessionHookFile: data}
	sessionContextFiles(files, filepath.Join(c.ConfigDir(), SessionContextScript), sessionContextMode("copilot"), hooks)
	return files, nil
}

// SessionHookScriptDir is where session assembly copies the scripts a session
// hook runs by a "./" path: the --plugin-dir plugin root, which
// ${PLUGIN_ROOT} names in GenerateHookConfig's commands.
func (c *Copilot) SessionHookScriptDir() string { return c.ConfigDir() }

// GeneratePluginHookConfig writes the plugin hook file, hooks.json at the
// plugin root, for `ynd export` and marketplace packages. The document is the
// session file's: "./" commands are anchored to "${PLUGIN_ROOT}", and the
// exporter copies the scripts into the plugin.
//
// In a Copilot-only export the manifest (.claude-plugin/plugin.json) names no
// hooks file, so Copilot finds this one by default. In a package that also
// targets Claude the shared manifest names hooks/claude.json, and CONFIRMED
// live (Copilot CLI 1.0.105, 2026-10-30) Copilot loads that file and ignores a root hooks.json, so
// the exporter does not write this file there (#499).
func (c *Copilot) GeneratePluginHookConfig(hooks map[string][]plugin.HookEntry) (map[string][]byte, error) {
	data, err := copilotHookDocument(hooks, pluginRootCommand(copilotPluginRootVar))
	if err != nil || data == nil {
		return nil, err
	}
	files := map[string][]byte{copilotPluginHookFile: data}
	sessionContextFiles(files, SessionContextScript, sessionContextMode("copilot"), hooks)
	return files, nil
}

// copilotHookDocument renders canonical hooks in Copilot's format, or nil when
// none of them maps to a Copilot event. The file is version 1 with a "hooks"
// object keyed by event, each value a flat array of command entries (no
// matcher groups): {"type": "command", "command": "...", "cwd": ".", "matcher": "..."}.
// "cwd": "." runs the command from the repository root rather than the plugin
// folder (CONFIRMED live, Copilot CLI 1.0.105, 2026-10-30); ynh's own "./"
// scripts are anchored with ${PLUGIN_ROOT} and unaffected.
// "command" is the cross-platform spelling; Copilot copies it to bash and
// powershell when those are absent. timeoutSec is written only when the hook
// declares a timeout; otherwise Copilot's documented default of 30 s applies.
// anchor rewrites each command for where the file is read.
func copilotHookDocument(hooks map[string][]plugin.HookEntry, anchor func(string) string) ([]byte, error) {
	if len(hooks) == 0 {
		return nil, nil
	}

	type copilotHookEntry struct {
		Type    string `json:"type"`
		Command string `json:"command"`
		Cwd     string `json:"cwd"`
		Matcher string `json:"matcher,omitempty"`
		Timeout int    `json:"timeoutSec,omitempty"`
	}

	allEvents := make(map[string][]copilotHookEntry)
	for _, event := range slices.Sorted(maps.Keys(hooks)) {
		copilotEvent, ok := copilotHookEventMap[event]
		if !ok {
			continue
		}
		var entries []copilotHookEntry
		for _, entry := range hooks[event] {
			e := copilotHookEntry{Type: "command", Command: sessionCommand(sessionContextMode("copilot"), event, entry.Command, anchor), Cwd: ".", Timeout: hookTimeoutValue("copilot", event, entry.Timeout)}
			if copilotMatcherEvents[copilotEvent] {
				e.Matcher = entry.Matcher
			}
			entries = append(entries, e)
		}
		allEvents[copilotEvent] = entries
	}
	if len(allEvents) == 0 {
		return nil, nil
	}

	data, err := json.MarshalIndent(map[string]any{"version": 1, "hooks": allEvents}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshalling hook config: %w", err)
	}
	return append(data, '\n'), nil
}

// GeneratePluginManifest writes the manifest for the `ynh run` layout, which
// `ynd preview` shares: skills and agents nest under .copilot/, the directory
// buildCopilotArgs passes to --plugin-dir, so the manifest sits at
// .copilot/.claude-plugin/plugin.json beside them. Confirmed by hand-testing
// that Copilot silently fails to load ANY plugin content via --plugin-dir when
// .claude-plugin/plugin.json isn't present at that exact directory's root.
// An export flattens its content to the plugin root and uses
// GenerateExportPluginManifest instead. The layout is chosen by the caller,
// never inferred from the files present: an export's own .copilot/.mcp.json
// once made it look like a run dir (#471).
func (c *Copilot) GeneratePluginManifest(hj *plugin.HarnessJSON, outputDir string) (map[string][]byte, error) {
	data, err := claudePluginManifest(hj, outputDir)
	if err != nil {
		return nil, err
	}
	return map[string][]byte{filepath.Join(c.ConfigDir(), c.PluginManifestDir(), "plugin.json"): data}, nil
}

// GenerateExportPluginManifest writes the manifest for an exported plugin, at
// .claude-plugin/plugin.json in the plugin root, next to the skills and
// agents the exporter copies there. Copilot's manifest search order includes
// that path (docs.github.com, Copilot CLI plugin reference).
//
// The file is Claude's manifest, so both are rendered by claudePluginManifest:
// in a merged package Claude and Copilot both write it, and a Copilot render
// that differed would drop Claude's "hooks" and "mcpServers" pointers whenever
// Copilot wrote last (#469, #481). Copilot's own hooks go in hooks.json, which
// this manifest does not name (see GeneratePluginHookConfig), and its MCP file, .github/mcp.json, is one of its
// defaults, so a Copilot-only export names neither. In a package that also
// carries Claude, the manifest's "mcpServers" names Claude's mcp/claude.json,
// which Copilot reads too; see GeneratePluginMCPConfig.
func (c *Copilot) GenerateExportPluginManifest(hj *plugin.HarnessJSON, outputDir string) (map[string][]byte, error) {
	data, err := claudePluginManifest(hj, outputDir)
	if err != nil {
		return nil, err
	}
	return map[string][]byte{filepath.Join(c.PluginManifestDir(), "plugin.json"): data}, nil
}

func (c *Copilot) ExportArtifactDirs() map[string]string {
	// Skills, agents and commands are carried. Rules have no reliable
	// "always-on" equivalent (Copilot's applyTo-scoped instructions files
	// aren't on by default), so they are excluded. Copilot CLI has no slash
	// commands of its own, but a plugin's commands/ load as skills, reached
	// by name through the skill tool (OBSERVED live on Copilot CLI 1.0.105,
	// 2026-11-05, ynh #733), the same as under `ynh run`.
	return map[string]string{"skills": "skills", "agents": "agents", "commands": "commands"}
}

func (c *Copilot) SupportsExportDelegates() bool { return true }

// ExportCarriesInstructions: a Copilot plugin cannot carry instructions. A
// plugin's AGENTS.md is installed with it but never reaches the session
// (OBSERVED live on Copilot CLI 1.0.105, 2026-11-05: `copilot -p --plugin-dir`
// on a `ynd export -v copilot` package, whose AGENTS.md held a codeword,
// returned "unknown" while the skill's codeword came back). Under `ynh run`
// ynh projects them into the project's .github/instructions instead.
func (c *Copilot) ExportCarriesInstructions() bool { return false }

// PluginManifestDir is the same as Claude: Copilot reads that manifest schema.
func (c *Copilot) PluginManifestDir() string { return ".claude-plugin" }

// AgentPluginLayout: Copilot loads the format natively and reads its own
// components from com.github.copilot/ (agents/, commands/, rules/,
// hooks/hooks.json), per docs.github.com/en/copilot/concepts/agents/about-plugins.
// What goes there follows ExportArtifactDirs, the same subset the legacy
// export ships.
func (c *Copilot) AgentPluginLayout() AgentPluginLayout {
	return AgentPluginLayout{
		LoadsFormat: true,
		Namespace:   "com.github.copilot",
		ArtifactDir: "com.github.copilot",
		Hooks:       "com.github.copilot/hooks/hooks.json",
	}
}

func (c *Copilot) MarketplaceManifestDir() string { return filepath.Join(".github", "plugin") }

// GenerateMarketplaceIndex: Copilot CLI 1.0.105 installed a marketplace from
// this index live (2026-11-05, `copilot plugin marketplace add <dir>` then
// `copilot plugin install`, with no prompt): docs/vendor-parity.md L28. The
// schema has not been diffed field by field against a published Copilot
// marketplace.json.
func (c *Copilot) GenerateMarketplaceIndex(cfg MarketplaceIndexConfig, plugins []MarketplacePluginInfo) ([]byte, error) {
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

// copilotMCPServer is the GitHub Copilot CLI MCP server schema, confirmed by
// hand-testing (v1.0.75, `copilot mcp add`/`get`/`list`): unlike Claude/Cursor,
// each server requires an explicit "type" field.
type copilotMCPServer struct {
	Type    string            `json:"type"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Tools   []string          `json:"tools"`
}

// GenerateMCPConfig writes the MCP file for the `ynh run` layout, into
// .copilot/ (the --plugin-dir target). buildCopilotArgs re-reads this same
// file and passes it with --additional-mcp-config, because the expanded
// values must not be written into the project. This file is the root .mcp.json
// placement, which --plugin-dir did not read on Copilot CLI v1.0.75 and has not
// been re-run on 1.0.105; the .github/mcp.json placement IS read under
// --plugin-dir on 1.0.105 (docs/vendor-parity.md L37). An export uses
// GeneratePluginMCPConfig.
func (c *Copilot) GenerateMCPConfig(servers map[string]plugin.MCPServer) (map[string][]byte, error) {
	data, err := copilotMCPDocument(servers)
	if err != nil || data == nil {
		return nil, err
	}
	return map[string][]byte{filepath.Join(c.ConfigDir(), ".mcp.json"): data}, nil
}

// GeneratePluginMCPConfig writes the MCP file for an exported plugin, at
// .github/mcp.json in the plugin root: one of the two default MCP paths a
// legacy (.claude-plugin) Copilot plugin reads (docs.github.com, Copilot CLI
// plugin reference). The other, .mcp.json, is where Codex keeps its own
// config, so a merged package carrying both vendors would have one overwrite
// the other. Same document as the run file; only the path differs (#471).
//
// The reference lists the manifest's "mcpServers" field as a third source
// and does not say how the three combine. In a package that also carries
// Claude, the shared manifest names Claude's mcp/claude.json, whose entries
// lack the "type" field Copilot's own schema carries. An installed Copilot
// plugin loads .github/mcp.json and not the manifest-named mcp/claude.json:
// CONFIRMED live on Copilot CLI 1.0.105, 2026-10-30, a merged package
// installed from a local marketplace (#621, fixing #499). A tool call is
// refused one-shot and runs interactively once approved: docs/vendor-parity.md
// L37.
func (c *Copilot) GeneratePluginMCPConfig(servers map[string]plugin.MCPServer) (map[string][]byte, error) {
	data, err := copilotMCPDocument(servers)
	if err != nil || data == nil {
		return nil, err
	}
	return map[string][]byte{filepath.Join(".github", "mcp.json"): data}, nil
}

// copilotMCPDocument renders MCP servers in Copilot's schema, or nil when there
// are none.
func copilotMCPDocument(servers map[string]plugin.MCPServer) ([]byte, error) {
	if len(servers) == 0 {
		return nil, nil
	}

	out := make(map[string]copilotMCPServer, len(servers))
	var names []string
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		s := servers[name]
		cs := copilotMCPServer{
			Command: s.Command,
			Args:    s.Args,
			Env:     s.Env,
			URL:     s.URL,
			Headers: s.Headers,
			Tools:   []string{"*"},
		}
		// Copilot's vocabulary: "local" for stdio, "http" for Streamable
		// HTTP, "sse" for the legacy transport (see the vendor-adapters
		// skill, references/copilot.md).
		switch s.Transport() {
		case plugin.MCPTypeStdio:
			cs.Type = "local"
		case plugin.MCPTypeSSE:
			cs.Type = "sse"
		default:
			cs.Type = "http"
		}
		out[name] = cs
	}

	data, err := json.MarshalIndent(map[string]any{"mcpServers": out}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshalling MCP config: %w", err)
	}
	return append(data, '\n'), nil
}

// copilotInstructionsRelPath is a uniquely-namespaced, fully ynh-owned file —
// safe to overwrite in full on every run, unlike the shared
// .github/copilot-instructions.md, which a user might hand-author themselves.
// applyTo: "**/*" makes it an always-on instructions file (confirmed by
// hand-testing: Copilot's path-scoped instructions files do nothing without
// an applyTo pattern).
const copilotInstructionsRelPath = ".github/instructions/ynh-harness.instructions.md"

// writeCopilotProjectFile fully overwrites a ynh-owned file in the project
// tree, creating parent directories as needed.
func writeCopilotProjectFile(projectDir, relPath string, data []byte) error {
	path := filepath.Join(projectDir, relPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(relPath), err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", relPath, err)
	}
	return nil
}

// projectCopilotInstructions reads the assembled AGENTS.md from the staging
// dir (configPath) and, if present, writes it as an always-on instructions
// file into the real project directory. Copilot does not read plugin-bundled
// instructions via --plugin-dir (confirmed by hand-testing), so this is the
// only mechanism that actually delivers the harness's instructions content.
func projectCopilotInstructions(configPath, projectDir string) error {
	agentsPath := filepath.Join(configPath, "AGENTS.md")
	content, err := os.ReadFile(agentsPath)
	if err != nil || len(content) == 0 {
		return nil
	}

	return writeCopilotProjectFile(projectDir, copilotInstructionsRelPath, copilotInstructions(content))
}

// copilotInstructions renders the harness instructions as an always-on
// Copilot instructions file with exactly one frontmatter block (#532). When
// the instructions open with their own block, its fields are kept and
// applyTo: "**/*" is put in front of them; a source applyTo is dropped,
// because this file must apply everywhere to deliver the harness at all.
func copilotInstructions(content []byte) []byte {
	var b bytes.Buffer
	b.WriteString("---\napplyTo: \"**/*\"\n")
	body := content
	if lines, rest, ok := splitFrontmatter(content); ok {
		for _, e := range frontmatterEntries(lines) {
			if e.key == "applyTo" {
				continue
			}
			for _, l := range e.raw {
				b.WriteString(l)
				b.WriteByte('\n')
			}
		}
		b.WriteString("---\n")
		if body = bytes.TrimLeft(rest, "\r\n"); len(body) > 0 {
			b.WriteByte('\n')
		}
	} else {
		b.WriteString("---\n")
	}
	b.Write(body)
	if len(body) > 0 && body[len(body)-1] != '\n' {
		b.WriteByte('\n')
	}
	return b.Bytes()
}

// copilotMCPArgs returns the launch arguments that hand Copilot the assembled
// MCP config from the run directory (written there by GenerateMCPConfig, at
// 0600), or nil when there is none. Copilot does not read plugin-bundled MCP
// config via --plugin-dir (confirmed by hand-testing), but
// --additional-mcp-config @<file> loads servers for this session only. The
// servers' ${VAR} values are already expanded, so the file must stay in the
// run directory: it is never copied into the project, where it could be
// committed. The path is absolute, and the flag takes a file, never the JSON
// inline, which would put the secrets on the command line.
func copilotMCPArgs(configPath string) []string {
	mcpPath := filepath.Join(configPath, ".copilot", ".mcp.json")
	content, err := os.ReadFile(mcpPath)
	if err != nil || len(content) == 0 {
		return nil
	}
	return []string{"--additional-mcp-config", "@" + mcpPath}
}

// buildCopilotArgs constructs the argument list for the Copilot CLI and
// projects assembled instructions into the real project directory (see
// package doc for why) and passes the MCP config by file, outside the project. initialPrompt, when non-empty, is passed via
// -i/--interactive, which pre-loads it as the first user message of an
// otherwise-interactive session (confirmed via `copilot help`).
//
// --no-auto-update is always passed: Copilot ships weekly and checks for an
// update on every launch. CONFIRMED by hand-testing (v1.0.80 -> v1.0.83): when
// an update is found, Copilot downloads it, swaps the binary, and tears down
// the foreground session mid-launch ("Successfully updated binary" followed
// immediately by "Unregistering foreground session" in ~/.copilot/logs) —
// silently dropping whatever initialPrompt/extraArgs this invocation carried,
// including -i. ynh's launch is a one-shot process replacement
// (syscall.Exec), so there is no opportunity to retry after such a restart;
// the update must simply not happen mid-launch.
func buildCopilotArgs(configPath string, initialPrompt string, extraArgs []string) ([]string, error) {
	args := []string{copilotCLI, "--no-auto-update"}

	if initialPrompt != "" {
		args = append(args, "-i", initialPrompt)
	}

	pluginDir := filepath.Join(configPath, ".copilot")
	args = append(args, "--plugin-dir", pluginDir)
	args = append(args, "--add-dir", configPath)

	if projectDir, err := os.Getwd(); err == nil {
		if err := projectCopilotInstructions(configPath, projectDir); err != nil {
			return nil, err
		}
	}
	args = append(args, copilotMCPArgs(configPath)...)
	if projectDir, err := os.Getwd(); err == nil {
		warnLegacyProjectMCP(os.Stderr, configPath, projectDir)
	}

	args = append(args, extraArgs...)
	return args, nil
}

// warnLegacyProjectMCP tells the user when the project's .github/mcp.json looks
// like one an older ynh wrote, which held the servers' expanded credentials. It
// never deletes or alters the file. The file carries no marker (JSON has no
// comments), so "looks like" is judged by shape: the only top-level key is
// mcpServers, every server has the "tools" and "type" fields ynh's document
// carries, and the server names are exactly those of this run's document. A
// hand-written file almost never matches all three, but a user's own file could,
// so the message says "looks like" and leaves the decision to the user.
func warnLegacyProjectMCP(w io.Writer, configPath, projectDir string) {
	run, err := os.ReadFile(filepath.Join(configPath, ".copilot", ".mcp.json"))
	if err != nil {
		return
	}
	runNames, ok := copilotMCPServerNames(run)
	if !ok {
		return
	}
	path := filepath.Join(projectDir, ".github", "mcp.json")
	proj, err := os.ReadFile(path)
	if err != nil {
		return
	}
	projNames, ok := copilotMCPServerNames(proj)
	if !ok || strings.Join(projNames, "\x00") != strings.Join(runNames, "\x00") {
		return
	}
	if !holdsLiteralSecret(proj) {
		return
	}
	_, _ = fmt.Fprintf(w, "warning: %s looks like one an older ynh wrote, and may hold your MCP credentials in plain text. ynh no longer writes it and has not touched it. If you did not write it yourself, delete it and rotate those credentials.\n", path)
}

// holdsLiteralSecret reports whether any env or headers value in a Copilot MCP
// document is a literal rather than a ${VAR} reference. The document an older
// ynh wrote into the project held the expanded values; the one ynd export
// writes keeps the references, so it holds nothing to rotate and must not
// draw the warning. A value is a reference when it contains "${". The values
// themselves are never reported.
func holdsLiteralSecret(data []byte) bool {
	var doc struct {
		MCPServers map[string]struct {
			Env     map[string]string `json:"env"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		// Unexpected value types: err on the side of warning.
		return true
	}
	for _, srv := range doc.MCPServers {
		for _, m := range []map[string]string{srv.Env, srv.Headers} {
			for _, v := range m {
				if !strings.Contains(v, "${") {
					return true
				}
			}
		}
	}
	return false
}

// copilotMCPServerNames returns the sorted server names of a Copilot MCP
// document, and false when it is not shaped like one ynh renders (see
// copilotMCPDocument): a sole top-level mcpServers key whose servers each carry
// "type" and "tools".
func copilotMCPServerNames(data []byte) ([]string, bool) {
	var doc map[string]map[string]map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil || len(doc) != 1 {
		return nil, false
	}
	servers, ok := doc["mcpServers"]
	if !ok || len(servers) == 0 {
		return nil, false
	}
	names := make([]string, 0, len(servers))
	for name, fields := range servers {
		if _, ok := fields["type"]; !ok {
			return nil, false
		}
		if _, ok := fields["tools"]; !ok {
			return nil, false
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, true
}

func launchCopilot(configPath string, initialPrompt string, extraArgs []string) error {
	copilotBin, err := exec.LookPath(copilotCLI)
	if err != nil {
		return err
	}

	args, err := buildCopilotArgs(configPath, initialPrompt, extraArgs)
	if err != nil {
		return err
	}
	return syscall.Exec(copilotBin, args, os.Environ())
}

// Workspace folders: Copilot CLI's /add-dir command is documented as "Allow
// file access to a directory and load its .github/skills and .github/agents as
// trusted configurations" (docs.github.com/en/copilot/reference/cli-command-reference).
// The launch flag --add-dir is the one this adapter already passes for the
// staging directory. A granted folder's agent configuration is therefore
// loaded as trusted, which is a wider grant than file access alone.
var copilotWorkspaceFlag = "--add-dir"
var copilotWorkspaceUnsupported = ""
