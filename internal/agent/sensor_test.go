package agent

import (
	"fmt"
	"strings"
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

	runSensorFn = func(ynh, harnessName, sensorName, cwd, overlayJSON string, _ []string) (*SensorResult, error) {
		return &SensorResult{Name: sensorName, Kind: "command", ExitCode: 0}, nil
	}

	result, err := RunSensor("ynh", "myharness", "build", "/tmp", "", nil)
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
	first := SensorHash(e, nil)
	if second := SensorHash(e, nil); first != second {
		t.Errorf("SensorHash should be deterministic: %q then %q", first, second)
	}
}

func TestSensorHash_DifferentOnChange(t *testing.T) {
	before := env(gate.Result{Name: "build", Kind: "command", Status: gate.StatusPass})
	after := env(gate.Result{Name: "build", Kind: "command", Status: gate.StatusFail})
	if SensorHash(before, nil) == SensorHash(after, nil) {
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
	if SensorHash(with, nil) != SensorHash(without, nil) {
		t.Error("a skipped sensor did not run and must not affect the hash")
	}
}

func TestSensorHash_NilEnvelope(t *testing.T) {
	if SensorHash(nil, nil) == "" {
		t.Error("SensorHash of a nil envelope should still return a string")
	}
}

// goTestFailure is realistic `go test` output for one failing test, with the
// timings a real run varies from one invocation to the next.
func goTestFailure(testDur, pkgDur string) string {
	return "--- FAIL: TestParse (" + testDur + ")\n" +
		"    parse_test.go:12: got 3, want 4\n" +
		"FAIL\n" +
		"FAIL\tgithub.com/x/y/pkg\t" + pkgDur + "\n" +
		"ok  \tgithub.com/x/y/other\t0.2s\n"
}

func failingTestEnv(stdout string) *gate.Envelope {
	return env(gate.Result{Name: "test", Kind: "command", Tolerance: "blocking", Status: gate.StatusFail, ExitCode: 1, Stdout: stdout})
}

// The same failure with different timings is no progress (#434).
func TestSensorHash_IgnoresTimings(t *testing.T) {
	a := SensorHash(failingTestEnv(goTestFailure("0.03s", "0.512s")), nil)
	b := SensorHash(failingTestEnv(goTestFailure("1.2s", "1m2.3s")), nil)
	if a != b {
		t.Errorf("output differing only in timings hashed differently: %s vs %s", a, b)
	}
	fixed := strings.Replace(goTestFailure("0.03s", "0.512s"), "want 4", "want 3", 1)
	if SensorHash(failingTestEnv(fixed), nil) == a {
		t.Error("a changed failure message must change the hash")
	}
}

// The watchdog fed real hashes: an identical failure whose timings vary must
// end the run as stuck once it has been unchanged for the threshold.
func TestWatchdog_NoProgressFiresDespiteTimings(t *testing.T) {
	w := NewWatchdog()
	durs := []string{"0.03s", "0.05s", "0.04s", "1.1s", "12ms", "0.07s", "0.02s", "0.09s"}
	for i, d := range durs {
		turn := i + 1
		h := SensorHash(failingTestEnv(goTestFailure(d, d)), nil)
		reason := w.RecordTurn(fmt.Sprintf("attempt %d", turn), h)
		// Turn 1 sets the reference hash; turns 2 to 6 are the five unchanged
		// turns NoProgressThreshold counts.
		if turn < 1+w.NoProgressThreshold {
			if reason != "" {
				t.Fatalf("turn %d: fired early: %s", turn, reason)
			}
			continue
		}
		if !strings.Contains(reason, "no sensor progress for 5 consecutive turns") {
			t.Fatalf("turn %d: want no-progress, got %q", turn, reason)
		}
		return
	}
	t.Fatal("no-progress never fired")
}
