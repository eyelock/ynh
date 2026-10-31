package assembler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/vendor"
)

// Codex reads a project's custom agents as TOML files in .codex/agents/, so a
// delegate is written as one, and no Markdown file is left beside it. Its MCP
// servers still cannot be carried: a custom agent inherits the parent's.
func TestAssembleDelegates_CodexWritesTOMLAgent(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "instructions.md"), []byte("Say \"hi\" from C:\\tmp.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := delegateRepo(t, dir, map[string]any{
		"name": "probe", "version": "0.1.0", "description": "Probes things.",
		"mcp_servers": map[string]any{"b": map[string]any{"command": "/bin/b"}},
	}, "lint")

	adapter, err := vendor.Get("codex")
	if err != nil {
		t.Fatal(err)
	}
	workDir := t.TempDir()
	var warn strings.Builder
	if _, err := AssembleDelegates(workDir, adapter, []harness.Delegate{{GitSource: harness.GitSource{Git: repo}}}, "", DelegateOptions{Warn: &warn, Launch: true}); err != nil {
		t.Fatal(err)
	}

	agents := filepath.Join(workDir, ".codex", "agents")
	data, err := os.ReadFile(filepath.Join(agents, "probe.toml"))
	if err != nil {
		t.Fatalf("agent file: %v", err)
	}
	want := "name = \"probe\"\n" +
		"description = \"Probes things.\"\n" +
		"developer_instructions = \"\"\"\n" +
		"You are the **probe** harness, invoked as a delegate.\n\n" +
		"## Instructions\n\n" +
		"Say \\\"hi\\\" from C:\\\\tmp.\n\n" +
		"## Available Skills\n\n" +
		"- lint\n\n" +
		"\"\"\"\n"
	if string(data) != want {
		t.Errorf("agent =\n%s\nwant\n%s", data, want)
	}
	if _, err := os.Stat(filepath.Join(agents, "probe.md")); err == nil {
		t.Error("Codex does not read a Markdown agent; none should be written")
	}

	wantWarn := "warning: delegate probe declares MCP servers (b) that OpenAI Codex subagents cannot carry; they are not available to it\n"
	if !strings.HasSuffix(warn.String(), wantWarn) {
		t.Errorf("warnings = %q, want it to end with %q", warn.String(), wantWarn)
	}
	if _, err := os.Stat(filepath.Join(workDir, vendor.ClaudeDelegateAgentsFile)); err == nil {
		t.Error("the launch file is Claude's alone")
	}
}
