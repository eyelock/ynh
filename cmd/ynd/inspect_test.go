package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseInspectArgs_VendorFlag(t *testing.T) {
	vendor, skip, _, err := parseInspectArgs([]string{"-v", "claude"})
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
	vendor, _, _, err := parseInspectArgs([]string{"--vendor", "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if vendor != "codex" {
		t.Errorf("vendor = %q, want %q", vendor, "codex")
	}
}

func TestParseInspectArgs_YesFlag(t *testing.T) {
	_, skip, _, err := parseInspectArgs([]string{"-y"})
	if err != nil {
		t.Fatal(err)
	}
	if !skip {
		t.Error("expected skipConfirm=true")
	}
}

func TestParseInspectArgs_Combined(t *testing.T) {
	vendor, skip, _, err := parseInspectArgs([]string{"-v", "claude", "--yes"})
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
	vendor, skip, _, err := parseInspectArgs(nil)
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
	_, _, _, err := parseInspectArgs([]string{"-v"})
	if err == nil {
		t.Fatal("expected error for -v without value")
	}
}

func TestParseInspectArgs_UnknownFlag(t *testing.T) {
	_, _, _, err := parseInspectArgs([]string{"--unknown"})
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
}

func TestParseInspectArgs_OutputDir(t *testing.T) {
	_, _, outDir, err := parseInspectArgs([]string{"-o", "/tmp/custom"})
	if err != nil {
		t.Fatal(err)
	}
	if outDir != "/tmp/custom" {
		t.Errorf("outputDir = %q, want %q", outDir, "/tmp/custom")
	}
}

func TestParseInspectArgs_LongOutputDir(t *testing.T) {
	_, _, outDir, err := parseInspectArgs([]string{"--output-dir", "/tmp/custom"})
	if err != nil {
		t.Fatal(err)
	}
	if outDir != "/tmp/custom" {
		t.Errorf("outputDir = %q, want %q", outDir, "/tmp/custom")
	}
}

func TestParseInspectArgs_MissingOutputDirValue(t *testing.T) {
	_, _, _, err := parseInspectArgs([]string{"-o"})
	if err == nil {
		t.Fatal("expected error for -o without value")
	}
}

func TestParseInspectArgs_AllFlags(t *testing.T) {
	vendor, skip, outDir, err := parseInspectArgs([]string{"-v", "claude", "-y", "-o", "/tmp/out"})
	if err != nil {
		t.Fatal(err)
	}
	if vendor != "claude" {
		t.Errorf("vendor = %q, want %q", vendor, "claude")
	}
	if !skip {
		t.Error("expected skipConfirm=true")
	}
	if outDir != "/tmp/out" {
		t.Errorf("outputDir = %q, want %q", outDir, "/tmp/out")
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

func TestDiscoverExistingSkillsAll(t *testing.T) {
	dir := t.TempDir()

	// Root-level skill
	mkdirAll(t, filepath.Join(dir, "skills", "build"))
	writeFile(t, filepath.Join(dir, "skills", "build", "SKILL.md"), []byte("---\nname: build\n---\n"))

	// Vendor-specific skill
	mkdirAll(t, filepath.Join(dir, ".claude", "skills", "test"))
	writeFile(t, filepath.Join(dir, ".claude", "skills", "test", "SKILL.md"), []byte("---\nname: test\n---\n"))

	// Different vendor
	mkdirAll(t, filepath.Join(dir, ".cursor", "skills", "lint"))
	writeFile(t, filepath.Join(dir, ".cursor", "skills", "lint", "SKILL.md"), []byte("---\nname: lint\n---\n"))

	skills := discoverExistingSkillsAll(dir)
	if len(skills) != 3 {
		t.Errorf("found %d skills, want 3", len(skills))
	}
}

func TestDiscoverExistingAgentsAll(t *testing.T) {
	dir := t.TempDir()

	// Root-level agent
	mkdirAll(t, filepath.Join(dir, "agents"))
	writeFile(t, filepath.Join(dir, "agents", "reviewer.md"), []byte("---\nname: reviewer\n---\n"))

	// Vendor-specific agent
	mkdirAll(t, filepath.Join(dir, ".claude", "agents"))
	writeFile(t, filepath.Join(dir, ".claude", "agents", "tester.md"), []byte("---\nname: tester\n---\n"))

	agents := discoverExistingAgentsAll(dir)
	if len(agents) != 2 {
		t.Errorf("found %d agents, want 2", len(agents))
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
3. **skill: ynh-author** — Guided harness authoring cycle.

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

func TestParseProposals_DashSeparatedFormat(t *testing.T) {
	text := `Here are suggestions:

1. **Skill** — ` + "`go-dev`" + ` — Run go fmt, go vet, go build, and go test in sequence.
2. **Skill** — ` + "`go-test`" + ` — Run go test -race -coverprofile=coverage.out ./...
3. **Agent** — ` + "`go-review`" + ` — Code reviewer that checks for Go idioms.
4. **Skill** — go-mod-tidy — Run go mod tidy and verify go.sum is clean.
`

	proposals := parseProposals(text)
	if len(proposals) != 4 {
		t.Fatalf("expected 4 proposals, got %d", len(proposals))
	}

	if proposals[0].Type != "skill" || proposals[0].Name != "go-dev" {
		t.Errorf("proposal 0: type=%q name=%q", proposals[0].Type, proposals[0].Name)
	}
	if proposals[1].Type != "skill" || proposals[1].Name != "go-test" {
		t.Errorf("proposal 1: type=%q name=%q", proposals[1].Type, proposals[1].Name)
	}
	if proposals[2].Type != "agent" || proposals[2].Name != "go-review" {
		t.Errorf("proposal 2: type=%q name=%q", proposals[2].Type, proposals[2].Name)
	}
	if proposals[3].Type != "skill" || proposals[3].Name != "go-mod-tidy" {
		t.Errorf("proposal 3: type=%q name=%q", proposals[3].Type, proposals[3].Name)
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

func TestWriteArtifact_VendorDir(t *testing.T) {
	dir := t.TempDir()
	outputDir := filepath.Join(dir, ".claude")

	p := proposal{Type: "skill", Name: "build"}
	content := "---\nname: build\ndescription: Build the project\n---\n\nRun make build.\n"
	if err := writeArtifact(content, p, outputDir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(outputDir, "skills", "build", "SKILL.md")
	data := readFileContext(path, 1000)
	if !strings.Contains(data, "name: build") {
		t.Errorf("expected skill content, got %q", data)
	}
}

func TestWriteArtifact_AgentVendorDir(t *testing.T) {
	dir := t.TempDir()
	outputDir := filepath.Join(dir, ".cursor")

	p := proposal{Type: "agent", Name: "reviewer"}
	content := "---\nname: reviewer\ndescription: Reviews code\ntools: Read, Grep\n---\n\nBody.\n"
	if err := writeArtifact(content, p, outputDir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(outputDir, "agents", "reviewer.md")
	data := readFileContext(path, 1000)
	if !strings.Contains(data, "name: reviewer") {
		t.Errorf("expected agent content, got %q", data)
	}
}

func TestDiscoverExistingSkillsAll_NoneExist(t *testing.T) {
	dir := t.TempDir()
	skills := discoverExistingSkillsAll(dir)
	if len(skills) != 0 {
		t.Errorf("expected 0 skills, got %d", len(skills))
	}
}

func TestDiscoverExistingAgentsAll_NoneExist(t *testing.T) {
	dir := t.TempDir()
	agents := discoverExistingAgentsAll(dir)
	if len(agents) != 0 {
		t.Errorf("expected 0 agents, got %d", len(agents))
	}
}

func TestDiscoverExistingSkillsAll_NoDuplicates(t *testing.T) {
	// If the same skill exists in root and a vendor dir via symlink or copy,
	// it should only appear once (deduped by absolute path).
	dir := t.TempDir()
	mkdirAll(t, filepath.Join(dir, "skills", "build"))
	writeFile(t, filepath.Join(dir, "skills", "build", "SKILL.md"), []byte("---\nname: build\n---\n"))

	skills := discoverExistingSkillsAll(dir)
	if len(skills) != 1 {
		t.Errorf("expected 1 skill (deduped), got %d", len(skills))
	}
}

func TestBuildSignalContext_TruncatesContent(t *testing.T) {
	dir := t.TempDir()
	longContent := strings.Repeat("x", 3000)
	writeFile(t, filepath.Join(dir, "go.mod"), []byte(longContent))

	signals := []signal{
		{catBuild, filepath.Join(dir, "go.mod"), 1},
	}

	ctx := buildSignalContext(dir, signals, 5)
	if !strings.Contains(ctx, "truncated") {
		t.Error("expected truncation marker for long content")
	}
}

func TestExtractContent_ShortString(t *testing.T) {
	// Regression: len(raw)-4 underflows on strings shorter than 4 chars
	for _, input := range []string{"", "a", "ab", "abc", "---"} {
		got := extractContent(input)
		if got == "" {
			t.Errorf("extractContent(%q) returned empty", input)
		}
	}
}

func TestExtractContent_YamlFence(t *testing.T) {
	input := "```yaml\n---\nname: test\n---\n\nBody.\n```\n"
	got := extractContent(input)
	if !strings.HasPrefix(got, "---\n") {
		t.Errorf("expected frontmatter start, got %q", got)
	}
	if strings.Contains(got, "```") {
		t.Error("expected fences stripped")
	}
}

func TestExtractContent_MdFence(t *testing.T) {
	input := "```md\n---\nname: test\n---\n\nBody.\n```\n"
	got := extractContent(input)
	if !strings.HasPrefix(got, "---\n") {
		t.Errorf("expected frontmatter start, got %q", got)
	}
}

func TestParseProposals_MixedFormats(t *testing.T) {
	// Test that both colon and dash-separated formats work in the same list
	text := `1. **Skill: ` + "`colon-style`" + `** — Description one.
2. **Agent** — ` + "`dash-style`" + ` — Description two.
3. **skill: plain-style** — Description three.
`
	proposals := parseProposals(text)
	if len(proposals) != 3 {
		t.Fatalf("expected 3 proposals, got %d", len(proposals))
	}
	if proposals[0].Name != "colon-style" {
		t.Errorf("proposal 0 name = %q", proposals[0].Name)
	}
	if proposals[1].Name != "dash-style" {
		t.Errorf("proposal 1 name = %q", proposals[1].Name)
	}
	if proposals[2].Name != "plain-style" {
		t.Errorf("proposal 2 name = %q", proposals[2].Name)
	}
}

func TestEnsureFrontmatter_AgentDefaultTools(t *testing.T) {
	// Agent with no tools in LLM output should get default tools
	p := proposal{Type: "agent", Name: "my-agent", Line: "1. **Agent: `my-agent`** — Reviews code."}
	raw := "Just agent instructions without any frontmatter.\n"
	got := ensureFrontmatter(raw, p)
	if !strings.Contains(got, "tools: Read, Grep, Glob") {
		t.Error("expected default tools for agent without frontmatter")
	}
	if !strings.Contains(got, "Just agent instructions") {
		t.Error("expected body preserved")
	}
}

func TestExtractDescription_NoSeparator(t *testing.T) {
	line := "1. **Skill: foo**"
	got := extractDescription(line)
	if got != "Generated by ynd inspect" {
		t.Errorf("got %q, want fallback description", got)
	}
}

func TestExtractDescription_RegularDash(t *testing.T) {
	line := "1. **Skill: foo** - Does stuff nicely."
	got := extractDescription(line)
	if got != "Does stuff nicely" {
		t.Errorf("got %q, want %q", got, "Does stuff nicely")
	}
}

func TestPromptAction_ReturnsChoice(t *testing.T) {
	original := promptActionFunc
	defer func() { promptActionFunc = original }()

	promptActionFunc = func(msg string, choices ...string) string {
		return "y"
	}

	got := promptAction("Choose:", "y", "n")
	if got != "y" {
		t.Errorf("got %q, want %q", got, "y")
	}
}

func TestPromptInput_ReturnsText(t *testing.T) {
	original := promptInputFunc
	defer func() { promptInputFunc = original }()

	promptInputFunc = func(msg string) string {
		return "user input"
	}

	got := promptInput("Enter:")
	if got != "user input" {
		t.Errorf("got %q, want %q", got, "user input")
	}
}

func TestCmdInspect_FullFlow_SkipConfirm(t *testing.T) {
	origLookPath := lookPathFunc
	t.Cleanup(func() { lookPathFunc = origLookPath })
	lookPathFunc = func(name string) (string, error) { return "/mock/" + name, nil }
	originalLLM := queryLLMFunc
	defer func() { queryLLMFunc = originalLLM }()

	callCount := 0
	queryLLMFunc = func(vendor, prompt string) (string, error) {
		callCount++
		switch callCount {
		case 1:
			// Step 1: project analysis
			return "- Go project\n- Uses modules\n", nil
		case 2:
			// Step 3: proposals
			return "1. **Skill: `go-dev`** — Dev workflow.\n", nil
		default:
			// Generate artifact
			return "---\nname: go-dev\ndescription: Dev workflow\n---\n\nRun go build.\n", nil
		}
	}

	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, filepath.Join(dir, "go.mod"), []byte("module test\n"))

	err := cmdInspect([]string{"-v", "claude", "-y"})
	if err != nil {
		t.Fatal(err)
	}

	// Verify artifact was written to .claude/
	skillPath := filepath.Join(dir, ".claude", "skills", "go-dev", "SKILL.md")
	if _, statErr := os.Stat(skillPath); os.IsNotExist(statErr) {
		t.Error("expected go-dev skill in .claude/skills/")
	}
}

func TestCmdInspect_Quit(t *testing.T) {
	origLookPath := lookPathFunc
	originalLLM := queryLLMFunc
	originalPrompt := promptActionFunc
	defer func() {
		lookPathFunc = origLookPath
		queryLLMFunc = originalLLM
		promptActionFunc = originalPrompt
	}()
	lookPathFunc = func(name string) (string, error) { return "/mock/" + name, nil }

	queryLLMFunc = func(vendor, prompt string) (string, error) {
		return "- Go project\n", nil
	}
	promptActionFunc = func(msg string, choices ...string) string {
		return "q"
	}

	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, filepath.Join(dir, "go.mod"), []byte("module test\n"))

	err := cmdInspect([]string{"-v", "claude"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCmdInspect_Refine(t *testing.T) {
	origLookPath := lookPathFunc
	originalLLM := queryLLMFunc
	originalPrompt := promptActionFunc
	originalInput := promptInputFunc
	defer func() {
		lookPathFunc = origLookPath
		queryLLMFunc = originalLLM
		promptActionFunc = originalPrompt
		promptInputFunc = originalInput
	}()
	lookPathFunc = func(name string) (string, error) { return "/mock/" + name, nil }

	callCount := 0
	queryLLMFunc = func(vendor, prompt string) (string, error) {
		callCount++
		switch callCount {
		case 1:
			return "- Go project\n", nil
		case 2:
			return "- Refined analysis\n", nil
		default:
			return "No additional artifacts needed.", nil
		}
	}

	promptCount := 0
	promptActionFunc = func(msg string, choices ...string) string {
		promptCount++
		switch promptCount {
		case 1:
			return "r" // refine
		case 2:
			return "c" // continue after refine
		default:
			return "s" // skip proposals
		}
	}
	promptInputFunc = func(msg string) string {
		return "adjust the analysis"
	}

	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, filepath.Join(dir, "go.mod"), []byte("module test\n"))

	err := cmdInspect([]string{"-v", "claude"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCmdInspect_GenerateYes(t *testing.T) {
	origLookPath := lookPathFunc
	originalLLM := queryLLMFunc
	originalPrompt := promptActionFunc
	defer func() {
		lookPathFunc = origLookPath
		queryLLMFunc = originalLLM
		promptActionFunc = originalPrompt
	}()
	lookPathFunc = func(name string) (string, error) { return "/mock/" + name, nil }

	callCount := 0
	queryLLMFunc = func(vendor, prompt string) (string, error) {
		callCount++
		switch callCount {
		case 1:
			return "- Go project\n", nil
		case 2:
			return "1. **Skill: `test-skill`** — A skill.\n", nil
		default:
			return "---\nname: test-skill\ndescription: A skill\n---\n\nBody.\n", nil
		}
	}

	promptCount := 0
	promptActionFunc = func(msg string, choices ...string) string {
		promptCount++
		switch promptCount {
		case 1:
			return "c" // continue past analysis
		case 2:
			return "y" // yes generate all
		default:
			return "s"
		}
	}

	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, filepath.Join(dir, "go.mod"), []byte("module test\n"))

	err := cmdInspect([]string{"-v", "claude"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCmdInspect_Walkthrough(t *testing.T) {
	origLookPath := lookPathFunc
	originalLLM := queryLLMFunc
	originalPrompt := promptActionFunc
	defer func() {
		lookPathFunc = origLookPath
		queryLLMFunc = originalLLM
		promptActionFunc = originalPrompt
	}()
	lookPathFunc = func(name string) (string, error) { return "/mock/" + name, nil }

	callCount := 0
	queryLLMFunc = func(vendor, prompt string) (string, error) {
		callCount++
		switch callCount {
		case 1:
			return "- Go project\n", nil
		case 2:
			return "1. **Skill: `wt-skill`** — A skill.\n2. **Agent: `wt-agent`** — An agent.\n", nil
		default:
			return "---\nname: generated\ndescription: Generated\n---\n\nBody.\n", nil
		}
	}

	promptCount := 0
	promptActionFunc = func(msg string, choices ...string) string {
		promptCount++
		switch promptCount {
		case 1:
			return "c" // continue past analysis
		case 2:
			return "w" // walkthrough
		case 3:
			return "g" // generate first proposal
		case 4:
			return "w" // write it
		case 5:
			return "s" // skip second proposal
		default:
			return "s"
		}
	}

	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, filepath.Join(dir, "go.mod"), []byte("module test\n"))

	err := cmdInspect([]string{"-v", "claude"})
	if err != nil {
		t.Fatal(err)
	}

	// First proposal should have been written
	skillPath := filepath.Join(dir, ".claude", "skills", "wt-skill", "SKILL.md")
	if _, statErr := os.Stat(skillPath); os.IsNotExist(statErr) {
		t.Error("expected wt-skill to be written")
	}
}

func TestCmdInspect_WalkthroughQuit(t *testing.T) {
	origLookPath := lookPathFunc
	originalLLM := queryLLMFunc
	originalPrompt := promptActionFunc
	defer func() {
		lookPathFunc = origLookPath
		queryLLMFunc = originalLLM
		promptActionFunc = originalPrompt
	}()
	lookPathFunc = func(name string) (string, error) { return "/mock/" + name, nil }

	callCount := 0
	queryLLMFunc = func(vendor, prompt string) (string, error) {
		callCount++
		switch callCount {
		case 1:
			return "- Go project\n", nil
		case 2:
			return "1. **Skill: `q-skill`** — A skill.\n", nil
		default:
			return "Body.", nil
		}
	}

	promptCount := 0
	promptActionFunc = func(msg string, choices ...string) string {
		promptCount++
		switch promptCount {
		case 1:
			return "c"
		case 2:
			return "w" // walkthrough
		case 3:
			return "q" // quit
		default:
			return "s"
		}
	}

	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, filepath.Join(dir, "go.mod"), []byte("module test\n"))

	err := cmdInspect([]string{"-v", "claude"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCmdInspect_WalkthroughSkipAfterGenerate(t *testing.T) {
	origLookPath := lookPathFunc
	originalLLM := queryLLMFunc
	originalPrompt := promptActionFunc
	defer func() {
		lookPathFunc = origLookPath
		queryLLMFunc = originalLLM
		promptActionFunc = originalPrompt
	}()
	lookPathFunc = func(name string) (string, error) { return "/mock/" + name, nil }

	callCount := 0
	queryLLMFunc = func(vendor, prompt string) (string, error) {
		callCount++
		switch callCount {
		case 1:
			return "- Go project\n", nil
		case 2:
			return "1. **Skill: `skip-skill`** — A skill.\n", nil
		default:
			return "---\nname: skip-skill\ndescription: Skip\n---\n\nBody.\n", nil
		}
	}

	promptCount := 0
	promptActionFunc = func(msg string, choices ...string) string {
		promptCount++
		switch promptCount {
		case 1:
			return "c"
		case 2:
			return "w" // walkthrough
		case 3:
			return "g" // generate
		case 4:
			return "s" // skip writing
		default:
			return "s"
		}
	}

	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, filepath.Join(dir, "go.mod"), []byte("module test\n"))

	err := cmdInspect([]string{"-v", "claude"})
	if err != nil {
		t.Fatal(err)
	}

	// Should NOT have been written since we skipped
	skillPath := filepath.Join(dir, ".claude", "skills", "skip-skill", "SKILL.md")
	if _, statErr := os.Stat(skillPath); statErr == nil {
		t.Error("expected skill NOT to be written after skip")
	}
}

func TestCmdInspect_WalkthroughLLMError(t *testing.T) {
	origLookPath := lookPathFunc
	originalLLM := queryLLMFunc
	originalPrompt := promptActionFunc
	defer func() {
		lookPathFunc = origLookPath
		queryLLMFunc = originalLLM
		promptActionFunc = originalPrompt
	}()
	lookPathFunc = func(name string) (string, error) { return "/mock/" + name, nil }

	callCount := 0
	queryLLMFunc = func(vendor, prompt string) (string, error) {
		callCount++
		switch callCount {
		case 1:
			return "- Go project\n", nil
		case 2:
			return "1. **Skill: `fail-skill`** — A skill.\n", nil
		default:
			return "", fmt.Errorf("LLM generation failed")
		}
	}

	promptCount := 0
	promptActionFunc = func(msg string, choices ...string) string {
		promptCount++
		switch promptCount {
		case 1:
			return "c"
		case 2:
			return "w" // walkthrough
		case 3:
			return "g" // generate (will fail)
		default:
			return "s"
		}
	}

	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, filepath.Join(dir, "go.mod"), []byte("module test\n"))

	err := cmdInspect([]string{"-v", "claude"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCmdInspect_NoSignals(t *testing.T) {
	origLookPath := lookPathFunc
	t.Cleanup(func() { lookPathFunc = origLookPath })
	lookPathFunc = func(name string) (string, error) { return "/mock/" + name, nil }

	dir := t.TempDir()
	t.Chdir(dir)

	err := cmdInspect([]string{"-v", "claude"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCmdInspect_VendorNotFound(t *testing.T) {
	err := cmdInspect([]string{"-v", "nonexistent-vendor-xyz"})
	if err == nil {
		t.Fatal("expected error for missing vendor")
	}
	if !strings.Contains(err.Error(), "not found on PATH") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCmdInspect_NoVendorAutoDetect(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, filepath.Join(dir, "go.mod"), []byte("module test\n"))

	// Should not error, just print "no CLI found"
	err := cmdInspect(nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestCmdInspect_BadFlag(t *testing.T) {
	err := cmdInspect([]string{"--unknown"})
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
}

func TestCmdInspect_AnalysisFails(t *testing.T) {
	origLookPath := lookPathFunc
	t.Cleanup(func() { lookPathFunc = origLookPath })
	lookPathFunc = func(name string) (string, error) { return "/mock/" + name, nil }
	originalLLM := queryLLMFunc
	defer func() { queryLLMFunc = originalLLM }()

	queryLLMFunc = func(vendor, prompt string) (string, error) {
		return "", fmt.Errorf("analysis failed")
	}

	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, filepath.Join(dir, "go.mod"), []byte("module test\n"))

	err := cmdInspect([]string{"-v", "claude", "-y"})
	if err == nil {
		t.Fatal("expected error when analysis fails")
	}
}

func TestCmdInspect_ProposalsFail(t *testing.T) {
	origLookPath := lookPathFunc
	t.Cleanup(func() { lookPathFunc = origLookPath })
	lookPathFunc = func(name string) (string, error) { return "/mock/" + name, nil }
	originalLLM := queryLLMFunc
	defer func() { queryLLMFunc = originalLLM }()

	callCount := 0
	queryLLMFunc = func(vendor, prompt string) (string, error) {
		callCount++
		if callCount == 1 {
			return "- Go project\n", nil
		}
		return "", fmt.Errorf("proposals failed")
	}

	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, filepath.Join(dir, "go.mod"), []byte("module test\n"))

	err := cmdInspect([]string{"-v", "claude", "-y"})
	if err == nil {
		t.Fatal("expected error when proposals fail")
	}
}

func TestCmdInspect_NoAdditionalArtifacts(t *testing.T) {
	origLookPath := lookPathFunc
	t.Cleanup(func() { lookPathFunc = origLookPath })
	lookPathFunc = func(name string) (string, error) { return "/mock/" + name, nil }
	originalLLM := queryLLMFunc
	defer func() { queryLLMFunc = originalLLM }()

	callCount := 0
	queryLLMFunc = func(vendor, prompt string) (string, error) {
		callCount++
		if callCount == 1 {
			return "- Go project\n", nil
		}
		return "No additional artifacts needed.", nil
	}

	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, filepath.Join(dir, "go.mod"), []byte("module test\n"))

	err := cmdInspect([]string{"-v", "claude", "-y"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestReviewExistingArtifact_ReadError(t *testing.T) {
	err := reviewExistingArtifact("claude", ".", "overview", "/nonexistent/file.md", "skill", false)
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestReviewExistingArtifact_ViewThenUpdate(t *testing.T) {
	origLookPath := lookPathFunc
	originalLLM := queryLLMFunc
	originalPrompt := promptActionFunc
	defer func() {
		lookPathFunc = origLookPath
		queryLLMFunc = originalLLM
		promptActionFunc = originalPrompt
	}()
	lookPathFunc = func(name string) (string, error) { return "/mock/" + name, nil }

	queryLLMFunc = func(vendor, prompt string) (string, error) {
		return "---\nname: test\ndescription: Updated.\n---\n\nUpdated.\n", nil
	}

	promptCount := 0
	promptActionFunc = func(msg string, choices ...string) string {
		promptCount++
		switch promptCount {
		case 1:
			return "v" // view
		case 2:
			return "u" // update (after viewing)
		default:
			return "s" // skip the proposed update
		}
	}

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), []byte("module test\n"))
	mkdirAll(t, filepath.Join(dir, "skills", "test"))
	path := filepath.Join(dir, "skills", "test", "SKILL.md")
	writeFile(t, path, []byte("---\nname: test\ndescription: Orig.\n---\nBody.\n"))

	err := reviewExistingArtifact("claude", dir, "overview", path, "skill", false)
	if err != nil {
		t.Fatal(err)
	}
}

func TestCmdInspect_EmptyProposals(t *testing.T) {
	origLookPath := lookPathFunc
	t.Cleanup(func() { lookPathFunc = origLookPath })
	lookPathFunc = func(name string) (string, error) { return "/mock/" + name, nil }
	originalLLM := queryLLMFunc
	defer func() { queryLLMFunc = originalLLM }()

	callCount := 0
	queryLLMFunc = func(vendor, prompt string) (string, error) {
		callCount++
		if callCount == 1 {
			return "- Go project\n", nil
		}
		return "", nil // empty proposals
	}

	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, filepath.Join(dir, "go.mod"), []byte("module test\n"))

	err := cmdInspect([]string{"-v", "claude", "-y"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCmdInspect_SkipProposals(t *testing.T) {
	origLookPath := lookPathFunc
	originalLLM := queryLLMFunc
	originalPrompt := promptActionFunc
	defer func() {
		lookPathFunc = origLookPath
		queryLLMFunc = originalLLM
		promptActionFunc = originalPrompt
	}()
	lookPathFunc = func(name string) (string, error) { return "/mock/" + name, nil }

	callCount := 0
	queryLLMFunc = func(vendor, prompt string) (string, error) {
		callCount++
		if callCount == 1 {
			return "- Go project\n", nil
		}
		return "1. **Skill: `test`** — Test.\n", nil
	}

	promptCount := 0
	promptActionFunc = func(msg string, choices ...string) string {
		promptCount++
		switch promptCount {
		case 1:
			return "c" // continue
		default:
			return "s" // skip proposals
		}
	}

	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, filepath.Join(dir, "go.mod"), []byte("module test\n"))

	err := cmdInspect([]string{"-v", "claude"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCmdInspect_WithExistingArtifacts(t *testing.T) {
	origLookPath := lookPathFunc
	originalLLM := queryLLMFunc
	originalPrompt := promptActionFunc
	defer func() {
		lookPathFunc = origLookPath
		queryLLMFunc = originalLLM
		promptActionFunc = originalPrompt
	}()
	lookPathFunc = func(name string) (string, error) { return "/mock/" + name, nil }

	callCount := 0
	queryLLMFunc = func(vendor, prompt string) (string, error) {
		callCount++
		switch callCount {
		case 1:
			return "- Go project\n", nil
		default:
			return "No additional artifacts needed.", nil
		}
	}

	promptActionFunc = func(msg string, choices ...string) string {
		return "c" // continue / skip through everything
	}

	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, filepath.Join(dir, "go.mod"), []byte("module test\n"))

	// Create existing artifacts
	mkdirAll(t, filepath.Join(dir, "skills", "existing"))
	writeFile(t, filepath.Join(dir, "skills", "existing", "SKILL.md"), []byte("---\nname: existing\ndescription: Existing.\n---\nBody.\n"))
	mkdirAll(t, filepath.Join(dir, "agents"))
	writeFile(t, filepath.Join(dir, "agents", "reviewer.md"), []byte("---\nname: reviewer\ndescription: Reviews.\ntools: Read\n---\nBody.\n"))

	err := cmdInspect([]string{"-v", "claude"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestReviewExistingArtifact_Skip(t *testing.T) {
	originalPrompt := promptActionFunc
	defer func() { promptActionFunc = originalPrompt }()

	promptActionFunc = func(msg string, choices ...string) string {
		return "s"
	}

	dir := t.TempDir()
	mkdirAll(t, filepath.Join(dir, "skills", "test"))
	path := filepath.Join(dir, "skills", "test", "SKILL.md")
	writeFile(t, path, []byte("---\nname: test\ndescription: A test.\n---\nBody.\n"))

	err := reviewExistingArtifact("claude", dir, "overview", path, "skill", false)
	if err != nil {
		t.Fatal(err)
	}
}

func TestReviewExistingArtifact_View(t *testing.T) {
	originalPrompt := promptActionFunc
	defer func() { promptActionFunc = originalPrompt }()

	promptCount := 0
	promptActionFunc = func(msg string, choices ...string) string {
		promptCount++
		switch promptCount {
		case 1:
			return "v" // view
		default:
			return "s" // skip after viewing
		}
	}

	dir := t.TempDir()
	mkdirAll(t, filepath.Join(dir, "skills", "test"))
	path := filepath.Join(dir, "skills", "test", "SKILL.md")
	writeFile(t, path, []byte("---\nname: test\ndescription: A test.\n---\nBody.\n"))

	err := reviewExistingArtifact("claude", dir, "overview", path, "skill", false)
	if err != nil {
		t.Fatal(err)
	}
}

func TestReviewExistingArtifact_Update(t *testing.T) {
	origLookPath := lookPathFunc
	originalLLM := queryLLMFunc
	originalPrompt := promptActionFunc
	defer func() {
		lookPathFunc = origLookPath
		queryLLMFunc = originalLLM
		promptActionFunc = originalPrompt
	}()
	lookPathFunc = func(name string) (string, error) { return "/mock/" + name, nil }

	queryLLMFunc = func(vendor, prompt string) (string, error) {
		return "---\nname: test\ndescription: Updated.\n---\n\nUpdated body.\n", nil
	}

	promptCount := 0
	promptActionFunc = func(msg string, choices ...string) string {
		promptCount++
		switch promptCount {
		case 1:
			return "u" // update
		default:
			return "a" // apply
		}
	}

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), []byte("module test\n"))
	mkdirAll(t, filepath.Join(dir, "skills", "test"))
	path := filepath.Join(dir, "skills", "test", "SKILL.md")
	writeFile(t, path, []byte("---\nname: test\ndescription: Original.\n---\nOriginal body.\n"))

	err := reviewExistingArtifact("claude", dir, "overview", path, "skill", false)
	if err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "Updated") {
		t.Error("expected file to be updated")
	}
}

func TestReviewExistingArtifact_SkipConfirm(t *testing.T) {
	dir := t.TempDir()
	mkdirAll(t, filepath.Join(dir, "agents"))
	path := filepath.Join(dir, "agents", "test.md")
	writeFile(t, path, []byte("---\nname: test\ndescription: A test.\ntools: Read\n---\nBody.\n"))

	err := reviewExistingArtifact("claude", dir, "overview", path, "agent", true)
	if err != nil {
		t.Fatal(err)
	}
}

func TestCmdInspect_OutputDir(t *testing.T) {
	origLookPath := lookPathFunc
	t.Cleanup(func() { lookPathFunc = origLookPath })
	lookPathFunc = func(name string) (string, error) { return "/mock/" + name, nil }
	originalLLM := queryLLMFunc
	defer func() { queryLLMFunc = originalLLM }()

	callCount := 0
	queryLLMFunc = func(vendor, prompt string) (string, error) {
		callCount++
		switch callCount {
		case 1:
			return "- Go project\n", nil
		case 2:
			return "1. **Skill: `out-skill`** — A skill.\n", nil
		default:
			return "---\nname: out-skill\ndescription: A skill\n---\n\nBody.\n", nil
		}
	}

	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, filepath.Join(dir, "go.mod"), []byte("module test\n"))
	outputDir := filepath.Join(dir, "custom-output")

	err := cmdInspect([]string{"-v", "claude", "-y", "-o", outputDir})
	if err != nil {
		t.Fatal(err)
	}

	// Should be in custom output dir, not .claude/
	skillPath := filepath.Join(outputDir, "skills", "out-skill", "SKILL.md")
	if _, statErr := os.Stat(skillPath); os.IsNotExist(statErr) {
		t.Error("expected skill in custom output dir")
	}
}

func TestExtractDescription_LongDescription(t *testing.T) {
	// No sentence break — should truncate to 120 chars
	long := strings.Repeat("word ", 50)
	line := "1. **Skill: `foo`** — " + long
	got := extractDescription(line)
	if len(got) > 120 {
		t.Errorf("description too long: %d chars", len(got))
	}
}

func TestExtractDescription_LongWithSentenceBreak(t *testing.T) {
	long := strings.Repeat("word ", 20) + "sentence end. " + strings.Repeat("more ", 20)
	line := "1. **Skill: `foo`** — " + long
	got := extractDescription(line)
	// Should truncate at first sentence
	if strings.Contains(got, "more") {
		t.Errorf("expected truncation at sentence break, got %q", got)
	}
}

func TestExtractDescription_EmDashNoSpaces(t *testing.T) {
	line := "1. **Skill: foo**—Does things"
	got := extractDescription(line)
	if got != "Does things" {
		t.Errorf("got %q, want %q", got, "Does things")
	}
}

func TestEnsureFrontmatter_WrappedInFence(t *testing.T) {
	p := proposal{Type: "skill", Name: "my-skill", Line: "1. **Skill: `my-skill`** — Does things."}
	raw := "```markdown\n---\nname: my-skill\ndescription: Does things\n---\n\nBody.\n```\n"
	got := ensureFrontmatter(raw, p)
	if !strings.HasPrefix(got, "---\nname: my-skill\n") {
		t.Errorf("expected correct frontmatter, got %q", got[:50])
	}
	if strings.Contains(got, "```") {
		t.Error("expected fences stripped")
	}
}

func TestEnsureFrontmatter_EmptyBody(t *testing.T) {
	p := proposal{Type: "skill", Name: "test", Line: "1. **Skill: test** — A test."}
	raw := "---\nname: test\ndescription: A test\n---\n"
	got := ensureFrontmatter(raw, p)
	if !strings.HasPrefix(got, "---\nname: test\n") {
		t.Errorf("expected frontmatter, got %q", got)
	}
}

func TestDiscoverExistingAgents_SkipsNonMarkdown(t *testing.T) {
	dir := t.TempDir()
	mkdirAll(t, filepath.Join(dir, "agents"))
	writeFile(t, filepath.Join(dir, "agents", "reviewer.md"), []byte("---\nname: reviewer\n---\n"))
	writeFile(t, filepath.Join(dir, "agents", "ignore.txt"), []byte("not markdown"))

	agents := discoverExistingAgents(dir)
	if len(agents) != 1 {
		t.Errorf("found %d agents, want 1", len(agents))
	}
}

func TestGenerateAllProposals(t *testing.T) {
	origLookPath2 := lookPathFunc
	t.Cleanup(func() { lookPathFunc = origLookPath2 })
	lookPathFunc = func(name string) (string, error) { return "/mock/" + name, nil }
	original := queryLLMFunc
	defer func() { queryLLMFunc = original }()

	queryLLMFunc = func(vendor, prompt string) (string, error) {
		return "---\nname: go-dev\ndescription: Dev workflow\n---\n\n## Instructions\n\nRun go build.\n", nil
	}

	dir := t.TempDir()
	proposals := "1. **Skill: `go-dev`** — Dev workflow for Go projects.\n2. **Agent: `go-review`** — Code reviewer for Go.\n"
	overview := "Go project with go.mod."

	err := generateAllProposals("claude", overview, proposals, dir, dir)
	if err != nil {
		t.Fatal(err)
	}

	// Verify skill was written
	skillPath := filepath.Join(dir, "skills", "go-dev", "SKILL.md")
	if _, err := os.Stat(skillPath); os.IsNotExist(err) {
		t.Error("expected go-dev skill to be created")
	}

	// Verify agent was written
	agentPath := filepath.Join(dir, "agents", "go-review.md")
	if _, err := os.Stat(agentPath); os.IsNotExist(err) {
		t.Error("expected go-review agent to be created")
	}
}

func TestGenerateAllProposals_NoProposals(t *testing.T) {
	err := generateAllProposals("claude", "overview", "No additional artifacts needed.", ".", ".")
	if err != nil {
		t.Fatal(err)
	}
	// Should print "Could not parse" but not error
}

func TestGenerateAllProposals_LLMError(t *testing.T) {
	origLookPath2 := lookPathFunc
	t.Cleanup(func() { lookPathFunc = origLookPath2 })
	lookPathFunc = func(name string) (string, error) { return "/mock/" + name, nil }
	original := queryLLMFunc
	defer func() { queryLLMFunc = original }()

	queryLLMFunc = func(vendor, prompt string) (string, error) {
		return "", fmt.Errorf("LLM failed")
	}

	dir := t.TempDir()
	proposals := "1. **Skill: `test-skill`** — Test.\n"

	// Should not return error, just print failure per-proposal
	err := generateAllProposals("claude", "overview", proposals, dir, dir)
	if err != nil {
		t.Fatal(err)
	}
}

func TestAnalyzeProject(t *testing.T) {
	origLookPath2 := lookPathFunc
	t.Cleanup(func() { lookPathFunc = origLookPath2 })
	lookPathFunc = func(name string) (string, error) { return "/mock/" + name, nil }
	original := queryLLMFunc
	defer func() { queryLLMFunc = original }()

	queryLLMFunc = func(vendor, prompt string) (string, error) {
		return "- Go project\n- Uses go modules\n", nil
	}

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), []byte("module test\n"))
	signals := scanSignals(dir)

	result, err := analyzeProject("claude", dir, signals)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "Go project") {
		t.Errorf("expected analysis, got %q", result)
	}
}

func TestRefineAnalysis(t *testing.T) {
	origLookPath2 := lookPathFunc
	t.Cleanup(func() { lookPathFunc = origLookPath2 })
	lookPathFunc = func(name string) (string, error) { return "/mock/" + name, nil }
	original := queryLLMFunc
	defer func() { queryLLMFunc = original }()

	queryLLMFunc = func(vendor, prompt string) (string, error) {
		return "- Updated analysis\n", nil
	}

	result, err := refineAnalysis("claude", "original", "fix this")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "Updated") {
		t.Errorf("expected refined analysis, got %q", result)
	}
}

func TestProposeNewArtifacts(t *testing.T) {
	origLookPath2 := lookPathFunc
	t.Cleanup(func() { lookPathFunc = origLookPath2 })
	lookPathFunc = func(name string) (string, error) { return "/mock/" + name, nil }
	original := queryLLMFunc
	defer func() { queryLLMFunc = original }()

	queryLLMFunc = func(vendor, prompt string) (string, error) {
		return "1. **Skill: `go-test`** — Run tests.\n", nil
	}

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), []byte("module test\n"))
	signals := scanSignals(dir)

	result, err := proposeNewArtifacts("claude", dir, "Go project", signals, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "go-test") {
		t.Errorf("expected proposals, got %q", result)
	}
}

func TestGenerateSingleArtifact(t *testing.T) {
	origLookPath2 := lookPathFunc
	t.Cleanup(func() { lookPathFunc = origLookPath2 })
	lookPathFunc = func(name string) (string, error) { return "/mock/" + name, nil }
	original := queryLLMFunc
	defer func() { queryLLMFunc = original }()

	queryLLMFunc = func(vendor, prompt string) (string, error) {
		return "---\nname: test\ndescription: Test skill.\n---\n\nInstructions.\n", nil
	}

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), []byte("module test\n"))

	result, err := generateSingleArtifact("claude", "Go project", "1. **Skill: test** — Test.", dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "Instructions") {
		t.Errorf("expected generated content, got %q", result)
	}
}

func TestProposeNewArtifacts_WithExisting(t *testing.T) {
	origLookPath2 := lookPathFunc
	t.Cleanup(func() { lookPathFunc = origLookPath2 })
	lookPathFunc = func(name string) (string, error) { return "/mock/" + name, nil }
	original := queryLLMFunc
	defer func() { queryLLMFunc = original }()

	queryLLMFunc = func(vendor, prompt string) (string, error) {
		// Verify the prompt includes existing artifacts
		if !strings.Contains(prompt, "skill:") || !strings.Contains(prompt, "SKILL.md") {
			return "", fmt.Errorf("expected existing skill in prompt, got:\n%s", prompt)
		}
		if !strings.Contains(prompt, "agent:") || !strings.Contains(prompt, "reviewer.md") {
			return "", fmt.Errorf("expected existing agent in prompt, got:\n%s", prompt)
		}
		return "1. **Skill: `go-test`** — Run tests.\n", nil
	}

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), []byte("module test\n"))
	signals := scanSignals(dir)

	// Use full paths under root so filepath.Rel works
	existingSkills := []string{filepath.Join(dir, "skills", "build", "SKILL.md")}
	existingAgents := []string{filepath.Join(dir, "agents", "reviewer.md")}

	result, err := proposeNewArtifacts("claude", dir, "Go project", signals, existingSkills, existingAgents)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "go-test") {
		t.Errorf("expected proposals, got %q", result)
	}
}

func TestGenerateAllProposals_AlreadyExists(t *testing.T) {
	origLookPath2 := lookPathFunc
	t.Cleanup(func() { lookPathFunc = origLookPath2 })
	lookPathFunc = func(name string) (string, error) { return "/mock/" + name, nil }
	original := queryLLMFunc
	defer func() { queryLLMFunc = original }()

	queryLLMFunc = func(vendor, prompt string) (string, error) {
		return "---\nname: existing\ndescription: Test.\n---\n\nBody.\n", nil
	}

	dir := t.TempDir()
	// Pre-create the skill so writeArtifact fails with "already exists"
	mkdirAll(t, filepath.Join(dir, "skills", "existing"))
	writeFile(t, filepath.Join(dir, "skills", "existing", "SKILL.md"), []byte("existing content"))

	proposals := "1. **Skill: `existing`** — Already exists.\n"
	// Should not error, just print write failure
	err := generateAllProposals("claude", "overview", proposals, dir, dir)
	if err != nil {
		t.Fatal(err)
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
