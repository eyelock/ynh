package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestParseInspectArgs_VendorFlag(t *testing.T) {
	vendor, skip, err := parseInspectArgs([]string{"-v", "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if vendor != "claude" {
		t.Errorf("vendor = %q, want %q", vendor, "claude")
	}
	if skip {
		t.Error("expected skipConfirm=false")
	}
}

func TestParseInspectArgs_LongVendorFlag(t *testing.T) {
	vendor, _, err := parseInspectArgs([]string{"--vendor", "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if vendor != "codex" {
		t.Errorf("vendor = %q, want %q", vendor, "codex")
	}
}

func TestParseInspectArgs_YesFlag(t *testing.T) {
	_, skip, err := parseInspectArgs([]string{"-y"})
	if err != nil {
		t.Fatal(err)
	}
	if !skip {
		t.Error("expected skipConfirm=true")
	}
}

func TestParseInspectArgs_Combined(t *testing.T) {
	vendor, skip, err := parseInspectArgs([]string{"-v", "claude", "--yes"})
	if err != nil {
		t.Fatal(err)
	}
	if vendor != "claude" {
		t.Errorf("vendor = %q, want %q", vendor, "claude")
	}
	if !skip {
		t.Error("expected skipConfirm=true")
	}
}

func TestParseInspectArgs_Empty(t *testing.T) {
	vendor, skip, err := parseInspectArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if vendor != "" {
		t.Errorf("expected empty vendor, got %q", vendor)
	}
	if skip {
		t.Error("expected skipConfirm=false")
	}
}

func TestParseInspectArgs_MissingVendorValue(t *testing.T) {
	_, _, err := parseInspectArgs([]string{"-v"})
	if err == nil {
		t.Fatal("expected error for -v without value")
	}
}

func TestParseInspectArgs_UnknownFlag(t *testing.T) {
	_, _, err := parseInspectArgs([]string{"--unknown"})
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
}

func TestDiscoverExistingSkills(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	mkdirAll(t, filepath.Join(dir, "skills", "commit"))
	mkdirAll(t, filepath.Join(dir, "skills", "deploy"))
	mkdirAll(t, filepath.Join(dir, "skills", "empty-dir"))
	writeFile(t, filepath.Join(dir, "skills", "commit", "SKILL.md"), []byte("---\nname: commit\n---\n"))
	writeFile(t, filepath.Join(dir, "skills", "deploy", "SKILL.md"), []byte("---\nname: deploy\n---\n"))

	skills := discoverExistingSkills(dir)
	if len(skills) != 2 {
		t.Errorf("found %d skills, want 2", len(skills))
	}
}

func TestDiscoverExistingSkills_NoSkillsDir(t *testing.T) {
	dir := t.TempDir()
	skills := discoverExistingSkills(dir)
	if len(skills) != 0 {
		t.Errorf("expected no skills, got %d", len(skills))
	}
}

func TestDiscoverExistingAgents(t *testing.T) {
	dir := t.TempDir()

	mkdirAll(t, filepath.Join(dir, "agents"))
	writeFile(t, filepath.Join(dir, "agents", "reviewer.md"), []byte("---\nname: reviewer\n---\n"))
	writeFile(t, filepath.Join(dir, "agents", "tester.md"), []byte("---\nname: tester\n---\n"))

	agents := discoverExistingAgents(dir)
	if len(agents) != 2 {
		t.Errorf("found %d agents, want 2", len(agents))
	}
}

func TestDiscoverExistingAgents_NoAgentsDir(t *testing.T) {
	dir := t.TempDir()
	agents := discoverExistingAgents(dir)
	if len(agents) != 0 {
		t.Errorf("expected no agents, got %d", len(agents))
	}
}

func TestDiscoverExistingAgents_SkipsDirectories(t *testing.T) {
	dir := t.TempDir()

	mkdirAll(t, filepath.Join(dir, "agents", "subdir"))
	writeFile(t, filepath.Join(dir, "agents", "reviewer.md"), []byte("---\nname: reviewer\n---\n"))

	agents := discoverExistingAgents(dir)
	if len(agents) != 1 {
		t.Errorf("found %d agents, want 1", len(agents))
	}
}

func TestReadFileContext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	writeFile(t, path, []byte("hello world"))

	content := readFileContext(path, 100)
	if content != "hello world" {
		t.Errorf("content = %q, want %q", content, "hello world")
	}
}

func TestReadFileContext_Truncated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	writeFile(t, path, []byte("hello world this is a long string"))

	content := readFileContext(path, 5)
	if !strings.HasPrefix(content, "hello") {
		t.Errorf("expected truncated content starting with 'hello', got %q", content)
	}
	if !strings.Contains(content, "truncated") {
		t.Error("expected truncation marker")
	}
}

func TestReadFileContext_Nonexistent(t *testing.T) {
	content := readFileContext("/nonexistent/file.txt", 100)
	if content != "" {
		t.Errorf("expected empty string for nonexistent file, got %q", content)
	}
}

func TestBuildSignalContext(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), []byte("module test\n"))

	signals := []signal{
		{catBuild, filepath.Join(dir, "go.mod"), 1},
	}

	ctx := buildSignalContext(dir, signals, 5)
	if !strings.Contains(ctx, "go.mod") {
		t.Error("expected go.mod in signal context")
	}
	if !strings.Contains(ctx, "Build") {
		t.Error("expected Build category in signal context")
	}
	if !strings.Contains(ctx, "module test") {
		t.Error("expected file content in signal context")
	}
}

func TestBuildSignalContext_Empty(t *testing.T) {
	ctx := buildSignalContext(".", nil, 5)
	if ctx != "" {
		t.Errorf("expected empty context, got %q", ctx)
	}
}

func TestExtractContent_Clean(t *testing.T) {
	input := "---\nname: test\n---\n\nBody.\n"
	got := extractContent(input)
	if !strings.HasPrefix(got, "---\n") {
		t.Errorf("expected frontmatter start, got %q", got)
	}
}

func TestExtractContent_MarkdownFence(t *testing.T) {
	input := "Here's the file:\n\n```markdown\n---\nname: test\n---\n\nBody.\n```\n"
	got := extractContent(input)
	if !strings.HasPrefix(got, "---\n") {
		t.Errorf("expected frontmatter start, got %q", got)
	}
	if strings.Contains(got, "```") {
		t.Errorf("expected fences stripped, got %q", got)
	}
}

func TestExtractContent_PlainFence(t *testing.T) {
	input := "```\n---\nname: test\n---\n\nBody.\n```\n"
	got := extractContent(input)
	if !strings.HasPrefix(got, "---\n") {
		t.Errorf("expected frontmatter start, got %q", got)
	}
}

func TestExtractContent_Preamble(t *testing.T) {
	input := "Sure, here is the generated file:\n\n---\nname: test\n---\n\nBody.\n"
	got := extractContent(input)
	if !strings.HasPrefix(got, "---\n") {
		t.Errorf("expected preamble stripped, got %q", got)
	}
}

func TestExtractContent_NoFrontmatter(t *testing.T) {
	input := "Just some text with no frontmatter."
	got := extractContent(input)
	if got != input+"\n" {
		t.Errorf("expected input preserved, got %q", got)
	}
}

func TestParseProposals(t *testing.T) {
	text := `Here are my suggestions:

1. **Skill: ` + "`ynh-add-vendor`" + `** — Step-by-step guide for implementing a new vendor adapter.
2. **Agent: ` + "`ynh-reviewer`" + `** — Code reviewer specialized in ynh's patterns.
3. **skill: ynh-author** — Guided persona authoring cycle.

Not suggested:
- Dev build cycle (already covered)
`

	proposals := parseProposals(text)
	if len(proposals) != 3 {
		t.Fatalf("expected 3 proposals, got %d", len(proposals))
	}

	if proposals[0].Type != "skill" || proposals[0].Name != "ynh-add-vendor" {
		t.Errorf("proposal 0: type=%q name=%q", proposals[0].Type, proposals[0].Name)
	}
	if proposals[1].Type != "agent" || proposals[1].Name != "ynh-reviewer" {
		t.Errorf("proposal 1: type=%q name=%q", proposals[1].Type, proposals[1].Name)
	}
	if proposals[2].Type != "skill" || proposals[2].Name != "ynh-author" {
		t.Errorf("proposal 2: type=%q name=%q", proposals[2].Type, proposals[2].Name)
	}
}

func TestParseProposals_NoMatches(t *testing.T) {
	text := "No additional artifacts needed."
	proposals := parseProposals(text)
	if len(proposals) != 0 {
		t.Errorf("expected 0 proposals, got %d", len(proposals))
	}
}

func TestExtractDescription(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`1. **Skill: ` + "`foo`" + `** — Step-by-step guide for something.`, "Step-by-step guide for something"},
		{`2. **Agent: bar** - Reviews code quality.`, "Reviews code quality"},
		{`3. **Skill: baz**`, "Generated by ynd inspect"},
	}
	for _, tt := range tests {
		got := extractDescription(tt.input)
		if got != tt.want {
			t.Errorf("extractDescription(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestEnsureFrontmatter_CleanContent(t *testing.T) {
	p := proposal{Type: "skill", Name: "my-skill", Line: "1. **Skill: `my-skill`** — Does things."}
	raw := "---\nname: my-skill\ndescription: Does things\n---\n\nBody content.\n"
	got := ensureFrontmatter(raw, p)
	if !strings.HasPrefix(got, "---\nname: my-skill\n") {
		t.Errorf("expected correct frontmatter, got %q", got[:50])
	}
	if !strings.Contains(got, "Body content.") {
		t.Error("expected body preserved")
	}
}

func TestEnsureFrontmatter_NoFrontmatter(t *testing.T) {
	p := proposal{Type: "skill", Name: "my-skill", Line: "1. **Skill: `my-skill`** — Does things."}
	raw := "Just instructions without any frontmatter.\n"
	got := ensureFrontmatter(raw, p)
	if !strings.HasPrefix(got, "---\nname: my-skill\n") {
		t.Errorf("expected injected frontmatter, got %q", got[:50])
	}
	if !strings.Contains(got, "Just instructions") {
		t.Error("expected body preserved")
	}
}

func TestEnsureFrontmatter_Agent(t *testing.T) {
	p := proposal{Type: "agent", Name: "my-agent", Line: "1. **Agent: `my-agent`** — Reviews code."}
	raw := "---\nname: my-agent\ndescription: Reviews\ntools: Read, Bash\n---\n\nBody.\n"
	got := ensureFrontmatter(raw, p)
	if !strings.Contains(got, "tools: Read, Bash") {
		t.Error("expected tools preserved from LLM output")
	}
}

func TestWriteArtifact_Skill(t *testing.T) {
	dir := t.TempDir()
	p := proposal{Type: "skill", Name: "build"}
	content := "---\nname: build\ndescription: Build the project\n---\n\nRun make build.\n"
	if err := writeArtifact(content, p, dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "skills", "build", "SKILL.md")
	data := readFileContext(path, 1000)
	if !strings.Contains(data, "name: build") {
		t.Errorf("expected skill content, got %q", data)
	}
}

func TestWriteArtifact_Agent(t *testing.T) {
	dir := t.TempDir()
	p := proposal{Type: "agent", Name: "reviewer"}
	content := "---\nname: reviewer\ndescription: Reviews code\ntools: Read, Grep\n---\n\nBody.\n"
	if err := writeArtifact(content, p, dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "agents", "reviewer.md")
	data := readFileContext(path, 1000)
	if !strings.Contains(data, "name: reviewer") {
		t.Errorf("expected agent content, got %q", data)
	}
}

func TestWriteArtifact_AlreadyExists(t *testing.T) {
	dir := t.TempDir()
	mkdirAll(t, filepath.Join(dir, "skills", "build"))
	writeFile(t, filepath.Join(dir, "skills", "build", "SKILL.md"), []byte("existing"))

	p := proposal{Type: "skill", Name: "build"}
	err := writeArtifact("content", p, dir)
	if err == nil {
		t.Fatal("expected error for existing file")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got %v", err)
	}
}
