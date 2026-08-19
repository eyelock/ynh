package main

import (
	"strings"
	"testing"
)

func TestRunAgentMode_FocusAndTaskExclusive(t *testing.T) {
	ra, err := parseRunArgs([]string{"x", "--agent", "--focus", "f", "--task", "t"})
	if err != nil {
		t.Fatalf("parseRunArgs: %v", err)
	}
	err = runAgentMode(ra)
	if err == nil || !strings.Contains(err.Error(), "--focus and --task") {
		t.Fatalf("expected --focus + --task rejection, got: %v", err)
	}
}

func TestRunAgentMode_FocusAndProfileExclusive(t *testing.T) {
	ra, err := parseRunArgs([]string{"x", "--agent", "--focus", "f", "--profile", "p"})
	if err != nil {
		t.Fatalf("parseRunArgs: %v", err)
	}
	err = runAgentMode(ra)
	if err == nil || !strings.Contains(err.Error(), "--focus and --profile") {
		t.Fatalf("expected --focus + --profile rejection, got: %v", err)
	}
}

func TestRunAgentMode_RequiresTaskOrFocus(t *testing.T) {
	ra, err := parseRunArgs([]string{"x", "--agent"})
	if err != nil {
		t.Fatalf("parseRunArgs: %v", err)
	}
	err = runAgentMode(ra)
	if err == nil || !strings.Contains(err.Error(), "--task") || !strings.Contains(err.Error(), "--focus") {
		t.Fatalf("expected --task-or-focus-required, got: %v", err)
	}
}

func TestRunAgentMode_RejectsHarnessFile(t *testing.T) {
	ra, err := parseRunArgs([]string{"--agent", "--task", "t", "--harness-file", "x.json"})
	if err != nil {
		t.Fatalf("parseRunArgs: %v", err)
	}
	err = runAgentMode(ra)
	if err == nil || !strings.Contains(err.Error(), "--harness-file") {
		t.Fatalf("expected --harness-file rejection, got: %v", err)
	}
}

func TestParseRunArgs_AgentMaxPlanIterations(t *testing.T) {
	ra, err := parseRunArgs([]string{"x", "--agent", "--task", "t", "--max-plan-iterations", "3"})
	if err != nil {
		t.Fatalf("parseRunArgs: %v", err)
	}
	if ra.AgentMaxPlanIter != 3 {
		t.Fatalf("expected AgentMaxPlanIter=3, got %d", ra.AgentMaxPlanIter)
	}
}

func TestParseRunArgs_AgentMaxPlanIterationsRejectsNonInt(t *testing.T) {
	_, err := parseRunArgs([]string{"x", "--agent", "--task", "t", "--max-plan-iterations", "lots"})
	if err == nil || !strings.Contains(err.Error(), "non-negative integer") {
		t.Fatalf("expected non-integer rejection, got: %v", err)
	}
}

func TestParseRunArgs_AgentMaxPlanIterationsRejectsNegative(t *testing.T) {
	_, err := parseRunArgs([]string{"x", "--agent", "--task", "t", "--max-plan-iterations", "-3"})
	if err == nil || !strings.Contains(err.Error(), "non-negative integer") {
		t.Fatalf("expected negative rejection, got: %v", err)
	}
}

func TestParseRunArgs_AgentResume(t *testing.T) {
	ra, err := parseRunArgs([]string{"x", "--agent", "--resume", "/tmp/session-dir"})
	if err != nil {
		t.Fatalf("parseRunArgs: %v", err)
	}
	if ra.AgentResume != "/tmp/session-dir" {
		t.Fatalf("expected AgentResume=/tmp/session-dir, got %q", ra.AgentResume)
	}
}
