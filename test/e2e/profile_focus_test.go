//go:build e2e

package e2e

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

const profileHarnessTmpl = `{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": %q,
  "version": "0.1.0",
  "hooks": {
    "before_tool": [{"command": "echo BASE"}]
  },
  "profiles": {
    "review": {
      "hooks": {
        "before_tool": [{"command": "echo REVIEW"}]
      }
    }
  },
  "focus": {
    "audit": {"profile": "review", "prompt": "audit the diff"}
  }
}
`

// TestRun_ProfileOverridesHooks verifies the documented profile merge rule:
// a profile's hooks REPLACE the base hooks at the event level. Running
// with --profile review must yield the profile's hook (REVIEW), not BASE.
func TestRun_ProfileOverridesHooks(t *testing.T) {
	s := newSandbox(t)
	harness := newProfileHarness(t, "with-profile")
	s.mustRunYnh(t, "install", harness)

	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	mustRunYnhInDir(t, s, project, "run", "with-profile", "-v", "cursor", "--profile", "review", "--install")

	hookFile := filepath.Join(s.home, "run", "with-profile", ".cursor", "hooks.json")
	body, err := os.ReadFile(hookFile)
	if err != nil {
		t.Fatalf("reading hook file: %v", err)
	}
	if !bytes.Contains(body, []byte("REVIEW")) {
		t.Errorf("hook file missing profile override REVIEW:\n%s", body)
	}
	if bytes.Contains(body, []byte("BASE")) {
		t.Errorf("hook file should not contain base hook BASE after profile override:\n%s", body)
	}
}

// TestRun_FocusResolvesProfile verifies that --focus <name> resolves to
// the named focus's profile, applying the same hook override as --profile
// would. Locks focus → profile resolution.
func TestRun_FocusResolvesProfile(t *testing.T) {
	s := newSandbox(t)
	harness := newProfileHarness(t, "with-focus")
	s.mustRunYnh(t, "install", harness)

	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	mustRunYnhInDir(t, s, project, "run", "with-focus", "-v", "cursor", "--focus", "audit", "--install")

	hookFile := filepath.Join(s.home, "run", "with-focus", ".cursor", "hooks.json")
	body, err := os.ReadFile(hookFile)
	if err != nil {
		t.Fatalf("reading hook file: %v", err)
	}
	if !bytes.Contains(body, []byte("REVIEW")) {
		t.Errorf("focus did not apply profile — hook file missing REVIEW:\n%s", body)
	}
}

func newProfileHarness(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(filepath.Join(dir, ".ynh-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(profileHarnessTmpl, name)
	if err := os.WriteFile(filepath.Join(dir, ".ynh-plugin", "plugin.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}
