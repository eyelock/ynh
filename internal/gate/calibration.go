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
