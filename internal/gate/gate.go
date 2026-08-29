// Package gate defines the wire contract of `ynh check` — the verdict, the
// per-sensor results, and the baseline summary that go with them.
//
// It lives in its own package because two things need the same shape: the
// command that produces it (cmd/ynh) and the agent loop that consumes it
// (internal/agent). The loop shelling out to `ynh check` and re-declaring the
// JSON itself would leave two definitions of one contract free to drift, and
// a consumer silently ignoring a field the producer renamed is exactly the
// class of failure a versioned envelope exists to prevent.
//
// The shape is pinned by docs/schema/cli/check.schema.json and
// test/golden/check.json. Changing it is subject to the capability-bump rule.
package gate

// Sensor statuses reported by `ynh check`.
//
// ynh deliberately owns only the thinnest possible pass/fail policy: a
// command sensor passes when it exits 0. Anything richer — thresholds,
// severity filters, convergence judgments — still belongs to a loop driver.
// Sensors whose result cannot be reduced to pass/fail mechanically say so
// rather than guessing, and never gate.
const (
	StatusPass     = "pass"     // command sensor exited 0
	StatusFail     = "fail"     // command sensor exited non-zero
	StatusReported = "reported" // files sensor: content surfaced, no verdict derivable
	StatusDeferred = "deferred" // focus sensor: needs an agent runtime ynh does not own
	StatusSkipped  = "skipped"  // filtered out by --only
	StatusKnown    = "known"    // failing, but every failure is in the baseline
)

// Verdicts. Only VerdictBlocked exits non-zero.
const (
	VerdictPass    = "pass"
	VerdictBlocked = "blocked"
)

// Envelope is the `ynh check --format json` payload.
type Envelope struct {
	Capabilities string        `json:"capabilities"`
	YnhVersion   string        `json:"ynh_version"`
	Harness      string        `json:"harness"`
	Verdict      string        `json:"verdict"` // pass | blocked
	Summary      Summary       `json:"summary"`
	Sensors      []Result      `json:"sensors"`
	Baseline     *BaselineInfo `json:"baseline,omitempty"`
}

// BaselineInfo tells a consumer whether a ratchet is in play and whether it
// could be tightened. Absent when no baseline has been recorded.
type BaselineInfo struct {
	RecordedAt string `json:"recorded_at"`
	Known      int    `json:"known"` // pre-existing failures forgiven this run
	Fixed      int    `json:"fixed"` // baseline entries no longer failing
	Stale      bool   `json:"stale"` // true when Fixed > 0: the baseline can be narrowed
}

// Summary is the per-run tally, one counter per status plus the blocking
// count that produced the verdict.
type Summary struct {
	Total    int `json:"total"`
	Passed   int `json:"passed"`
	Failed   int `json:"failed"`
	Blocking int `json:"blocking"` // failures that caused a block
	Reported int `json:"reported"`
	Deferred int `json:"deferred"`
	Skipped  int `json:"skipped"`
	Known    int `json:"known"` // sensors failing only in ways the baseline records
}

// Result is one sensor's outcome.
type Result struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Category   string `json:"category,omitempty"`
	Tolerance  string `json:"tolerance"`
	Status     string `json:"status"`
	ExitCode   int    `json:"exit_code"`
	DurationMS int64  `json:"duration_ms"`
	Stdout     string `json:"stdout,omitempty"`
	Stderr     string `json:"stderr,omitempty"`
	Note       string `json:"note,omitempty"`
	// NewCount and KnownCount are set for failing command sensors when a
	// baseline exists. NewOutput carries only the lines not in the baseline —
	// the ones the author is actually being asked to fix.
	NewCount   int    `json:"new_count,omitempty"`
	KnownCount int    `json:"known_count,omitempty"`
	NewOutput  string `json:"new_output,omitempty"`
}

// Gating reports whether this result is one the verdict may block on.
//
// It is deliberately not "did it fail". A files sensor has no derivable
// verdict, a focus sensor needs a runtime ynh does not own, and a sensor
// whose every failure is already in the baseline is recorded debt rather than
// a regression. Only StatusFail is a live failure, and only a blocking
// tolerance makes it gate.
func (r Result) Gating() bool {
	return r.Status == StatusFail && r.Tolerance == "blocking"
}

// Ran reports whether the sensor was actually executed this run.
func (r Result) Ran() bool { return r.Status != StatusSkipped }
