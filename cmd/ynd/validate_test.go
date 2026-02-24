package main

import (
	"path/filepath"
	"testing"
)

func TestValidatePersona_Valid(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	persona := filepath.Join(dir, "test-persona")
	mkdirAll(t, filepath.Join(persona, ".claude-plugin"))
	mkdirAll(t, filepath.Join(persona, "skills", "hello"))
	mkdirAll(t, filepath.Join(persona, "agents"))

	writeFile(t, filepath.Join(persona, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"test-persona","version":"0.1.0"}`))
	writeFile(t, filepath.Join(persona, "skills", "hello", "SKILL.md"),
		[]byte("---\nname: hello\ndescription: Say hello\n---\n\nHello skill.\n"))
	writeFile(t, filepath.Join(persona, "agents", "reviewer.md"),
		[]byte("---\nname: reviewer\ndescription: Reviews code\ntools: Read, Grep\n---\n\nBody.\n"))

	if err := validatePersona(persona); err != nil {
		t.Errorf("validatePersona failed: %v", err)
	}
}

func TestValidatePersona_MissingPluginJSON(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	persona := filepath.Join(dir, "bad-persona")
	mkdirAll(t, persona)

	err := validatePersona(persona)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidatePersona_MissingSkillMD(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	persona := filepath.Join(dir, "bad-persona")
	mkdirAll(t, filepath.Join(persona, ".claude-plugin"))
	mkdirAll(t, filepath.Join(persona, "skills", "empty-skill"))
	writeFile(t, filepath.Join(persona, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"bad-persona","version":"0.1.0"}`))

	err := validatePersona(persona)
	if err == nil {
		t.Fatal("expected validation error for missing SKILL.md")
	}
}

func TestValidatePersona_SkillNameMismatch(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	persona := filepath.Join(dir, "test-persona")
	mkdirAll(t, filepath.Join(persona, ".claude-plugin"))
	mkdirAll(t, filepath.Join(persona, "skills", "hello"))

	writeFile(t, filepath.Join(persona, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"test-persona","version":"0.1.0"}`))
	writeFile(t, filepath.Join(persona, "skills", "hello", "SKILL.md"),
		[]byte("---\nname: wrong-name\ndescription: Say hello\n---\n"))

	err := validatePersona(persona)
	if err == nil {
		t.Fatal("expected validation error for name mismatch")
	}
}

func TestValidatePersona_AgentMissingTools(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	persona := filepath.Join(dir, "test-persona")
	mkdirAll(t, filepath.Join(persona, ".claude-plugin"))
	mkdirAll(t, filepath.Join(persona, "agents"))

	writeFile(t, filepath.Join(persona, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"test-persona","version":"0.1.0"}`))
	writeFile(t, filepath.Join(persona, "agents", "reviewer.md"),
		[]byte("---\nname: reviewer\ndescription: Reviews code\n---\n"))

	err := validatePersona(persona)
	if err == nil {
		t.Fatal("expected validation error for missing tools field")
	}
}

func TestFindPersonaRoots(t *testing.T) {
	dir := t.TempDir()

	for _, name := range []string{"p1", "p2"} {
		pluginDir := filepath.Join(dir, name, ".claude-plugin")
		mkdirAll(t, pluginDir)
		writeFile(t, filepath.Join(pluginDir, "plugin.json"),
			[]byte(`{"name":"`+name+`","version":"0.1.0"}`))
	}

	mkdirAll(t, filepath.Join(dir, "not-a-persona"))

	roots := findPersonaRoots(dir)
	if len(roots) != 2 {
		t.Errorf("found %d persona roots, want 2", len(roots))
	}
}

func TestFindPersonaRoots_SelfIsPersona(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, ".claude-plugin")
	mkdirAll(t, pluginDir)
	writeFile(t, filepath.Join(pluginDir, "plugin.json"), []byte(`{}`))

	roots := findPersonaRoots(dir)
	if len(roots) != 1 {
		t.Errorf("found %d persona roots, want 1", len(roots))
	}
}

func TestIsPersonaRoot(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, ".claude-plugin")
	mkdirAll(t, pluginDir)
	writeFile(t, filepath.Join(pluginDir, "plugin.json"), []byte(`{}`))

	if !isPersonaRoot(dir) {
		t.Error("expected persona root")
	}

	notPersona := t.TempDir()
	if isPersonaRoot(notPersona) {
		t.Error("expected non-persona root")
	}
}
