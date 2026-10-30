//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Launch focus e2e: the production binary with a fake claude on PATH that
// records its argv, run against a harness that declares launch_focus.

func launchFocusRun(t *testing.T, s *sandbox, harnessDir, argvFile string, args ...string) (argv, stderr string, launched bool) {
	t.Helper()
	_ = os.Remove(argvFile)
	shims := filepath.Join(t.TempDir(), "shims")
	if err := os.MkdirAll(shims, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shims, "claude"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$ARGV_OUT\"\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(ynhBinary(t), append([]string{"run", harnessDir}, args...)...)
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(),
		"PATH="+shims+string(os.PathListSeparator)+os.Getenv("PATH"),
		"YNH_HOME="+s.home, "ARGV_OUT="+argvFile, "YNH_FOCUS=", "YNH_PROFILE=")
	var errb strings.Builder
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("ynh run %v: %v\n%s", args, err, errb.String())
	}
	data, err := os.ReadFile(argvFile)
	return string(data), errb.String(), err == nil
}

func TestLaunchFocus(t *testing.T) {
	s := newSandbox(t)
	harnessDir := filepath.Join(t.TempDir(), "wake")
	if err := os.MkdirAll(filepath.Join(harnessDir, ".agents/harness"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"name":"wake","version":"0.1.0","default_vendor":"claude",
	  "profiles":{"live":{"hooks":{"on_stop":[{"command":"echo live-profile-marker"}]}}},
	  "focuses":{"wake":{"prompt":"Begin: act on your wake block","profile":"live"},"next":{"prompt":"Take the next task"}},
	  "launch_focus":"wake"}`
	if err := os.WriteFile(filepath.Join(harnessDir, ".agents/harness/plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	argvFile := filepath.Join(t.TempDir(), "argv.txt")
	hooksFile := func() string {
		matches, _ := filepath.Glob(filepath.Join(s.home, "run", "*", ".claude", "hooks", "hooks.json"))
		if len(matches) == 0 {
			return ""
		}
		data, _ := os.ReadFile(matches[0])
		return string(data)
	}

	t.Run("bare launch: interactive, wake prompt first, profile applied, announced", func(t *testing.T) {
		argv, stderr, ok := launchFocusRun(t, s, harnessDir, argvFile)
		if !ok {
			t.Fatal("vendor not launched")
		}
		lines := strings.Split(strings.TrimSpace(argv), "\n")
		if lines[0] != "Begin: act on your wake block" || hasLine(lines, "-p") {
			t.Errorf("argv = %q, want an interactive launch with the wake prompt first", lines)
		}
		if !strings.Contains(hooksFile(), "live-profile-marker") {
			t.Error("the launch focus's profile was not applied")
		}
		if !strings.Contains(stderr, `launch_focus "wake"`) {
			t.Errorf("stderr does not announce the launch focus:\n%s", stderr)
		}
	})

	t.Run("explicit --focus stays one-shot and does not involve the launch focus", func(t *testing.T) {
		argv, stderr, _ := launchFocusRun(t, s, harnessDir, argvFile, "--focus", "next")
		lines := strings.Split(strings.TrimSpace(argv), "\n")
		if !hasLine(lines, "-p") || !hasLine(lines, "Take the next task") || hasLine(lines, "Begin: act on your wake block") {
			t.Errorf("argv = %q, want a one-shot of the next focus", lines)
		}
		if strings.Contains(hooksFile(), "live-profile-marker") {
			t.Error("the launch focus's profile leaked into an explicit focus")
		}
		if strings.Contains(stderr, "launch_focus") {
			t.Errorf("explicit --focus must not mention the launch focus:\n%s", stderr)
		}
	})

	t.Run("explicit prompt keeps the profile and stays one-shot", func(t *testing.T) {
		argv, _, _ := launchFocusRun(t, s, harnessDir, argvFile, "--", "do this instead")
		lines := strings.Split(strings.TrimSpace(argv), "\n")
		if !hasLine(lines, "-p") || !hasLine(lines, "do this instead") || hasLine(lines, "Begin: act on your wake block") {
			t.Errorf("argv = %q", lines)
		}
		if !strings.Contains(hooksFile(), "live-profile-marker") {
			t.Error("the launch focus's profile was not applied to an explicit prompt")
		}
	})
}

func hasLine(lines []string, want string) bool {
	for _, l := range lines {
		if l == want {
			return true
		}
	}
	return false
}
