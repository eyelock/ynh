//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRun_FocusAndProfileMutex locks the documented constraint that --focus
// and --profile cannot be combined: focus already includes a profile, so
// the combination is ambiguous. Error path covers cmd/ynh/main.go:594.
func TestRun_FocusAndProfileMutex(t *testing.T) {
	s := newSandbox(t)
	harness := newSyntheticSkillHarness(t, "mutex-test")
	s.mustRunYnh(t, "install", harness)

	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}

	_, errOut, err := runYnhInDirRaw(t, s, project,
		"run", "mutex-test", "-v", "cursor",
		"--focus", "f", "--profile", "p", "--install")
	if err == nil {
		t.Fatalf("expected --focus + --profile to fail, got success")
	}
	if !strings.Contains(errOut, "focus") || !strings.Contains(errOut, "profile") {
		t.Errorf("expected error to mention both 'focus' and 'profile', got: %s", errOut)
	}
}

// TestRun_FocusUnknown_Errors verifies that --focus referencing a name
// not declared in the harness manifest fails with a clear error. Locks
// the resolver path at cmd/ynh/main.go:655.
func TestRun_FocusUnknown_Errors(t *testing.T) {
	s := newSandbox(t)
	harness := newSyntheticSkillHarness(t, "no-focus")
	s.mustRunYnh(t, "install", harness)

	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}

	_, errOut, err := runYnhInDirRaw(t, s, project,
		"run", "no-focus", "-v", "cursor", "--focus", "phantom", "--install")
	if err == nil {
		t.Fatalf("expected unknown --focus to fail, got success")
	}
	if !strings.Contains(errOut, "focus") || !strings.Contains(errOut, "phantom") {
		t.Errorf("expected error to mention 'focus' and the bad name 'phantom', got: %s", errOut)
	}
}
