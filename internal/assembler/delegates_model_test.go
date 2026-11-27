package assembler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/vendor"
)

// nativeAgent assembles one native delegate, whose own manifest carries
// delegateDefaults, behind an entry carrying entryModel and entryEffort, and
// returns the text of its agent file and the warnings.
func nativeAgent(t *testing.T, vendorName string, delegateDefaults map[string]any, entryModel, entryEffort string, withServers bool) (agent, warnings, launch string) {
	t.Helper()
	t.Setenv("YNH_HOME", t.TempDir())
	manifest := map[string]any{"name": "probe", "version": "0.1.0", "description": "Probes things."}
	for k, v := range delegateDefaults {
		manifest[k] = v
	}
	if withServers {
		manifest["mcp_servers"] = map[string]any{"b": map[string]any{"command": "/bin/b"}}
	}
	repo := delegateRepo(t, t.TempDir(), manifest, "lint")
	adapter, err := vendor.Get(vendorName)
	if err != nil {
		t.Fatal(err)
	}
	workDir := t.TempDir()
	var warn strings.Builder
	_, err = AssembleDelegates(workDir, adapter,
		[]harness.Delegate{{Transport: harness.TransportNative, Model: entryModel, Effort: entryEffort, GitSource: harness.GitSource{Git: repo}}}, "",
		DelegateOptions{Warn: &warn, Launch: true})
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(workDir, vendor.ClaudeDelegateAgentsFile)); err == nil {
		launch = string(data)
	}
	dir := filepath.Join(workDir, adapter.ConfigDir(), adapter.ArtifactDirs()["agents"])
	if entries, _ := os.ReadDir(dir); len(entries) == 1 {
		data, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
		if err != nil {
			t.Fatal(err)
		}
		agent = string(data)
	}
	return agent, warn.String(), launch
}

// On Claude Code the subagent's own "model" and "effort" carry the entry's
// choice, else the delegate harness's default, else nothing (the session's).
func TestNativeSubagentCarriesModelAndEffortOnClaude(t *testing.T) {
	defaults := map[string]any{"default_model": "dm", "default_effort": "low"}
	tests := []struct {
		name         string
		defaults     map[string]any
		model, eff   string
		want, absent []string
	}{
		{"entry wins over the delegate default", defaults, "em", "high", []string{`model: "em"`, `effort: "high"`}, []string{`"dm"`, `"low"`}},
		{"delegate default wins over the vendor", defaults, "", "", []string{`model: "dm"`, `effort: "low"`}, nil},
		{"each on its own", defaults, "em", "", []string{`model: "em"`, `effort: "low"`}, []string{`"dm"`}},
		{"vendor default when nothing names one", nil, "", "", nil, []string{"model:", "effort:"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent, warnings, _ := nativeAgent(t, "claude", tt.defaults, tt.model, tt.eff, false)
			if agent == "" {
				t.Fatal("no agent file")
			}
			front := agent[:strings.Index(agent[4:], "---")+4]
			for _, w := range tt.want {
				if !strings.Contains(front, w) {
					t.Errorf("frontmatter lacks %s:\n%s", w, front)
				}
			}
			for _, a := range tt.absent {
				if strings.Contains(front, a) {
					t.Errorf("frontmatter must not hold %s:\n%s", a, front)
				}
			}
			if warnings != "" {
				t.Errorf("warnings: %q", warnings)
			}
		})
	}
}

// A subagent that declares MCP servers is handed to Claude Code as JSON, which
// carries the same model and effort.
func TestNativeSubagentWithServersCarriesModelAndEffortInTheLaunchFile(t *testing.T) {
	_, _, launch := nativeAgent(t, "claude", map[string]any{"default_model": "dm", "default_effort": "low"}, "em", "", true)
	if !strings.Contains(launch, `"model":"em"`) || !strings.Contains(launch, `"effort":"low"`) {
		t.Errorf("launch file = %s", launch)
	}
	_, _, plain := nativeAgent(t, "claude", nil, "", "", true)
	if strings.Contains(plain, `"model"`) || strings.Contains(plain, `"effort"`) {
		t.Errorf("no model or effort named, so none written: %s", plain)
	}
}

func TestNativeSubagentCarriesModelAndEffortOnCodex(t *testing.T) {
	agent, warnings, _ := nativeAgent(t, "codex", map[string]any{"default_model": "dm", "default_effort": "low"}, "em", "", false)
	if !strings.Contains(agent, `model = "em"`) || !strings.Contains(agent, `model_reasoning_effort = "low"`) {
		t.Errorf("codex agent:\n%s", agent)
	}
	if warnings != "" {
		t.Errorf("warnings: %q", warnings)
	}
	agent, _, _ = nativeAgent(t, "codex", nil, "", "", false)
	if strings.Contains(agent, "model") {
		t.Errorf("nothing named, so nothing written:\n%s", agent)
	}
}

// A vendor whose subagents ynh cannot give a model or effort says so, and the
// subagent runs on the session's own: it is never silently dropped.
func TestNativeSubagentOnAVendorThatCannotCarryThemWarns(t *testing.T) {
	for _, name := range []string{"copilot", "opencode"} {
		t.Run(name, func(t *testing.T) {
			agent, warnings, _ := nativeAgent(t, name, nil, "em", "high", false)
			if !strings.Contains(warnings, "cannot be given") || !strings.Contains(warnings, "probe") {
				t.Errorf("warnings = %q", warnings)
			}
			if strings.Contains(agent, "em") && strings.Contains(agent, "model:") {
				t.Errorf("the model must not be written where it is not understood:\n%s", agent)
			}
		})
	}
	_, warnings, _ := nativeAgent(t, "opencode", nil, "", "", false)
	if warnings != "" {
		t.Errorf("no model or effort named, so nothing to warn about: %q", warnings)
	}
}
