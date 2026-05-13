//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newSyntheticHarnessWithFocus writes a harness directory carrying one skill,
// one profile, and one focus that references the profile. Used to exercise the
// `--focus` flag on `ynd preview/diff/export`.
func newSyntheticHarnessWithFocus(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(filepath.Join(dir, ".ynh-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "skills", "hello"), 0o755); err != nil {
		t.Fatal(err)
	}
	plugin := `{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "` + name + `",
  "version": "0.1.0",
  "default_vendor": "claude",
  "hooks": {
    "before_tool": [{"command": "echo base"}]
  },
  "profiles": {
    "review": {
      "hooks": {
        "before_tool": [{"command": "echo REVIEW"}]
      }
    }
  },
  "focuses": {
    "code-review": {"profile": "review", "prompt": "review this code"}
  }
}
`
	if err := os.WriteFile(filepath.Join(dir, ".ynh-plugin", "plugin.json"), []byte(plugin), 0o644); err != nil {
		t.Fatal(err)
	}
	skill := "---\nname: hello\ndescription: A trivial skill for focus E2E tests.\n---\n\n# hello\n"
	if err := os.WriteFile(filepath.Join(dir, "skills", "hello", "SKILL.md"), []byte(skill), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestYnd_Preview_WithFocus asserts that `ynd preview --focus <name>` resolves
// the focus's profile binding — the assembled output should reflect the
// profile's hook overrides, not the harness-level defaults.
func TestYnd_Preview_WithFocus(t *testing.T) {
	harness := newSyntheticHarnessWithFocus(t, "focus-preview")
	out := filepath.Join(t.TempDir(), "preview")

	mustRunYnd(t, "preview", harness, "-v", "claude", "-o", out, "--focus", "code-review")

	// Claude vendor writes hooks as .claude/settings.json — look for the
	// profile-resolved REVIEW command rather than the base.
	hookFile := filepath.Join(out, ".claude", "hooks", "hooks.json")
	body, err := os.ReadFile(hookFile)
	if err != nil {
		t.Fatalf("reading preview hook config: %v", err)
	}
	if !strings.Contains(string(body), "echo REVIEW") {
		t.Errorf("focus did not resolve to profile — expected echo REVIEW, got:\n%s", body)
	}
	if strings.Contains(string(body), "echo base") {
		t.Errorf("base hook leaked despite focus resolution:\n%s", body)
	}
}

// TestYnd_Preview_FocusAndProfileMutex locks the documented constraint that
// `ynd preview --focus X --profile Y` is rejected.
func TestYnd_Preview_FocusAndProfileMutex(t *testing.T) {
	harness := newSyntheticHarnessWithFocus(t, "focus-mutex")
	_, errOut, err := runYnd(t, "preview", harness, "--focus", "code-review", "--profile", "review")
	if err == nil {
		t.Fatal("expected --focus + --profile to be rejected")
	}
	if !strings.Contains(errOut, "focus") || !strings.Contains(errOut, "profile") {
		t.Errorf("error should mention both focus and profile, got:\n%s", errOut)
	}
}

// TestYnd_Preview_UnknownFocus asserts a missing focus name fails loudly.
func TestYnd_Preview_UnknownFocus(t *testing.T) {
	harness := newSyntheticHarnessWithFocus(t, "focus-unknown")
	_, errOut, err := runYnd(t, "preview", harness, "--focus", "nope")
	if err == nil {
		t.Fatal("expected unknown focus to be rejected")
	}
	if !strings.Contains(errOut, "focus") {
		t.Errorf("error should mention focus, got:\n%s", errOut)
	}
}

// TestYnd_Diff_WithFocus mirrors the preview test: --focus on diff must
// resolve through to the profile and produce profile-resolved hook content.
func TestYnd_Diff_WithFocus(t *testing.T) {
	harness := newSyntheticHarnessWithFocus(t, "focus-diff")
	// diff between two vendors with focus applied — output should not error.
	stdout, stderr, err := runYnd(t, "diff", harness, "-v", "claude,cursor", "--focus", "code-review")
	if err != nil {
		t.Fatalf("ynd diff --focus failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	// Diff only enumerates files, not their content — assert the command
	// produced the focus-resolved hook layout for at least one vendor.
	if !strings.Contains(stdout, "hooks") {
		t.Errorf("diff output should reference hook files:\n%s", stdout)
	}
}

// TestYnd_Export_WithFocus asserts --focus on export produces hooks resolved
// through the focus's bound profile.
func TestYnd_Export_WithFocus(t *testing.T) {
	harness := newSyntheticHarnessWithFocus(t, "focus-export")
	out := filepath.Join(t.TempDir(), "export")

	mustRunYnd(t, "export", harness, "-v", "claude", "-o", out, "--focus", "code-review", "--clean")

	// Export writes hook configuration under <out>/claude/.claude/hooks/hooks.json
	// (Claude's hooks namespace). Search for any hooks.json under the export tree
	// to tolerate layout variation.
	var hookFile string
	_ = filepath.Walk(out, func(path string, info os.FileInfo, _ error) error {
		if info != nil && !info.IsDir() && strings.HasSuffix(path, "hooks.json") {
			hookFile = path
		}
		return nil
	})
	if hookFile == "" {
		t.Fatalf("no hooks.json produced under %s", out)
	}
	body, err := os.ReadFile(hookFile)
	if err != nil {
		t.Fatalf("reading export hook config: %v", err)
	}
	if !strings.Contains(string(body), "echo REVIEW") {
		t.Errorf("focus did not resolve to profile on export — got:\n%s", body)
	}
}

// TestYnd_Export_FocusAndProfileMutex locks the focus+profile rejection on the
// export command.
func TestYnd_Export_FocusAndProfileMutex(t *testing.T) {
	harness := newSyntheticHarnessWithFocus(t, "focus-export-mutex")
	_, errOut, err := runYnd(t, "export", harness, "--focus", "code-review", "--profile", "review")
	if err == nil {
		t.Fatal("expected --focus + --profile to be rejected")
	}
	if !strings.Contains(errOut, "focus") || !strings.Contains(errOut, "profile") {
		t.Errorf("error should mention both focus and profile, got:\n%s", errOut)
	}
}
