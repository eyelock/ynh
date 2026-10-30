package main

import (
	"bytes"
	"encoding/json"
	"os"
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

	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
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
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
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

	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
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

	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
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
		writeFile(t, filepath.Join(dir, name, ".agents/harness", "plugin.json"),
			[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"`+name+`","version":"0.1.0"}`))
	}

	mkdirAll(t, filepath.Join(dir, "not-a-harness"))

	roots, _ := findHarnessRoots(dir)
	if len(roots) != 2 {
		t.Errorf("found %d harness roots, want 2", len(roots))
	}
}

func TestFindHarnessRoots_SelfIsHarness(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".agents/harness", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"self","version":"0.1.0"}`))

	roots, _ := findHarnessRoots(dir)
	if len(roots) != 1 {
		t.Errorf("found %d harness roots, want 1", len(roots))
	}
}

func TestIsHarnessRoot(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".agents/harness", "plugin.json"),
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
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
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

	writeFile(t, filepath.Join(dir, ".agents/harness", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"self","version":"0.1.0"}`))

	err := cmdValidate(nil)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestCmdValidate_SingleFile_PluginJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".agents/harness", "plugin.json")
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
	path := filepath.Join(dir, ".agents/harness", "plugin.json")
	writeFile(t, path, []byte(`{}`))

	err := cmdValidate([]string{path})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestCmdValidate_HarnessFlag(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".agents/harness", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"flag-test","version":"0.1.0"}`))

	err := cmdValidate([]string{"--harness", dir})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestCmdValidate_HarnessEnvVar(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".agents/harness", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"env-test","version":"0.1.0"}`))

	t.Setenv("YNH_HARNESS", dir)

	err := cmdValidate(nil)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestCmdValidate_HarnessFlagOverridesEnv(t *testing.T) {
	goodDir := t.TempDir()
	writeFile(t, filepath.Join(goodDir, ".agents/harness", "plugin.json"),
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
		writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
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
	path := filepath.Join(dir, ".agents/harness", "plugin.json")
	writeFile(t, path, []byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"test","version":"0.1.0"}`))

	err := validateFile(path)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidateFile_PluginJSON_Invalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".agents/harness", "plugin.json")
	writeFile(t, path, []byte(`{}`))

	err := validateFile(path)
	if err == nil {
		t.Fatal("expected validation error")
	}
}

// agent.effort takes one of the three neutral levels; a vendor's own word
// such as "max" is not one of them.
func TestValidateFile_PluginJSON_AgentEffort(t *testing.T) {
	for _, tt := range []struct {
		effort  string
		wantErr bool
	}{
		{"low", false},
		{"medium", false},
		{"high", false},
		{"max", true},
		{"minimal", true},
		{"", true},
	} {
		t.Run("effort="+tt.effort, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".agents/harness", "plugin.json")
			writeFile(t, path, []byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"test","version":"0.1.0",`+
				`"agent":{"max_turns":10,"effort":"`+tt.effort+`"}}`))
			err := validateFile(path)
			if tt.wantErr && err == nil {
				t.Fatalf("agent.effort %q validated, want a schema error", tt.effort)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("agent.effort %q: %v", tt.effort, err)
			}
		})
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
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"), []byte(`{not json}`))

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
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
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
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
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
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
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
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
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
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
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
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
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
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
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
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
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
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
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
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
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
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
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
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
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
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
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
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
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
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
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
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"hooks-bad-event","version":"0.1.0","hooks":{"unknown_event":[{"command":"echo hi"}]}}`))

	err := validateHarness(hr)
	if err == nil {
		t.Fatal("expected validation error for unknown hook event")
	}
}

func TestHookEventMessage(t *testing.T) {
	tests := []struct {
		event       string
		wantContain string
	}{
		{"PreToolUse", "before_tool"},
		{"PostToolUse", "after_tool"},
		{"afterFileEdit", "after_tool"},
		{"stop", "on_stop"},
		{"totally_made_up", "unknown event"},
	}
	for _, tt := range tests {
		t.Run(tt.event, func(t *testing.T) {
			got := hookEventMessage(tt.event)
			if !strings.Contains(got, tt.wantContain) {
				t.Errorf("hookEventMessage(%q) = %q, want contains %q", tt.event, got, tt.wantContain)
			}
			if _, isVendor := vendorHookEventNames[tt.event]; isVendor && !strings.Contains(got, "vendor-native") {
				t.Errorf("vendor name %q should yield a vendor-native hint, got %q", tt.event, got)
			}
		})
	}
}

func TestValidateHarness_HooksEmptyCommand(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "hooks-empty-cmd")
	mkdirAll(t, hr)
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
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
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
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
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
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
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
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
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
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
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
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
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
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
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
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
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
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
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
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
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"focus-valid","version":"0.1.0","profiles":{"ci":{}},"focuses":{"review":{"profile":"ci","prompt":"Review code"}}}`))

	if err := validateHarness(hr); err != nil {
		t.Errorf("valid focus should pass: %v", err)
	}
}

func TestValidateHarness_FocusMissingPrompt(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "focus-no-prompt")
	mkdirAll(t, hr)
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"focus-no-prompt","version":"0.1.0","focuses":{"review":{"profile":"ci"}}}`))

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
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"focus-bad-profile","version":"0.1.0","focuses":{"review":{"profile":"nonexistent","prompt":"Review code"}}}`))

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
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"focus-no-profile","version":"0.1.0","focuses":{"docs":{"prompt":"Generate docs"}}}`))

	if err := validateHarness(hr); err != nil {
		t.Errorf("focus without profile ref should pass: %v", err)
	}
}

func TestValidateHarness_AgentsMDOnly(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	hr := filepath.Join(dir, "agents-only")
	mkdirAll(t, hr)
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
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
	pluginDir := filepath.Join(dir, ".agents/harness")
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
	pluginDir := filepath.Join(dir, ".agents/harness")
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
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"my-harness","version":"0.1.0"}`))
	// Create an invalid root marketplace.json (missing $schema)
	writeFile(t, filepath.Join(dir, ".agents/harness", "marketplace.json"),
		[]byte(`{"name":"my-registry","owner":{"name":"me"},"harnesses":[{"name":"t","source":"./t"}]}`))

	if err := cmdValidate([]string{dir}); err == nil {
		t.Error("expected error: root marketplace.json is missing $schema")
	}
}

func TestValidateDir_ValidRootMarketplace(t *testing.T) {
	dir := t.TempDir()
	// Create a valid harness sub-directory
	hr := filepath.Join(dir, "my-harness")
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"my-harness","version":"0.1.0"}`))
	// Create a valid root marketplace.json
	writeFile(t, filepath.Join(dir, ".agents/harness", "marketplace.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/marketplace.schema.json","name":"my-registry","owner":{"name":"me"},"harnesses":[{"name":"t","source":"./t"}]}`))

	if err := cmdValidate([]string{dir}); err != nil {
		t.Errorf("expected valid, got: %v", err)
	}
}

func TestValidateDir_NoRootMarketplace(t *testing.T) {
	dir := t.TempDir()
	// Directory with harness but no root marketplace.json — should still pass
	hr := filepath.Join(dir, "my-harness")
	writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"my-harness","version":"0.1.0"}`))

	if err := cmdValidate([]string{dir}); err != nil {
		t.Errorf("expected valid (no marketplace.json is fine), got: %v", err)
	}
}

func TestValidateHarnessSensors_Valid(t *testing.T) {
	hj := map[string]any{
		"focuses": map[string]any{
			"infer-vulns": map[string]any{"prompt": "find vulns"},
		},
		"profiles": map[string]any{
			"ci": map[string]any{},
		},
		"sensors": map[string]any{
			"build": map[string]any{
				"category": "maintainability",
				"source":   map[string]any{"command": "make check"},
				"output":   map[string]any{"format": "text"},
			},
			"sec": map[string]any{
				"source": map[string]any{"focus": "infer-vulns"},
				"output": map[string]any{"format": "markdown"},
			},
			"inline": map[string]any{
				"source": map[string]any{
					"focus": map[string]any{"profile": "ci", "prompt": "judge"},
				},
				"output": map[string]any{"format": "markdown"},
			},
		},
	}
	if issues := validateHarnessSensors(hj); len(issues) != 0 {
		t.Errorf("expected no issues, got %v", issues)
	}
}

func TestValidateHarnessSensors_Issues(t *testing.T) {
	hj := map[string]any{
		"sensors": map[string]any{
			"two-source": map[string]any{
				"source": map[string]any{
					"command": "x",
					"files":   []any{"a"},
				},
				"output": map[string]any{"format": "text"},
			},
			"missing-format": map[string]any{
				"source": map[string]any{"command": "x"},
				"output": map[string]any{"format": ""},
			},
			"unknown-focus": map[string]any{
				"source": map[string]any{"focus": "missing"},
				"output": map[string]any{"format": "markdown"},
			},
			"bad-category": map[string]any{
				"category": "wrong",
				"source":   map[string]any{"command": "x"},
				"output":   map[string]any{"format": "text"},
			},
			"inline-bad-profile": map[string]any{
				"source": map[string]any{
					"focus": map[string]any{"profile": "missing", "prompt": "x"},
				},
				"output": map[string]any{"format": "markdown"},
			},
		},
	}
	issues := validateHarnessSensors(hj)
	wants := []string{
		`"two-source": source must have exactly one`,
		`"missing-format": output.format must not be empty`,
		`"unknown-focus": source.focus references undefined focus "missing"`,
		`"bad-category": category "wrong"`,
		`"inline-bad-profile": source.focus references unknown profile "missing"`,
	}
	for _, want := range wants {
		found := false
		for _, got := range issues {
			if strings.Contains(got, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing expected issue %q in %v", want, issues)
		}
	}
}

// The defect: a manifest referencing ${VAR} it never declared passed validate
// and lint, then failed at assembly. Worst under automation — an unattended
// `ynh agent run` does its setup and dies on a config error CI could have
// caught.
func TestValidate_CatchesUndeclaredMCPEnvRef(t *testing.T) {
	dir := writeValidateHarness(t, `{
      "$schema": "https://ynh.sh/schema/plugin.schema.json",
      "name": "a", "version": "0.1.0", "default_vendor": "claude",
      "env_passthrough": ["KNOWN"],
      "mcp_servers": {"docs": {"url": "https://x", "headers": {"Authorization": "Bearer ${MISSING}"}}}
    }`)
	if err := validateHarness(dir); err == nil {
		t.Fatal("an undeclared ${VAR} must fail validation when an allowlist exists")
	}
	// validateHarness prints detail and returns only a count, so assert the
	// message on the rule itself — a report that says "1 issue" without
	// naming the variable is not actionable.
	data, err := os.ReadFile(filepath.Join(dir, ".agents/harness", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	issues := validateMCPEnvDeclarations(data)
	if len(issues) != 1 {
		t.Fatalf("got %v", issues)
	}
	if !strings.Contains(issues[0], "${MISSING}") || !strings.Contains(issues[0], "env_passthrough") {
		t.Errorf("the issue must name the variable and the field, got: %s", issues[0])
	}
}

// The counter-example that shaped the design. `ynd export` deliberately leaves
// ${VAR} literal — resolving there would bake the exporter's credentials into
// a shared bundle — and strips env_passthrough from the artifact entirely. A
// harness authored purely for distribution therefore works today, and must not
// be reddened for an allowlist that never reaches what it ships.
func TestValidate_DistributionOnlyHarnessIsNotFlagged(t *testing.T) {
	dir := writeValidateHarness(t, `{
      "$schema": "https://ynh.sh/schema/plugin.schema.json",
      "name": "c", "version": "0.1.0", "default_vendor": "claude",
      "mcp_servers": {"docs": {"url": "https://x", "headers": {"Authorization": "Bearer ${DOCS_API_KEY}"}}}
    }`)
	if err := validateHarness(dir); err != nil {
		t.Fatalf("a harness declaring no env_passthrough may be distribution-only: %v", err)
	}
}

// A complete allowlist must pass, or the check is just noise.
func TestValidate_CompleteAllowlistPasses(t *testing.T) {
	dir := writeValidateHarness(t, `{
      "$schema": "https://ynh.sh/schema/plugin.schema.json",
      "name": "b", "version": "0.1.0", "default_vendor": "claude",
      "env_passthrough": ["TOKEN"],
      "mcp_servers": {"docs": {"url": "https://x", "headers": {"Authorization": "Bearer ${TOKEN}"}}}
    }`)
	if err := validateHarness(dir); err != nil {
		t.Fatalf("a complete allowlist must validate: %v", err)
	}
}

// A profile may replace the allowlist, so its servers are checked against the
// one actually in force for it.
func TestValidate_ProfileAllowlistIsHonoured(t *testing.T) {
	dir := writeValidateHarness(t, `{
      "$schema": "https://ynh.sh/schema/plugin.schema.json",
      "name": "d", "version": "0.1.0", "default_vendor": "claude",
      "env_passthrough": ["ROOT_TOKEN"],
      "profiles": {"ci": {
        "env_passthrough": ["CI_TOKEN"],
        "mcp_servers": {"docs": {"url": "https://x", "headers": {"Authorization": "Bearer ${ROOT_TOKEN}"}}}
      }}
    }`)
	if err := validateHarness(dir); err == nil {
		t.Fatal("a profile that replaces the allowlist no longer permits the root's variables")
	}
	data, err := os.ReadFile(filepath.Join(dir, ".agents/harness", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	issues := validateMCPEnvDeclarations(data)
	if len(issues) != 1 || !strings.Contains(issues[0], "${ROOT_TOKEN}") {
		t.Errorf("got %v", issues)
	}
}

// writeValidateHarness writes a manifest into a temp harness dir.
func writeValidateHarness(t *testing.T, manifest string) string {
	t.Helper()
	dir := t.TempDir()
	pd := filepath.Join(dir, ".agents/harness")
	if err := os.MkdirAll(pd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pd, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// The validator must enforce the traversal rule the resolver enforces.
//
// A `local` include may not point above the harness directory. The resolver
// refused one at assembly time; the validator did not — so a manifest passed
// `ynd validate` and then failed at `ynd compose` and `ynd preview`. Since
// validate is what authors are told to run and what `make check-artifacts`
// gates on, that moved the error from authoring time to run time, which is the
// failure mode validate exists to prevent.
func TestValidateHarnessIncludes(t *testing.T) {
	cases := []struct {
		name      string
		includes  string
		wantIssue bool
	}{
		{"local escaping upward", `[{"local": "../shared"}]`, true},
		{"local escaping further", `[{"local": "../../x"}]`, true},
		{"local absolute", `[{"local": "/etc"}]`, true},
		{"local subdirectory", `[{"local": "shared"}]`, false},
		{"local nested subdirectory", `[{"local": "vendor/shared"}]`, false},
		{"path escaping upward", `[{"git": "https://x/y", "path": "../out"}]`, true},
		{"path subdirectory", `[{"git": "https://x/y", "path": "skills"}]`, false},
		{"git only", `[{"git": "https://x/y"}]`, false},
		{"no includes", ``, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := `{"name":"h","version":"0.1.0"`
			if c.includes != "" {
				src += `,"includes":` + c.includes
			}
			src += `}`
			var hj map[string]any
			if err := json.Unmarshal([]byte(src), &hj); err != nil {
				t.Fatalf("fixture is not valid JSON: %v", err)
			}
			issues := validateHarnessIncludes(hj)
			if c.wantIssue && len(issues) == 0 {
				t.Errorf("expected an issue for %s, got none", c.includes)
			}
			if !c.wantIssue && len(issues) > 0 {
				t.Errorf("unexpected issues for %s: %v", c.includes, issues)
			}
		})
	}
}

// And the check must actually be wired into validateHarness.
//
// A unit test on validateHarnessIncludes alone would still pass if the call
// site were deleted — which is exactly the state this issue reported, a rule
// that existed in the resolver and was never consulted by the validator.
func TestValidateHarness_RejectsTraversingLocalInclude(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".agents/harness", "plugin.json"), []byte(`{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "app",
  "version": "0.1.0",
  "default_vendor": "claude",
  "includes": [{"local": "../shared"}]
}`))
	if err := validateHarness(dir); err == nil {
		t.Error("a local include traversing above the harness should fail validation")
	}

	// And the legal form still passes, so the check is not simply refusing all
	// local includes.
	writeFile(t, filepath.Join(dir, ".agents/harness", "plugin.json"), []byte(`{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "app",
  "version": "0.1.0",
  "default_vendor": "claude",
  "includes": [{"local": "shared"}]
}`))
	if err := validateHarness(dir); err != nil {
		t.Errorf("a local include inside the harness should validate: %v", err)
	}
}

// Only a command sensor's verdict is an exit code about the tree it was
// pointed at, so only a command sensor can answer the question calibration
// asks. A validator in internal/plugin once held this rule and had no
// production caller, so nothing enforced it here — and --calibrate did not
// skip such a sensor. It ran it and reported "the sensor passed a fixture
// built to trip it — it is no longer observing", failing the run with a false
// accusation about a healthy sensor.
func TestValidateHarnessSensors_ReferenceRequiresCommand(t *testing.T) {
	ref := map[string]any{"path": "fixture", "expect": "fail"}
	out := map[string]any{"format": "text"}

	rejected := map[string]map[string]any{
		"files-with-ref":  {"files": []any{"a.json"}},
		"focus-with-ref":  {"focus": map[string]any{"prompt": "p"}},
		"status-with-ref": {"github_status": map[string]any{"context": "c"}},
		"check-with-ref":  {"github_check": map[string]any{"name": "n"}},
	}
	for name, src := range rejected {
		t.Run(name, func(t *testing.T) {
			issues := validateHarnessSensors(map[string]any{
				"sensors": map[string]any{
					"s": map[string]any{"source": src, "output": out, "reference": ref},
				},
			})
			var found bool
			for _, i := range issues {
				if strings.Contains(i, "cannot declare a reference") {
					found = true
				}
			}
			if !found {
				t.Errorf("expected a reference refusal, got: %v", issues)
			}
		})
	}

	t.Run("command-with-ref is the one that is allowed", func(t *testing.T) {
		issues := validateHarnessSensors(map[string]any{
			"sensors": map[string]any{
				"s": map[string]any{
					"source": map[string]any{"command": "true"}, "output": out, "reference": ref,
				},
			},
		})
		for _, i := range issues {
			if strings.Contains(i, "cannot declare a reference") {
				t.Errorf("a command sensor must keep its fixture: %v", issues)
			}
		}
	})

	t.Run("no reference is never refused", func(t *testing.T) {
		for name, src := range rejected {
			issues := validateHarnessSensors(map[string]any{
				"sensors": map[string]any{"s": map[string]any{"source": src, "output": out}},
			})
			for _, i := range issues {
				if strings.Contains(i, "cannot declare a reference") {
					t.Errorf("%s without a reference must be accepted: %v", name, issues)
				}
			}
		}
	})
}

// "valid" is never a bare claim: the report lists each part it checked and what it found,
// with "none" for parts the harness does not have, so the reader can see they were looked at.
func TestValidateHarness_ReportsWhatWasChecked(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	hr := filepath.Join(dir, "h")
	mkdirAll(t, filepath.Join(hr, "skills", "hello"))
	writeFile(t, filepath.Join(hr, ".agents", "harness", "plugin.json"), []byte(`{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "h", "version": "0.1.0",
  "includes": [{"git": "https://github.com/eyelock/ynm", "pick": ["skills/ynm-memory"]}],
  "mcp_servers": {"ynm": {"command": "ynm", "args": ["serve"]}},
  "hooks": {"on_stop": [{"command": "ynm hook stop"}]}
}`))
	writeFile(t, filepath.Join(hr, "skills", "hello", "SKILL.md"),
		[]byte("---\nname: hello\ndescription: Say hello\n---\n\nHello.\n"))

	var buf bytes.Buffer
	withStdout(t, &buf, func() {
		if err := validateHarness(hr); err != nil {
			t.Fatalf("validateHarness: %v", err)
		}
	})
	out := buf.String()
	for _, want := range []string{
		": valid",
		"checked:",
		"manifest     " + filepath.Join(hr, ".agents", "harness", "plugin.json") + " against https://eyelock.github.io/ynh/schema/plugin.schema.json",
		"includes     https://github.com/eyelock/ynm pick skills/ynm-memory",
		"mcp_servers  ynm runs `ynm serve`",
		"hooks        on_stop runs `ynm hook stop`",
		"skills       hello",
		"agents       none",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// ynd validate does not read .ynh-plugin: a tree that keeps its manifest
// there, or in both directories, fails validation with the reason and the fix
// (#404). Only a tree in .agents/harness alone is valid.
func TestValidateHarness_LegacyManifestDir(t *testing.T) {
	const manifest = `{"$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json", "name": "h", "version": "0.1.0"}`
	const installed = `{"source_type":"local","source":"/src","installed_at":"now"}`
	cases := []struct {
		name    string
		files   map[string]string // relative path -> body
		wantErr bool
		want    []string
	}{
		{
			name:  "canonical only",
			files: map[string]string{".agents/harness/plugin.json": manifest, ".agents/harness/installed.json": installed},
		},
		{
			name:    "legacy only",
			files:   map[string]string{".ynh-plugin/plugin.json": manifest, ".ynh-plugin/installed.json": installed},
			wantErr: true,
			want:    []string{"keeps its manifest in .ynh-plugin/", "which ynh no longer reads; move it with: ynd migrate"},
		},
		{
			name: "siblings split from plugin.json",
			files: map[string]string{
				".agents/harness/plugin.json":  manifest,
				".ynh-plugin/installed.json":   installed,
				".ynh-plugin/marketplace.json": `{"name":"reg","owner":{"name":"o"},"harnesses":[]}`,
			},
			wantErr: true,
			want:    []string{"cannot be moved automatically", "both .agents/harness and .ynh-plugin exist"},
		},
		{
			name:    "both plugin.json",
			files:   map[string]string{".agents/harness/plugin.json": manifest, ".ynh-plugin/plugin.json": manifest},
			wantErr: true,
			want:    []string{"cannot be moved automatically", "nothing was merged"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			hr := filepath.Join(dir, "h")
			for rel, body := range tc.files {
				writeFile(t, filepath.Join(hr, filepath.FromSlash(rel)), []byte(body))
			}
			var buf bytes.Buffer
			var err error
			withStdout(t, &buf, func() { err = validateHarness(hr) })
			out := buf.String()
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateHarness err = %v, wantErr %v\n%s", err, tc.wantErr, out)
			}
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("output missing %q:\n%s", want, out)
				}
			}
		})
	}
}

// A convergence verifier must be able to return pass. A files sensor reports
// freshness and a focus sensor is deferred, so neither ever can, and a harness
// that names one as its verifier runs every agent session to the turn cap
// (#447). `ynd validate` says so instead.
func TestValidateHarnessSensors_ConvergenceVerifierSource(t *testing.T) {
	out := map[string]any{"format": "text"}
	cases := []struct {
		name    string
		source  map[string]any
		role    string
		refused bool
	}{
		{"focus verifier", map[string]any{"focus": map[string]any{"prompt": "p"}}, "convergence-verifier", true},
		{"files verifier", map[string]any{"files": []any{"done.txt"}}, "convergence-verifier", true},
		{"command verifier", map[string]any{"command": "make verify"}, "convergence-verifier", false},
		{"github_check verifier", map[string]any{"github_check": map[string]any{"name": "build"}}, "convergence-verifier", false},
		{"focus without the role", map[string]any{"focus": map[string]any{"prompt": "p"}}, "", false},
		{"files without the role", map[string]any{"files": []any{"done.txt"}}, "regular", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			entry := map[string]any{"source": c.source, "output": out}
			if c.role != "" {
				entry["role"] = c.role
			}
			issues := validateHarnessSensors(map[string]any{"sensors": map[string]any{"s": entry}})
			var found bool
			for _, i := range issues {
				if strings.Contains(i, "role convergence-verifier requires a command source") {
					found = true
				}
			}
			if found != c.refused {
				t.Errorf("refused=%v want %v; issues: %v", found, c.refused, issues)
			}
		})
	}
}

func TestSchemaPlugin_IncludeSensorsFlag(t *testing.T) {
	cases := []struct {
		name      string
		value     string
		wantIssue bool
	}{
		{"false", `false`, false},
		{"true", `true`, false},
		{"string", `"false"`, true},
		{"number", `0`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := `{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"h","version":"0.1.0","includes":[{"git":"github.com/x/y","sensors":` + c.value + `}]}`
			path := filepath.Join(t.TempDir(), "plugin.json")
			writeFile(t, path, []byte(src))
			issues := lintHarnessJSON(path)
			if c.wantIssue && len(issues) == 0 {
				t.Errorf("expected an issue for sensors: %s", c.value)
			}
			if !c.wantIssue && len(issues) > 0 {
				t.Errorf("unexpected issues for sensors: %s: %v", c.value, issues)
			}
		})
	}
}

func TestValidateHarnessProfiles_SensorOverrides(t *testing.T) {
	out := map[string]any{"format": "text"}
	own := map[string]any{
		"e2e-status": map[string]any{"source": map[string]any{"command": "true"}, "output": out},
		"verify":     map[string]any{"role": "convergence-verifier", "source": map[string]any{"command": "true"}, "output": out},
	}
	ov := func(m map[string]any) map[string]any {
		return map[string]any{"sensor_overrides": m}
	}
	cases := []struct {
		name     string
		hj       map[string]any
		wantSub  string // "" means no issue
		wantFrom string // extra substring
	}{
		{"softening an own sensor is fine",
			map[string]any{"sensors": own, "profiles": map[string]any{"factory": ov(map[string]any{"e2e-status": map[string]any{"tolerance": "advisory"}})}},
			"", ""},
		{"ratchet refused",
			map[string]any{"sensors": own, "profiles": map[string]any{"factory": ov(map[string]any{"e2e-status": map[string]any{"tolerance": "advisory", "ratchet": "count"}})}},
			"ratchet cannot be set by a profile", `profile "factory"`},
		{"bad tolerance",
			map[string]any{"sensors": own, "profiles": map[string]any{"factory": ov(map[string]any{"e2e-status": map[string]any{"tolerance": "loud"}})}},
			"must be one of blocking, advisory, report", ""},
		{"verifier refused, even as blocking",
			map[string]any{"sensors": own, "profiles": map[string]any{"factory": ov(map[string]any{"verify": map[string]any{"tolerance": "blocking"}})}},
			"convergence verifier", "verify"},
		{"unknown sensor with no includes",
			map[string]any{"sensors": own, "profiles": map[string]any{"factory": ov(map[string]any{"ghost": map[string]any{"tolerance": "advisory"}})}},
			"no sensor of that name is declared", "ghost"},
		{"unknown sensor is undecidable when an include may bring it",
			map[string]any{"sensors": own, "includes": []any{map[string]any{"git": "github.com/x/y"}},
				"profiles": map[string]any{"factory": ov(map[string]any{"ghost": map[string]any{"tolerance": "advisory"}})}},
			"", ""},
		{"unknown sensor is undecidable when the profile adds an include",
			map[string]any{"sensors": own, "profiles": map[string]any{"factory": map[string]any{
				"includes":         []any{map[string]any{"git": "github.com/x/y"}},
				"sensor_overrides": map[string]any{"ghost": map[string]any{"tolerance": "advisory"}}}}},
			"", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			issues := validateHarnessProfiles(c.hj)
			joined := strings.Join(issues, "\n")
			if c.wantSub == "" {
				if len(issues) != 0 {
					t.Fatalf("unexpected issues: %v", issues)
				}
				return
			}
			if !strings.Contains(joined, c.wantSub) || !strings.Contains(joined, c.wantFrom) {
				t.Errorf("issues %q should contain %q and %q", joined, c.wantSub, c.wantFrom)
			}
		})
	}
}

// The published schema rejects a ratchet in a profile override too, so the
// refusal does not depend on the cross-field check running.
func TestSchemaPlugin_ProfileSensorOverrides(t *testing.T) {
	cases := []struct {
		name      string
		override  string
		wantIssue bool
	}{
		{"tolerance", `{"tolerance":"advisory"}`, false},
		{"ratchet", `{"tolerance":"advisory","ratchet":"count"}`, true},
		{"ratchet alone", `{"ratchet":"count"}`, true},
		{"bad tolerance", `{"tolerance":"loud"}`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := `{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"h","version":"0.1.0","profiles":{"factory":{"sensor_overrides":{"s":` + c.override + `}}}}`
			path := filepath.Join(t.TempDir(), "plugin.json")
			writeFile(t, path, []byte(src))
			issues := lintHarnessJSON(path)
			if c.wantIssue != (len(issues) > 0) {
				t.Errorf("issues = %v, wantIssue %v", issues, c.wantIssue)
			}
		})
	}
}

// A harness's top-level sensor_overrides are checked statically (#613), with
// the wording of the runtime's refusals so the author sees the same message
// at `ynd validate` that `ynh check` would give later.
func TestValidateIncludeSensorOverrides(t *testing.T) {
	out := map[string]any{"format": "text"}
	upSensor := map[string]any{"source": map[string]any{"command": "true"}, "output": out}
	cmdSensor := upSensor

	type tc struct {
		name      string
		overrides any
		own       map[string]any
		includes  []any // local include dirs are created under "up" with upSensors
		upSensors map[string]any
		wantIssue string // substring, "" for none
		wantNote  string // substring, "" for none
	}
	cases := []tc{
		{name: "valid override of an included sensor",
			overrides: map[string]any{"lint": map[string]any{"tolerance": "advisory", "ratchet": "count"}},
			includes:  []any{map[string]any{"local": "up"}}, upSensors: map[string]any{"lint": upSensor}},
		{name: "bad tolerance",
			overrides: map[string]any{"lint": map[string]any{"tolerance": "loud"}},
			includes:  []any{map[string]any{"local": "up"}}, upSensors: map[string]any{"lint": upSensor},
			wantIssue: `sensor_overrides.lint: tolerance "loud" must be one of blocking, advisory, report`},
		{name: "bad ratchet",
			overrides: map[string]any{"lint": map[string]any{"ratchet": "forever"}},
			includes:  []any{map[string]any{"local": "up"}}, upSensors: map[string]any{"lint": upSensor},
			wantIssue: `ratchet "forever" must be one of fingerprint, count`},
		{name: "not an object",
			overrides: map[string]any{"lint": "advisory"},
			wantIssue: "sensor_overrides.lint: must be an object"},
		{name: "section not an object",
			overrides: []any{"x"},
			wantIssue: "'sensor_overrides' must be an object"},
		{name: "only tolerance and ratchet may change",
			overrides: map[string]any{"lint": map[string]any{"tolerance": "advisory", "command": "x"}},
			includes:  []any{map[string]any{"local": "up"}}, upSensors: map[string]any{"lint": upSensor},
			wantIssue: `"command" cannot be overridden`},
		{name: "own sensor refused",
			overrides: map[string]any{"mine": map[string]any{"tolerance": "advisory"}},
			own:       map[string]any{"mine": cmdSensor},
			wantIssue: `sensor "mine" is declared by this harness, so edit it directly rather than overriding it`},
		{name: "name no include declares",
			overrides: map[string]any{"ghost": map[string]any{"tolerance": "advisory"}},
			includes:  []any{map[string]any{"local": "up"}}, upSensors: map[string]any{"lint": upSensor},
			wantIssue: `override names sensor "ghost", which no include or this harness declares`},
		{name: "no includes at all",
			overrides: map[string]any{"ghost": map[string]any{"tolerance": "advisory"}},
			wantIssue: "which no include or this harness declares"},
		{name: "opted-out include contributes nothing",
			overrides: map[string]any{"lint": map[string]any{"tolerance": "advisory"}},
			includes:  []any{map[string]any{"local": "up", "sensors": false}}, upSensors: map[string]any{"lint": upSensor},
			wantIssue: "which no include or this harness declares"},
		{name: "unreadable include is a note, not a pass or a failure",
			overrides: map[string]any{"lint": map[string]any{"tolerance": "advisory"}},
			includes:  []any{map[string]any{"local": "gone"}},
			wantNote:  "include gone could not be read"},
		{name: "found in a readable include, other include unreadable: no note",
			overrides: map[string]any{"lint": map[string]any{"tolerance": "advisory"}},
			includes:  []any{map[string]any{"local": "up"}, map[string]any{"local": "gone"}}, upSensors: map[string]any{"lint": upSensor}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if c.upSensors != nil {
				hj, _ := json.Marshal(map[string]any{"name": "up", "version": "1.0.0", "sensors": c.upSensors})
				if err := writePluginJSONFile(filepath.Join(dir, "up"), hj); err != nil {
					t.Fatal(err)
				}
			}
			m := map[string]any{"name": "app", "version": "1.0.0", "sensor_overrides": c.overrides}
			if c.own != nil {
				m["sensors"] = c.own
			}
			if c.includes != nil {
				m["includes"] = c.includes
			}
			data, _ := json.Marshal(m)
			issues, notes := validateIncludeSensorOverrides(dir, m, data)
			if c.wantIssue == "" && len(issues) != 0 {
				t.Errorf("unexpected issues: %v", issues)
			}
			if c.wantIssue != "" && !strings.Contains(strings.Join(issues, "\n"), c.wantIssue) {
				t.Errorf("issues %v should contain %q", issues, c.wantIssue)
			}
			if c.wantNote == "" && len(notes) != 0 {
				t.Errorf("unexpected notes: %v", notes)
			}
			if c.wantNote != "" {
				if !strings.Contains(strings.Join(notes, "\n"), c.wantNote) {
					t.Errorf("notes %v should contain %q", notes, c.wantNote)
				}
				if len(issues) != 0 {
					t.Errorf("an include that cannot be read must not fail validation: %v", issues)
				}
			}
		})
	}
}

// An uncached git include is skipped, and says so in the output; the run is
// still valid.
func TestValidateHarness_SkippedIncludeIsReported(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("YNH_HOME", "")
	dir := t.TempDir()
	data := `{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"app","version":"1.0.0",` +
		`"includes":[{"git":"github.com/example/uncached-sensors"}],` +
		`"sensor_overrides":{"lint":{"tolerance":"advisory"}}}`
	if err := writePluginJSONFile(dir, []byte(data)); err != nil {
		t.Fatal(err)
	}
	var verr error
	var buf bytes.Buffer
	withStdout(t, &buf, func() { verr = validateHarness(dir) })
	out := buf.String()
	if verr != nil {
		t.Fatalf("an unreadable include must not fail validation: %v\n%s", verr, out)
	}
	if !strings.Contains(out, "not checked: sensor_overrides.lint") || !strings.Contains(out, "uncached-sensors") {
		t.Errorf("the skipped check must be reported:\n%s", out)
	}
}

func TestValidateSensorDetails(t *testing.T) {
	dir := t.TempDir()
	mkdirAll(t, filepath.Join(dir, "fixtures", "trips"))
	writeFile(t, filepath.Join(dir, "fixtures", "afile"), []byte("x"))
	build := func(entry string) []byte {
		return []byte(`{"name":"set","version":"1.0.0","sensors":{"s":` + entry + `}}`)
	}
	cmd := `"source":{"command":"true"},"output":{"format":"text"}`
	cases := []struct {
		name, entry, want string
	}{
		{"clean with fixture", `{` + cmd + `,"reference":{"path":"fixtures/trips","expect":"fail"}}`, ""},
		{"count ratchet on a command", `{` + cmd + `,"ratchet":"count"}`, ""},
		{"count ratchet needs a command",
			`{"source":{"files":["a"]},"output":{"format":"text"},"ratchet":"count"}`, "ratchet count requires a command source"},
		{"match that does not compile",
			`{"source":{"command":"true"},"output":{"format":"text","match":"("}}`, "output.match"},
		{"fixture missing", `{` + cmd + `,"reference":{"path":"fixtures/nope","expect":"fail"}}`, `reference.path "fixtures/nope" is not a directory`},
		{"fixture is a file", `{` + cmd + `,"reference":{"path":"fixtures/afile","expect":"fail"}}`, "is not a directory"},
		{"fixture escapes", `{` + cmd + `,"reference":{"path":"../x","expect":"fail"}}`, "relative path inside the harness"},
		{"fixture absolute", `{` + cmd + `,"reference":{"path":"/etc","expect":"fail"}}`, "relative path inside the harness"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := strings.Join(validateSensorDetails(dir, build(c.entry)), "\n")
			if c.want == "" && got != "" {
				t.Errorf("unexpected: %s", got)
			}
			if c.want != "" && !strings.Contains(got, c.want) {
				t.Errorf("%q should contain %q", got, c.want)
			}
		})
	}
}
