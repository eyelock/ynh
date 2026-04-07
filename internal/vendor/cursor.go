package vendor

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/eyelock/ynh/internal/plugin"
)

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

func (c *Cursor) GenerateHookConfig(hooks map[string][]plugin.HookEntry) map[string][]byte {
	if len(hooks) == 0 {
		return nil
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
		return nil
	}

	config := map[string]any{
		"version": 1,
		"hooks":   allEvents,
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil
	}
	data = append(data, '\n')

	return map[string][]byte{
		filepath.Join(".cursor", "hooks.json"): data,
	}
}

func (c *Cursor) GenerateMCPConfig(servers map[string]plugin.MCPServer) map[string][]byte {
	if len(servers) == 0 {
		return nil
	}

	// Cursor uses .cursor/mcp.json with "mcpServers" key — same structure as Claude
	config := map[string]any{
		"mcpServers": servers,
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil
	}
	data = append(data, '\n')

	return map[string][]byte{
		filepath.Join(".cursor", "mcp.json"): data,
	}
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
