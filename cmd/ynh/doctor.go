// `ynh doctor`: read-only diagnosis of an ynh setup.
//
// It reports; it never repairs. A diagnostic that changes things is one people
// hesitate to run, and hesitating to run it is the failure this command exists
// to prevent. Every finding carries the command that fixes it instead.
//
// It used to inspect Claude hook wiring and nothing else, so a user with a
// missing vendor CLI, a dangling harness pointer or a bin directory off PATH
// was told everything was fine (#279). Hook wiring is now one check of six.

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/vendor"
)

// claudeSettingsFiles are the project files Claude Code auto-loads in a plain
// session. ynh doctor inspects them for hook-wiring mistakes that fail silently.
var claudeSettingsFiles = []string{
	filepath.Join(".claude", "settings.json"),
	filepath.Join(".claude", "settings.local.json"),
}

// doctorReport is the `--format json` payload.
type doctorReport struct {
	Capabilities string        `json:"capabilities"`
	YnhVersion   string        `json:"ynh_version"`
	Summary      doctorSummary `json:"summary"`
	Checks       []doctorCheck `json:"checks"`
}

type doctorSummary struct {
	Checks   int `json:"checks"`
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
}

func cmdDoctor(args []string) error {
	return cmdDoctorTo(args, os.Stdout, os.Stderr)
}

func cmdDoctorTo(args []string, stdout, stderr io.Writer) error {
	structured := detectJSONFormat(args)

	format := "text"
	i := 0
	for i < len(args) {
		switch args[i] {
		case "--format":
			if i+1 >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--format requires a value")
			}
			i++
			format = args[i]
		default:
			if strings.HasPrefix(args[i], "-") {
				return cliError(stderr, structured, errCodeInvalidInput,
					fmt.Sprintf("unknown flag: %s", args[i]))
			}
			return cliError(stderr, structured, errCodeInvalidInput,
				fmt.Sprintf("unexpected argument: %s", args[i]))
		}
		i++
	}

	report := runDoctor()

	switch format {
	case "text":
		return printDoctorText(stdout, report)
	case "json":
		enc := json.NewEncoder(stdout)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	default:
		return cliError(stderr, structured, errCodeInvalidInput,
			fmt.Sprintf("invalid --format value %q (want text or json)", format))
	}
}

// runDoctor executes every check. Order is by how often each bites, so the
// first thing printed is the most likely cause of whatever sent the user here.
func runDoctor() doctorReport {
	checks := []doctorCheck{
		checkVendors(),
		checkHarnesses(),
		checkSymlinks(),
		checkLauncher(),
		checkQuarantine(),
		checkHooks(),
	}
	r := doctorReport{
		Capabilities: config.CapabilitiesVersion,
		YnhVersion:   config.Version,
		Checks:       checks,
	}
	r.Summary.Checks = len(checks)
	for i := range r.Checks {
		// A consumer iterating findings must not have to special-case null.
		// Empty means "this check ran and found nothing", which is different
		// from absent and must serialise as [].
		if r.Checks[i].Findings == nil {
			r.Checks[i].Findings = []doctorFinding{}
		}
	}
	for _, c := range checks {
		for _, f := range c.Findings {
			switch f.Severity {
			case sevError:
				r.Summary.Errors++
			case sevWarn:
				r.Summary.Warnings++
			}
		}
	}
	return r
}

// printDoctorText renders the report. Exit status stays 0 even with findings:
// doctor diagnoses, and a user running it because something is already broken
// should not have the tool itself fail on them. Callers wanting a gate read
// summary.errors from --format json.
func printDoctorText(w io.Writer, r doctorReport) error {
	for _, c := range r.Checks {
		_, _ = fmt.Fprintf(w, "ynh doctor: %s\n", c.Title)
		if len(c.Findings) == 0 {
			_, _ = fmt.Fprintln(w, "  ok    no problems found.")
			_, _ = fmt.Fprintln(w)
			continue
		}
		for _, f := range c.Findings {
			subject := f.Subject
			if subject != "" {
				subject += ": "
			}
			_, _ = fmt.Fprintf(w, "  %-5s %s%s\n", f.Severity, subject, f.Message)
			if f.Remedy != "" {
				_, _ = fmt.Fprintf(w, "        run: %s\n", f.Remedy)
			}
		}
		_, _ = fmt.Fprintln(w)
	}

	switch {
	case r.Summary.Errors > 0:
		_, _ = fmt.Fprintf(w, "%d error(s), %d warning(s).\n", r.Summary.Errors, r.Summary.Warnings)
	case r.Summary.Warnings > 0:
		_, _ = fmt.Fprintf(w, "%d warning(s).\n", r.Summary.Warnings)
	default:
		_, _ = fmt.Fprintln(w, "No problems found.")
	}
	return nil
}

// ---- hook wiring ---------------------------------------------------------

// checkHooks is the original doctor, preserved as one check among several.
func checkHooks() doctorCheck {
	c := doctorCheck{Name: "hooks", Title: "Claude hook wiring"}

	anyFile := false
	for _, rel := range claudeSettingsFiles {
		present, fs := checkClaudeSettings(rel)
		if present {
			anyFile = true
		}
		c.Findings = append(c.Findings, fs...)
	}
	if !anyFile {
		c.Findings = append(c.Findings, doctorFinding{
			Severity: sevInfo,
			Message: "no .claude/settings.json in this project; hooks declared in " +
				".ynh-plugin/plugin.json do not auto-activate in a plain Claude session",
			Remedy: "ynh hook export <harness> --target settings",
		})
	}
	c.Status = c.worst()
	return c
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
		return true, []doctorFinding{{Severity: sevError, Subject: path,
			Message: fmt.Sprintf("not valid JSON: %v", err)}}
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
				Severity: sevWarn,
				Subject:  path,
				Message: fmt.Sprintf("event %q is an ynh canonical name; Claude only recognises %q here, "+
					"and canonical names belong in .ynh-plugin/plugin.json", event, native),
				Remedy: "ynh hook export <harness> --target settings",
			})
		}
		// cwd-relative commands break after the agent changes directory, and a
		// blocking guard hook then silently fails open.
		for _, cmd := range extractHookCommands(hooks[event]) {
			if strings.HasPrefix(cmd, "./") || strings.HasPrefix(cmd, "../") {
				findings = append(findings, doctorFinding{
					Severity: sevWarn,
					Subject:  path,
					Message: fmt.Sprintf("hook command %q is cwd-relative and breaks after the agent "+
						"changes directory", cmd),
					Remedy: "anchor it to $CLAUDE_PROJECT_DIR",
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
