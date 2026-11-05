package assembler

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/resolver"
	"github.com/eyelock/ynh/internal/vendor"
)

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for path, body := range files {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// Codex reads no commands from a project, so a command reaches a run directory
// as .codex/skills/<name>/SKILL.md beside the harness's own skills, and there
// is no .codex/commands.
func TestAssemble_CodexWritesCommandsAsSkills(t *testing.T) {
	src := t.TempDir()
	writeFiles(t, src, map[string]string{
		"commands/deploy.md":     "---\ndescription: Ship it\n---\nDeploy.\n",
		"skills/review/SKILL.md": "---\nname: review\ndescription: r\n---\nReview.\n",
	})
	adapter, err := vendor.Get("codex")
	if err != nil {
		t.Fatal(err)
	}
	run := filepath.Join(t.TempDir(), "run")
	if err := AssembleTo(run, adapter, []resolver.ResolvedContent{{BasePath: src}}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(run, ".codex", "skills", "deploy", "SKILL.md"))
	if err != nil {
		t.Fatalf("command skill not written: %v", err)
	}
	if !strings.Contains(string(got), `name: "deploy"`) || !strings.Contains(string(got), "Deploy.") {
		t.Errorf("command skill = %q", got)
	}
	if _, err := os.Stat(filepath.Join(run, ".codex", "skills", "review", "SKILL.md")); err != nil {
		t.Errorf("harness skill missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(run, ".codex", "commands")); err == nil {
		t.Error(".codex/commands should not exist")
	}
}

// A command and a skill of the same name: the skill is kept whole, with none
// of the command's file beside it, and a warning names the command. Repeated
// so that copy order cannot hide a difference.
func TestAssemble_CodexCommandSkillNameClashKeepsSkill(t *testing.T) {
	src := t.TempDir()
	writeFiles(t, src, map[string]string{
		"commands/deploy.md":                "---\ndescription: from the command\n---\nCommand body.\n",
		"skills/deploy/SKILL.md":            "---\nname: deploy\ndescription: from the skill\n---\nSkill body.\n",
		"skills/deploy/references/notes.md": "notes",
	})
	adapter, err := vendor.Get("codex")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		var warned bytes.Buffer
		warnOut = &warned
		run := filepath.Join(t.TempDir(), "run")
		err := AssembleTo(run, adapter, []resolver.ResolvedContent{{BasePath: src}})
		warnOut = os.Stderr
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(run, ".codex", "skills", "deploy", "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(got), "Skill body.") || strings.Contains(string(got), "Command body.") {
			t.Fatalf("run %d: SKILL.md = %q, want the skill's", i, got)
		}
		if _, err := os.Stat(filepath.Join(run, ".codex", "skills", "deploy", "references", "notes.md")); err != nil {
			t.Errorf("run %d: skill's own file missing: %v", i, err)
		}
		if !strings.Contains(warned.String(), `command "deploy"`) {
			t.Errorf("run %d: no warning naming the command: %q", i, warned.String())
		}
	}
}
