package assembler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/vendor"
)

// selectRepo is a delegate harness with a base server, a profile that adds
// one and removes the base one, and focuses with and without a profile.
func selectRepo(t *testing.T) string {
	t.Helper()
	return delegateRepo(t, t.TempDir(), map[string]any{
		"name": "probe", "version": "0.1.0", "description": "Probe agent",
		"mcp_servers": map[string]any{
			"base": map[string]any{"command": "base-srv"},
		},
		"profiles": map[string]any{
			"ci": map[string]any{"mcp_servers": map[string]any{
				"base": nil,
				"ci":   map[string]any{"command": "ci-srv"},
			}},
		},
		"focuses": map[string]any{
			"triage": map[string]any{"profile": "ci", "prompt": "Triage the failing build first."},
			"review": map[string]any{"prompt": "Review only; change nothing."},
		},
		"launch_focus": "review",
	})
}

func assembleSelected(t *testing.T, del harness.Delegate) (string, error) {
	t.Helper()
	t.Setenv("YNH_HOME", t.TempDir())
	adapter, err := vendor.Get("claude")
	if err != nil {
		t.Fatal(err)
	}
	workDir := t.TempDir()
	var warn strings.Builder
	if _, err := AssembleDelegates(workDir, adapter, []harness.Delegate{del}, "", DelegateOptions{Warn: &warn}); err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(workDir, adapter.ConfigDir(), adapter.ArtifactDirs()["agents"], "probe.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data), nil
}

func agentServers(t *testing.T, agent string) []string {
	t.Helper()
	var names []string
	for _, e := range frontmatterMCP(t, agent) {
		for k := range e {
			names = append(names, k)
		}
	}
	return names
}

// A delegates_to entry's profile or focus decides what the delegate's agent
// carries: the profile's servers, or the focus's profile and its prompt as
// standing instructions. With neither, the base config, and launch_focus is
// never applied.
func TestAssembleDelegates_ProfileAndFocus(t *testing.T) {
	repo := selectRepo(t)
	src := harness.GitSource{Git: repo}
	tests := []struct {
		name        string
		del         harness.Delegate
		servers     string
		focus       string // a line the body must hold, "" for no focus section
		noFocusText []string
	}{
		{name: "neither", del: harness.Delegate{GitSource: src}, servers: "base",
			noFocusText: []string{"## Focus", "Review only", "Triage"}},
		{name: "profile", del: harness.Delegate{GitSource: src, Profile: "ci"}, servers: "ci",
			noFocusText: []string{"## Focus"}},
		{name: "focus with a profile", del: harness.Delegate{GitSource: src, Focus: "triage"}, servers: "ci",
			focus: "## Focus: triage\n\nTriage the failing build first."},
		{name: "focus without a profile", del: harness.Delegate{GitSource: src, Focus: "review"}, servers: "base",
			focus: "## Focus: review\n\nReview only; change nothing."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent, err := assembleSelected(t, tt.del)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(agentServers(t, agent), ","); got != tt.servers {
				t.Errorf("servers = %q, want %q\n%s", got, tt.servers, agent)
			}
			if tt.focus != "" && !strings.Contains(agent, tt.focus) {
				t.Errorf("agent lacks %q:\n%s", tt.focus, agent)
			}
			for _, s := range tt.noFocusText {
				if strings.Contains(agent, s) {
					t.Errorf("agent holds %q:\n%s", s, agent)
				}
			}
		})
	}
}

// Naming both is an error, as is a name the delegate does not declare; each
// message names what went wrong.
func TestAssembleDelegates_SelectionErrors(t *testing.T) {
	repo := selectRepo(t)
	src := harness.GitSource{Git: repo}
	tests := []struct {
		name string
		del  harness.Delegate
		want string
	}{
		{"both", harness.Delegate{GitSource: src, Profile: "ci", Focus: "triage"}, `names both profile "ci" and focus "triage"`},
		{"unknown profile", harness.Delegate{GitSource: src, Profile: "nope"}, `profile "nope" not defined`},
		{"unknown focus", harness.Delegate{GitSource: src, Focus: "nope"}, `focus "nope" not defined in its manifest (available: [review triage])`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := assembleSelected(t, tt.del)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want one containing %q", err, tt.want)
			}
		})
	}
}

// On Codex the delegate is a TOML custom agent, and a selected focus's prompt
// is in its developer_instructions, as it is in the Markdown agent's body.
func TestAssembleDelegates_CodexDelegateCarriesFocus(t *testing.T) {
	repo := selectRepo(t)
	t.Setenv("YNH_HOME", t.TempDir())
	adapter, err := vendor.Get("codex")
	if err != nil {
		t.Fatal(err)
	}
	workDir := t.TempDir()
	var warn strings.Builder
	if _, err := AssembleDelegates(workDir, adapter, []harness.Delegate{{GitSource: harness.GitSource{Git: repo}, Focus: "triage"}}, "", DelegateOptions{Warn: &warn}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(workDir, ".codex", "agents", "probe.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "## Focus: triage\n\nTriage the failing build first.") {
		t.Errorf("developer_instructions lack the focus:\n%s", data)
	}
}
