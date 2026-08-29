package agent

import (
	"testing"

	"github.com/eyelock/ynh/internal/gate"
)

func TestSensorResult_Summary_Command(t *testing.T) {
	passing := &SensorResult{Kind: "command", ExitCode: 0}
	if passing.Summary() != "passed" {
		t.Errorf("expected 'passed', got %q", passing.Summary())
	}

	failing := &SensorResult{
		Kind:     "command",
		ExitCode: 1,
		Output:   SensorRunOutput{Stdout: "line1\nline2\nline3\nline4\nline5"},
	}
	summary := failing.Summary()
	// Should truncate to 3 lines with ellipsis.
	if summary == "" {
		t.Error("failing sensor should have non-empty summary")
	}
}

func TestRunSensor_MockReplacement(t *testing.T) {
	original := runSensorFn
	defer func() { runSensorFn = original }()

	runSensorFn = func(ynh, harnessName, sensorName, cwd, overlayJSON string) (*SensorResult, error) {
		return &SensorResult{Name: sensorName, Kind: "command", ExitCode: 0}, nil
	}

	result, err := RunSensor("ynh", "myharness", "build", "/tmp", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Name != "build" || result.ExitCode != 0 {
		t.Errorf("unexpected result: %+v", result)
	}
}

func TestSensorHash_Deterministic(t *testing.T) {
	e := env(
		gate.Result{Name: "build", Status: gate.StatusPass},
		gate.Result{Name: "test", Kind: "command", Tolerance: "blocking", Status: gate.StatusFail},
	)
	first := SensorHash(e)
	if second := SensorHash(e); first != second {
		t.Errorf("SensorHash should be deterministic: %q then %q", first, second)
	}
}

func TestSensorHash_DifferentOnChange(t *testing.T) {
	before := env(gate.Result{Name: "build", Kind: "command", Status: gate.StatusPass})
	after := env(gate.Result{Name: "build", Kind: "command", Status: gate.StatusFail})
	if SensorHash(before) == SensorHash(after) {
		t.Error("a sensor changing status should produce a different hash")
	}
}

// A skipped sensor did not run, so it must contribute nothing. Otherwise the
// watchdog would see the sensor set change every time --only changed.
func TestSensorHash_IgnoresSkipped(t *testing.T) {
	with := env(
		gate.Result{Name: "build", Kind: "command", Status: gate.StatusPass},
		gate.Result{Name: "slow", Kind: "command", Status: gate.StatusSkipped},
	)
	without := env(gate.Result{Name: "build", Kind: "command", Status: gate.StatusPass})
	if SensorHash(with) != SensorHash(without) {
		t.Error("a skipped sensor did not run and must not affect the hash")
	}
}

func TestSensorHash_NilEnvelope(t *testing.T) {
	if SensorHash(nil) == "" {
		t.Error("SensorHash of a nil envelope should still return a string")
	}
}
