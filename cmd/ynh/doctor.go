package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eyelock/ynh/internal/vendor"
)

// claudeSettingsFiles are the project files Claude Code auto-loads in a plain
// session. ynh doctor inspects them for hook-wiring mistakes that fail silently.
var claudeSettingsFiles = []string{
	filepath.Join(".claude", "settings.json"),
	filepath.Join(".claude", "settings.local.json"),
}

// doctorFinding is one advisory result from a doctor check.
type doctorFinding struct {
	file    string
	message string
}

func cmdDoctor(args []string) error {
	return cmdDoctorTo(args, os.Stdout)
}

func cmdDoctorTo(args []string, stdout io.Writer) error {
	if len(args) > 0 {
		return fmt.Errorf("ynh doctor takes no arguments")
	}

	var findings []doctorFinding
	anyFile := false
	for _, rel := range claudeSettingsFiles {
		present, fs := checkClaudeSettings(rel)
		if present {
			anyFile = true
		}
		findings = append(findings, fs...)
	}

	_, _ = fmt.Fprintln(stdout, "ynh doctor — hook wiring")

	if !anyFile {
		_, _ = fmt.Fprintln(stdout, "  info  no .claude/settings.json found — hooks declared in .ynh-plugin/plugin.json")
		_, _ = fmt.Fprintln(stdout, "        do not auto-activate in a plain Claude session. To wire them in, run:")
		_, _ = fmt.Fprintln(stdout, "        ynh hook export <harness> --target settings")
		return nil
	}

	if len(findings) == 0 {
		_, _ = fmt.Fprintln(stdout, "  ok    Claude hook settings look correctly wired.")
		return nil
	}

	for _, f := range findings {
		_, _ = fmt.Fprintf(stdout, "  warn  %s: %s\n", f.file, f.message)
	}
	_, _ = fmt.Fprintf(stdout, "\n%d warning(s) found.\n", len(findings))
	return nil
}

// checkClaudeSettings inspects one settings file for hook-wiring mistakes.
// It reports whether the file exists, and any findings.
func checkClaudeSettings(path string) (present bool, findings []doctorFinding) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, nil
	}

	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return true, []doctorFinding{{file: path, message: fmt.Sprintf("not valid JSON: %v", err)}}
	}

	hooks, ok := doc["hooks"].(map[string]any)
	if !ok || len(hooks) == 0 {
		return true, nil
	}

	events := make([]string, 0, len(hooks))
	for e := range hooks {
		events = append(events, e)
	}
	sort.Strings(events)

	for _, event := range events {
		// Canonical name leaked into a vendor-native settings file: Claude
		// silently ignores it. This is the most common silent failure.
		if native, isCanonical := vendor.ClaudeHookEvent(event); isCanonical {
			findings = append(findings, doctorFinding{
				file:    path,
				message: fmt.Sprintf("event %q is an ynh canonical name — Claude only recognises %q here; canonical names belong in .ynh-plugin/plugin.json. Run: ynh hook export <harness> --target settings", event, native),
			})
		}
		// cwd-relative commands break after the agent changes directory, and a
		// blocking guard hook then silently fails open.
		for _, cmd := range extractHookCommands(hooks[event]) {
			if strings.HasPrefix(cmd, "./") || strings.HasPrefix(cmd, "../") {
				findings = append(findings, doctorFinding{
					file:    path,
					message: fmt.Sprintf("hook command %q is cwd-relative and breaks after the agent changes directory; anchor it to $CLAUDE_PROJECT_DIR", cmd),
				})
			}
		}
	}
	return true, findings
}

// extractHookCommands pulls every command string out of one event's value,
// tolerating both Claude's nested shape ([{hooks:[{command}]}]) and the flat
// shape a confused author might write ([{command}]).
func extractHookCommands(eventVal any) []string {
	groups, ok := eventVal.([]any)
	if !ok {
		return nil
	}
	var cmds []string
	for _, g := range groups {
		group, ok := g.(map[string]any)
		if !ok {
			continue
		}
		if cmd, ok := group["command"].(string); ok && cmd != "" {
			cmds = append(cmds, cmd)
		}
		inner, ok := group["hooks"].([]any)
		if !ok {
			continue
		}
		for _, h := range inner {
			hm, ok := h.(map[string]any)
			if !ok {
				continue
			}
			if cmd, ok := hm["command"].(string); ok && cmd != "" {
				cmds = append(cmds, cmd)
			}
		}
	}
	return cmds
}
