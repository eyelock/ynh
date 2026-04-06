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

	// Codex two-level format: { "hooks": { "PreToolUse": [ { "matcher": "Bash", "command": "..." } ] } }
	type codexHookEntry struct {
		Matcher string `json:"matcher,omitempty"`
		Command string `json:"command"`
	}

	allEvents := make(map[string][]codexHookEntry)

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

		var hookEntries []codexHookEntry
		for _, entry := range entries {
			hookEntries = append(hookEntries, codexHookEntry{
				Matcher: entry.Matcher,
				Command: entry.Command,
			})
		}

		allEvents[codexEvent] = hookEntries
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
