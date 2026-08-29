package agent

import (
	"strings"
	"testing"
)

// A resume that lost its harness ran zero sensors, and an empty result set made
// the all-passed check vacuously true — so the loop reported converged and
// exited 0 having verified nothing. The single most safety-critical value in
// the contract was forgeable by omitting a flag.
func TestCheckConvergence_NoEvidenceCannotConverge(t *testing.T) {
	converged, reason := checkConvergence(nil, "", "", "", "", newNullTrajectory(), 1, true)
	if converged {
		t.Fatal("converged with no sensor results — a verdict with no evidence behind it")
	}
	if !strings.Contains(reason, "no sensors ran") {
		t.Errorf("reason = %q, want it to name the missing evidence", reason)
	}
}

// A run started without a harness never asked to be verified; the worker
// declaring itself done is the only signal available, and requiring evidence
// there would break the plain agent-runner mode.
func TestCheckConvergence_UnverifiedRunStillConverges(t *testing.T) {
	converged, _ := checkConvergence(nil, "", "", "", "", newNullTrajectory(), 1, false)
	if !converged {
		t.Error("a run with no harness configured should still converge on worker completion")
	}
}

// `report` is documented as pure observation and `advisory` as non-gating, but
// the loop treated every result alike — so a report sensor that never passes
// held convergence open forever, and the loop contradicted `ynh check` on the
// same declaration.
func TestCheckConvergence_NonGatingSensorsDoNotBlock(t *testing.T) {
	results := []*SensorResult{
		{Name: "build", Kind: "command", Tolerance: "blocking", ExitCode: 0},
		{Name: "deps", Kind: "command", Tolerance: "report", ExitCode: 1},
		{Name: "typos", Kind: "command", Tolerance: "advisory", ExitCode: 1},
	}
	converged, reason := checkConvergence(results, "", "", "", "", newNullTrajectory(), 1, true)
	if !converged {
		t.Errorf("advisory and report failures must not gate; got reason %q", reason)
	}
}

func TestCheckConvergence_BlockingFailureStillGates(t *testing.T) {
	results := []*SensorResult{
		{Name: "lint", Kind: "command", Tolerance: "blocking", ExitCode: 1},
		{Name: "typos", Kind: "command", Tolerance: "advisory", ExitCode: 1},
	}
	if converged, _ := checkConvergence(results, "", "", "", "", newNullTrajectory(), 1, true); converged {
		t.Error("a failing blocking sensor must hold convergence open")
	}
}

// Every sensor being non-gating means nothing verifies anything, which is not
// the same as everything passing.
func TestCheckConvergence_AllNonGatingIsNotEvidence(t *testing.T) {
	results := []*SensorResult{
		{Name: "deps", Kind: "command", Tolerance: "report", ExitCode: 0},
	}
	if converged, _ := checkConvergence(results, "", "", "", "", newNullTrajectory(), 1, true); converged {
		t.Error("a run whose sensors are all non-gating has verified nothing")
	}
}

// Hashing name and exit code alone meant fixing three of fifty findings
// produced an identical hash: real progress read as no progress, and the
// watchdog killed the run for doing what it was asked.
func TestSensorHash_SeesProgressWithinAFailingSensor(t *testing.T) {
	before := &SensorResult{Name: "lint", Kind: "command", ExitCode: 1}
	before.Output.Stderr = "a.go:1: one\nb.go:2: two\nc.go:3: three\n"

	after := &SensorResult{Name: "lint", Kind: "command", ExitCode: 1}
	after.Output.Stderr = "a.go:1: one\n"

	if SensorHash([]*SensorResult{before}) == SensorHash([]*SensorResult{after}) {
		t.Error("fixing findings while the sensor still fails must change the hash")
	}
}

// Positions moving is not progress, and must not reset the watchdog either.
func TestSensorHash_IgnoresPositionDrift(t *testing.T) {
	a := &SensorResult{Name: "lint", Kind: "command", ExitCode: 1}
	a.Output.Stderr = "a.go:1:2: one\n"
	b := &SensorResult{Name: "lint", Kind: "command", ExitCode: 1}
	b.Output.Stderr = "a.go:99:2: one\n"

	if SensorHash([]*SensorResult{a}) != SensorHash([]*SensorResult{b}) {
		t.Error("a finding moving down the file is not progress")
	}
}
