//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRelativeGitInclude_ResolvesAgainstHarness pins #461. Two harnesses each
// name "./inc", a git repo inside their own directory, with a different skill
// in each. Installed, run and previewed from an unrelated working directory,
// each must pick up its own repo: the relative source is resolved against the
// harness directory and keyed in the cache by that absolute path.
func TestRelativeGitInclude_ResolvesAgainstHarness(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"alpha", "beta"} {
		writeRelativeIncHarness(t, filepath.Join(root, name), name)
	}
	elsewhere := filepath.Join(root, "elsewhere")
	if err := os.MkdirAll(elsewhere, 0o755); err != nil {
		t.Fatal(err)
	}

	s := newSandbox(t)
	for _, name := range []string{"alpha", "beta"} {
		out, _ := mustRunYnhInDir(t, s, elsewhere, "install", filepath.Join(root, name))
		want := "Fetched " + filepath.Join(root, name, "inc")
		if !strings.Contains(out, want) {
			t.Errorf("install %s output = %q, want it to contain %q", name, out, want)
		}
	}

	for _, name := range []string{"alpha", "beta"} {
		t.Run("run "+name, func(t *testing.T) {
			project := filepath.Join(t.TempDir(), "project")
			if err := os.MkdirAll(project, 0o755); err != nil {
				t.Fatal(err)
			}
			mustRunYnhInDir(t, s, project, "run", "local/"+name, "-v", "cursor", "--install")
			assertDirExists(t, filepath.Join(project, ".cursor", "skills", name+"-skill"))
			other := map[string]string{"alpha": "beta", "beta": "alpha"}[name]
			if _, err := os.Stat(filepath.Join(project, ".cursor", "skills", other+"-skill")); err == nil {
				t.Errorf("%s picked up %s's include", name, other)
			}
		})

		t.Run("preview "+name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out")
			mustRunYndInDir(t, elsewhere, "preview", filepath.Join(root, name), "-v", "cursor", "-o", out)
			assertDirExists(t, filepath.Join(out, ".cursor", "skills", name+"-skill"))
		})
	}
}

// writeRelativeIncHarness creates a harness at dir whose only include is
// {"git": "./inc"}, a committed git repo holding one skill named <name>-skill.
func writeRelativeIncHarness(t *testing.T, dir, name string) {
	t.Helper()
	inc := filepath.Join(dir, "inc")
	skill := filepath.Join(inc, "skills", name+"-skill")
	if err := os.MkdirAll(skill, 0o755); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf("---\nname: %s-skill\ndescription: The %s skill.\n---\n", name, name)
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, inc, "init", "-q")
	mustGit(t, inc, "-c", "user.email=e2e@test", "-c", "user.name=e2e", "add", ".")
	mustGit(t, inc, "-c", "user.email=e2e@test", "-c", "user.name=e2e", "commit", "-q", "-m", "init")

	if err := os.MkdirAll(filepath.Join(dir, ".agents/harness"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`{"name":%q,"version":"0.1.0","includes":[{"git":"./inc"}]}`, name)
	if err := os.WriteFile(filepath.Join(dir, ".agents/harness", "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
}
