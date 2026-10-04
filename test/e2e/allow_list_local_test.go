//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAllowList_LocalGitInclude asserts that allowed_remote_sources governs a
// local-path git include at install and at run, with the same answer at both:
// unlisted it is refused with a message naming the entry to add, listed (as
// an exact path or a glob) it is allowed.
func TestAllowList_LocalGitInclude(t *testing.T) {
	root := t.TempDir()
	inc := filepath.Join(root, "shared", "inc")
	skill := filepath.Join(inc, "skills", "shared-skill")
	if err := os.MkdirAll(skill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"),
		[]byte("---\nname: shared-skill\ndescription: A shared skill.\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, inc, "init", "-q")
	mustGit(t, inc, "-c", "user.email=e2e@test", "-c", "user.name=e2e", "add", ".")
	mustGit(t, inc, "-c", "user.email=e2e@test", "-c", "user.name=e2e", "commit", "-q", "-m", "init")

	harness := filepath.Join(root, "uses-inc")
	if err := os.MkdirAll(filepath.Join(harness, ".agents/harness"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`{"name":"uses-inc","version":"0.1.0","includes":[{"git":%q}]}`, inc)
	if err := os.WriteFile(filepath.Join(harness, ".agents/harness", "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	unlisted := []string{"github.com/eyelock/**"}
	wantRefusal := fmt.Sprintf("source %q is not in the allowed sources list (add %q to allowed_remote_sources)", inc, inc)

	t.Run("unlisted is refused at install", func(t *testing.T) {
		s := newSandbox(t)
		writeAllowList(t, s, unlisted)
		_, errOut, err := s.runYnh(t, "install", harness)
		if err == nil {
			t.Fatal("install should refuse an unlisted local include")
		}
		if !strings.Contains(errOut, wantRefusal) {
			t.Errorf("stderr = %q, want it to contain %q", errOut, wantRefusal)
		}
		if strings.Contains(errOut, "remote source") {
			t.Errorf("a local path is not a remote source: %q", errOut)
		}
	})

	for _, tc := range []struct{ name, entry string }{
		{"listed exactly", inc},
		{"listed by star", filepath.Dir(inc) + "/*"},
		{"listed by double star", root + "/**"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newSandbox(t)
			writeAllowList(t, s, []string{"github.com/eyelock/**", tc.entry})
			s.mustRunYnh(t, "install", harness)

			project := filepath.Join(t.TempDir(), "project")
			if err := os.MkdirAll(project, 0o755); err != nil {
				t.Fatal(err)
			}
			mustRunYnhInDir(t, s, project, "run", "local/uses-inc", "-v", "cursor", "--install")
			assertDirExists(t, filepath.Join(project, ".cursor", "skills", "shared-skill"))

			// Taking the entry away governs the next run: includes are
			// re-resolved from their sources on every launch.
			writeAllowList(t, s, unlisted)
			_, errOut, err := runYnhInDirRaw(t, s, project, "run", "local/uses-inc", "-v", "cursor", "--install")
			if err == nil {
				t.Fatal("run should refuse an include no longer on the allow-list")
			}
			if !strings.Contains(errOut, wantRefusal) {
				t.Errorf("stderr = %q, want it to contain %q", errOut, wantRefusal)
			}
		})
	}
}

func writeAllowList(t *testing.T, s *sandbox, entries []string) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"allowed_remote_sources": entries})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.home, "config.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}
