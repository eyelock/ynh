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

func TestCmdAgentRun_ResumeRequiresValue(t *testing.T) {
	err := cmdAgentRunTo(t, []string{"--resume"})
	if err == nil || !strings.Contains(err.Error(), "--resume requires a value") {
		t.Fatalf("expected --resume value-required, got: %v", err)
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

func TestCmdAgentRun_MaxPlanIterationsRequiresValue(t *testing.T) {
	err := cmdAgentRunTo(t, []string{"--harness", "x", "--task", "t", "--max-plan-iterations"})
	if err == nil || !strings.Contains(err.Error(), "--max-plan-iterations requires a value") {
		t.Fatalf("expected --max-plan-iterations value-required, got: %v", err)
	}
}

func TestCmdAgentRun_MaxPlanIterationsRejectsNonInt(t *testing.T) {
	err := cmdAgentRunTo(t, []string{"--harness", "x", "--task", "t", "--max-plan-iterations", "lots"})
	if err == nil || !strings.Contains(err.Error(), "non-negative integer") {
		t.Fatalf("expected non-integer rejection, got: %v", err)
	}
}

func TestCmdAgentRun_MaxPlanIterationsRejectsNegative(t *testing.T) {
	err := cmdAgentRunTo(t, []string{"--harness", "x", "--task", "t", "--max-plan-iterations", "-3"})
	if err == nil || !strings.Contains(err.Error(), "non-negative integer") {
		t.Fatalf("expected negative rejection, got: %v", err)
	}
}

func cmdAgentRunTo(t *testing.T, args []string) error {
	t.Helper()
	return cmdAgentRun(args, &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""))
}

func TestCmdAgentRun_AutoApproveRequiresValue(t *testing.T) {
	err := cmdAgentRunTo(t, []string{"--task", "t", "--auto-approve"})
	if err == nil || !strings.Contains(err.Error(), "--auto-approve requires a value") {
		t.Fatalf("expected --auto-approve value-required, got: %v", err)
	}
}

// A level the backend cannot honour fails before any run starts.
func TestCmdAgentRun_AutoApproveValidatedBeforeTheRun(t *testing.T) {
	tests := []struct {
		args    []string
		wantSub string
	}{
		{[]string{"--task", "t", "--auto-approve", "sometimes"}, "unknown --auto-approve level"},
		{[]string{"--task", "t", "--backend", "codex", "--auto-approve", "edits"}, "codex cannot auto-approve edits only"},
		{[]string{"--task", "t", "--backend", "cursor", "--auto-approve", "edits"}, "cursor cannot auto-approve edits only"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			err := cmdAgentRunTo(t, tt.args)
			if err == nil || !strings.Contains(err.Error(), tt.wantSub) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.wantSub)
			}
		})
	}
}
