package vendor

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"syscall"

	"github.com/eyelock/ynh/internal/plugin"
)

func init() {
	Register(&Claude{})
}

// Claude implements the Adapter interface for Claude Code CLI.
type Claude struct{}

func (c *Claude) Name() string    { return "claude" }
func (c *Claude) CLIName() string { return "claude" }

func (c *Claude) ConfigDir() string {
	return ".claude"
}

func (c *Claude) InstructionsFile() string { return "CLAUDE.md" }

func (c *Claude) ArtifactDirs() map[string]string { return DefaultArtifactDirs() }

func (c *Claude) NeedsSymlinks() bool { return false }

func (c *Claude) Install(stagingDir string, projectDir string) ([]SymlinkEntry, error) {
	return nil, nil
}

func (c *Claude) Clean(entries []SymlinkEntry) error {
	return nil
}

func (c *Claude) LaunchInteractive(configPath string, extraArgs []string) error {
	return launchClaude(configPath, extraArgs)
}

func (c *Claude) LaunchNonInteractive(configPath string, prompt string, extraArgs []string) error {
	args := append([]string{"-p", prompt}, extraArgs...)
	return launchClaude(configPath, args)
}

// buildClaudeArgs constructs the argument list for the Claude Code CLI.
// It adds --plugin-dir for artifact loading and --append-system-prompt
// for harness instructions, then appends any vendor pass-through args.
func buildClaudeArgs(configPath string, extraArgs []string) []string {
	args := []string{"claude"}

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
	"before_tool":   "PreToolUse",
	"after_tool":    "PostToolUse",
	"before_prompt": "UserPromptSubmit",
	"on_stop":       "Stop",
}

func (c *Claude) GenerateHookConfig(hooks map[string][]plugin.HookEntry) map[string][]byte {
	if len(hooks) == 0 {
		return nil
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
				inner = append(inner, claudeInnerHook{Type: "command", Command: cmd})
			}
			hookGroups = append(hookGroups, claudeHookGroup{
				Matcher: g.matcher,
				Hooks:   inner,
			})
		}

		allEvents[claudeEvent] = hookGroups
	}

	if len(allEvents) == 0 {
		return nil
	}

	settings := map[string]any{
		"hooks": allEvents,
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return nil
	}
	data = append(data, '\n')

	return map[string][]byte{
		filepath.Join(".claude", "settings.json"): data,
	}
}

func (c *Claude) GenerateMCPConfig(servers map[string]plugin.MCPServer) map[string][]byte {
	if len(servers) == 0 {
		return nil
	}

	// Claude uses .mcp.json with "mcpServers" key — direct passthrough
	config := map[string]any{
		"mcpServers": servers,
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil
	}
	data = append(data, '\n')

	return map[string][]byte{
		".mcp.json": data,
	}
}

func launchClaude(configPath string, extraArgs []string) error {
	claudeBin, err := exec.LookPath("claude")
	if err != nil {
		return err
	}

	args := buildClaudeArgs(configPath, extraArgs)
	return syscall.Exec(claudeBin, args, os.Environ())
}
