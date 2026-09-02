package gate

// BaselineReport is the output of `ynh baseline`.
//
// The ratchet had no read surface: the only way to learn what it forgave was
// to read the JSON by hand, and that JSON stores twelve-character hashes
// rather than findings. "What does this gate let through" — the question an
// auditor actually asks — had no answer.
type BaselineReport struct {
	Capabilities string           `json:"capabilities"`
	YnhVersion   string           `json:"ynh_version"`
	Harness      string           `json:"harness"`
	Summary      BaselineSummary  `json:"summary"`
	Sensors      []BaselineSensor `json:"sensors"`
}

// BaselineSummary counts the shape of the debt.
type BaselineSummary struct {
	Total int `json:"total"`
	// Recorded is how many sensors have accepted debt; Unrecorded how many
	// forgive nothing. A sensor absent from the baseline was passing when it
	// was taken, which is different from one recording zero.
	Recorded   int `json:"recorded"`
	Unrecorded int `json:"unrecorded"`
	Forgiven   int `json:"forgiven"`
}

// BaselineSensor is one sensor's recorded debt.
type BaselineSensor struct {
	Name    string `json:"name"`
	Kind    string `json:"kind,omitempty"`
	Ratchet string `json:"ratchet,omitempty"`
	// Recorded distinguishes "forgives nothing" from "recorded zero".
	Recorded   bool   `json:"recorded"`
	RecordedAt string `json:"recorded_at,omitempty"`
	Status     string `json:"status,omitempty"`
	// Forgiven is the number of distinct findings accepted; Total the raw
	// count a count-ratchet sensor is measured against.
	Forgiven  int  `json:"forgiven,omitempty"`
	Total     int  `json:"total,omitempty"`
	Truncated bool `json:"truncated,omitempty"`
	// Findings are the actual lines the recorded fingerprints forgive,
	// resolved by running the sensor. Present only under --explain: a hash
	// cannot be reversed, but current output can be hashed and matched.
	Findings []string `json:"findings,omitempty"`
	Note     string   `json:"note,omitempty"`
}
