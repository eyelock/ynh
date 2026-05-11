//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestYnd_Create_Skill scaffolds a new skill in the cwd. ynd create
// operates relative to the current directory; locks the file layout
// the templates produce.
func TestYnd_Create_Skill(t *testing.T) {
	dir := t.TempDir()
	mustRunYndInDir(t, dir, "create", "skill", "my-skill")

	path := filepath.Join(dir, "skills", "my-skill", "SKILL.md")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected %s: %v", path, err)
	}
	for _, want := range []string{"name: my-skill", "## Instructions"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("SKILL.md missing %q\n%s", want, body)
		}
	}
}

// TestYnd_Create_Agent scaffolds a new agent file.
func TestYnd_Create_Agent(t *testing.T) {
	dir := t.TempDir()
	mustRunYndInDir(t, dir, "create", "agent", "reviewer")

	path := filepath.Join(dir, "agents", "reviewer.md")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected %s: %v", path, err)
	}
}

// TestYnd_Create_Rule scaffolds a new rule file.
func TestYnd_Create_Rule(t *testing.T) {
	dir := t.TempDir()
	mustRunYndInDir(t, dir, "create", "rule", "always-test")

	path := filepath.Join(dir, "rules", "always-test.md")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected %s: %v", path, err)
	}
}

// TestYnd_Create_Command scaffolds a new command file.
func TestYnd_Create_Command(t *testing.T) {
	dir := t.TempDir()
	mustRunYndInDir(t, dir, "create", "command", "deploy")

	path := filepath.Join(dir, "commands", "deploy.md")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected %s: %v", path, err)
	}
}

// TestYnd_Create_Harness scaffolds a new harness with --description and
// --vendor flags. Locks the documented harness-creation entry point.
func TestYnd_Create_Harness(t *testing.T) {
	dir := t.TempDir()
	mustRunYndInDir(t, dir, "create", "harness", "my-harness",
		"--description", "Test harness", "--vendor", "claude")

	// `ynd create harness` makes a subdirectory named after the harness.
	body, err := os.ReadFile(filepath.Join(dir, "my-harness", ".ynh-plugin", "plugin.json"))
	if err != nil {
		t.Fatalf("expected plugin.json: %v", err)
	}
	for _, want := range []string{`"name": "my-harness"`, `"description": "Test harness"`, `"default_vendor": "claude"`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("plugin.json missing %q\n%s", want, body)
		}
	}
}

// TestYnd_Create_InvalidName rejects names that don't match validName.
func TestYnd_Create_InvalidName(t *testing.T) {
	dir := t.TempDir()
	_, errOut, err := runYndInDirEnv(t, dir, nil, "create", "skill", "../escape")
	if err == nil {
		t.Fatalf("expected invalid name to fail, got success")
	}
	if !strings.Contains(errOut, "invalid name") {
		t.Errorf("expected 'invalid name' in error, got: %s", errOut)
	}
}

// TestYnd_Create_DuplicateErrors verifies that creating a skill that
// already exists fails with a clear message rather than silently
// overwriting the user's file.
func TestYnd_Create_DuplicateErrors(t *testing.T) {
	dir := t.TempDir()
	mustRunYndInDir(t, dir, "create", "skill", "twice")

	_, errOut, err := runYndInDirEnv(t, dir, nil, "create", "skill", "twice")
	if err == nil {
		t.Fatalf("expected duplicate create to fail, got success")
	}
	if !strings.Contains(errOut, "already exists") {
		t.Errorf("expected 'already exists' in error, got: %s", errOut)
	}
}

// TestYnd_Create_UnknownType rejects an unknown artifact type clearly.
func TestYnd_Create_UnknownType(t *testing.T) {
	dir := t.TempDir()
	_, errOut, err := runYndInDirEnv(t, dir, nil, "create", "widget", "x")
	if err == nil {
		t.Fatalf("expected unknown type to fail, got success")
	}
	if !strings.Contains(errOut, "unknown type") {
		t.Errorf("expected 'unknown type' in error, got: %s", errOut)
	}
}
