package gate

// Calibration statuses. A sensor that has never been calibrated and one whose
// calibration failed are different facts, and collapsing them would hide the
// worse of the two.
const (
	// CalibCalibrated: the reference produced the declared result.
	CalibCalibrated = "calibrated"
	// CalibFailed: the reference ran and produced the wrong result. For an
	// `expect: fail` fixture this means the sensor did not flag something it
	// was built to flag — it has stopped observing.
	CalibFailed = "failed"
	// CalibUncalibrated: no reference declared. Not a failure; a gap. The
	// count of these is a coverage number worth reading.
	CalibUncalibrated = "uncalibrated"
	// CalibError: the reference could not be run at all — a missing fixture
	// directory, or a command that would not start. Distinct from a wrong
	// answer, because the remedy is different.
	CalibError = "error"
)

// Calibration verdicts.
const (
	VerdictCalibrated = "calibrated"
	VerdictBroken     = "broken"
)

// CalibrationEnvelope is the result of `ynh check --calibrate`.
//
// A separate mode on purpose: `ynh check` stays fast and never runs references.
// A gate that calibrates on every invocation is a gate people disable.
type CalibrationEnvelope struct {
	Capabilities string              `json:"capabilities"`
	YnhVersion   string              `json:"ynh_version"`
	Harness      string              `json:"harness"`
	Verdict      string              `json:"verdict"` // calibrated | broken
	Summary      CalibrationSummary  `json:"summary"`
	Sensors      []CalibrationResult `json:"sensors"`
}

// CalibrationSummary counts each outcome. Uncalibrated is reported rather than
// hidden: how much of a gate is proven to observe is itself a useful number.
type CalibrationSummary struct {
	Total        int `json:"total"`
	Calibrated   int `json:"calibrated"`
	Failed       int `json:"failed"`
	Uncalibrated int `json:"uncalibrated"`
	Errored      int `json:"errored"`
}

// CalibrationResult is one sensor's calibration outcome.
type CalibrationResult struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
	// Reference is the fixture path, absent when none is declared.
	Reference string `json:"reference,omitempty"`
	// Expected and Observed are the declared and actual results — "fail" or
	// "pass". Stating both means a report says what went wrong rather than
	// only that something did.
	Expected string `json:"expected,omitempty"`
	Observed string `json:"observed,omitempty"`
	ExitCode int    `json:"exit_code,omitempty"`
	Note     string `json:"note,omitempty"`
}

// CalibrationOutcome converts an exit code to the vocabulary a reference
// declares, so expected and observed are directly comparable.
func CalibrationOutcome(exitCode int) string {
	if exitCode == 0 {
		return "pass"
	}
	return "fail"
}

// Shell exit codes that mean the command never ran.
//
// Sensors execute through `/bin/sh -c`, and the shell itself always starts —
// so a missing or non-executable command is not a spawn error Go can see. It
// arrives as the shell's own status, and POSIX reserves two values for it.
const (
	// ExitCommandNotFound is sh's status when the command does not exist.
	ExitCommandNotFound = 127
	// ExitNotExecutable is sh's status when it exists but cannot be run —
	// a missing execute bit, or a bad interpreter line.
	ExitNotExecutable = 126
)

// CommandDidNotRun reports whether an exit code means the sensor's command was
// never executed, as opposed to executed and returning a failure.
//
// The distinction is the whole point of calibration. A reference declaring
// `expect: "fail"` is asking "does this sensor still detect the defect in the
// fixture?" — and any non-zero status answers yes, including 127. So a sensor
// whose script has been deleted, renamed or had its execute bit stripped
// reports as successfully calibrated: the single worst false positive available
// in a feature whose entire purpose is catching sensors that have stopped
// observing.
//
// It is not a theoretical hole. `ynh check` reaches the same code, so a sensor
// that cannot run looks like an ordinary failure — and `--update-baseline`
// would then record "command not found" as accepted debt and forgive it
// permanently.
func CommandDidNotRun(exitCode int) bool {
	return exitCode == ExitCommandNotFound || exitCode == ExitNotExecutable
}

// ExitObservationUnavailable marks a sensor that ran but could not obtain the
// observation it exists to make: the GitHub status it selects is absent, the
// check run has not concluded, or the API could not be reached.
//
// It is separate from a failing sensor for the reason CommandDidNotRun is.
// "The scanner says this commit is bad" and "no scanner has reported on this
// commit" are different facts, and only the first is a finding about the code.
// Collapsing them would let a renamed status, a revoked token or an unreachable
// network read as either a clean bill of health or a defect, and both are lies.
const ExitObservationUnavailable = -2

// ObservationUnavailable reports whether an exit code means the sensor could
// not see, as opposed to seeing something bad.
func ObservationUnavailable(exitCode int) bool {
	return exitCode == ExitObservationUnavailable
}
