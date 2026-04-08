package vendor

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/eyelock/ynh/internal/plugin"
)

// codexPluginJSON is the Codex plugin.json schema — identity fields plus path pointers.
// Codex manifests require path pointers (skills, mcpServers) so the plugin system
// knows where to find components within the plugin directory.
type codexPluginJSON struct {
	Name        string             `json:"name"`
	Version     string             `json:"version"`
	Description string             `json:"description,omitempty"`
	Author      *plugin.AuthorInfo `json:"author,omitempty"`
	Keywords    []string           `json:"keywords,omitempty"`
	Skills      string             `json:"skills,omitempty"`
	MCPServers  string             `json:"mcpServers,omitempty"`
}

func init() {
	Register(&Codex{})
}

// Codex implements the Adapter interface for OpenAI Codex CLI.
type Codex struct{}

func (c *Codex) Name() string    { return "codex" }
func (c *Codex) CLIName() string { return "codex" }

func (c *Codex) ConfigDir() string {
	return ".codex"
}

func (c *Codex) InstructionsFile() string { return "codex.md" }

func (c *Codex) ArtifactDirs() map[string]string { return DefaultArtifactDirs() }

func (c *Codex) GenerateSystemPrompt(content []byte) map[string][]byte {
	// AGENTS.md: Codex natively reads this
	return map[string][]byte{
		"AGENTS.md": content,
	}
}

func (c *Codex) NeedsSymlinks() bool { return true }

func (c *Codex) Install(stagingDir string, projectDir string) ([]SymlinkEntry, error) {
	return installSymlinks(stagingDir, projectDir, c.ConfigDir(), c.ArtifactDirs())
}

func (c *Codex) Clean(entries []SymlinkEntry) error {
	return cleanSymlinks(entries)
}

func (c *Codex) LaunchInteractive(configPath string, extraArgs []string) error {
	return launchCodex(configPath, extraArgs)
}

func (c *Codex) LaunchNonInteractive(configPath string, prompt string, extraArgs []string) error {
	args := append([]string{"exec"}, extraArgs...)
	args = append(args, prompt)
	return launchCodex(configPath, args)
}

// codexHookEventMap maps canonical event names to Codex hook events.
var codexHookEventMap = map[string]string{
	"before_tool":   "PreToolUse",
	"after_tool":    "PostToolUse",
	"before_prompt": "UserPromptSubmit",
	"on_stop":       "Stop",
}

func (c *Codex) GenerateHookConfig(hooks map[string][]plugin.HookEntry) (map[string][]byte, error) {
	if len(hooks) == 0 {
		return nil, nil
	}

	// Codex three-level format (same structure as Claude Code):
	// { "hooks": { "PreToolUse": [ { "matcher": "Bash", "hooks": [ { "type": "command", "command": "..." } ] } ] } }

	type codexInnerHook struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	}
	type codexHookGroup struct {
		Matcher string           `json:"matcher,omitempty"`
		Hooks   []codexInnerHook `json:"hooks"`
	}

	allEvents := make(map[string][]codexHookGroup)

	var events []string
	for event := range hooks {
		events = append(events, event)
	}
	sort.Strings(events)

	for _, event := range events {
		entries := hooks[event]
		codexEvent, ok := codexHookEventMap[event]
		if !ok {
			continue
		}

		// Group entries by matcher (same logic as Claude adapter)
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

		var hookGroups []codexHookGroup
		for _, g := range groups {
			var inner []codexInnerHook
			for _, cmd := range g.cmds {
				inner = append(inner, codexInnerHook{Type: "command", Command: cmd})
			}
			hookGroups = append(hookGroups, codexHookGroup{
				Matcher: g.matcher,
				Hooks:   inner,
			})
		}

		allEvents[codexEvent] = hookGroups
	}

	if len(allEvents) == 0 {
		return nil, nil
	}

	config := map[string]any{
		"hooks": allEvents,
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshalling hook config: %w", err)
	}
	data = append(data, '\n')

	return map[string][]byte{
		filepath.Join(".codex", "hooks.json"): data,
	}, nil
}

func (c *Codex) GeneratePluginManifest(hj *plugin.HarnessJSON, outputDir string) (map[string][]byte, error) {
	cpj := &codexPluginJSON{
		Name:        hj.Name,
		Version:     hj.Version,
		Description: hj.Description,
		Author:      hj.Author,
		Keywords:    hj.Keywords,
	}

	// Add path pointers for components that exist in the output directory
	if dirHasContent(filepath.Join(outputDir, "skills")) {
		cpj.Skills = "./skills/"
	}
	if fileExists(filepath.Join(outputDir, ".mcp.json")) {
		cpj.MCPServers = "./.mcp.json"
	}

	data, err := json.MarshalIndent(cpj, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshalling plugin.json: %w", err)
	}
	data = append(data, '\n')
	return map[string][]byte{
		filepath.Join(".codex-plugin", "plugin.json"): data,
	}, nil
}

// dirHasContent returns true if dir exists and contains at least one entry.
func dirHasContent(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}

// fileExists returns true if path exists and is a regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func (c *Codex) ExportArtifactDirs() map[string]string {
	// Codex only supports skills in its plugin format
	return map[string]string{"skills": "skills"}
}

func (c *Codex) SupportsExportDelegates() bool { return false }

func (c *Codex) MarketplaceManifestDir() string { return filepath.Join(".agents", "plugins") }

func (c *Codex) GenerateMarketplaceIndex(cfg MarketplaceIndexConfig, plugins []MarketplacePluginInfo) ([]byte, error) {
	type source struct {
		Source string `json:"source"`
		Path   string `json:"path"`
	}
	type policy struct {
		Installation   string `json:"installation,omitempty"`
		Authentication string `json:"authentication,omitempty"`
	}
	type indexPlugin struct {
		Name     string `json:"name"`
		Source   source `json:"source"`
		Policy   policy `json:"policy,omitempty"`
		Category string `json:"category,omitempty"`
	}
	type iface struct {
		DisplayName string `json:"displayName"`
	}
	type indexJSON struct {
		Name      string        `json:"name"`
		Interface iface         `json:"interface"`
		Plugins   []indexPlugin `json:"plugins"`
	}

	idx := indexJSON{
		Name:      cfg.Name,
		Interface: iface{DisplayName: cfg.Name},
	}
	for _, p := range plugins {
		idx.Plugins = append(idx.Plugins, indexPlugin{
			Name: p.Name,
			Source: source{
				Source: "local",
				Path:   "./plugins/" + p.Name,
			},
			Policy: policy{
				Installation: "AVAILABLE",
			},
		})
	}

	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
	return data, nil
}

func (c *Codex) GenerateMCPConfig(servers map[string]plugin.MCPServer) (map[string][]byte, error) {
	if len(servers) == 0 {
		return nil, nil
	}

	// Codex plugin format uses .mcp.json at plugin root (JSON, same as Claude).
	// See https://developers.openai.com/codex/plugins/build
	config := map[string]any{
		"mcpServers": servers,
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshalling MCP config: %w", err)
	}
	data = append(data, '\n')

	return map[string][]byte{
		".mcp.json": data,
	}, nil
}

func launchCodex(configPath string, extraArgs []string) error {
	codexBin, err := exec.LookPath("codex")
	if err != nil {
		return err
	}

	// Launch as child process so ynh stays alive for signal handling.
	// Use cmd.Dir instead of --cd for CWD control.
	cmd := exec.Command(codexBin, extraArgs...)
	cmd.Dir = configPath
	return runChildProcess(cmd)
}
