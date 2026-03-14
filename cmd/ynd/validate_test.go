package main

import (
	"path/filepath"
	"strings"
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

func TestCmdValidate_Dir(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	persona := filepath.Join(dir, "my-persona")
	mkdirAll(t, filepath.Join(persona, ".claude-plugin"))
	writeFile(t, filepath.Join(persona, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"my-persona","version":"0.1.0"}`))

	err := cmdValidate([]string{dir})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestCmdValidate_NoPersonas(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	err := cmdValidate(nil)
	if err != nil {
		t.Errorf("expected no error for empty dir, got %v", err)
	}
}

func TestCmdValidate_InsidePersona(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	mkdirAll(t, filepath.Join(dir, ".claude-plugin"))
	writeFile(t, filepath.Join(dir, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"self","version":"0.1.0"}`))

	err := cmdValidate(nil)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestCmdValidate_SingleFile_PluginJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugin.json")
	writeFile(t, path, []byte(`{"name":"test","version":"0.1.0"}`))

	err := cmdValidate([]string{path})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestCmdValidate_SingleFile_MetadataJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "metadata.json")
	writeFile(t, path, []byte(`{"other":"data"}`))

	err := cmdValidate([]string{path})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestCmdValidate_SingleFile_Markdown(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "readme.md")
	writeFile(t, path, []byte("# Hello\n\nClean file.\n"))

	err := cmdValidate([]string{path})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestCmdValidate_SingleFile_Unknown(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.go")
	writeFile(t, path, []byte("package main"))

	err := cmdValidate([]string{path})
	if err == nil {
		t.Fatal("expected error for unknown file type")
	}
	if !strings.Contains(err.Error(), "don't know how to validate") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCmdValidate_SingleFile_WithIssues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugin.json")
	writeFile(t, path, []byte(`{}`))

	err := cmdValidate([]string{path})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestCmdValidate_Nonexistent(t *testing.T) {
	err := cmdValidate([]string{"/nonexistent/path"})
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
}

func TestCmdValidate_MultiplePersonas(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	for _, name := range []string{"good", "bad"} {
		persona := filepath.Join(dir, name)
		mkdirAll(t, filepath.Join(persona, ".claude-plugin"))
		writeFile(t, filepath.Join(persona, ".claude-plugin", "plugin.json"),
			[]byte(`{"name":"`+name+`","version":"0.1.0"}`))
	}

	// Make "bad" persona invalid by adding a skill dir without SKILL.md
	mkdirAll(t, filepath.Join(dir, "bad", "skills", "orphan"))

	err := cmdValidate(nil)
	if err == nil {
		t.Fatal("expected validation error for bad persona")
	}
}

func TestValidateFile_PluginJSON_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugin.json")
	writeFile(t, path, []byte(`{"name":"test","version":"0.1.0"}`))

	err := validateFile(path)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidateFile_PluginJSON_Invalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugin.json")
	writeFile(t, path, []byte(`{}`))

	err := validateFile(path)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateFile_MetadataJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "metadata.json")
	writeFile(t, path, []byte(`{"other":"data"}`))

	err := validateFile(path)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidateFile_Markdown_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	writeFile(t, path, []byte("# Clean\n"))

	err := validateFile(path)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidateFile_Markdown_Invalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	writeFile(t, path, []byte("trailing spaces   "))

	err := validateFile(path)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateFile_Unknown(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.go")
	writeFile(t, path, []byte("package main"))

	err := validateFile(path)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "don't know how to validate") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidatePersona_InvalidPluginJSON(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	persona := filepath.Join(dir, "bad")
	mkdirAll(t, filepath.Join(persona, ".claude-plugin"))
	writeFile(t, filepath.Join(persona, ".claude-plugin", "plugin.json"), []byte(`{not json}`))

	err := validatePersona(persona)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidatePersona_InvalidMetadataJSON(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	persona := filepath.Join(dir, "bad")
	mkdirAll(t, filepath.Join(persona, ".claude-plugin"))
	writeFile(t, filepath.Join(persona, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"bad","version":"0.1.0"}`))
	writeFile(t, filepath.Join(persona, "metadata.json"), []byte(`{not json}`))

	err := validatePersona(persona)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidatePersona_NonMarkdownInArtifactDir(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	persona := filepath.Join(dir, "bad")
	mkdirAll(t, filepath.Join(persona, ".claude-plugin"))
	mkdirAll(t, filepath.Join(persona, "agents"))
	writeFile(t, filepath.Join(persona, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"bad","version":"0.1.0"}`))
	writeFile(t, filepath.Join(persona, "agents", "stray.txt"), []byte("not markdown"))

	err := validatePersona(persona)
	if err == nil {
		t.Fatal("expected validation error for non-markdown file in agents/")
	}
}

func TestValidatePersona_AgentMissingFrontmatter(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	persona := filepath.Join(dir, "bad")
	mkdirAll(t, filepath.Join(persona, ".claude-plugin"))
	mkdirAll(t, filepath.Join(persona, "agents"))
	writeFile(t, filepath.Join(persona, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"bad","version":"0.1.0"}`))
	writeFile(t, filepath.Join(persona, "agents", "reviewer.md"), []byte("Just text.\n"))

	err := validatePersona(persona)
	if err == nil {
		t.Fatal("expected validation error for agent missing frontmatter")
	}
}

func TestValidatePersona_AgentNameMismatch(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	persona := filepath.Join(dir, "bad")
	mkdirAll(t, filepath.Join(persona, ".claude-plugin"))
	mkdirAll(t, filepath.Join(persona, "agents"))
	writeFile(t, filepath.Join(persona, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"bad","version":"0.1.0"}`))
	writeFile(t, filepath.Join(persona, "agents", "reviewer.md"),
		[]byte("---\nname: wrong\ndescription: Reviews code\ntools: Read\n---\n"))

	err := validatePersona(persona)
	if err == nil {
		t.Fatal("expected validation error for agent name mismatch")
	}
}

func TestValidatePersona_AgentMissingDescription(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	persona := filepath.Join(dir, "bad")
	mkdirAll(t, filepath.Join(persona, ".claude-plugin"))
	mkdirAll(t, filepath.Join(persona, "agents"))
	writeFile(t, filepath.Join(persona, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"bad","version":"0.1.0"}`))
	writeFile(t, filepath.Join(persona, "agents", "reviewer.md"),
		[]byte("---\nname: reviewer\ntools: Read\n---\n"))

	err := validatePersona(persona)
	if err == nil {
		t.Fatal("expected validation error for missing description")
	}
}

func TestValidatePersona_AgentWithDescriptionButNoTools(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	persona := filepath.Join(dir, "bad")
	mkdirAll(t, filepath.Join(persona, ".claude-plugin"))
	mkdirAll(t, filepath.Join(persona, "agents"))
	writeFile(t, filepath.Join(persona, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"bad","version":"0.1.0"}`))
	writeFile(t, filepath.Join(persona, "agents", "reviewer.md"),
		[]byte("---\nname: reviewer\ndescription: Reviews\n---\n"))

	err := validatePersona(persona)
	if err == nil {
		t.Fatal("expected validation error for missing tools")
	}
}

func TestValidatePersona_SkillMissingFrontmatter(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	persona := filepath.Join(dir, "bad")
	mkdirAll(t, filepath.Join(persona, ".claude-plugin"))
	mkdirAll(t, filepath.Join(persona, "skills", "hello"))
	writeFile(t, filepath.Join(persona, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"bad","version":"0.1.0"}`))
	writeFile(t, filepath.Join(persona, "skills", "hello", "SKILL.md"), []byte("No frontmatter.\n"))

	err := validatePersona(persona)
	if err == nil {
		t.Fatal("expected validation error for skill missing frontmatter")
	}
}

func TestValidatePersona_SkillMissingName(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	persona := filepath.Join(dir, "bad")
	mkdirAll(t, filepath.Join(persona, ".claude-plugin"))
	mkdirAll(t, filepath.Join(persona, "skills", "hello"))
	writeFile(t, filepath.Join(persona, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"bad","version":"0.1.0"}`))
	writeFile(t, filepath.Join(persona, "skills", "hello", "SKILL.md"),
		[]byte("---\ndescription: A skill\n---\n"))

	err := validatePersona(persona)
	if err == nil {
		t.Fatal("expected validation error for skill missing name")
	}
}

func TestValidatePersona_SkillMissingDescription(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	persona := filepath.Join(dir, "bad")
	mkdirAll(t, filepath.Join(persona, ".claude-plugin"))
	mkdirAll(t, filepath.Join(persona, "skills", "hello"))
	writeFile(t, filepath.Join(persona, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"bad","version":"0.1.0"}`))
	writeFile(t, filepath.Join(persona, "skills", "hello", "SKILL.md"),
		[]byte("---\nname: hello\n---\n"))

	err := validatePersona(persona)
	if err == nil {
		t.Fatal("expected validation error for skill missing description")
	}
}

func TestValidatePersona_PluginJSONMissingName(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	persona := filepath.Join(dir, "bad")
	mkdirAll(t, filepath.Join(persona, ".claude-plugin"))
	writeFile(t, filepath.Join(persona, ".claude-plugin", "plugin.json"),
		[]byte(`{"version":"0.1.0"}`))

	err := validatePersona(persona)
	if err == nil {
		t.Fatal("expected validation error for missing name in plugin.json")
	}
}

func TestValidatePersona_PluginJSONMissingVersion(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	persona := filepath.Join(dir, "bad")
	mkdirAll(t, filepath.Join(persona, ".claude-plugin"))
	writeFile(t, filepath.Join(persona, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"bad"}`))

	err := validatePersona(persona)
	if err == nil {
		t.Fatal("expected validation error for missing version in plugin.json")
	}
}

func TestValidatePersona_NonMarkdownInRules(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	persona := filepath.Join(dir, "bad")
	mkdirAll(t, filepath.Join(persona, ".claude-plugin"))
	mkdirAll(t, filepath.Join(persona, "rules"))
	writeFile(t, filepath.Join(persona, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"bad","version":"0.1.0"}`))
	writeFile(t, filepath.Join(persona, "rules", "stray.txt"), []byte("not markdown"))

	err := validatePersona(persona)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidatePersona_NonMarkdownInCommands(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	persona := filepath.Join(dir, "bad")
	mkdirAll(t, filepath.Join(persona, ".claude-plugin"))
	mkdirAll(t, filepath.Join(persona, "commands"))
	writeFile(t, filepath.Join(persona, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"bad","version":"0.1.0"}`))
	writeFile(t, filepath.Join(persona, "commands", "stray.py"), []byte("not markdown"))

	err := validatePersona(persona)
	if err == nil {
		t.Fatal("expected validation error")
	}
}
