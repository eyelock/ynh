package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateHarness_Valid(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "test-harness")
	mkdirAll(t, filepath.Join(hr, ".claude-plugin"))
	mkdirAll(t, filepath.Join(hr, "skills", "hello"))
	mkdirAll(t, filepath.Join(hr, "agents"))

	writeFile(t, filepath.Join(hr, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"test-harness","version":"0.1.0"}`))
	writeFile(t, filepath.Join(hr, "skills", "hello", "SKILL.md"),
		[]byte("---\nname: hello\ndescription: Say hello\n---\n\nHello skill.\n"))
	writeFile(t, filepath.Join(hr, "agents", "reviewer.md"),
		[]byte("---\nname: reviewer\ndescription: Reviews code\ntools: Read, Grep\n---\n\nBody.\n"))

	if err := validateHarness(hr); err != nil {
		t.Errorf("validateHarness failed: %v", err)
	}
}

func TestValidateHarness_MissingPluginJSON(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "bad-harness")
	mkdirAll(t, hr)

	err := validateHarness(hr)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateHarness_MissingSkillMD(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "bad-harness")
	mkdirAll(t, filepath.Join(hr, ".claude-plugin"))
	mkdirAll(t, filepath.Join(hr, "skills", "empty-skill"))
	writeFile(t, filepath.Join(hr, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"bad-harness","version":"0.1.0"}`))

	err := validateHarness(hr)
	if err == nil {
		t.Fatal("expected validation error for missing SKILL.md")
	}
}

func TestValidateHarness_SkillNameMismatch(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "test-harness")
	mkdirAll(t, filepath.Join(hr, ".claude-plugin"))
	mkdirAll(t, filepath.Join(hr, "skills", "hello"))

	writeFile(t, filepath.Join(hr, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"test-harness","version":"0.1.0"}`))
	writeFile(t, filepath.Join(hr, "skills", "hello", "SKILL.md"),
		[]byte("---\nname: wrong-name\ndescription: Say hello\n---\n"))

	err := validateHarness(hr)
	if err == nil {
		t.Fatal("expected validation error for name mismatch")
	}
}

func TestValidateHarness_AgentMissingTools(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "test-harness")
	mkdirAll(t, filepath.Join(hr, ".claude-plugin"))
	mkdirAll(t, filepath.Join(hr, "agents"))

	writeFile(t, filepath.Join(hr, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"test-harness","version":"0.1.0"}`))
	writeFile(t, filepath.Join(hr, "agents", "reviewer.md"),
		[]byte("---\nname: reviewer\ndescription: Reviews code\n---\n"))

	err := validateHarness(hr)
	if err == nil {
		t.Fatal("expected validation error for missing tools field")
	}
}

func TestFindHarnessRoots(t *testing.T) {
	dir := t.TempDir()

	for _, name := range []string{"p1", "p2"} {
		pluginDir := filepath.Join(dir, name, ".claude-plugin")
		mkdirAll(t, pluginDir)
		writeFile(t, filepath.Join(pluginDir, "plugin.json"),
			[]byte(`{"name":"`+name+`","version":"0.1.0"}`))
	}

	mkdirAll(t, filepath.Join(dir, "not-a-harness"))

	roots := findHarnessRoots(dir)
	if len(roots) != 2 {
		t.Errorf("found %d harness roots, want 2", len(roots))
	}
}

func TestFindHarnessRoots_SelfIsHarness(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, ".claude-plugin")
	mkdirAll(t, pluginDir)
	writeFile(t, filepath.Join(pluginDir, "plugin.json"), []byte(`{}`))

	roots := findHarnessRoots(dir)
	if len(roots) != 1 {
		t.Errorf("found %d harness roots, want 1", len(roots))
	}
}

func TestIsHarnessRoot(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, ".claude-plugin")
	mkdirAll(t, pluginDir)
	writeFile(t, filepath.Join(pluginDir, "plugin.json"), []byte(`{}`))

	if !isHarnessRoot(dir) {
		t.Error("expected harness root")
	}

	notHarness := t.TempDir()
	if isHarnessRoot(notHarness) {
		t.Error("expected non-harness root")
	}
}

func TestCmdValidate_Dir(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "my-harness")
	mkdirAll(t, filepath.Join(hr, ".claude-plugin"))
	writeFile(t, filepath.Join(hr, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"my-harness","version":"0.1.0"}`))

	err := cmdValidate([]string{dir})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestCmdValidate_NoHarnesses(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	err := cmdValidate(nil)
	if err != nil {
		t.Errorf("expected no error for empty dir, got %v", err)
	}
}

func TestCmdValidate_InsideHarness(t *testing.T) {
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

func TestCmdValidate_MultipleHarnesses(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	for _, name := range []string{"good", "bad"} {
		hr := filepath.Join(dir, name)
		mkdirAll(t, filepath.Join(hr, ".claude-plugin"))
		writeFile(t, filepath.Join(hr, ".claude-plugin", "plugin.json"),
			[]byte(`{"name":"`+name+`","version":"0.1.0"}`))
	}

	// Make "bad" harness invalid by adding a skill dir without SKILL.md
	mkdirAll(t, filepath.Join(dir, "bad", "skills", "orphan"))

	err := cmdValidate(nil)
	if err == nil {
		t.Fatal("expected validation error for bad harness")
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

func TestValidateHarness_InvalidPluginJSON(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "bad")
	mkdirAll(t, filepath.Join(hr, ".claude-plugin"))
	writeFile(t, filepath.Join(hr, ".claude-plugin", "plugin.json"), []byte(`{not json}`))

	err := validateHarness(hr)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateHarness_InvalidMetadataJSON(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "bad")
	mkdirAll(t, filepath.Join(hr, ".claude-plugin"))
	writeFile(t, filepath.Join(hr, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"bad","version":"0.1.0"}`))
	writeFile(t, filepath.Join(hr, "metadata.json"), []byte(`{not json}`))

	err := validateHarness(hr)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateHarness_NonMarkdownInArtifactDir(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "bad")
	mkdirAll(t, filepath.Join(hr, ".claude-plugin"))
	mkdirAll(t, filepath.Join(hr, "agents"))
	writeFile(t, filepath.Join(hr, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"bad","version":"0.1.0"}`))
	writeFile(t, filepath.Join(hr, "agents", "stray.txt"), []byte("not markdown"))

	err := validateHarness(hr)
	if err == nil {
		t.Fatal("expected validation error for non-markdown file in agents/")
	}
}

func TestValidateHarness_AgentMissingFrontmatter(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "bad")
	mkdirAll(t, filepath.Join(hr, ".claude-plugin"))
	mkdirAll(t, filepath.Join(hr, "agents"))
	writeFile(t, filepath.Join(hr, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"bad","version":"0.1.0"}`))
	writeFile(t, filepath.Join(hr, "agents", "reviewer.md"), []byte("Just text.\n"))

	err := validateHarness(hr)
	if err == nil {
		t.Fatal("expected validation error for agent missing frontmatter")
	}
}

func TestValidateHarness_AgentNameMismatch(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "bad")
	mkdirAll(t, filepath.Join(hr, ".claude-plugin"))
	mkdirAll(t, filepath.Join(hr, "agents"))
	writeFile(t, filepath.Join(hr, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"bad","version":"0.1.0"}`))
	writeFile(t, filepath.Join(hr, "agents", "reviewer.md"),
		[]byte("---\nname: wrong\ndescription: Reviews code\ntools: Read\n---\n"))

	err := validateHarness(hr)
	if err == nil {
		t.Fatal("expected validation error for agent name mismatch")
	}
}

func TestValidateHarness_AgentMissingDescription(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "bad")
	mkdirAll(t, filepath.Join(hr, ".claude-plugin"))
	mkdirAll(t, filepath.Join(hr, "agents"))
	writeFile(t, filepath.Join(hr, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"bad","version":"0.1.0"}`))
	writeFile(t, filepath.Join(hr, "agents", "reviewer.md"),
		[]byte("---\nname: reviewer\ntools: Read\n---\n"))

	err := validateHarness(hr)
	if err == nil {
		t.Fatal("expected validation error for missing description")
	}
}

func TestValidateHarness_AgentWithDescriptionButNoTools(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "bad")
	mkdirAll(t, filepath.Join(hr, ".claude-plugin"))
	mkdirAll(t, filepath.Join(hr, "agents"))
	writeFile(t, filepath.Join(hr, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"bad","version":"0.1.0"}`))
	writeFile(t, filepath.Join(hr, "agents", "reviewer.md"),
		[]byte("---\nname: reviewer\ndescription: Reviews\n---\n"))

	err := validateHarness(hr)
	if err == nil {
		t.Fatal("expected validation error for missing tools")
	}
}

func TestValidateHarness_SkillMissingFrontmatter(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "bad")
	mkdirAll(t, filepath.Join(hr, ".claude-plugin"))
	mkdirAll(t, filepath.Join(hr, "skills", "hello"))
	writeFile(t, filepath.Join(hr, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"bad","version":"0.1.0"}`))
	writeFile(t, filepath.Join(hr, "skills", "hello", "SKILL.md"), []byte("No frontmatter.\n"))

	err := validateHarness(hr)
	if err == nil {
		t.Fatal("expected validation error for skill missing frontmatter")
	}
}

func TestValidateHarness_SkillMissingName(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "bad")
	mkdirAll(t, filepath.Join(hr, ".claude-plugin"))
	mkdirAll(t, filepath.Join(hr, "skills", "hello"))
	writeFile(t, filepath.Join(hr, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"bad","version":"0.1.0"}`))
	writeFile(t, filepath.Join(hr, "skills", "hello", "SKILL.md"),
		[]byte("---\ndescription: A skill\n---\n"))

	err := validateHarness(hr)
	if err == nil {
		t.Fatal("expected validation error for skill missing name")
	}
}

func TestValidateHarness_SkillMissingDescription(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "bad")
	mkdirAll(t, filepath.Join(hr, ".claude-plugin"))
	mkdirAll(t, filepath.Join(hr, "skills", "hello"))
	writeFile(t, filepath.Join(hr, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"bad","version":"0.1.0"}`))
	writeFile(t, filepath.Join(hr, "skills", "hello", "SKILL.md"),
		[]byte("---\nname: hello\n---\n"))

	err := validateHarness(hr)
	if err == nil {
		t.Fatal("expected validation error for skill missing description")
	}
}

func TestValidateHarness_PluginJSONMissingName(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "bad")
	mkdirAll(t, filepath.Join(hr, ".claude-plugin"))
	writeFile(t, filepath.Join(hr, ".claude-plugin", "plugin.json"),
		[]byte(`{"version":"0.1.0"}`))

	err := validateHarness(hr)
	if err == nil {
		t.Fatal("expected validation error for missing name in plugin.json")
	}
}

func TestValidateHarness_PluginJSONMissingVersion(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "bad")
	mkdirAll(t, filepath.Join(hr, ".claude-plugin"))
	writeFile(t, filepath.Join(hr, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"bad"}`))

	err := validateHarness(hr)
	if err == nil {
		t.Fatal("expected validation error for missing version in plugin.json")
	}
}

func TestValidateHarness_NonMarkdownInRules(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "bad")
	mkdirAll(t, filepath.Join(hr, ".claude-plugin"))
	mkdirAll(t, filepath.Join(hr, "rules"))
	writeFile(t, filepath.Join(hr, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"bad","version":"0.1.0"}`))
	writeFile(t, filepath.Join(hr, "rules", "stray.txt"), []byte("not markdown"))

	err := validateHarness(hr)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateHarness_NonMarkdownInCommands(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "bad")
	mkdirAll(t, filepath.Join(hr, ".claude-plugin"))
	mkdirAll(t, filepath.Join(hr, "commands"))
	writeFile(t, filepath.Join(hr, ".claude-plugin", "plugin.json"),
		[]byte(`{"name":"bad","version":"0.1.0"}`))
	writeFile(t, filepath.Join(hr, "commands", "stray.py"), []byte("not markdown"))

	err := validateHarness(hr)
	if err == nil {
		t.Fatal("expected validation error")
	}
}
