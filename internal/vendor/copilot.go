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

func init() {
	Register(&Copilot{})
}

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
func (c *Copilot) CLIName() string     { return "copilot" }

func (c *Copilot) ConfigDir() string {
	return ".copilot"
}

func (c *Copilot) InstructionsFile() string { return "AGENTS.md" }

func (c *Copilot) ArtifactDirs() map[string]string { return DefaultArtifactDirs() }

func (c *Copilot) GenerateSystemPrompt(content []byte) map[string][]byte {
	// AGENTS.md: Copilot natively reads this (confirmed: --no-custom-instructions
	// flag exists specifically to disable AGENTS.md loading, proving it's read
	// by default). Whether a properly-installed plugin's bundled AGENTS.md is
	// read is untested — --plugin-dir loading confirmed it is NOT (see package
	// doc). Kept for export-path consistency with the other three adapters.
	return map[string][]byte{
		"AGENTS.md": content,
	}
}

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
func (c *Copilot) LaunchResume(configPath, sessionID string, extraArgs []string) error {
	var resumeArgs []string
	if sessionID != "" {
		resumeArgs = []string{"--resume=" + sessionID}
	} else {
		resumeArgs = []string{"--continue"}
	}
	return launchCopilot(configPath, "", append(resumeArgs, extraArgs...))
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

// GenerateHookConfig always returns nil: hooks are confirmed to silently
// no-op when the run directory is not a Copilot "trusted folder" (hand-tested
// against v1.0.75 — a correctly-configured, confirmed-real preToolUse hook
// never fired in an untrusted scratch repo, with no error surfaced). ynh's
// staging dir is never pre-trusted and no CLI flag grants trust per-invocation
// (--add-dir and --allow-all-paths were both tested and do not). Emitting a
// hook config that silently never fires is worse than the honest gap this
// documents. See .claude/skills/vendor-adapters/SKILL.md § "Copilot CLI Hook
// Events" for the full finding and remediation options.
func (c *Copilot) GenerateHookConfig(hooks map[string][]plugin.HookEntry) (map[string][]byte, error) {
	return nil, nil
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
// Copilot wrote last (#469, #481). Copilot itself emits no hooks (see
// GenerateHookConfig), and its MCP file, .github/mcp.json, is one of its
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
	// Skills and agents are confirmed supported in Copilot plugins. Rules have
	// no reliable "always-on" equivalent (Copilot's applyTo-scoped instructions
	// files aren't on by default) and commands aren't supported at all
	// (confirmed gap, open feature requests upstream) — excluded, matching the
	// precedent set by Codex's ExportArtifactDirs for its own unsupported types.
	return map[string]string{"skills": "skills", "agents": "agents"}
}

func (c *Copilot) SupportsExportDelegates() bool { return true }

// PluginManifestDir is the same as Claude: Copilot reads that manifest schema.
func (c *Copilot) PluginManifestDir() string { return ".claude-plugin" }

func (c *Copilot) MarketplaceManifestDir() string { return filepath.Join(".github", "plugin") }

// GenerateMarketplaceIndex is best-effort: Copilot's marketplace.json schema
// was researched but not hand-verified field-by-field the way the plugin
// manifest, MCP config, and hook events were. Revisit once a real
// copilot-plugins or awesome-copilot marketplace.json has been diffed against
// this output.
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
// file and projects it into the project's own .github/mcp.json, which is the
// path confirmed to actually work (see package doc): a plugin loaded via
// --plugin-dir does not get its bundled MCP config read. An export uses
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
// lack the "type" field Copilot's own schema carries; which file an installed
// Copilot plugin then loads has not been hand-tested (#499).
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
		if s.Command != "" {
			cs.Type = "local"
		} else {
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

// copilotProjectMCPRelPath is ynh's dedicated MCP config file in the real
// project tree. Copilot loads .github/mcp.json additively alongside any
// root .mcp.json the user maintains themselves, so this file is fully
// ynh-owned without risk of clobbering user-managed MCP servers.
const copilotProjectMCPRelPath = ".github/mcp.json"

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

	var body []byte
	body = append(body, []byte("---\napplyTo: \"**/*\"\n---\n")...)
	body = append(body, content...)
	if body[len(body)-1] != '\n' {
		body = append(body, '\n')
	}

	return writeCopilotProjectFile(projectDir, copilotInstructionsRelPath, body)
}

// projectCopilotMCPConfig reads the assembled MCP config from the staging
// dir (written there by GenerateMCPConfig) and re-projects it into the real
// project directory. Copilot does not read plugin-bundled MCP config via
// --plugin-dir (confirmed by hand-testing), so this is the only mechanism
// that actually delivers MCP servers.
func projectCopilotMCPConfig(configPath, projectDir string) error {
	mcpPath := filepath.Join(configPath, ".copilot", ".mcp.json")
	content, err := os.ReadFile(mcpPath)
	if err != nil || len(content) == 0 {
		return nil
	}
	return writeCopilotProjectFile(projectDir, copilotProjectMCPRelPath, content)
}

// buildCopilotArgs constructs the argument list for the Copilot CLI and
// projects assembled instructions/MCP config into the real project directory
// (see package doc for why). initialPrompt, when non-empty, is passed via
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
	args := []string{"copilot", "--no-auto-update"}

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
		if err := projectCopilotMCPConfig(configPath, projectDir); err != nil {
			return nil, err
		}
	}

	args = append(args, extraArgs...)
	return args, nil
}

func launchCopilot(configPath string, initialPrompt string, extraArgs []string) error {
	copilotBin, err := exec.LookPath("copilot")
	if err != nil {
		return err
	}

	args, err := buildCopilotArgs(configPath, initialPrompt, extraArgs)
	if err != nil {
		return err
	}
	return syscall.Exec(copilotBin, args, os.Environ())
}
