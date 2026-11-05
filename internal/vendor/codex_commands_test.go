package vendor_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/resolver"
	"github.com/eyelock/ynh/internal/vendor"
)

// Commands share the skills directory, so a run directory holds a command at
// .codex/skills/<name>/SKILL.md beside the harness's own skills.
func TestCodexAssembly_CommandsReachSkillsDir(t *testing.T) {
	src := t.TempDir()
	for path, body := range map[string]string{
		"commands/deploy.md":     "---\ndescription: Ship it\n---\nDeploy.\n",
		"skills/review/SKILL.md": "---\nname: review\ndescription: r\n---\nReview.\n",
	} {
		full := filepath.Join(src, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run := filepath.Join(t.TempDir(), "run")
	if err := assembler.AssembleTo(run, &vendor.Codex{}, []resolver.ResolvedContent{{BasePath: src}}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(run, ".codex", "skills", "deploy", "SKILL.md"))
	if err != nil {
		t.Fatalf("command skill not written: %v", err)
	}
	if !strings.Contains(string(got), "name: \"deploy\"") || !strings.Contains(string(got), "Deploy.") {
		t.Errorf("command skill = %q", got)
	}
	if _, err := os.Stat(filepath.Join(run, ".codex", "skills", "review", "SKILL.md")); err != nil {
		t.Errorf("harness skill missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(run, ".codex", "commands")); err == nil {
		t.Error(".codex/commands should not exist")
	}
}
