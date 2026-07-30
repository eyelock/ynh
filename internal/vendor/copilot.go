package vendor

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"syscall"

	"github.com/eyelock/ynh/internal/plugin"
)

// copilotPluginJSON is the GitHub Copilot CLI plugin.json schema — identity
// fields only. Confirmed by hand-testing (v1.0.75): Copilot's manifest search
// order includes .claude-plugin/plugin.json (a documented compatibility
// path), and a bundled skills/agents dir alongside it loads and activates
// correctly via --plugin-dir. See .claude/skills/vendor-adapters/SKILL.md
// § "Copilot CLI" for the full research trail.
type copilotPluginJSON struct {
	Name        string             `json:"name"`
	Version     string             `json:"version"`
	Description string             `json:"description,omitempty"`
	Author      *plugin.AuthorInfo `json:"author,omitempty"`
	Keywords    []string           `json:"keywords,omitempty"`
}

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

// copilotRunDirLayout reports whether outputDir looks like a `ynh run`
// staging directory (skills/agents nested under .copilot/, matching what
// buildCopilotArgs passes to --plugin-dir) rather than an `ynd export`
// output directory (which flattens skills/agents to its own root — see
// exporter.exportForVendor/exportMerged). The manifest must sit alongside
// wherever skills/agents actually landed: confirmed by hand-testing that
// Copilot silently fails to load ANY plugin content via --plugin-dir when
// .claude-plugin/plugin.json isn't present at that exact directory's root.
func copilotRunDirLayout(outputDir string) bool {
	return dirHasContent(filepath.Join(outputDir, ".copilot"))
}

func (c *Copilot) GeneratePluginManifest(hj *plugin.HarnessJSON, outputDir string) (map[string][]byte, error) {
	pj := &copilotPluginJSON{
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

	relPath := filepath.Join(".claude-plugin", "plugin.json")
	if copilotRunDirLayout(outputDir) {
		relPath = filepath.Join(".copilot", ".claude-plugin", "plugin.json")
	}
	return map[string][]byte{relPath: data}, nil
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

func (c *Copilot) GenerateMCPConfig(servers map[string]plugin.MCPServer) (map[string][]byte, error) {
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

	config := map[string]any{"mcpServers": out}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshalling MCP config: %w", err)
	}
	data = append(data, '\n')

	// Written into .copilot/ (the --plugin-dir target) for ynh-run
	// consistency — buildCopilotArgs re-reads this same file and projects it
	// into the project's own .github/mcp.json, which is the path confirmed
	// to actually work (see package doc). Unlike GeneratePluginManifest, this
	// method has no outputDir parameter to detect the `ynd export` flattened
	// layout, so exported plugins get this nested under a .copilot/ that
	// doesn't otherwise exist there — orphaned but harmless, since
	// plugin-bundled MCP config isn't read by --plugin-dir loading either way
	// (confirmed by hand-testing); untested whether a real `copilot plugin
	// install` reads root-level bundled MCP config at all.
	return map[string][]byte{
		filepath.Join(".copilot", ".mcp.json"): data,
	}, nil
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
func buildCopilotArgs(configPath string, initialPrompt string, extraArgs []string) ([]string, error) {
	args := []string{"copilot"}

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
