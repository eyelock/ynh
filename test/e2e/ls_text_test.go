//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// TestLs_TextOutput asserts the human-readable form of `ynh ls`. JSON shape
// is covered elsewhere; the table form is what shell users see and is part
// of the documented UX. Locks column headers + the harness row presence.
func TestLs_TextOutput(t *testing.T) {
	s := newSandbox(t)
	harness := newSyntheticSkillHarness(t, "list-me")
	s.mustRunYnh(t, "install", harness)

	out, _ := s.mustRunYnh(t, "ls")

	// Header columns documented in cmd/ynh/list.go: NAME / VENDOR / SOURCE /
	// ARTIFACTS / INCLUDES / DELEGATES TO. Plus the harness row.
	for _, want := range []string{"NAME", "VENDOR", "SOURCE", "ARTIFACTS", "list-me"} {
		if !strings.Contains(out, want) {
			t.Errorf("ls text output missing %q\n%s", want, out)
		}
	}
}
