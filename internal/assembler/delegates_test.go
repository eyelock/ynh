package assembler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/persona"
)

func TestBuildDelegateAgent(t *testing.T) {
	// Create a fake delegate persona directory
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "skills", "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rules", "testing.md"), []byte("Always write tests."), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skills", "deploy", "SKILL.md"), []byte("deploy skill"), 0o644); err != nil {
		t.Fatal(err)
	}

	p := &persona.Persona{
		Name:          "team-dev",
		DefaultVendor: "claude",
	}

	content := buildDelegateAgent(p, dir)

	// Check frontmatter
	if !strings.Contains(content, "name: team-dev") {
		t.Error("missing name in frontmatter")
	}
	if !strings.Contains(content, "description:") {
		t.Error("missing description in frontmatter")
	}

	// Check rules are inlined
	if !strings.Contains(content, "Always write tests.") {
		t.Error("rules not inlined in agent content")
	}
	if !strings.Contains(content, "### testing") {
		t.Error("rule name not included as heading")
	}

	// Check skills are listed
	if !strings.Contains(content, "- deploy") {
		t.Error("skills not listed in agent content")
	}
}

func TestBuildDelegateAgent_NoRulesNoSkills(t *testing.T) {
	dir := t.TempDir()
	p := &persona.Persona{
		Name: "minimal",
	}

	content := buildDelegateAgent(p, dir)

	if !strings.Contains(content, "name: minimal") {
		t.Error("missing name")
	}
	// Should not have Rules or Skills sections
	if strings.Contains(content, "## Rules") {
		t.Error("should not have Rules section when no rules exist")
	}
	if strings.Contains(content, "## Available Skills") {
		t.Error("should not have Skills section when no skills exist")
	}
}

func TestAssembleDelegates_WithLocalRepo(t *testing.T) {
	// Create a delegate persona as a local git repo
	delegateDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(delegateDir, "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	pluginDir := filepath.Join(delegateDir, ".claude-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"team-ops","version":"0.1.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(delegateDir, "metadata.json"), []byte(`{"ynh":{"default_vendor":"claude"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(delegateDir, "rules", "safety.md"), []byte("Always check before deploying."), 0o644); err != nil {
		t.Fatal(err)
	}

	// Init git repo so resolver can clone it
	runGit(t, delegateDir, "init")
	runGit(t, delegateDir, "config", "user.email", "test@test.com")
	runGit(t, delegateDir, "config", "user.name", "Test")
	runGit(t, delegateDir, "add", ".")
	runGit(t, delegateDir, "commit", "-m", "init")

	t.Setenv("YNH_HOME", "")
	t.Setenv("HOME", t.TempDir())

	// Assemble into a work directory
	workDir := t.TempDir()
	adapter := &mockAdapter{}

	// Create the config structure first
	configDir := filepath.Join(workDir, adapter.ConfigDir())
	if err := os.MkdirAll(filepath.Join(configDir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}

	delegates := []persona.Delegate{
		{GitSource: persona.GitSource{Git: delegateDir}},
	}

	if err := AssembleDelegates(workDir, adapter, delegates); err != nil {
		t.Fatalf("AssembleDelegates failed: %v", err)
	}

	// Verify agent file was created
	agentFile := filepath.Join(configDir, "agents", "team-ops.md")
	data, err := os.ReadFile(agentFile)
	if err != nil {
		t.Fatalf("delegate agent file not created: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "name: team-ops") {
		t.Error("agent file missing name")
	}
	if !strings.Contains(content, "Always check before deploying.") {
		t.Error("agent file missing inlined rules")
	}
}

func TestAssembleDelegates_Empty(t *testing.T) {
	workDir := t.TempDir()
	adapter := &mockAdapter{}

	// Should be a no-op
	if err := AssembleDelegates(workDir, adapter, nil); err != nil {
		t.Fatalf("AssembleDelegates with nil delegates failed: %v", err)
	}
	if err := AssembleDelegates(workDir, adapter, []persona.Delegate{}); err != nil {
		t.Fatalf("AssembleDelegates with empty delegates failed: %v", err)
	}
}

// runGit is a helper for delegate tests (same pattern as other test files).
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}
