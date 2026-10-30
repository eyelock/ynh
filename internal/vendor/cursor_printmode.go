package vendor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// CursorPrintModeWarnings returns a warning naming each assembled Cursor hook
// event that the Cursor CLI does not run in print mode (-p / --print), the mode
// ynh uses for a non-interactive launch and for the agent loop's Cursor worker.
// CONFIRMED live (cursor-agent 2026.10.01-e373342, 2026-10-30): with all six
// events in .cursor/hooks.json, sessionStart, sessionEnd, preToolUse and
// postToolUse fired and beforeSubmitPrompt and stop did not. Cursor's docs do
// not say which hooks the CLI runs; interactive CLI and IDE behaviour is
// UNVERIFIED. configPath is the assembled directory holding .cursor/hooks.json.
func CursorPrintModeWarnings(configPath string) []string {
	data, err := os.ReadFile(filepath.Join(configPath, ".cursor", "hooks.json"))
	if err != nil {
		return nil
	}
	var doc struct {
		Hooks map[string]json.RawMessage `json:"hooks"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return nil
	}
	var warnings []string
	for _, ev := range []struct{ native, canonical string }{
		{cursorHookEventMap["before_prompt"], "before_prompt"},
		{cursorHookEventMap["on_stop"], "on_stop"},
	} {
		if _, ok := doc.Hooks[ev.native]; ok {
			warnings = append(warnings, fmt.Sprintf("hook event %s: Cursor's CLI does not run %s hooks in print mode (confirmed live), so those hooks will not fire this session", ev.canonical, ev.native))
		}
	}
	slices.Sort(warnings)
	return warnings
}
