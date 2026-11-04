package exporter

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eyelock/ynh/internal/resolver"
)

// An Agent Plugin export copies the portable core in its own pass; a
// top-level skill folder picked by name is a skill, so it stays in that pass
// rather than being dropped as an unknown type.
func TestRestrictContentKeepsTopLevelSkillPick(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "bash-scripting"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "bash-scripting", "SKILL.md"), []byte("bash\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	in := []resolver.ResolvedContent{{BasePath: repo, Paths: []string{"bash-scripting", "agents/a.md"}}}
	got := restrictContent(in, map[string]string{"skills": "skills"})
	if len(got) != 1 || len(got[0].Paths) != 1 || got[0].Paths[0] != "bash-scripting" {
		t.Errorf("restricted = %+v, want only the bash-scripting pick", got)
	}
}
