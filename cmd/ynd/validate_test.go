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
	mkdirAll(t, filepath.Join(hr, "skills", "hello"))
	mkdirAll(t, filepath.Join(hr, "agents"))

	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"test-harness","version":"0.1.0"}`))
	writeFile(t, filepath.Join(hr, "skills", "hello", "SKILL.md"),
		[]byte("---\nname: hello\ndescription: Say hello\n---\n\nHello skill.\n"))
	writeFile(t, filepath.Join(hr, "agents", "reviewer.md"),
		[]byte("---\nname: reviewer\ndescription: Reviews code\ntools: Read, Grep\n---\n\nBody.\n"))

	if err := validateHarness(hr); err != nil {
		t.Errorf("validateHarness failed: %v", err)
	}
}

func TestValidateHarness_MissingHarnessJSON(t *testing.T) {
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
	mkdirAll(t, filepath.Join(hr, "skills", "empty-skill"))
	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"bad-harness","version":"0.1.0"}`))

	err := validateHarness(hr)
	if err == nil {
		t.Fatal("expected validation error for missing SKILL.md")
	}
}

func TestValidateHarness_SkillNameMismatch(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "test-harness")
	mkdirAll(t, filepath.Join(hr, "skills", "hello"))

	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"test-harness","version":"0.1.0"}`))
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
	mkdirAll(t, filepath.Join(hr, "agents"))

	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"test-harness","version":"0.1.0"}`))
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
		mkdirAll(t, filepath.Join(dir, name))
		writeFile(t, filepath.Join(dir, name, ".ynh-plugin", "plugin.json"),
			[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"`+name+`","version":"0.1.0"}`))
	}

	mkdirAll(t, filepath.Join(dir, "not-a-harness"))

	roots := findHarnessRoots(dir)
	if len(roots) != 2 {
		t.Errorf("found %d harness roots, want 2", len(roots))
	}
}

func TestFindHarnessRoots_SelfIsHarness(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"self","version":"0.1.0"}`))

	roots := findHarnessRoots(dir)
	if len(roots) != 1 {
		t.Errorf("found %d harness roots, want 1", len(roots))
	}
}

func TestIsHarnessRoot(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"test","version":"0.1.0"}`))

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
	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"my-harness","version":"0.1.0"}`))

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

	writeFile(t, filepath.Join(dir, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"self","version":"0.1.0"}`))

	err := cmdValidate(nil)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestCmdValidate_SingleFile_PluginJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".ynh-plugin", "plugin.json")
	writeFile(t, path, []byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"test","version":"0.1.0"}`))

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

func TestValidateMarketplaceConfig_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "marketplace.json")
	writeFile(t, path, []byte(`{
		"name": "test-marketplace",
		"owner": {"name": "tester"},
		"harnesses": [{"type": "plugin", "source": "./plugins/my-plugin"}]
	}`))

	if err := cmdValidate([]string{path}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateMarketplaceConfig_InvalidRemoteSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "marketplace.json")
	writeFile(t, path, []byte(`{
		"name": "test-marketplace",
		"owner": {"name": "tester"},
		"harnesses": [{"type": "plugin", "source": "github.com/user"}]
	}`))

	err := cmdValidate([]string{path})
	if err == nil {
		t.Fatal("expected validation error for bad remote source")
	}
	if !strings.Contains(err.Error(), "validation failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCmdValidate_SingleFile_WithIssues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".ynh-plugin", "plugin.json")
	writeFile(t, path, []byte(`{}`))

	err := cmdValidate([]string{path})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestCmdValidate_HarnessFlag(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"flag-test","version":"0.1.0"}`))

	err := cmdValidate([]string{"--harness", dir})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestCmdValidate_HarnessEnvVar(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"env-test","version":"0.1.0"}`))

	t.Setenv("YNH_HARNESS", dir)

	err := cmdValidate(nil)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestCmdValidate_HarnessFlagOverridesEnv(t *testing.T) {
	goodDir := t.TempDir()
	writeFile(t, filepath.Join(goodDir, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"good","version":"0.1.0"}`))

	t.Setenv("YNH_HARNESS", "/nonexistent/path")

	err := cmdValidate([]string{"--harness", goodDir})
	if err != nil {
		t.Errorf("--harness flag should override YNH_HARNESS: %v", err)
	}
}

func TestCmdValidate_UnknownFlag(t *testing.T) {
	err := cmdValidate([]string{"--bogus"})
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
	if !strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("unexpected error: %v", err)
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
		writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
			[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"`+name+`","version":"0.1.0"}`))
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
	path := filepath.Join(dir, ".ynh-plugin", "plugin.json")
	writeFile(t, path, []byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"test","version":"0.1.0"}`))

	err := validateFile(path)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidateFile_PluginJSON_Invalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".ynh-plugin", "plugin.json")
	writeFile(t, path, []byte(`{}`))

	err := validateFile(path)
	if err == nil {
		t.Fatal("expected validation error")
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

func TestValidateHarness_InvalidHarnessJSON(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "bad")
	mkdirAll(t, hr)
	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"), []byte(`{not json}`))

	err := validateHarness(hr)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateHarness_NonMarkdownInArtifactDir(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "bad")
	mkdirAll(t, filepath.Join(hr, "agents"))
	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"bad","version":"0.1.0"}`))
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
	mkdirAll(t, filepath.Join(hr, "agents"))
	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"bad","version":"0.1.0"}`))
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
	mkdirAll(t, filepath.Join(hr, "agents"))
	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"bad","version":"0.1.0"}`))
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
	mkdirAll(t, filepath.Join(hr, "agents"))
	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"bad","version":"0.1.0"}`))
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
	mkdirAll(t, filepath.Join(hr, "agents"))
	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"bad","version":"0.1.0"}`))
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
	mkdirAll(t, filepath.Join(hr, "skills", "hello"))
	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"bad","version":"0.1.0"}`))
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
	mkdirAll(t, filepath.Join(hr, "skills", "hello"))
	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"bad","version":"0.1.0"}`))
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
	mkdirAll(t, filepath.Join(hr, "skills", "hello"))
	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"bad","version":"0.1.0"}`))
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
	mkdirAll(t, hr)
	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
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
	mkdirAll(t, hr)
	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
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
	mkdirAll(t, filepath.Join(hr, "rules"))
	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"bad","version":"0.1.0"}`))
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
	mkdirAll(t, filepath.Join(hr, "commands"))
	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"bad","version":"0.1.0"}`))
	writeFile(t, filepath.Join(hr, "commands", "stray.py"), []byte("not markdown"))

	err := validateHarness(hr)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateHarness_ConflictingInstructions(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "conflict")
	mkdirAll(t, hr)
	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"conflict","version":"0.1.0"}`))
	writeFile(t, filepath.Join(hr, "instructions.md"), []byte("one thing"))
	writeFile(t, filepath.Join(hr, "AGENTS.md"), []byte("another thing"))

	err := validateHarness(hr)
	if err == nil {
		t.Fatal("expected validation error for conflicting instructions files")
	}
	if !strings.Contains(err.Error(), "issue") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateHarness_IdenticalInstructionsOK(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "ok")
	mkdirAll(t, hr)
	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"ok","version":"0.1.0"}`))
	writeFile(t, filepath.Join(hr, "instructions.md"), []byte("same content"))
	writeFile(t, filepath.Join(hr, "AGENTS.md"), []byte("same content"))

	if err := validateHarness(hr); err != nil {
		t.Errorf("identical instructions should pass: %v", err)
	}
}

func TestValidateHarness_HooksValid(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "hooks-valid")
	mkdirAll(t, hr)
	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"hooks-valid","version":"0.1.0","hooks":{"before_tool":[{"command":"echo hi"}]}}`))

	if err := validateHarness(hr); err != nil {
		t.Errorf("valid hooks should pass: %v", err)
	}
}

func TestValidateHarness_HooksUnknownEvent(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "hooks-bad-event")
	mkdirAll(t, hr)
	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"hooks-bad-event","version":"0.1.0","hooks":{"unknown_event":[{"command":"echo hi"}]}}`))

	err := validateHarness(hr)
	if err == nil {
		t.Fatal("expected validation error for unknown hook event")
	}
}

func TestValidateHarness_HooksEmptyCommand(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "hooks-empty-cmd")
	mkdirAll(t, hr)
	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"hooks-empty-cmd","version":"0.1.0","hooks":{"before_tool":[{"command":""}]}}`))

	err := validateHarness(hr)
	if err == nil {
		t.Fatal("expected validation error for empty hook command")
	}
}

func TestValidateHarness_MCPServersValid(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "mcp-valid")
	mkdirAll(t, hr)
	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"mcp-valid","version":"0.1.0","mcp_servers":{"github":{"command":"npx","args":["-y","server"]}}}`))

	if err := validateHarness(hr); err != nil {
		t.Errorf("valid MCP servers should pass: %v", err)
	}
}

func TestValidateHarness_MCPServersURLOnly(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "mcp-url")
	mkdirAll(t, hr)
	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"mcp-url","version":"0.1.0","mcp_servers":{"api":{"url":"https://api.example.com/mcp"}}}`))

	if err := validateHarness(hr); err != nil {
		t.Errorf("URL-only MCP server should pass: %v", err)
	}
}

func TestValidateHarness_MCPServersNeitherCommandNorURL(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "mcp-neither")
	mkdirAll(t, hr)
	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"mcp-neither","version":"0.1.0","mcp_servers":{"bad":{}}}`))

	err := validateHarness(hr)
	if err == nil {
		t.Fatal("expected validation error for MCP server with neither command nor url")
	}
}

func TestValidateHarness_MCPServersBothCommandAndURL(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "mcp-both")
	mkdirAll(t, hr)
	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"mcp-both","version":"0.1.0","mcp_servers":{"bad":{"command":"npx","url":"https://example.com"}}}`))

	err := validateHarness(hr)
	if err == nil {
		t.Fatal("expected validation error for MCP server with both command and url")
	}
}

func TestValidateHarness_MCPServersNotObject(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "mcp-bad-type")
	mkdirAll(t, hr)
	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"mcp-bad-type","version":"0.1.0","mcp_servers":"not-an-object"}`))

	err := validateHarness(hr)
	if err == nil {
		t.Fatal("expected validation error for mcp_servers not being an object")
	}
}

func TestValidateHarness_ProfilesValid(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "prof-valid")
	mkdirAll(t, hr)
	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"prof-valid","version":"0.1.0","profiles":{"ci":{"hooks":{"before_tool":[{"command":"echo ci"}]}}}}`))

	if err := validateHarness(hr); err != nil {
		t.Errorf("valid profiles should pass: %v", err)
	}
}

func TestValidateHarness_ProfilesInvalidHookEvent(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "prof-bad")
	mkdirAll(t, hr)
	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"prof-bad","version":"0.1.0","profiles":{"ci":{"hooks":{"bad_event":[{"command":"echo"}]}}}}`))

	err := validateHarness(hr)
	if err == nil {
		t.Fatal("expected validation error for invalid hook event in profile")
	}
}

func TestValidateHarness_ProfilesMCPServerInvalid(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "prof-mcp-bad")
	mkdirAll(t, hr)
	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"prof-mcp-bad","version":"0.1.0","profiles":{"ci":{"mcp_servers":{"bad":{}}}}}`))

	err := validateHarness(hr)
	if err == nil {
		t.Fatal("expected validation error for MCP server in profile with no command/url")
	}
}

func TestValidateHarness_ProfileNullMCPServerValid(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "null-mcp")
	mkdirAll(t, hr)
	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"null-mcp","version":"0.1.0","mcp_servers":{"pg":{"command":"pg-mcp"}},"profiles":{"ci":{"mcp_servers":{"pg":null}}}}`))

	if err := validateHarness(hr); err != nil {
		t.Errorf("null MCP server in profile should pass validation: %v", err)
	}
}

func TestValidateHarness_FocusValid(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "focus-valid")
	mkdirAll(t, hr)
	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"focus-valid","version":"0.1.0","profiles":{"ci":{}},"focus":{"review":{"profile":"ci","prompt":"Review code"}}}`))

	if err := validateHarness(hr); err != nil {
		t.Errorf("valid focus should pass: %v", err)
	}
}

func TestValidateHarness_FocusMissingPrompt(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "focus-no-prompt")
	mkdirAll(t, hr)
	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"focus-no-prompt","version":"0.1.0","focus":{"review":{"profile":"ci"}}}`))

	err := validateHarness(hr)
	if err == nil {
		t.Fatal("expected validation error for focus with missing prompt")
	}
}

func TestValidateHarness_FocusUnknownProfile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "focus-bad-profile")
	mkdirAll(t, hr)
	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"focus-bad-profile","version":"0.1.0","focus":{"review":{"profile":"nonexistent","prompt":"Review code"}}}`))

	err := validateHarness(hr)
	if err == nil {
		t.Fatal("expected validation error for focus referencing unknown profile")
	}
}

func TestValidateHarness_FocusNoProfile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "focus-no-profile")
	mkdirAll(t, hr)
	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"focus-no-profile","version":"0.1.0","focus":{"docs":{"prompt":"Generate docs"}}}`))

	if err := validateHarness(hr); err != nil {
		t.Errorf("focus without profile ref should pass: %v", err)
	}
}

func TestValidateHarness_AgentsMDOnly(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "agents-only")
	mkdirAll(t, hr)
	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"agents-only","version":"0.1.0"}`))
	writeFile(t, filepath.Join(hr, "AGENTS.md"), []byte("just agents"))

	if err := validateHarness(hr); err != nil {
		t.Errorf("AGENTS.md-only harness should be valid: %v", err)
	}
}

// --- Schema validation tests ---

func schemaFixture(t *testing.T, name string) string {
	t.Helper()
	base := filepath.Join("..", "..", "testdata", "schema-fixtures")
	return filepath.Join(base, name)
}

func TestSchemaPlugin_ValidFixtures(t *testing.T) {
	for _, name := range []string{"valid/plugin.json", "valid/plugin-with-includes.json"} {
		t.Run(name, func(t *testing.T) {
			issues := lintHarnessJSON(schemaFixture(t, name))
			if len(issues) != 0 {
				t.Errorf("expected no issues, got: %v", issues)
			}
		})
	}
}

func TestSchemaPlugin_MissingName(t *testing.T) {
	issues := lintHarnessJSON(schemaFixture(t, "invalid/plugin-missing-name.json"))
	if len(issues) == 0 {
		t.Fatal("expected schema error for missing name")
	}
	if !strings.Contains(issues[0].Message, "name") {
		t.Errorf("expected 'name' in error, got: %s", issues[0].Message)
	}
}

func TestSchemaPlugin_MissingVersion(t *testing.T) {
	issues := lintHarnessJSON(schemaFixture(t, "invalid/plugin-missing-version.json"))
	if len(issues) == 0 {
		t.Fatal("expected schema error for missing version")
	}
	if !strings.Contains(issues[0].Message, "version") {
		t.Errorf("expected 'version' in error, got: %s", issues[0].Message)
	}
}

func TestSchemaPlugin_UnknownField(t *testing.T) {
	issues := lintHarnessJSON(schemaFixture(t, "invalid/plugin-unknown-field.json"))
	if len(issues) == 0 {
		t.Fatal("expected schema error for unknown field")
	}
	found := false
	for _, i := range issues {
		if strings.Contains(i.Message, "additional properties") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected additionalProperties error, got: %v", issues)
	}
}

func TestSchemaPlugin_BadVendor(t *testing.T) {
	issues := lintHarnessJSON(schemaFixture(t, "invalid/plugin-bad-vendor.json"))
	if len(issues) == 0 {
		t.Fatal("expected schema error for invalid default_vendor")
	}
	found := false
	for _, i := range issues {
		if strings.Contains(i.Message, "default_vendor") || strings.Contains(i.Message, "one of") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected enum error for default_vendor, got: %v", issues)
	}
}

func TestSchemaMarketplace_ValidFixture(t *testing.T) {
	issues := lintRegistryMarketplace(schemaFixture(t, "valid/marketplace.json"))
	if len(issues) != 0 {
		t.Errorf("expected no issues, got: %v", issues)
	}
}

func TestSchemaMarketplace_OldEntriesKey(t *testing.T) {
	issues := lintRegistryMarketplace(schemaFixture(t, "invalid/marketplace-old-entries.json"))
	if len(issues) == 0 {
		t.Fatal("expected schema error for old 'entries' key")
	}
	found := false
	for _, i := range issues {
		if strings.Contains(i.Message, "harnesses") || strings.Contains(i.Message, "entries") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected harnesses/entries error, got: %v", issues)
	}
}

func TestSchemaMarketplace_MissingOwner(t *testing.T) {
	issues := lintRegistryMarketplace(schemaFixture(t, "invalid/marketplace-missing-owner.json"))
	if len(issues) == 0 {
		t.Fatal("expected schema error for missing owner")
	}
	if !strings.Contains(issues[0].Message, "owner") {
		t.Errorf("expected 'owner' in error, got: %s", issues[0].Message)
	}
}

func TestSchemaMarketplace_SourceNoDotSlash(t *testing.T) {
	issues := lintRegistryMarketplace(schemaFixture(t, "invalid/marketplace-source-no-dotslash.json"))
	if len(issues) == 0 {
		t.Fatal("expected schema error for source without ./")
	}
}

func TestSchemaPlugin_WrongSchemaURL(t *testing.T) {
	issues := lintHarnessJSON(schemaFixture(t, "invalid/plugin-wrong-schema.json"))
	if len(issues) == 0 {
		t.Fatal("expected schema error for wrong $schema URL")
	}
	found := false
	for _, i := range issues {
		if strings.Contains(i.Message, "$schema") || strings.Contains(i.Message, "pattern") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected $schema pattern error, got: %v", issues)
	}
}

func TestSchemaMarketplace_MissingSchemaField(t *testing.T) {
	issues := lintRegistryMarketplace(schemaFixture(t, "invalid/marketplace-missing-schema.json"))
	if len(issues) == 0 {
		t.Fatal("expected schema error for missing $schema field")
	}
	found := false
	for _, i := range issues {
		if strings.Contains(i.Message, "$schema") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected $schema error, got: %v", issues)
	}
}

func TestValidateFile_RegistryMarketplace_Valid(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, ".ynh-plugin")
	path := filepath.Join(pluginDir, "marketplace.json")
	writeFile(t, path, []byte(`{
		"$schema": "https://eyelock.github.io/ynh/schema/marketplace.schema.json",
		"name": "test-registry",
		"owner": {"name": "tester"},
		"harnesses": [{"name": "test", "source": "./harnesses/test"}]
	}`))
	if err := cmdValidate([]string{path}); err != nil {
		t.Errorf("expected valid, got: %v", err)
	}
}

func TestValidateFile_RegistryMarketplace_OldEntries(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, ".ynh-plugin")
	path := filepath.Join(pluginDir, "marketplace.json")
	writeFile(t, path, []byte(`{
		"name": "test-registry",
		"owner": {"name": "tester"},
		"entries": [{"type": "harness", "source": "./harnesses/test"}]
	}`))
	if err := cmdValidate([]string{path}); err == nil {
		t.Error("expected error for old 'entries' key in registry marketplace.json")
	}
}

func TestValidateDir_ValidatesRootMarketplace(t *testing.T) {
	dir := t.TempDir()
	// Create a valid harness sub-directory
	hr := filepath.Join(dir, "my-harness")
	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"my-harness","version":"0.1.0"}`))
	// Create an invalid root marketplace.json (missing $schema)
	writeFile(t, filepath.Join(dir, ".ynh-plugin", "marketplace.json"),
		[]byte(`{"name":"my-registry","owner":{"name":"me"},"harnesses":[{"name":"t","source":"./t"}]}`))

	if err := cmdValidate([]string{dir}); err == nil {
		t.Error("expected error: root marketplace.json is missing $schema")
	}
}

func TestValidateDir_ValidRootMarketplace(t *testing.T) {
	dir := t.TempDir()
	// Create a valid harness sub-directory
	hr := filepath.Join(dir, "my-harness")
	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"my-harness","version":"0.1.0"}`))
	// Create a valid root marketplace.json
	writeFile(t, filepath.Join(dir, ".ynh-plugin", "marketplace.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/marketplace.schema.json","name":"my-registry","owner":{"name":"me"},"harnesses":[{"name":"t","source":"./t"}]}`))

	if err := cmdValidate([]string{dir}); err != nil {
		t.Errorf("expected valid, got: %v", err)
	}
}

func TestValidateDir_NoRootMarketplace(t *testing.T) {
	dir := t.TempDir()
	// Directory with harness but no root marketplace.json — should still pass
	hr := filepath.Join(dir, "my-harness")
	writeFile(t, filepath.Join(hr, ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"my-harness","version":"0.1.0"}`))

	if err := cmdValidate([]string{dir}); err != nil {
		t.Errorf("expected valid (no marketplace.json is fine), got: %v", err)
	}
}
