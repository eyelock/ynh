package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/vendor"
)

// checkMCPAllow checks that the parent vendor would let the parent call the
// tools of the ynh-delegates MCP server, for each installed harness with A2A
// delegates. Every finding is a warning (or a note): ynh edits no vendor
// settings, and a run still starts without the rule.
//
// What it reads is the vendor's settings files on disk. It cannot see an allow
// rule passed on a command line (--allowedTools, --allow-tool, -c), so a rule
// given that way still warns; the findings say so.
func checkMCPAllow() doctorCheck {
	return checkMCPAllowWith(defaultA2AProbe())
}

func checkMCPAllowWith(probe a2aProbe) doctorCheck {
	c := doctorCheck{Name: "mcp-allow", Title: "ynh-delegates MCP allow rule"}
	list, err := harness.ListAll()
	if err != nil {
		c.Status = c.worst() // checkHarnesses reports the unlistable directory
		return c
	}
	cfg, _ := config.Load()
	for _, e := range list {
		id := canonicalIDForEntry(e)
		h, err := harness.LoadByID(id)
		if err != nil || !hasA2ADelegates(h) {
			continue
		}
		name := h.DefaultVendor
		if name == "" && cfg != nil {
			name = cfg.DefaultVendor
		}
		adapter, err := vendor.Get(name)
		if err != nil {
			continue // checkVendors reports an unknown vendor
		}
		c.Findings = append(c.Findings, mcpAllowFindings(id, h, adapter, probe)...)
	}
	c.Status = c.worst()
	return c
}

func mcpAllowFindings(id string, h *harness.Harness, adapter vendor.Adapter, probe a2aProbe) []doctorFinding {
	const server = delegateMCPServerName
	switch adapter.Name() {
	case "claude":
		prefix := "mcp__plugin_" + h.Name + "_" + server
		allowed, denied, notes := claudeMCPRules(a2aAllowSources("claude", probe), prefix)
		out := notes
		switch {
		case denied:
			out = append(out, doctorFinding{Severity: sevWarn, Subject: id,
				Message: "a permissions.deny rule in Claude Code's settings denies the ynh-delegates MCP tools, and deny wins over any allow rule, so the parent's delegate_* calls are denied",
				Remedy:  fmt.Sprintf(`remove the rule naming %s from permissions.deny in the settings file that holds it`, prefix)})
		case !allowed:
			out = append(out, doctorFinding{Severity: sevWarn, Subject: id,
				Message: "no allow rule for the ynh-delegates MCP tools in Claude Code's settings, so a headless run's delegate_* calls are denied. Read from the settings files only: a rule passed as --allowedTools is not seen",
				Remedy:  fmt.Sprintf(`add "%s" to permissions.allow in .claude/settings.json (or ~/.claude/settings.json), or pass --allowedTools %s`, prefix, prefix)})
		}
		return out
	case "copilot":
		if !settingsAllowPrefix(a2aAllowSources("copilot", probe), server) {
			return []doctorFinding{{Severity: sevWarn, Subject: id,
				Message: "no allow rule for the ynh-delegates MCP tools in Copilot CLI's settings, so a headless run's delegate_* calls are denied unless every tool is allowed (a one-shot ynh run passes --allow-all-tools). Read from the settings files only: a rule passed as --allow-tool is not seen",
				Remedy:  `add "ynh-delegates" to the allowed tools in ~/.copilot/config.json, or pass --allow-tool=ynh-delegates`}}
		}
	case "codex":
		mode, set := codexDelegatesApproval(probe.home)
		switch {
		case set && mode != "approve":
			return []doctorFinding{{Severity: sevWarn, Subject: id,
				Message: fmt.Sprintf(`Codex's config sets mcp_servers.ynh-delegates.default_tools_approval_mode = %q, which wins over the "approve" ynh passes, so a one-shot run's delegate_* calls wait for an approval nobody gives. A -c override on a command line is not seen`, mode),
				Remedy:  `remove that line from ~/.codex/config.toml, or set it to "approve"`}}
		case !set && h.MCPManualApproval:
			return []doctorFinding{{Severity: sevWarn, Subject: id,
				Message: "the harness sets mcp_manual_approval, so ynh does not approve its MCP tools on a one-shot run, and Codex's config does not approve ynh-delegates, so its delegate_* calls wait for an approval nobody gives. A -c override on a command line is not seen",
				Remedy:  `set mcp_servers.ynh-delegates.default_tools_approval_mode = "approve" in ~/.codex/config.toml, or drop mcp_manual_approval`}}
		}
	case "cursor":
		return []doctorFinding{{Severity: sevInfo, Subject: id,
			Message: "the allow rule for the ynh-delegates MCP tools is not checked on Cursor: ynh's route has not been run there, so what it needs is not known"}}
	}
	// OpenCode needs no allow rule.
	return nil
}

// claudeMCPRules reads Claude Code's settings files as JSON and reports
// whether permissions.allow and permissions.deny each hold a rule for the
// server (prefix) or one of its tools (prefix__tool). A file that is not valid
// JSON, or whose permissions are not lists of strings, comes back as an info
// note: its rules could not be read, so none are counted from it.
func claudeMCPRules(paths []string, prefix string) (allowed, denied bool, notes []doctorFinding) {
	matches := func(rule string) bool {
		return rule == prefix || strings.HasPrefix(rule, prefix+"__")
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var doc struct {
			Permissions struct {
				Allow []string `json:"allow"`
				Deny  []string `json:"deny"`
			} `json:"permissions"`
		}
		if err := json.Unmarshal(b, &doc); err != nil {
			notes = append(notes, doctorFinding{Severity: sevInfo, Subject: p,
				Message: fmt.Sprintf("could not read Claude Code's settings as JSON (%v), so its allow and deny rules for ynh-delegates were not counted from it", err),
				Remedy:  "fix the file's JSON"})
			continue
		}
		for _, r := range doc.Permissions.Allow {
			allowed = allowed || matches(r)
		}
		for _, r := range doc.Permissions.Deny {
			denied = denied || matches(r)
		}
	}
	return allowed, denied, notes
}

// settingsAllowPrefix reports whether any of the files holds a quoted rule
// that starts with prefix: the server's rule alone, or one naming a tool of it.
// It reads text, not the vendor's schema (Copilot's is unconfirmed, and its
// settings are not known to have a deny list, so one is not read).
func settingsAllowPrefix(paths []string, prefix string) bool {
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if strings.Contains(string(b), `"`+prefix) {
			return true
		}
	}
	return false
}

// codexDelegatesApproval reads the approval mode Codex's config.toml sets for
// the ynh-delegates server, from a [mcp_servers.ynh-delegates] table or a
// dotted key. set is false when the config does not set it.
func codexDelegatesApproval(home string) (mode string, set bool) {
	dir := os.Getenv("CODEX_HOME")
	if dir == "" {
		dir = filepath.Join(home, ".codex")
	}
	b, err := os.ReadFile(filepath.Join(dir, "config.toml"))
	if err != nil {
		return "", false
	}
	section := ""
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if i := strings.Index(line, "#"); i >= 0 && !strings.Contains(line[:i], `"`) {
			line = strings.TrimSpace(line[:i])
		}
		if strings.HasPrefix(line, "[") {
			section = strings.ReplaceAll(strings.Trim(line, "[] "), `"`, "")
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.ReplaceAll(strings.TrimSpace(k), `"`, ""), strings.Trim(strings.TrimSpace(v), `"`)
		if (section == "mcp_servers.ynh-delegates" && k == "default_tools_approval_mode") ||
			(section == "" && k == "mcp_servers.ynh-delegates.default_tools_approval_mode") {
			mode, set = v, true
		}
	}
	return mode, set
}
