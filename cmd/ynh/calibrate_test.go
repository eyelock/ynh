package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/gate"
	"github.com/eyelock/ynh/internal/harness"
)

// calibrationHarness installs a harness with the given sensors plus two
// fixtures: one an "expect: fail" sensor must trip on, one it must not.
func calibrationHarness(t *testing.T, sensors string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	t.Setenv("CI", "")
	t.Setenv("YNH_AGENT_SESSION", "")

	manifest := `{"name":"cal","version":"0.1.0","default_vendor":"claude","sensors":` + sensors + `}`
	installListTestHarness(t, home, "cal", manifest)

	// Fixtures live inside the installed harness, which is what a reference
	// path is relative to. They are also outside the agent's worktree — a
	// reference an agent can edit calibrates nothing.
	dir := harness.InstalledDirByID("local/cal")
	for _, d := range []string{"testdata/calibration/dirty", "testdata/calibration/clean"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "testdata/calibration/dirty/offender.txt"), []byte("BAD\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "testdata/calibration/clean/fine.txt"), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return home
}

func calibrate(t *testing.T, args ...string) (gate.CalibrationEnvelope, error) {
	t.Helper()
	var out bytes.Buffer
	err := cmdCheck(append([]string{"local/cal", "--calibrate", "--format", "json"}, args...), &out, io.Discard)
	var env gate.CalibrationEnvelope
	if out.Len() > 0 {
		if jErr := json.Unmarshal(out.Bytes(), &env); jErr != nil {
			t.Fatalf("decoding calibration output: %v\n%s", jErr, out.String())
		}
	}
	return env, err
}

// The defect this exists for: a sensor whose command has quietly stopped
// examining anything exits 0, and `ynh check` reports green. A live instance
// was two harnesses both named collective-dev, one with seven sensors and one
// with none — the empty one was installed, and the gate passed.
func TestCalibrate_CatchesASensorThatStoppedObserving(t *testing.T) {
	calibrationHarness(t, `{
      "stopped": {"tolerance":"blocking","source":{"command":"exit 0"},"output":{"format":"text"},
                  "reference":{"path":"testdata/calibration/dirty","expect":"fail"}}
    }`)
	env, err := calibrate(t)
	if err == nil {
		t.Fatal("a sensor that cannot detect its own fixture must fail calibration")
	}
	if env.Verdict != gate.VerdictBroken {
		t.Errorf("verdict = %q, want broken", env.Verdict)
	}
	if len(env.Sensors) != 1 || env.Sensors[0].Status != gate.CalibFailed {
		t.Fatalf("got %+v", env.Sensors)
	}
	r := env.Sensors[0]
	if r.Expected != "fail" || r.Observed != "pass" {
		t.Errorf("expected/observed = %q/%q, want fail/pass", r.Expected, r.Observed)
	}
	if !strings.Contains(r.Note, "no longer observing") {
		t.Errorf("the note must say what is wrong, got %q", r.Note)
	}
}

// A sensor that does its job must pass, or the check is useless.
func TestCalibrate_AWorkingSensorCalibrates(t *testing.T) {
	calibrationHarness(t, `{
      "working": {"tolerance":"blocking","source":{"command":"grep -rq BAD . && exit 1 || exit 0"},"output":{"format":"text"},
                  "reference":{"path":"testdata/calibration/dirty","expect":"fail"}}
    }`)
	env, err := calibrate(t)
	if err != nil {
		t.Fatalf("a working sensor must calibrate: %v", err)
	}
	if env.Verdict != gate.VerdictCalibrated || env.Summary.Calibrated != 1 {
		t.Errorf("got verdict %q, summary %+v", env.Verdict, env.Summary)
	}
}

// The opposite failure: a sensor firing on clean input. `expect: pass` catches
// it, which is why expect is not a boolean.
func TestCalibrate_CatchesAFalsePositive(t *testing.T) {
	calibrationHarness(t, `{
      "noisy": {"tolerance":"blocking","source":{"command":"exit 1"},"output":{"format":"text"},
                "reference":{"path":"testdata/calibration/clean","expect":"pass"}}
    }`)
	env, err := calibrate(t)
	if err == nil {
		t.Fatal("a sensor that fails a clean fixture must fail calibration")
	}
	if !strings.Contains(env.Sensors[0].Note, "not there") {
		t.Errorf("note should describe a false positive, got %q", env.Sensors[0].Note)
	}
}

// Absent is not empty. A sensor nobody has calibrated is a gap to count, not a
// failure to report — and the count is a coverage number worth reading.
func TestCalibrate_UncalibratedIsAGapNotAFailure(t *testing.T) {
	calibrationHarness(t, `{
      "unchecked": {"tolerance":"blocking","source":{"command":"exit 0"},"output":{"format":"text"}}
    }`)
	env, err := calibrate(t)
	if err != nil {
		t.Fatalf("an uncalibrated sensor must not fail the run: %v", err)
	}
	if env.Verdict != gate.VerdictCalibrated {
		t.Errorf("verdict = %q, want calibrated — a gap is not a failure", env.Verdict)
	}
	if env.Summary.Uncalibrated != 1 {
		t.Errorf("uncalibrated count = %d, want 1", env.Summary.Uncalibrated)
	}
	if env.Sensors[0].Status != gate.CalibUncalibrated {
		t.Errorf("status = %q", env.Sensors[0].Status)
	}
}

// A missing fixture needs a different remedy from a wrong answer, so it is a
// distinct status — but it still breaks the run, because a reference that
// cannot be run proves nothing.
func TestCalibrate_MissingFixtureIsAnErrorNotAFailure(t *testing.T) {
	calibrationHarness(t, `{
      "gone": {"tolerance":"blocking","source":{"command":"exit 1"},"output":{"format":"text"},
               "reference":{"path":"testdata/calibration/does-not-exist","expect":"fail"}}
    }`)
	env, err := calibrate(t)
	if err == nil {
		t.Fatal("a reference that cannot be run must break the calibration")
	}
	if env.Sensors[0].Status != gate.CalibError {
		t.Errorf("status = %q, want error — distinct from a wrong answer", env.Sensors[0].Status)
	}
	if env.Summary.Errored != 1 {
		t.Errorf("errored count = %d, want 1", env.Summary.Errored)
	}
}

// `ynh check` stays fast and never runs references. A gate that calibrates on
// every invocation is a gate people disable, and then nothing is calibrated.
func TestCheck_DoesNotRunReferences(t *testing.T) {
	home := calibrationHarness(t, `{
      "stopped": {"tolerance":"blocking","source":{"command":"exit 0"},"output":{"format":"text"},
                  "reference":{"path":"testdata/calibration/dirty","expect":"fail"}}
    }`)
	_ = home
	var out bytes.Buffer
	if err := cmdCheck([]string{"local/cal", "--cwd", t.TempDir(), "--format", "json"}, &out, io.Discard); err != nil {
		t.Fatalf("the ordinary gate must pass — the sensor exits 0: %v", err)
	}
	var env gate.Envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Verdict != gate.VerdictPass {
		t.Errorf("verdict = %q — check must not calibrate", env.Verdict)
	}
}

// Baseline flags and overlays are meaningless against a fixture, so they are
// rejected rather than silently ignored — the caller would otherwise believe
// something happened that did not.
func TestCalibrate_RejectsMeaninglessFlags(t *testing.T) {
	calibrationHarness(t, `{
      "s": {"tolerance":"blocking","source":{"command":"exit 1"},"output":{"format":"text"},
            "reference":{"path":"testdata/calibration/dirty","expect":"fail"}}
    }`)
	for _, args := range [][]string{
		{"local/cal", "--calibrate", "--update-baseline"},
		{"local/cal", "--calibrate", "--no-baseline"},
		{"local/cal", "--calibrate", "--sensor-overlay", `{"s":{"source":{"command":"true"}}}`},
	} {
		if err := cmdCheck(args, io.Discard, io.Discard); err == nil {
			t.Errorf("%v should have been rejected", args[2:])
		}
	}
}
