package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/a2acli"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/namespace"
	"github.com/eyelock/ynh/internal/vendor"
)

// hasA2ADelegates reports whether p has a delegates_to entry whose transport
// resolves to a2a: the harnesses the a2a CLI route is for.
func hasA2ADelegates(p *harness.Harness) bool {
	for _, d := range p.DelegatesTo {
		if p.TransportOf(d) == harness.TransportA2A {
			return true
		}
	}
	return false
}

// writeA2ACLISkills puts the vendored a2a-cli skill and ynh's companion skill
// in the parent's vendor skill folder, when the harness has A2A delegates.
func writeA2ACLISkills(p *harness.Harness, adapter vendor.Adapter, runDir string) error {
	if !hasA2ADelegates(p) {
		return nil
	}
	dir, ok := adapter.ArtifactDirs()["skills"]
	if !ok {
		return nil
	}
	return a2acli.WriteSkills(filepath.Join(runDir, adapter.ConfigDir(), dir))
}

// a2aProbe is what doctor reads of the machine, replaceable in tests.
type a2aProbe struct {
	binary func() (found bool, version string)
	home   string // the user's home directory
	cwd    string // the project directory
	// skillsDir is the vendor skill folder of the harness's run dir, "" when
	// there is none yet.
	skillsDir func(id string, adapter vendor.Adapter) string
}

func defaultA2AProbe() a2aProbe {
	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()
	return a2aProbe{
		binary: a2acli.Binary,
		home:   home,
		cwd:    cwd,
		skillsDir: func(id string, adapter vendor.Adapter) string {
			dir, ok := adapter.ArtifactDirs()["skills"]
			if !ok {
				return ""
			}
			return filepath.Join(config.RunDir(), namespace.IDToFSName(id), adapter.ConfigDir(), dir)
		},
	}
}

// checkA2ACLI checks what the a2a CLI route needs, for each installed harness
// with A2A delegates. Every finding is a warning: ynh installs nothing, and a
// run still starts without any of it, but the parent could not reach its
// delegates by the shell route.
func checkA2ACLI() doctorCheck {
	return checkA2ACLIWith(defaultA2AProbe())
}

func checkA2ACLIWith(probe a2aProbe) doctorCheck {
	c := doctorCheck{Name: "a2a-cli", Title: "a2a CLI route"}
	list, err := harness.ListAll()
	if err != nil {
		c.Status = c.worst() // checkHarnesses reports the unlistable directory
		return c
	}
	cfg, _ := config.Load()
	binaryDone := false
	for _, e := range list {
		id := canonicalIDForEntry(e)
		h, err := harness.LoadByID(id)
		if err != nil || !hasA2ADelegates(h) {
			continue
		}
		if !binaryDone {
			binaryDone = true
			c.Findings = append(c.Findings, a2aBinaryFindings(id, probe)...)
		}
		name := h.DefaultVendor
		if name == "" && cfg != nil {
			name = cfg.DefaultVendor
		}
		adapter, err := vendor.Get(name)
		if err != nil {
			continue // checkVendors reports an unknown vendor
		}
		c.Findings = append(c.Findings, a2aVendorFindings(id, adapter, probe)...)
	}
	c.Status = c.worst()
	return c
}

func a2aBinaryFindings(id string, probe a2aProbe) []doctorFinding {
	install := fmt.Sprintf("install it: curl -fsSL https://raw.githubusercontent.com/a2aproject/a2a-cli/main/install.sh | A2A_VERSION=%s sh, or brew tap a2aproject/a2a-cli https://github.com/a2aproject/a2a-cli && brew install a2a", a2acli.CLIVersion)
	found, version := probe.binary()
	switch {
	case !found:
		return []doctorFinding{{Severity: sevWarn, Subject: id,
			Message: "has A2A delegates, but the a2a CLI is not on PATH, so the parent cannot reach them by the shell route (ynh does not install it)",
			Remedy:  install}}
	case version != "" && a2acli.Older(version, a2acli.CLIVersion):
		return []doctorFinding{{Severity: sevWarn, Subject: id,
			Message: fmt.Sprintf("a2a is version %s, older than the %s ynh's route is pinned to", version, a2acli.CLIVersion),
			Remedy:  install}}
	}
	return nil
}

func a2aVendorFindings(id string, adapter vendor.Adapter, probe a2aProbe) []doctorFinding {
	var out []doctorFinding
	if dir := probe.skillsDir(id, adapter); dir != "" {
		// A run dir that does not exist yet is not a finding: the skill is
		// written when the harness first runs.
		if _, err := os.Stat(filepath.Dir(dir)); err == nil {
			switch a2acli.CheckSkill(dir) {
			case a2acli.SkillMissing:
				out = append(out, doctorFinding{Severity: sevWarn, Subject: id,
					Message: fmt.Sprintf("the %s skill is missing from %s's skill folder (%s)", a2acli.SkillName, adapter.Name(), dir),
					Remedy:  fmt.Sprintf("ynh run %s, which writes it", id)})
			case a2acli.SkillDiffers:
				out = append(out, doctorFinding{Severity: sevWarn, Subject: id,
					Message: fmt.Sprintf("the %s skill in %s differs from the pin (a2a-cli commit %s)", a2acli.SkillName, dir, a2acli.SkillCommit),
					Remedy:  fmt.Sprintf("ynh run %s, which writes the pinned copy again", id)})
			}
		}
	}
	switch adapter.Name() {
	case "codex":
		if !codexNetworkAllowed(probe.home) {
			out = append(out, doctorFinding{Severity: sevWarn, Subject: id,
				Message: "on Codex the a2a CLI cannot reach 127.0.0.1 unless the sandbox is workspace-write with network_access on (the default sandbox answers \"connect: operation not permitted\"). Turning it on has a cost: the shell is then no longer sandboxed from localhost generally",
				Remedy:  "in ~/.codex/config.toml: sandbox_mode = \"workspace-write\", and under [sandbox_workspace_write] network_access = true"})
		}
	case "claude":
		if !settingsAllow(a2aAllowSources("claude", probe), "Bash(a2a:*)", "Bash(a2a *)") {
			out = append(out, doctorFinding{Severity: sevWarn, Subject: id,
				Message: "no allow rule for the a2a command in Claude Code's settings, so a headless run's a2a calls are denied",
				Remedy:  `add "Bash(a2a:*)" to permissions.allow in .claude/settings.json (or ~/.claude/settings.json), or pass --allowedTools 'Bash(a2a:*)'`})
		}
	case "copilot":
		if !settingsAllow(a2aAllowSources("copilot", probe), "shell(a2a:*)") {
			out = append(out, doctorFinding{Severity: sevWarn, Subject: id,
				Message: "no allow rule for the a2a command in Copilot CLI's settings, so a headless run's a2a calls are denied unless every tool is allowed (a one-shot ynh run passes --allow-all-tools)",
				Remedy:  `add "shell(a2a:*)" to the allowed tools in ~/.copilot/config.json, or pass --allow-tool 'shell(a2a:*)'`})
		}
	}
	// OpenCode needs no allow rule; Cursor's route is untested.
	return out
}

// a2aAllowSources are the files that may hold a vendor's allow rules.
func a2aAllowSources(vendorName string, probe a2aProbe) []string {
	switch vendorName {
	case "claude":
		return []string{
			filepath.Join(probe.home, ".claude", "settings.json"),
			filepath.Join(probe.cwd, ".claude", "settings.json"),
			filepath.Join(probe.cwd, ".claude", "settings.local.json"),
		}
	case "copilot":
		return []string{
			filepath.Join(probe.home, ".copilot", "config.json"),
			filepath.Join(probe.home, ".copilot", "settings.json"),
			filepath.Join(probe.cwd, ".copilot", "settings.json"),
		}
	}
	return nil
}

// settingsAllow reports whether any of the files names one of the rules. It
// looks for the rule's text rather than parsing each vendor's schema: a rule
// written anywhere in a settings file is the user's intent to allow it.
func settingsAllow(paths []string, rules ...string) bool {
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, r := range rules {
			if strings.Contains(string(b), `"`+r+`"`) {
				return true
			}
		}
	}
	return false
}

// codexNetworkAllowed reports whether Codex's config.toml sets the
// workspace-write sandbox with network_access on, the only setting under which
// the a2a CLI reaches localhost. It reads the two lines that matter, not TOML.
func codexNetworkAllowed(home string) bool {
	dir := os.Getenv("CODEX_HOME")
	if dir == "" {
		dir = filepath.Join(home, ".codex")
	}
	b, err := os.ReadFile(filepath.Join(dir, "config.toml"))
	if err != nil {
		return false
	}
	mode, network := "", false
	section := ""
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if i := strings.Index(line, "#"); i >= 0 && !strings.Contains(line[:i], `"`) {
			line = strings.TrimSpace(line[:i])
		}
		if strings.HasPrefix(line, "[") {
			section = strings.Trim(line, "[] ")
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), `"`)
		switch {
		case section == "" && k == "sandbox_mode":
			mode = v
		case section == "sandbox_workspace_write" && k == "network_access":
			network = v == "true"
		}
	}
	return mode == "workspace-write" && network
}
