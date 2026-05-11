package agent

import (
	"testing"
)

func TestSensorResult_Passed_Command(t *testing.T) {
	cases := []struct {
		exit   int
		passed bool
	}{
		{0, true},
		{1, false},
		{127, false},
	}
	for _, c := range cases {
		r := &SensorResult{Kind: "command", ExitCode: c.exit}
		if r.Passed() != c.passed {
			t.Errorf("exit=%d: expected Passed()=%v", c.exit, c.passed)
		}
	}
}

func TestSensorResult_Passed_Files(t *testing.T) {
	empty := &SensorResult{Kind: "files"}
	if empty.Passed() {
		t.Error("files sensor with no files should not pass")
	}

	withFiles := &SensorResult{Kind: "files", Output: SensorRunOutput{
		Files: []SensorFile{{Path: "foo.txt"}},
	}}
	if !withFiles.Passed() {
		t.Error("files sensor with files should pass")
	}
}

func TestSensorResult_Passed_Focus(t *testing.T) {
	r := &SensorResult{Kind: "focus"}
	if !r.Passed() {
		t.Error("focus sensor should always pass (informational)")
	}
}

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

func TestSensorHash_Deterministic(t *testing.T) {
	results := []*SensorResult{
		{Name: "build", ExitCode: 0},
		{Name: "test", ExitCode: 1},
	}
	h1 := SensorHash(results)
	h2 := SensorHash(results)
	if h1 != h2 {
		t.Error("SensorHash should be deterministic")
	}
}

func TestSensorHash_DifferentOnChange(t *testing.T) {
	before := []*SensorResult{{Name: "build", ExitCode: 0}}
	after := []*SensorResult{{Name: "build", ExitCode: 1}}
	if SensorHash(before) == SensorHash(after) {
		t.Error("different exit codes should produce different hashes")
	}
}

func TestSensorHash_EmptySlice(t *testing.T) {
	h := SensorHash(nil)
	if h == "" {
		t.Error("SensorHash of empty slice should still return a string")
	}
}

func TestSensorTier(t *testing.T) {
	cases := []struct {
		category string
		tier     int
	}{
		{"build", 1},
		{"Build", 1},
		{"lint", 2},
		{"test", 3},
		{"quality", 4},
		{"behaviour", 4},
		{"", 4},
	}
	for _, c := range cases {
		if got := sensorTier(c.category); got != c.tier {
			t.Errorf("sensorTier(%q): expected %d, got %d", c.category, c.tier, got)
		}
	}
}

func TestRunSensor_MockReplacement(t *testing.T) {
	original := runSensorFn
	defer func() { runSensorFn = original }()

	runSensorFn = func(ynh, harnessName, sensorName, cwd string) (*SensorResult, error) {
		return &SensorResult{Name: sensorName, Kind: "command", ExitCode: 0}, nil
	}

	result, err := RunSensor("ynh", "myharness", "build", "/tmp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Name != "build" || result.ExitCode != 0 {
		t.Errorf("unexpected result: %+v", result)
	}
}
