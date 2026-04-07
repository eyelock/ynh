package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateSkill(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := createSkill("hello"); err != nil {
		t.Fatalf("createSkill failed: %v", err)
	}

	path := filepath.Join(dir, "skills", "hello", "SKILL.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("SKILL.md not created: %v", err)
	}

	content := string(data)
	if !strings.HasPrefix(content, "---\n") {
		t.Error("SKILL.md missing frontmatter")
	}
	if !strings.Contains(content, "name: hello") {
		t.Error("SKILL.md missing name in frontmatter")
	}
	if !strings.Contains(content, "description:") {
		t.Error("SKILL.md missing description in frontmatter")
	}
}

func TestCreateSkill_AlreadyExists(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := createSkill("hello"); err != nil {
		t.Fatalf("first createSkill failed: %v", err)
	}

	err := createSkill("hello")
	if err == nil {
		t.Fatal("expected error for duplicate skill")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCreateAgent(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := createAgent("reviewer"); err != nil {
		t.Fatalf("createAgent failed: %v", err)
	}

	path := filepath.Join(dir, "agents", "reviewer.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("agent file not created: %v", err)
	}

	content := string(data)
	if !strings.HasPrefix(content, "---\n") {
		t.Error("agent file missing frontmatter")
	}
	if !strings.Contains(content, "name: reviewer") {
		t.Error("agent file missing name in frontmatter")
	}
	if !strings.Contains(content, "description:") {
		t.Error("agent file missing description in frontmatter")
	}
	if !strings.Contains(content, "tools:") {
		t.Error("agent file missing tools in frontmatter")
	}
}

func TestCreateAgent_AlreadyExists(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := createAgent("reviewer"); err != nil {
		t.Fatalf("first createAgent failed: %v", err)
	}

	err := createAgent("reviewer")
	if err == nil {
		t.Fatal("expected error for duplicate agent")
	}
}

func TestCreateHarness(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := createHarness("my-team"); err != nil {
		t.Fatalf("createHarness failed: %v", err)
	}

	expectedFiles := []string{
		"my-team/harness.json",
		"my-team/AGENTS.md",
	}
	for _, f := range expectedFiles {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("expected file %s not created: %v", f, err)
		}
	}

	expectedDirs := []string{
		"my-team/skills",
		"my-team/agents",
		"my-team/rules",
		"my-team/commands",
	}
	for _, d := range expectedDirs {
		info, err := os.Stat(filepath.Join(dir, d))
		if err != nil {
			t.Errorf("expected dir %s not created: %v", d, err)
		} else if !info.IsDir() {
			t.Errorf("%s is not a directory", d)
		}
	}

	// Verify harness.json content
	data, err := os.ReadFile(filepath.Join(dir, "my-team/harness.json"))
	if err != nil {
		t.Fatalf("reading harness.json: %v", err)
	}
	if !strings.Contains(string(data), `"name": "my-team"`) {
		t.Error("harness.json missing name")
	}
	if !strings.Contains(string(data), `"version": "0.1.0"`) {
		t.Error("harness.json missing version")
	}
}

func TestCreateHarness_AlreadyExists(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := createHarness("my-team"); err != nil {
		t.Fatalf("first createHarness failed: %v", err)
	}

	err := createHarness("my-team")
	if err == nil {
		t.Fatal("expected error for duplicate harness")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCreateRule(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := createRule("be-concise"); err != nil {
		t.Fatalf("createRule failed: %v", err)
	}

	path := filepath.Join(dir, "rules", "be-concise.md")
	if _, err := os.ReadFile(path); err != nil {
		t.Fatalf("rule file not created: %v", err)
	}
}

func TestCreateCommand(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := createCommand("check"); err != nil {
		t.Fatalf("createCommand failed: %v", err)
	}

	path := filepath.Join(dir, "commands", "check.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("command file not created: %v", err)
	}

	if !strings.Contains(string(data), "```bash") {
		t.Error("command file missing bash code block")
	}
}

func TestCreateHarness_InsideHarness(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// Make CWD look like a harness
	writeFile(t, filepath.Join(dir, "harness.json"), []byte(`{"name":"test","version":"0.1.0"}`))

	err := createHarness("nested")
	if err == nil {
		t.Fatal("expected error when creating harness inside a harness")
	}
	if !strings.Contains(err.Error(), "already inside a harness") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCmdCreate_InvalidName(t *testing.T) {
	err := cmdCreate([]string{"skill", "../bad-name"})
	if err == nil {
		t.Fatal("expected error for invalid name")
	}
	if !strings.Contains(err.Error(), "invalid name") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCmdCreate_UnknownType(t *testing.T) {
	err := cmdCreate([]string{"widget", "foo"})
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
	if !strings.Contains(err.Error(), "unknown type") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCmdCreate_NotEnoughArgs(t *testing.T) {
	err := cmdCreate([]string{})
	if err == nil {
		t.Fatal("expected error for no args")
	}
	if !strings.Contains(err.Error(), "usage") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCreateRule_AlreadyExists(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := createRule("be-concise"); err != nil {
		t.Fatalf("first createRule failed: %v", err)
	}

	err := createRule("be-concise")
	if err == nil {
		t.Fatal("expected error for duplicate rule")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCreateCommand_AlreadyExists(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := createCommand("deploy"); err != nil {
		t.Fatalf("first createCommand failed: %v", err)
	}

	err := createCommand("deploy")
	if err == nil {
		t.Fatal("expected error for duplicate command")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCmdCreate_SingleArg(t *testing.T) {
	err := cmdCreate([]string{"skill"})
	if err == nil {
		t.Fatal("expected error for single arg")
	}
}

func TestCreateRule_Content(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := createRule("test-rule"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "rules", "test-rule.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Error("rule file is empty")
	}
}

func TestCreateCommand_Content(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := createCommand("test-cmd"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "commands", "test-cmd.md"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "```bash") {
		t.Error("missing bash code block")
	}
	if !strings.Contains(content, "# test-cmd") {
		t.Error("missing heading with command name")
	}
}

func TestCmdCreate_Skill(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	err := cmdCreate([]string{"skill", "test-skill"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "skills", "test-skill", "SKILL.md")); err != nil {
		t.Error("expected SKILL.md to exist")
	}
}

func TestCmdCreate_Agent(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	err := cmdCreate([]string{"agent", "test-agent"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "agents", "test-agent.md")); err != nil {
		t.Error("expected agent file to exist")
	}
}

func TestCmdCreate_Harness(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	err := cmdCreate([]string{"harness", "test-harness"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "test-harness", "harness.json")); err != nil {
		t.Error("expected harness.json to exist")
	}
}

func TestCmdCreate_Rule(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	err := cmdCreate([]string{"rule", "test-rule"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "rules", "test-rule.md")); err != nil {
		t.Error("expected rule file to exist")
	}
}

func TestCmdCreate_Command(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	err := cmdCreate([]string{"command", "test-cmd"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "commands", "test-cmd.md")); err != nil {
		t.Error("expected command file to exist")
	}
}

func TestValidName(t *testing.T) {
	valid := []string{"hello", "my-skill", "v2.0", "test_agent", "A1"}
	for _, name := range valid {
		if !validName.MatchString(name) {
			t.Errorf("%q should be valid", name)
		}
	}

	invalid := []string{"", "../bad", ".hidden", "-start", "_start"}
	for _, name := range invalid {
		if validName.MatchString(name) {
			t.Errorf("%q should be invalid", name)
		}
	}
}
