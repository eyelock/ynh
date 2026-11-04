package harness

import (
	"os"
	"path/filepath"
	"testing"
)

// mindrallyShaped lays out a repository the way Mindrally/skills is: each
// skill a folder at the top level with its SKILL.md at the folder's root, no
// skills/ directory, plus the files such a repo carries beside them.
func mindrallyShaped(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"README.md":                  "# skills\n",
		"LICENSE":                    "Apache-2.0\n",
		"bash-scripting/SKILL.md":    "---\nname: bash-scripting\ndescription: Shell scripts.\n---\nWrite safe bash.\n",
		"bash-scripting/examples.md": "set -euo pipefail\n",
		"python/SKILL.md":            "---\nname: python\ndescription: Python.\n---\nWrite Python.\n",
		"notes/README.md":            "not a skill\n",
	}
	for p, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestTopLevelSkill(t *testing.T) {
	dir := mindrallyShaped(t)
	for pick, want := range map[string]bool{
		"bash-scripting":          true,
		"python":                  true,
		"bash-scripting/":         true,
		"notes":                   false, // a folder with no SKILL.md
		"README.md":               false, // a file
		"missing":                 false,
		"skills/bash-scripting":   false, // the type/name form is the classic pick
		"bash-scripting/SKILL.md": false,
		"..":                      false,
		"../bash-scripting":       false,
		"":                        false,
	} {
		if got := TopLevelSkill(dir, pick); got != want {
			t.Errorf("TopLevelSkill(%q) = %v, want %v", pick, got, want)
		}
	}
	if got := PickArtifactType(dir, "bash-scripting"); got != "skills" {
		t.Errorf("PickArtifactType = %q, want skills", got)
	}
	if got := PickArtifactType(dir, "agents/x.md"); got != "agents" {
		t.Errorf("PickArtifactType(agents/x.md) = %q", got)
	}
	for pick, want := range map[string]string{"bash-scripting": "bash-scripting", "skills/go": "go", "agents/a.md": ""} {
		if got, _ := PickedSkillName(dir, pick); got != want {
			t.Errorf("PickedSkillName(%q) = %q, want %q", pick, got, want)
		}
	}
}

// ynh include validates a top-level skill folder picked by name; a bare name
// that is not one still fails as before.
func TestValidatePicks_TopLevelSkill(t *testing.T) {
	dir := mindrallyShaped(t)
	if err := ValidatePicks(dir, []string{"bash-scripting", "python"}); err != nil {
		t.Fatalf("top-level skill folders should validate: %v", err)
	}
	for _, bad := range []string{"notes", "missing", "skills/bash-scripting"} {
		if err := ValidatePicks(dir, []string{bad}); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
}
