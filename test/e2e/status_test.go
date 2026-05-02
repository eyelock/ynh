//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStatus_Text verifies `ynh status` reflects symlink installations made
// by `ynh run -v <vendor> --install`. Status only supports text output; this
// test pins the column header line and checks the row's harness/vendor/project
// fields appear after a Cursor install.
func TestStatus_Text(t *testing.T) {
	s := newSandbox(t)

	// Empty case first.
	out, _ := s.mustRunYnh(t, "status")
	if !strings.Contains(out, "No symlink installations") {
		t.Errorf("empty status should report 'No symlink installations', got:\n%s", out)
	}

	// Install + run --install to register a symlink installation.
	harness := newSyntheticSkillHarness(t, "with-skill-status")
	s.mustRunYnh(t, "install", harness)

	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	mustRunYnhInDir(t, s, project, "run", "with-skill-status", "-v", "cursor", "--install")

	out, _ = s.mustRunYnh(t, "status")
	for _, want := range []string{"HARNESS", "VENDOR", "PROJECT", "with-skill-status", "cursor", project} {
		if !strings.Contains(out, want) {
			t.Errorf("status output missing %q\n%s", want, out)
		}
	}
}
