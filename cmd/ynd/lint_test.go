package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile is a test helper that creates parent directories if needed,
// writes a file, and fails the test on error.
func writeFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
}

// mkdirAll is a test helper that creates directories and fails the test on error.
func mkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

// mockLLM replaces queryLLMFunc and lookPathFunc for tests that use LLM commands.
// Returns a cleanup function that restores the originals.
func mockLLM(t *testing.T, fn func(vendor, prompt string) (string, error)) {
	t.Helper()
	origLLM := queryLLMFunc
	origLookPath := lookPathFunc
	queryLLMFunc = fn
	lookPathFunc = func(name string) (string, error) { return "/mock/" + name, nil }
	t.Cleanup(func() {
		queryLLMFunc = origLLM
		lookPathFunc = origLookPath
	})
}

func TestLintMarkdown_TrailingWhitespace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	writeFile(t, path, []byte("hello   \nworld\n"))

	issues := lintMarkdown(path)
	found := false
	for _, issue := range issues {
		if strings.Contains(issue.Message, "trailing whitespace") {
			found = true
			if issue.Line != 1 {
				t.Errorf("line = %d, want 1", issue.Line)
			}
		}
	}
	if !found {
		t.Error("trailing whitespace issue not found")
	}
}

func TestLintMarkdown_NoTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	writeFile(t, path, []byte("hello"))

	issues := lintMarkdown(path)
	found := false
	for _, issue := range issues {
		if strings.Contains(issue.Message, "newline") {
			found = true
		}
	}
	if !found {
		t.Error("expected missing newline issue")
	}
}

func TestLintMarkdown_MultipleBlankLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	writeFile(t, path, []byte("hello\n\n\nworld\n"))

	issues := lintMarkdown(path)
	found := false
	for _, issue := range issues {
		if strings.Contains(issue.Message, "consecutive blank") {
			found = true
		}
	}
	if !found {
		t.Error("expected consecutive blank lines issue")
	}
}

func TestLintMarkdown_CleanFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	writeFile(t, path, []byte("# Hello\n\nThis is clean.\n"))

	issues := lintMarkdown(path)
	if len(issues) != 0 {
		t.Errorf("expected no issues, got %d: %v", len(issues), issues)
	}
}

func TestLintSkillFrontmatter_Valid(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "hello")
	mkdirAll(t, skillDir)

	path := filepath.Join(skillDir, "SKILL.md")
	content := "---\nname: hello\ndescription: Say hello\n---\n\nBody.\n"
	writeFile(t, path, []byte(content))

	issues := lintSkillFrontmatter(path, content)
	if len(issues) != 0 {
		t.Errorf("expected no issues, got %v", issues)
	}
}

func TestLintSkillFrontmatter_NameMismatch(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "hello")
	mkdirAll(t, skillDir)

	path := filepath.Join(skillDir, "SKILL.md")
	content := "---\nname: wrong-name\ndescription: Say hello\n---\n\nBody.\n"
	writeFile(t, path, []byte(content))

	issues := lintSkillFrontmatter(path, content)
	found := false
	for _, issue := range issues {
		if strings.Contains(issue.Message, "does not match") {
			found = true
		}
	}
	if !found {
		t.Error("expected name mismatch issue")
	}
}

func TestLintAgentFrontmatter_Valid(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "agents")
	mkdirAll(t, agentDir)

	path := filepath.Join(agentDir, "reviewer.md")
	content := "---\nname: reviewer\ndescription: Reviews code\ntools: Read, Grep\n---\n\nBody.\n"
	writeFile(t, path, []byte(content))

	issues := lintAgentFrontmatter(path, content)
	if len(issues) != 0 {
		t.Errorf("expected no issues, got %v", issues)
	}
}

func TestLintAgentFrontmatter_MissingFields(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "agents")
	mkdirAll(t, agentDir)

	path := filepath.Join(agentDir, "bad.md")
	content := "---\nfoo: bar\n---\n\nBody.\n"
	writeFile(t, path, []byte(content))

	issues := lintAgentFrontmatter(path, content)
	if len(issues) < 3 {
		t.Errorf("expected at least 3 issues (missing name, description, tools), got %d", len(issues))
	}
}

func TestLintAgentFrontmatter_NameMismatch(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "agents")
	mkdirAll(t, agentDir)

	path := filepath.Join(agentDir, "reviewer.md")
	content := "---\nname: wrong\ndescription: Reviews code\ntools: Read\n---\n\nBody.\n"
	writeFile(t, path, []byte(content))

	issues := lintAgentFrontmatter(path, content)
	found := false
	for _, issue := range issues {
		if strings.Contains(issue.Message, "does not match") {
			found = true
		}
	}
	if !found {
		t.Error("expected name mismatch issue")
	}
}

func TestLintAgentFrontmatter_NoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "agents")
	mkdirAll(t, agentDir)

	path := filepath.Join(agentDir, "bad.md")
	content := "Just plain text.\n"
	writeFile(t, path, []byte(content))

	issues := lintAgentFrontmatter(path, content)
	if len(issues) == 0 {
		t.Fatal("expected frontmatter missing issue")
	}
	if !strings.Contains(issues[0].Message, "missing YAML frontmatter") {
		t.Errorf("unexpected message: %s", issues[0].Message)
	}
}

func TestLintHarnessJSON_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".harness.json")
	writeFile(t, path, []byte(`{"name":"test","version":"0.1.0"}`))

	issues := lintHarnessJSONFile(path)
	if len(issues) != 0 {
		t.Errorf("expected no issues, got %v", issues)
	}
}

func TestLintHarnessJSON_MissingFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".harness.json")
	writeFile(t, path, []byte(`{}`))

	issues := lintHarnessJSONFile(path)
	if len(issues) < 2 {
		t.Errorf("expected at least 2 issues, got %d", len(issues))
	}
}

func TestLintHarnessJSON_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".harness.json")
	writeFile(t, path, []byte(`{not json}`))

	issues := lintHarnessJSONFile(path)
	if len(issues) == 0 {
		t.Fatal("expected invalid JSON issue")
	}
	if !strings.Contains(issues[0].Message, "invalid JSON") {
		t.Errorf("expected 'invalid JSON' message, got %q", issues[0].Message)
	}
}

func TestLintHarnessJSON_InvalidName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".harness.json")
	writeFile(t, path, []byte(`{"name":"../bad","version":"1.0"}`))

	issues := lintHarnessJSONFile(path)
	found := false
	for _, issue := range issues {
		if strings.Contains(issue.Message, "naming convention") {
			found = true
		}
	}
	if !found {
		t.Error("expected naming convention issue")
	}
}

func TestLintHarnessJSON_MissingGitInIncludes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".harness.json")
	writeFile(t, path, []byte(`{"name":"test","version":"0.1.0","includes":[{"ref":"main"}]}`))

	issues := lintHarnessJSONFile(path)
	found := false
	for _, issue := range issues {
		if strings.Contains(issue.Message, "missing required field 'git'") {
			found = true
		}
	}
	if !found {
		t.Error("expected missing 'git' field issue")
	}
}

func TestLintShell_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.sh")
	writeFile(t, path, []byte("#!/bin/bash\necho hello\n"))

	issues := lintShell(path)
	if len(issues) != 0 {
		t.Errorf("expected no issues, got %v", issues)
	}
}

func TestLintShell_MissingShebang(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.sh")
	writeFile(t, path, []byte("echo hello\n"))

	issues := lintShell(path)
	found := false
	for _, issue := range issues {
		if strings.Contains(issue.Message, "shebang") {
			found = true
		}
	}
	if !found {
		t.Error("expected missing shebang issue")
	}
}

func TestLintShellBlocks_ValidBlock(t *testing.T) {
	content := "# Test\n\n```bash\necho hello\n```\n"
	issues := lintShellBlocks("test.md", content)
	if len(issues) != 0 {
		t.Errorf("expected no issues, got %v", issues)
	}
}

func TestLintShellBlocks_InvalidBlock(t *testing.T) {
	content := "# Test\n\n```bash\nif true; then\n```\n"
	issues := lintShellBlocks("test.md", content)
	if len(issues) == 0 {
		t.Error("expected shell syntax error issue")
	}
}

func TestLintShellBlocks_TemplatePlaceholders(t *testing.T) {
	content := "# Test\n\n```bash\nynh install <output-dir>\n```\n"
	issues := lintShellBlocks("test.md", content)
	if len(issues) != 0 {
		t.Errorf("expected no issues for template block, got %v", issues)
	}
}

func TestHasTemplatePlaceholders(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"ynh install <output-dir>", true},
		{"<name> --flag", true},
		{"cd <harness-dir>", true},
		{"echo hello", false},
		{"echo hello > file", false},
		{"if [ $x -lt 5 ]; then echo ok; fi", false},
		{"", false},
	}

	for _, tt := range tests {
		got := hasTemplatePlaceholders(tt.input)
		if got != tt.want {
			t.Errorf("hasTemplatePlaceholders(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestParseFrontmatter_Valid(t *testing.T) {
	content := "---\nname: test\ndescription: A test\ntools: Read\n---\nBody\n"
	fm := parseFrontmatter(content)
	if fm == nil {
		t.Fatal("expected frontmatter, got nil")
	}
	if fm["name"] != "test" {
		t.Errorf("name = %q, want %q", fm["name"], "test")
	}
	if fm["description"] != "A test" {
		t.Errorf("description = %q, want %q", fm["description"], "A test")
	}
	if fm["tools"] != "Read" {
		t.Errorf("tools = %q, want %q", fm["tools"], "Read")
	}
}

func TestParseFrontmatter_NoFrontmatter(t *testing.T) {
	fm := parseFrontmatter("Just text.\n")
	if fm != nil {
		t.Errorf("expected nil, got %v", fm)
	}
}

func TestExpectedFrontmatterName_Skill(t *testing.T) {
	got := expectedFrontmatterName("/foo/skills/hello/SKILL.md")
	if got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}

func TestExpectedFrontmatterName_Agent(t *testing.T) {
	got := expectedFrontmatterName("/foo/agents/reviewer.md")
	if got != "reviewer" {
		t.Errorf("got %q, want %q", got, "reviewer")
	}
}

func TestCmdLint_NoFiles(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	err := cmdLint(nil)
	if err != nil {
		t.Errorf("expected no error for empty dir, got %v", err)
	}
}

func TestCmdLint_Clean(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeFile(t, filepath.Join(dir, "readme.md"), []byte("# Hello\n\nClean file.\n"))

	err := cmdLint(nil)
	if err != nil {
		t.Errorf("expected no error for clean files, got %v", err)
	}
}

func TestCmdLint_WithIssues(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeFile(t, filepath.Join(dir, "bad.md"), []byte("trailing spaces   "))

	err := cmdLint(nil)
	if err == nil {
		t.Fatal("expected lint error")
	}
	if !strings.Contains(err.Error(), "lint failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCmdLint_SingleFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	writeFile(t, path, []byte("# Clean\n"))

	err := cmdLint([]string{path})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestCmdLint_WithShellAndJSON(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeFile(t, filepath.Join(dir, "test.sh"), []byte("#!/bin/bash\necho hello\n"))
	writeFile(t, filepath.Join(dir, ".harness.json"), []byte(`{"name":"test","version":"0.1.0"}`))

	err := cmdLint(nil)
	if err != nil {
		t.Errorf("expected no error for valid files, got %v", err)
	}
}

func TestCmdLint_IssueWithLineNumber(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeFile(t, filepath.Join(dir, "test.md"), []byte("line1\nline2   \nline3\n"))

	err := cmdLint(nil)
	if err == nil {
		t.Fatal("expected lint error")
	}
}

func TestCmdLint_IssueWithoutLineNumber(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeFile(t, filepath.Join(dir, ".ynh-plugin", "plugin.json"), []byte(`{}`))

	err := cmdLint(nil)
	if err == nil {
		t.Fatal("expected lint error for plugin.json missing fields")
	}
}

func TestLintSkillFrontmatter_Missing(t *testing.T) {
	issues := lintSkillFrontmatter("skills/foo/SKILL.md", "Just text.\n")
	if len(issues) == 0 {
		t.Fatal("expected issue for missing frontmatter")
	}
	if !strings.Contains(issues[0].Message, "missing YAML frontmatter") {
		t.Errorf("unexpected message: %s", issues[0].Message)
	}
}

func TestLintSkillFrontmatter_Unclosed(t *testing.T) {
	issues := lintSkillFrontmatter("skills/foo/SKILL.md", "---\nname: foo\nno closing\n")
	if len(issues) == 0 {
		t.Fatal("expected issue for unclosed frontmatter")
	}
	if !strings.Contains(issues[0].Message, "unclosed frontmatter") {
		t.Errorf("unexpected message: %s", issues[0].Message)
	}
}

func TestLintSkillFrontmatter_EmptyName(t *testing.T) {
	content := "---\nname: \ndescription: Something\n---\n"
	issues := lintSkillFrontmatter("skills/foo/SKILL.md", content)
	found := false
	for _, issue := range issues {
		if strings.Contains(issue.Message, "'name' is empty") {
			found = true
		}
	}
	if !found {
		t.Error("expected empty name issue")
	}
}

func TestLintSkillFrontmatter_MissingName(t *testing.T) {
	content := "---\ndescription: Something\n---\n"
	issues := lintSkillFrontmatter("skills/foo/SKILL.md", content)
	found := false
	for _, issue := range issues {
		if strings.Contains(issue.Message, "missing required field 'name'") {
			found = true
		}
	}
	if !found {
		t.Error("expected missing name issue")
	}
}

func TestLintSkillFrontmatter_MissingDescription(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "foo")
	mkdirAll(t, skillDir)
	path := filepath.Join(skillDir, "SKILL.md")
	content := "---\nname: foo\n---\n"

	issues := lintSkillFrontmatter(path, content)
	found := false
	for _, issue := range issues {
		if strings.Contains(issue.Message, "missing required field 'description'") {
			found = true
		}
	}
	if !found {
		t.Error("expected missing description issue")
	}
}

func TestLintAgentFrontmatter_Unclosed(t *testing.T) {
	issues := lintAgentFrontmatter("agents/bad.md", "---\nname: bad\nno closing\n")
	if len(issues) == 0 {
		t.Fatal("expected issue for unclosed frontmatter")
	}
	if !strings.Contains(issues[0].Message, "unclosed frontmatter") {
		t.Errorf("unexpected message: %s", issues[0].Message)
	}
}

func TestLintAgentFrontmatter_EmptyName(t *testing.T) {
	content := "---\nname: \ndescription: Something\ntools: Read\n---\n"
	issues := lintAgentFrontmatter("agents/bad.md", content)
	found := false
	for _, issue := range issues {
		if strings.Contains(issue.Message, "'name' is empty") {
			found = true
		}
	}
	if !found {
		t.Error("expected empty name issue")
	}
}

func TestLintHarnessJSON_IncludesNotArray(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".harness.json")
	writeFile(t, path, []byte(`{"name":"test","version":"0.1.0","includes":"not-array"}`))

	issues := lintHarnessJSONFile(path)
	found := false
	for _, issue := range issues {
		if strings.Contains(issue.Message, "'includes' must be an array") {
			found = true
		}
	}
	if !found {
		t.Error("expected includes not array issue")
	}
}

func TestLintHarnessJSON_IncludesItemNotObject(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".harness.json")
	writeFile(t, path, []byte(`{"name":"test","version":"0.1.0","includes":["not-object"]}`))

	issues := lintHarnessJSONFile(path)
	found := false
	for _, issue := range issues {
		if strings.Contains(issue.Message, "must be an object") {
			found = true
		}
	}
	if !found {
		t.Error("expected includes item not object issue")
	}
}

func TestLintHarnessJSON_DelegatesToMissingGit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".harness.json")
	writeFile(t, path, []byte(`{"name":"test","version":"0.1.0","delegates_to":[{"ref":"main"}]}`))

	issues := lintHarnessJSONFile(path)
	found := false
	for _, issue := range issues {
		if strings.Contains(issue.Message, "missing required field 'git'") {
			found = true
		}
	}
	if !found {
		t.Error("expected missing git field issue")
	}
}

func TestLintHarnessJSON_DelegatesToNotArray(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".harness.json")
	writeFile(t, path, []byte(`{"name":"test","version":"0.1.0","delegates_to":"not-array"}`))

	issues := lintHarnessJSONFile(path)
	found := false
	for _, issue := range issues {
		if strings.Contains(issue.Message, "'delegates_to' must be an array") {
			found = true
		}
	}
	if !found {
		t.Error("expected delegates_to not array issue")
	}
}

func TestLintHarnessJSON_DelegatesToItemNotObject(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".harness.json")
	writeFile(t, path, []byte(`{"name":"test","version":"0.1.0","delegates_to":["not-object"]}`))

	issues := lintHarnessJSONFile(path)
	found := false
	for _, issue := range issues {
		if strings.Contains(issue.Message, "must be an object") {
			found = true
		}
	}
	if !found {
		t.Error("expected delegates_to item not object issue")
	}
}

func TestLintHarnessJSON_ValidWithIncludes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".harness.json")
	writeFile(t, path, []byte(`{"name":"test","version":"0.1.0","includes":[{"git":"https://example.com/repo"}]}`))

	issues := lintHarnessJSONFile(path)
	if len(issues) != 0 {
		t.Errorf("expected no issues, got %v", issues)
	}
}

func TestLintHarnessJSON_ValidWithDelegatesTo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".harness.json")
	writeFile(t, path, []byte(`{"name":"test","version":"0.1.0","delegates_to":[{"git":"https://example.com/repo"}]}`))

	issues := lintHarnessJSONFile(path)
	if len(issues) != 0 {
		t.Errorf("expected no issues, got %v", issues)
	}
}

func TestLintHarnessJSON_EmptyName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".harness.json")
	writeFile(t, path, []byte(`{"name":"","version":"0.1.0"}`))

	issues := lintHarnessJSONFile(path)
	found := false
	for _, issue := range issues {
		if strings.Contains(issue.Message, "non-empty string") {
			found = true
		}
	}
	if !found {
		t.Error("expected empty name issue")
	}
}

func TestLintHarnessJSON_EmptyVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".harness.json")
	writeFile(t, path, []byte(`{"name":"test","version":""}`))

	issues := lintHarnessJSONFile(path)
	found := false
	for _, issue := range issues {
		if strings.Contains(issue.Message, "'version' must be a non-empty string") {
			found = true
		}
	}
	if !found {
		t.Error("expected empty version issue")
	}
}

func TestLintShell_SyntaxError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.sh")
	writeFile(t, path, []byte("#!/bin/bash\nif true; then\n"))

	issues := lintShell(path)
	found := false
	for _, issue := range issues {
		if strings.Contains(issue.Message, "syntax error") {
			found = true
		}
	}
	if !found {
		t.Error("expected syntax error issue")
	}
}

func TestCheckBashSyntax_Valid(t *testing.T) {
	err := checkBashSyntax("echo hello")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestCheckBashSyntax_Invalid(t *testing.T) {
	err := checkBashSyntax("if true; then")
	if err == nil {
		t.Error("expected syntax error")
	}
}

func TestLintMarkdown_ReadError(t *testing.T) {
	issues := lintMarkdown("/nonexistent/path/file.md")
	if len(issues) == 0 {
		t.Fatal("expected read error issue")
	}
	if !strings.Contains(issues[0].Message, "read error") {
		t.Errorf("unexpected message: %s", issues[0].Message)
	}
}

func TestLintShell_ReadError(t *testing.T) {
	issues := lintShell("/nonexistent/path/test.sh")
	if len(issues) == 0 {
		t.Fatal("expected read error issue")
	}
	if !strings.Contains(issues[0].Message, "read error") {
		t.Errorf("unexpected message: %s", issues[0].Message)
	}
}

func TestLintHarnessJSON_ReadError(t *testing.T) {
	issues := lintHarnessJSONFile("/nonexistent/path/.harness.json")
	if len(issues) == 0 {
		t.Fatal("expected read error issue")
	}
	if !strings.Contains(issues[0].Message, "read error") {
		t.Errorf("unexpected message: %s", issues[0].Message)
	}
}

func TestIsSkillFile(t *testing.T) {
	if !isSkillFile("skills/foo/SKILL.md") {
		t.Error("expected true for SKILL.md")
	}
	if isSkillFile("agents/foo.md") {
		t.Error("expected false for non-SKILL.md")
	}
}

func TestIsAgentFile(t *testing.T) {
	if !isAgentFile("agents/reviewer.md") {
		t.Error("expected true for agents/reviewer.md")
	}
	if isAgentFile("skills/foo/SKILL.md") {
		t.Error("expected false for non-agent file")
	}
}

func TestLintMarkdown_SkillContextCheck(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "test")
	mkdirAll(t, skillDir)
	path := filepath.Join(skillDir, "SKILL.md")
	writeFile(t, path, []byte("---\nname: test\ndescription: Test skill\n---\n\nBody.\n"))

	issues := lintMarkdown(path)
	if len(issues) != 0 {
		t.Errorf("expected no issues for valid skill, got %v", issues)
	}
}

func TestLintMarkdown_AgentContextCheck(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "agents")
	mkdirAll(t, agentDir)
	path := filepath.Join(agentDir, "reviewer.md")
	writeFile(t, path, []byte("---\nname: reviewer\ndescription: Reviews\ntools: Read\n---\n\nBody.\n"))

	issues := lintMarkdown(path)
	if len(issues) != 0 {
		t.Errorf("expected no issues for valid agent, got %v", issues)
	}
}

func TestParseFrontmatter_Unclosed(t *testing.T) {
	fm := parseFrontmatter("---\nname: test\nno closing\n")
	if fm != nil {
		t.Errorf("expected nil for unclosed frontmatter, got %v", fm)
	}
}

func TestParseFrontmatter_QuotedValues(t *testing.T) {
	content := "---\nname: \"quoted\"\ndescription: 'single'\n---\n"
	fm := parseFrontmatter(content)
	if fm == nil {
		t.Fatal("expected frontmatter")
	}
	if fm["name"] != "quoted" {
		t.Errorf("name = %q, want %q", fm["name"], "quoted")
	}
	if fm["description"] != "single" {
		t.Errorf("description = %q, want %q", fm["description"], "single")
	}
}

func TestLintShellBlocks_ShBlock(t *testing.T) {
	content := "# Test\n\n```sh\necho hello\n```\n"
	issues := lintShellBlocks("test.md", content)
	if len(issues) != 0 {
		t.Errorf("expected no issues, got %v", issues)
	}
}

func TestLintShellBlocks_EmptyBlock(t *testing.T) {
	content := "# Test\n\n```bash\n```\n"
	issues := lintShellBlocks("test.md", content)
	if len(issues) != 0 {
		t.Errorf("expected no issues for empty block, got %v", issues)
	}
}
