package vendor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/eyelock/ynh/internal/plugin"
)

// ClaudeSessionEndEnvVar is the variable Claude Code reads for the SessionEnd
// budget, in milliseconds. It sets the shared budget and the timeout of each
// SessionEnd hook that has none of its own (since v2.1.268). It is the only way
// to raise the budget for a plugin's hooks: code.claude.com/docs/en/hooks,
// SessionEnd, says "Timeouts set on plugin-provided hooks don't raise the
// budget." CONFIRMED live (Claude Code, 2026-10-30): a plugin hook with
// "timeout": 10 and a 3 s body was still "Hook cancelled" at about 1.5 s.
const ClaudeSessionEndEnvVar = "CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS"

// claudeSessionEndMaxSeconds is the documented ceiling of the SessionEnd budget.
const claudeSessionEndMaxSeconds = 60

// claudeDefaultSessionEndSeconds is the default shared budget, 1.5 s, rounded
// up: a whole-second timeout above 1 exceeds it.
const claudeDefaultSessionEndSeconds = 1

// ClaudeSessionEndEnv returns the environment a Claude Code process needs so
// the assembled plugin's SessionEnd hooks get the time they ask for: the
// variable set to the largest SessionEnd hook timeout in the assembled hooks
// file under configPath, in milliseconds, capped at 60000. lookup reads the
// environment the process will otherwise get. A value the user already set is
// never overridden; one lower than the hooks ask for produces a warning. Nothing
// is returned when no SessionEnd hook has a timeout.
func ClaudeSessionEndEnv(configPath string, lookup func(string) (string, bool)) (env []string, warnings []string) {
	data, err := os.ReadFile(filepath.Join(configPath, (&Claude{}).ConfigDir(), "hooks", "hooks.json"))
	if err != nil {
		return nil, nil
	}
	var doc struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Timeout int `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return nil, nil
	}
	longest := 0
	for _, g := range doc.Hooks["SessionEnd"] {
		for _, h := range g.Hooks {
			longest = max(longest, h.Timeout)
		}
	}
	if longest == 0 {
		return nil, nil
	}
	want := min(longest, claudeSessionEndMaxSeconds) * 1000

	if v, set := lookup(ClaudeSessionEndEnvVar); set {
		if have, err := strconv.Atoi(v); err != nil || have < want {
			warnings = append(warnings, fmt.Sprintf("%s=%s is lower than the %d ms the on_session_end hooks ask for, so Claude Code cancels them early; not overriding your value", ClaudeSessionEndEnvVar, v, want))
		}
		return nil, warnings
	}
	return []string{fmt.Sprintf("%s=%d", ClaudeSessionEndEnvVar, want)}, nil
}

// ExportHookWarnings is HookWarnings plus what only an export needs said. For
// Claude Code, a plugin's SessionEnd hook timeout does not raise the budget, and
// ynh sets the environment variable only for sessions it launches, so a
// packaged hook with a timeout above the 1.5 s default needs the user to set it.
func ExportHookWarnings(vendorName string, hooks map[string][]plugin.HookEntry) []string {
	warnings := HookWarnings(vendorName, hooks)
	if vendorName != "claude" {
		return warnings
	}
	longest := 0
	for _, e := range hooks["on_session_end"] {
		longest = max(longest, e.Timeout)
	}
	if longest > claudeDefaultSessionEndSeconds {
		warnings = append(warnings, fmt.Sprintf("claude ignores a plugin hook's timeout for SessionEnd, so on_session_end will be cancelled after about 1.5 s; set %s=%d in the environment that launches Claude Code",
			ClaudeSessionEndEnvVar, min(longest, claudeSessionEndMaxSeconds)*1000))
	}
	return warnings
}
