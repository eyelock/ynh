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

	writeFile(t, filepath.Join(hr, "harness.json"),
		[]byte(`{"name":"test-harness","version":"0.1.0"}`))
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
	writeFile(t, filepath.Join(hr, "harness.json"),
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
	mkdirAll(t, filepath.Join(hr, "skills", "hello"))

	writeFile(t, filepath.Join(hr, "harness.json"),
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
	mkdirAll(t, filepath.Join(hr, "agents"))

	writeFile(t, filepath.Join(hr, "harness.json"),
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
		mkdirAll(t, filepath.Join(dir, name))
		writeFile(t, filepath.Join(dir, name, "harness.json"),
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
	writeFile(t, filepath.Join(dir, "harness.json"),
		[]byte(`{"name":"self","version":"0.1.0"}`))

	roots := findHarnessRoots(dir)
	if len(roots) != 1 {
		t.Errorf("found %d harness roots, want 1", len(roots))
	}
}

func TestIsHarnessRoot(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "harness.json"),
		[]byte(`{"name":"test","version":"0.1.0"}`))

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
	writeFile(t, filepath.Join(hr, "harness.json"),
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

	writeFile(t, filepath.Join(dir, "harness.json"),
		[]byte(`{"name":"self","version":"0.1.0"}`))

	err := cmdValidate(nil)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestCmdValidate_SingleFile_HarnessJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "harness.json")
	writeFile(t, path, []byte(`{"name":"test","version":"0.1.0"}`))

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
	path := filepath.Join(dir, "harness.json")
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
		writeFile(t, filepath.Join(hr, "harness.json"),
			[]byte(`{"name":"`+name+`","version":"0.1.0"}`))
	}

	// Make "bad" harness invalid by adding a skill dir without SKILL.md
	mkdirAll(t, filepath.Join(dir, "bad", "skills", "orphan"))

	err := cmdValidate(nil)
	if err == nil {
		t.Fatal("expected validation error for bad harness")
	}
}

func TestValidateFile_HarnessJSON_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "harness.json")
	writeFile(t, path, []byte(`{"name":"test","version":"0.1.0"}`))

	err := validateFile(path)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidateFile_HarnessJSON_Invalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "harness.json")
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
	writeFile(t, filepath.Join(hr, "harness.json"), []byte(`{not json}`))

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
	writeFile(t, filepath.Join(hr, "harness.json"),
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
	mkdirAll(t, filepath.Join(hr, "agents"))
	writeFile(t, filepath.Join(hr, "harness.json"),
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
	mkdirAll(t, filepath.Join(hr, "agents"))
	writeFile(t, filepath.Join(hr, "harness.json"),
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
	mkdirAll(t, filepath.Join(hr, "agents"))
	writeFile(t, filepath.Join(hr, "harness.json"),
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
	mkdirAll(t, filepath.Join(hr, "agents"))
	writeFile(t, filepath.Join(hr, "harness.json"),
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
	mkdirAll(t, filepath.Join(hr, "skills", "hello"))
	writeFile(t, filepath.Join(hr, "harness.json"),
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
	mkdirAll(t, filepath.Join(hr, "skills", "hello"))
	writeFile(t, filepath.Join(hr, "harness.json"),
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
	mkdirAll(t, filepath.Join(hr, "skills", "hello"))
	writeFile(t, filepath.Join(hr, "harness.json"),
		[]byte(`{"name":"bad","version":"0.1.0"}`))
	writeFile(t, filepath.Join(hr, "skills", "hello", "SKILL.md"),
		[]byte("---\nname: hello\n---\n"))

	err := validateHarness(hr)
	if err == nil {
		t.Fatal("expected validation error for skill missing description")
	}
}

func TestValidateHarness_HarnessJSONMissingName(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "bad")
	mkdirAll(t, hr)
	writeFile(t, filepath.Join(hr, "harness.json"),
		[]byte(`{"version":"0.1.0"}`))

	err := validateHarness(hr)
	if err == nil {
		t.Fatal("expected validation error for missing name in harness.json")
	}
}

func TestValidateHarness_HarnessJSONMissingVersion(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "bad")
	mkdirAll(t, hr)
	writeFile(t, filepath.Join(hr, "harness.json"),
		[]byte(`{"name":"bad"}`))

	err := validateHarness(hr)
	if err == nil {
		t.Fatal("expected validation error for missing version in harness.json")
	}
}

func TestValidateHarness_NonMarkdownInRules(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "bad")
	mkdirAll(t, filepath.Join(hr, "rules"))
	writeFile(t, filepath.Join(hr, "harness.json"),
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
	mkdirAll(t, filepath.Join(hr, "commands"))
	writeFile(t, filepath.Join(hr, "harness.json"),
		[]byte(`{"name":"bad","version":"0.1.0"}`))
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
	writeFile(t, filepath.Join(hr, "harness.json"),
		[]byte(`{"name":"conflict","version":"0.1.0"}`))
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
	writeFile(t, filepath.Join(hr, "harness.json"),
		[]byte(`{"name":"ok","version":"0.1.0"}`))
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
	writeFile(t, filepath.Join(hr, "harness.json"),
		[]byte(`{"name":"hooks-valid","version":"0.1.0","hooks":{"before_tool":[{"command":"echo hi"}]}}`))

	if err := validateHarness(hr); err != nil {
		t.Errorf("valid hooks should pass: %v", err)
	}
}

func TestValidateHarness_HooksUnknownEvent(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "hooks-bad-event")
	mkdirAll(t, hr)
	writeFile(t, filepath.Join(hr, "harness.json"),
		[]byte(`{"name":"hooks-bad-event","version":"0.1.0","hooks":{"unknown_event":[{"command":"echo hi"}]}}`))

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
	writeFile(t, filepath.Join(hr, "harness.json"),
		[]byte(`{"name":"hooks-empty-cmd","version":"0.1.0","hooks":{"before_tool":[{"command":""}]}}`))

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
	writeFile(t, filepath.Join(hr, "harness.json"),
		[]byte(`{"name":"mcp-valid","version":"0.1.0","mcp_servers":{"github":{"command":"npx","args":["-y","server"]}}}`))

	if err := validateHarness(hr); err != nil {
		t.Errorf("valid MCP servers should pass: %v", err)
	}
}

func TestValidateHarness_MCPServersURLOnly(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "mcp-url")
	mkdirAll(t, hr)
	writeFile(t, filepath.Join(hr, "harness.json"),
		[]byte(`{"name":"mcp-url","version":"0.1.0","mcp_servers":{"api":{"url":"https://api.example.com/mcp"}}}`))

	if err := validateHarness(hr); err != nil {
		t.Errorf("URL-only MCP server should pass: %v", err)
	}
}

func TestValidateHarness_MCPServersNeitherCommandNorURL(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "mcp-neither")
	mkdirAll(t, hr)
	writeFile(t, filepath.Join(hr, "harness.json"),
		[]byte(`{"name":"mcp-neither","version":"0.1.0","mcp_servers":{"bad":{}}}`))

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
	writeFile(t, filepath.Join(hr, "harness.json"),
		[]byte(`{"name":"mcp-both","version":"0.1.0","mcp_servers":{"bad":{"command":"npx","url":"https://example.com"}}}`))

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
	writeFile(t, filepath.Join(hr, "harness.json"),
		[]byte(`{"name":"mcp-bad-type","version":"0.1.0","mcp_servers":"not-an-object"}`))

	err := validateHarness(hr)
	if err == nil {
		t.Fatal("expected validation error for mcp_servers not being an object")
	}
}

func TestValidateHarness_AgentsMDOnly(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "agents-only")
	mkdirAll(t, hr)
	writeFile(t, filepath.Join(hr, "harness.json"),
		[]byte(`{"name":"agents-only","version":"0.1.0"}`))
	writeFile(t, filepath.Join(hr, "AGENTS.md"), []byte("just agents"))

	if err := validateHarness(hr); err != nil {
		t.Errorf("AGENTS.md-only harness should be valid: %v", err)
	}
}
