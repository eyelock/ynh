package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

const apManifest = `{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"hello-plugin","version":"1.0.0"}`

func writeAgentPlugin(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "plugin.json"), []byte(apManifest))
	writeFile(t, filepath.Join(dir, "skills", "greet", "SKILL.md"), []byte("---\nname: greet\ndescription: Greet the user.\n---\nGreet.\n"))
	writeFile(t, filepath.Join(dir, "mcp.json"), []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/mcp.schema.json","mcpServers":{"docs":{"type":"streamable-http","url":"https://docs.example.com/mcp"}}}`))
}

func TestValidate_AgentPluginRoot(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeAgentPlugin(t, dir)
	var out bytes.Buffer
	var err error
	withStdout(t, &out, func() { err = cmdValidate([]string{dir}) })
	if err != nil {
		t.Fatalf("cmdValidate: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "valid (Agent Plugin 1.0.0)") {
		t.Errorf("output = %q", out.String())
	}
}

func TestValidate_AgentPluginIssues(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeAgentPlugin(t, dir)
	writeFile(t, filepath.Join(dir, "plugin.json"), []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"hello-plugin","hooks":"hooks.json"}`))
	writeFile(t, filepath.Join(dir, "skills", "broken", "SKILL.md"), []byte("no frontmatter\n"))
	var out bytes.Buffer
	var err error
	withStdout(t, &out, func() { err = cmdValidate([]string{dir}) })
	if err == nil {
		t.Fatal("expected validation failure")
	}
	for _, want := range []string{"INVALID (Agent Plugin)", `unknown field "hooks"`, "skills/broken/SKILL.md: skipped"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}

// A tree holding both kinds of package validates each by its own rules.
func TestValidate_WalkFindsAgentPluginsBesideHarnesses(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeAgentPlugin(t, filepath.Join(dir, "portable"))
	writeFile(t, filepath.Join(dir, "harness", ".ynh-plugin", "plugin.json"),
		[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"h","version":"0.1.0"}`))
	var out bytes.Buffer
	var err error
	withStdout(t, &out, func() { err = cmdValidate([]string{dir}) })
	if err != nil {
		t.Fatalf("cmdValidate: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "portable: valid (Agent Plugin") || !strings.Contains(out.String(), "harness: valid") {
		t.Errorf("output = %q", out.String())
	}
}

// A single plugin.json validates against the schema its $schema names.
func TestValidateFile_PluginJSONByDeclaredSchema(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	p := filepath.Join(dir, "plugin.json")
	writeFile(t, p, []byte(apManifest))
	var out bytes.Buffer
	var err error
	withStdout(t, &out, func() { err = cmdValidate([]string{p}) })
	if err != nil {
		t.Errorf("conforming Agent Plugins manifest failed: %v\n%s", err, out.String())
	}
	writeFile(t, p, []byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"Bad Name"}`))
	withStdout(t, &out, func() { err = cmdValidate([]string{p}) })
	if err == nil {
		t.Error("manifest with an invalid name passed")
	}
}

// ynd lint walks every plugin.json; an Agent Plugins manifest must be linted
// by its own rules, which have no version requirement and a stricter name.
func TestLint_AgentPluginManifestNotHeldToYnhRules(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, filepath.Join(dir, "plugin.json"),
		[]byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"acme.tools"}`))
	var out bytes.Buffer
	var err error
	withStdout(t, &out, func() { err = cmdLint([]string{dir}) })
	if err != nil {
		t.Errorf("lint failed on a conforming manifest: %v\n%s", err, out.String())
	}
	writeFile(t, filepath.Join(dir, "plugin.json"),
		[]byte(`{"$schema":"https://agent-plugins.org/schemas/1.0.0/plugin.schema.json","name":"acme.tools","author":{"name":"a","x":1}}`))
	withStdout(t, &out, func() { err = cmdLint([]string{dir}) })
	if err == nil {
		t.Error("lint passed a manifest the spec rejects")
	}
}
