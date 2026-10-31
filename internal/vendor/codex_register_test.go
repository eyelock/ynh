package vendor

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeCodexAgents writes agent files into runDir/.codex/agents/ the way a
// run does: a delegate through DelegateAgentFile and a harness agent through
// TransformArtifact.
func writeCodexAgents(t *testing.T, runDir string) string {
	t.Helper()
	dir := filepath.Join(runDir, ".codex", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	c := &Codex{}
	delName, delData, err := c.DelegateAgentFile("helper", `Says "hi"`, "Reply hi.")
	if err != nil {
		t.Fatal(err)
	}
	agentName, agentData := c.TransformArtifact("agents", "file-name.md",
		[]byte("---\nname: front-matter\ndescription: A test agent.\n---\n\nReply ok.\n"))
	for name, data := range map[string][]byte{delName: delData, agentName: agentData} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestCodexAgentArgs_RegistersEachAgentByConfigFile(t *testing.T) {
	runDir := t.TempDir()
	dir := writeCodexAgents(t, runDir)

	args, warnings := CodexAgentArgs(runDir)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %q, want none", warnings)
	}
	// Sorted by file: file-name.toml (registered by its frontmatter name),
	// then helper.toml. The description is the file's TOML string as written.
	want := []string{
		"-c", `agents.front-matter.config_file="` + filepath.Join(dir, "file-name.toml") + `"`,
		"-c", `agents.front-matter.description="A test agent."`,
		"-c", `agents.helper.config_file="` + filepath.Join(dir, "helper.toml") + `"`,
		"-c", `agents.helper.description="Says \"hi\""`,
	}
	if !equalArgs(args, want) {
		t.Errorf("args =\n%q\nwant\n%q", args, want)
	}
}

func TestCodexAgentArgs_NoTrustOverride(t *testing.T) {
	runDir := t.TempDir()
	writeCodexAgents(t, runDir)
	args, _ := CodexAgentArgs(runDir)
	for _, a := range args {
		if strings.HasPrefix(a, "projects.") {
			t.Errorf("args %q carry a trust override %q; registration needs none", args, a)
		}
	}
}

func TestCodexAgentArgs_DottedNameIsWarnedNotRegistered(t *testing.T) {
	runDir := t.TempDir()
	dir := filepath.Join(runDir, ".codex", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_, data, err := (&Codex{}).DelegateAgentFile("team.dev", "A delegate.", "Do it.")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "team.dev.toml"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	args, warnings := CodexAgentArgs(runDir)
	if len(args) != 0 {
		t.Errorf("args = %q, want none for a dotted name", args)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], `"team.dev"`) || !strings.Contains(warnings[0], "'.'") {
		t.Errorf("warnings = %q, want one naming \"team.dev\" and the '.'", warnings)
	}
}

func TestCodexAgentArgs_NoAgents(t *testing.T) {
	if args, warnings := CodexAgentArgs(""); args != nil || warnings != nil {
		t.Errorf("empty run dir: args %q, warnings %q", args, warnings)
	}
	if args, warnings := CodexAgentArgs(t.TempDir()); args != nil || warnings != nil {
		t.Errorf("no .codex/agents: args %q, warnings %q", args, warnings)
	}
}

func TestCodexLaunchArgs_RegisterAgents(t *testing.T) {
	runDir := t.TempDir()
	writeCodexAgents(t, runDir)
	agents, _ := CodexAgentArgs(runDir)
	extra := []string{"--model", "m"}
	var w bytes.Buffer

	tests := []struct {
		name string
		got  []string
		want []string
	}{
		{
			name: "interactive",
			got:  codexInteractiveArgs(runDir, extra, &w),
			want: append(append([]string{}, agents...), "--model", "m"),
		},
		{
			name: "exec",
			got:  codexExecArgs(runDir, "go", extra, &w),
			want: append(append([]string{"exec", "--skip-git-repo-check"}, agents...), "--model", "m", "go"),
		},
		{
			name: "interactive resume",
			got:  withCodexRunArgs([]string{"resume", "abc"}, agents),
			want: append(append([]string{"resume"}, agents...), "abc"),
		},
		{
			name: "one-shot resume",
			got:  withCodexRunArgs([]string{"exec", "resume", "--last", "go"}, agents),
			want: append(append([]string{"exec", "resume", "--skip-git-repo-check"}, agents...), "--last", "go"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !equalArgs(tt.got, tt.want) {
				t.Errorf("args =\n%q\nwant\n%q", tt.got, tt.want)
			}
		})
	}
	if w.Len() != 0 {
		t.Errorf("unexpected warnings: %s", w.String())
	}
}

func TestCodexLaunchArgs_PrintWarnings(t *testing.T) {
	runDir := t.TempDir()
	dir := filepath.Join(runDir, ".codex", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_, data, _ := (&Codex{}).DelegateAgentFile("a.b", "d", "p")
	if err := os.WriteFile(filepath.Join(dir, "a.b.toml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	var w bytes.Buffer
	codexExecArgs(runDir, "go", nil, &w)
	if !strings.HasPrefix(w.String(), "warning: agent \"a.b\"") {
		t.Errorf("stderr = %q, want a warning naming the agent", w.String())
	}
}
