package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestCmdAgentRun_FocusAndTaskExclusive(t *testing.T) {
	err := cmdAgentRunTo(t, []string{"--harness", "x", "--focus", "f", "--task", "t"})
	if err == nil || !strings.Contains(err.Error(), "--focus and --task") {
		t.Fatalf("expected --focus + --task rejection, got: %v", err)
	}
}

func TestCmdAgentRun_FocusAndProfileExclusive(t *testing.T) {
	err := cmdAgentRunTo(t, []string{"--harness", "x", "--focus", "f", "--profile", "p"})
	if err == nil || !strings.Contains(err.Error(), "--focus and --profile") {
		t.Fatalf("expected --focus + --profile rejection, got: %v", err)
	}
}

func TestCmdAgentRun_RequiresTaskOrFocus(t *testing.T) {
	err := cmdAgentRunTo(t, []string{"--harness", "x"})
	if err == nil || !strings.Contains(err.Error(), "--task or --focus is required") {
		t.Fatalf("expected --task-or-focus-required, got: %v", err)
	}
}

func TestCmdAgentRun_ProfileFlagAccepted(t *testing.T) {
	err := cmdAgentRunTo(t, []string{"--harness", "x", "--profile"})
	if err == nil || !strings.Contains(err.Error(), "--profile requires a value") {
		t.Fatalf("expected --profile value-required, got: %v", err)
	}
}

func TestCmdAgentRun_FocusFlagAccepted(t *testing.T) {
	err := cmdAgentRunTo(t, []string{"--harness", "x", "--focus"})
	if err == nil || !strings.Contains(err.Error(), "--focus requires a value") {
		t.Fatalf("expected --focus value-required, got: %v", err)
	}
}

func cmdAgentRunTo(t *testing.T, args []string) error {
	t.Helper()
	return cmdAgentRun(args, &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""))
}
