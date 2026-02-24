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

func TestCreatePersona(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := createPersona("my-team"); err != nil {
		t.Fatalf("createPersona failed: %v", err)
	}

	expectedFiles := []string{
		"my-team/.claude-plugin/plugin.json",
		"my-team/metadata.json",
		"my-team/instructions.md",
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

	// Verify plugin.json content
	data, err := os.ReadFile(filepath.Join(dir, "my-team/.claude-plugin/plugin.json"))
	if err != nil {
		t.Fatalf("reading plugin.json: %v", err)
	}
	if !strings.Contains(string(data), `"name": "my-team"`) {
		t.Error("plugin.json missing name")
	}
	if !strings.Contains(string(data), `"version": "0.1.0"`) {
		t.Error("plugin.json missing version")
	}
}

func TestCreatePersona_AlreadyExists(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := createPersona("my-team"); err != nil {
		t.Fatalf("first createPersona failed: %v", err)
	}

	err := createPersona("my-team")
	if err == nil {
		t.Fatal("expected error for duplicate persona")
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
