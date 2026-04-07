package vendor

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/eyelock/ynh/internal/plugin"
)

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

func (c *Codex) GenerateHookConfig(hooks map[string][]plugin.HookEntry) map[string][]byte {
	if len(hooks) == 0 {
		return nil
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
		return nil
	}

	config := map[string]any{
		"hooks": allEvents,
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil
	}
	data = append(data, '\n')

	return map[string][]byte{
		filepath.Join(".codex", "hooks.json"): data,
	}
}

func (c *Codex) GenerateMCPConfig(servers map[string]plugin.MCPServer) map[string][]byte {
	if len(servers) == 0 {
		return nil
	}

	toml := renderMCPTOML(servers)
	return map[string][]byte{
		filepath.Join(".codex", "config.toml"): []byte(toml),
	}
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
