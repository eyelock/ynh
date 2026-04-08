package vendor

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"

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

func (c *Cursor) Name() string    { return "cursor" }
func (c *Cursor) CLIName() string { return "agent" }

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

// cursorHookEventMap maps canonical event names to Cursor hook events.
// Cursor supports: beforeSubmitPrompt, beforeShellExecution, beforeMCPExecution,
// beforeReadFile, afterFileEdit, stop. There is no afterShellExecution event.
var cursorHookEventMap = map[string]string{
	"before_tool":   "beforeShellExecution",
	"after_tool":    "afterFileEdit",
	"before_prompt": "beforeSubmitPrompt",
	"on_stop":       "stop",
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

	return map[string][]byte{
		filepath.Join(".cursor", "hooks.json"): data,
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

	// Cursor uses .cursor/mcp.json with "mcpServers" key — same structure as Claude
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
	}, nil
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
