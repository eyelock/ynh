package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/gate"
	"github.com/eyelock/ynh/internal/harness"
)

// runCalibration executes every sensor against its declared reference fixture
// and reports whether each still observes what it claims to.
//
// Nothing else verifies this. A sensor is a command plus an expectation about
// its exit code; if the command quietly stops examining anything — a config
// change excluding a directory, an upgrade renaming a rule, a path that no
// longer matches — it exits 0 and `ynh check` reports green. Everything else
// depends on sensors telling the truth: the ratchet forgives against their
// output, the loop converges on their verdicts, and any yield figure derives
// from them. A sensor that has quietly stopped working makes every other
// control decorative while appearing to function.
//
// Note the asymmetry this closes: judged sensors already require verdict
// stability before they may gate. Deterministic sensors, trusted more, had no
// check at all.
//
// It reuses runSensor, the same execution path `ynh check` uses, with the
// fixture as the working directory. A second implementation would be free to
// drift from the thing it is meant to prove.
func runCalibration(p *harness.Harness, wanted map[string]bool, stdout, stderr io.Writer, structured bool) error {
	env := gate.CalibrationEnvelope{
		Capabilities: config.CapabilitiesVersion,
		YnhVersion:   config.Version,
		Harness:      p.Name,
		Verdict:      gate.VerdictCalibrated,
		Sensors:      []gate.CalibrationResult{},
	}

	names := make([]string, 0, len(p.Sensors))
	for name := range p.Sensors {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		s := p.Sensors[name]
		if len(wanted) > 0 && !wanted[name] {
			continue
		}
		res := gate.CalibrationResult{Name: name, Kind: s.Source.Kind()}

		if s.Reference == nil {
			// Absent is not empty. A sensor nobody has calibrated is a gap to
			// count, not a failure to report.
			res.Status = gate.CalibUncalibrated
			env.Summary.Uncalibrated++
			env.Sensors = append(env.Sensors, res)
			continue
		}

		res.Reference = s.Reference.Path
		res.Expected = s.Reference.Expect

		fixture := filepath.Join(p.Dir, s.Reference.Path)
		if fi, err := os.Stat(fixture); err != nil || !fi.IsDir() {
			res.Status = gate.CalibError
			res.Note = fmt.Sprintf("reference fixture not found at %s", fixture)
			env.Summary.Errored++
			env.Verdict = gate.VerdictBroken
			env.Sensors = append(env.Sensors, res)
			continue
		}

		run, runErr := runSensor(p, name, s, fixture, false)
		if runErr != nil {
			res.Status = gate.CalibError
			res.Note = runErr.Error()
			env.Summary.Errored++
			env.Verdict = gate.VerdictBroken
			env.Sensors = append(env.Sensors, res)
			continue
		}

		res.ExitCode = run.ExitCode

		// A command that never ran is not a wrong answer, it is no answer.
		// Without this the sensor's own absence satisfies `expect: "fail"`,
		// and a deleted script reports as calibrated.
		if gate.CommandDidNotRun(run.ExitCode) {
			res.Status = gate.CalibError
			res.Note = fmt.Sprintf(
				"the sensor's command did not run (exit %d) — it was not found or is not executable, "+
					"so this proves nothing about whether it still observes", run.ExitCode)
			env.Summary.Errored++
			env.Verdict = gate.VerdictBroken
			env.Sensors = append(env.Sensors, res)
			continue
		}

		res.Observed = gate.CalibrationOutcome(run.ExitCode)
		if res.Observed == res.Expected {
			res.Status = gate.CalibCalibrated
			env.Summary.Calibrated++
		} else {
			res.Status = gate.CalibFailed
			env.Summary.Failed++
			env.Verdict = gate.VerdictBroken
			if s.Reference.Expect == "fail" {
				res.Note = "the sensor passed a fixture built to trip it — it is no longer observing"
			} else {
				res.Note = "the sensor failed a clean fixture — it is reporting findings that are not there"
			}
		}
		env.Sensors = append(env.Sensors, res)
	}
	env.Summary.Total = len(env.Sensors)

	if structured {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(env); err != nil {
			return err
		}
	} else {
		printCalibrationText(stdout, env)
	}

	// Reuses errCheckBlocked so --calibrate shares check's exit vocabulary:
	// 0 all good, 1 a sensor is not observing, 2 ynh could not run at all.
	// A caller branching on $? should not need to know which mode ran.
	if env.Verdict == gate.VerdictBroken {
		return errCheckBlocked
	}
	return nil
}

// printCalibrationText renders the human-readable report.
func printCalibrationText(w io.Writer, env gate.CalibrationEnvelope) {
	_, _ = fmt.Fprintf(w, "Calibration for %s\n\n", env.Harness)
	for _, r := range env.Sensors {
		switch r.Status {
		case gate.CalibCalibrated:
			_, _ = fmt.Fprintf(w, "  ✓ %-24s calibrated (%s → %s)\n", r.Name, r.Reference, r.Observed)
		case gate.CalibFailed:
			_, _ = fmt.Fprintf(w, "  ✗ %-24s FAILED   expected %s, observed %s — %s\n",
				r.Name, r.Expected, r.Observed, r.Note)
		case gate.CalibError:
			_, _ = fmt.Fprintf(w, "  ! %-24s error    %s\n", r.Name, r.Note)
		default:
			_, _ = fmt.Fprintf(w, "  · %-24s uncalibrated\n", r.Name)
		}
	}
	s := env.Summary
	_, _ = fmt.Fprintf(w, "\n%d sensors: %d calibrated, %d failed, %d errored, %d uncalibrated\n",
		s.Total, s.Calibrated, s.Failed, s.Errored, s.Uncalibrated)
	if s.Uncalibrated > 0 {
		_, _ = fmt.Fprintf(w,
			"\n%d sensor(s) declare no reference. An uncalibrated sensor is not a failing one,\nbut nothing proves it still observes.\n",
			s.Uncalibrated)
	}
}
