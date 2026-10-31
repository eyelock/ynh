package assembler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/resolver"
	"github.com/eyelock/ynh/internal/vendor"
)

// mkTree writes files (slash paths) under a new temp dir.
func mkTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
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

// A repository shaped like Mindrally/skills: top-level skill folders, no
// skills/ directory.
var mindrally = map[string]string{
	"README.md":                  "# skills\n",
	"bash-scripting/SKILL.md":    "---\nname: bash-scripting\n---\nWrite safe bash.\n",
	"bash-scripting/examples.md": "set -euo pipefail\n",
	"python/SKILL.md":            "---\nname: python\n---\nWrite Python.\n",
}

// Picking a top-level skill folder by name stages it as skills/<name>, with
// everything in the folder; the other folders are not taken.
func TestAssemble_PicksTopLevelSkillFolder(t *testing.T) {
	repo := mkTree(t, mindrally)
	adapter, err := vendor.Get("claude")
	if err != nil {
		t.Fatal(err)
	}
	staged, err := Assemble(adapter, []resolver.ResolvedContent{{BasePath: repo, Paths: []string{"bash-scripting"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer Cleanup(staged)
	skills := filepath.Join(staged, adapter.ConfigDir(), adapter.ArtifactDirs()["skills"])
	for _, f := range []string{"bash-scripting/SKILL.md", "bash-scripting/examples.md"} {
		if _, err := os.Stat(filepath.Join(skills, f)); err != nil {
			t.Errorf("%s not staged: %v", f, err)
		}
	}
	if _, err := os.Stat(filepath.Join(skills, "python")); err == nil {
		t.Error("python was not picked but was staged")
	}
}

// Existing includes resolve exactly as before: a whole include of such a
// repository still yields no artifacts (nothing is guessed), the type/name
// form is unchanged, and a bare name that is not a skill folder still fails.
func TestAssemble_TopLevelSkillLeavesOtherIncludesAlone(t *testing.T) {
	repo := mkTree(t, mindrally)
	adapter, err := vendor.Get("claude")
	if err != nil {
		t.Fatal(err)
	}
	staged, err := Assemble(adapter, []resolver.ResolvedContent{{BasePath: repo}})
	if err != nil {
		t.Fatal(err)
	}
	defer Cleanup(staged)
	if entries, _ := os.ReadDir(filepath.Join(staged, adapter.ConfigDir(), adapter.ArtifactDirs()["skills"])); len(entries) != 0 {
		t.Errorf("a whole include of a repo with no skills/ staged %d skills", len(entries))
	}

	classic := mkTree(t, map[string]string{"skills/go/SKILL.md": "go\n"})
	staged2, err := Assemble(adapter, []resolver.ResolvedContent{{BasePath: classic, Paths: []string{"skills/go"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer Cleanup(staged2)
	if _, err := os.Stat(filepath.Join(staged2, adapter.ConfigDir(), adapter.ArtifactDirs()["skills"], "go", "SKILL.md")); err != nil {
		t.Errorf("classic pick broke: %v", err)
	}

	for _, bad := range []string{"README.md", "missing", "../bash-scripting"} {
		_, err := Assemble(adapter, []resolver.ResolvedContent{{BasePath: repo, Paths: []string{bad}}})
		if err == nil {
			t.Errorf("pick %q: want an error", bad)
		} else if bad == "missing" && !strings.Contains(err.Error(), "top-level folder with a SKILL.md") {
			t.Errorf("pick %q: error should name the new form: %v", bad, err)
		}
	}
}
