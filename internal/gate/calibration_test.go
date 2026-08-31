package gate

import "testing"

// A command that never ran must be distinguishable from one that ran and
// failed. Without this, a sensor whose script has been deleted satisfies an
// `expect: "fail"` reference and reports as calibrated — the feature reporting
// success for precisely the condition it exists to detect.
func TestCommandDidNotRun(t *testing.T) {
	cases := []struct {
		name string
		code int
		want bool
	}{
		{"command not found", 127, true},
		{"not executable", 126, true},
		{"ran and passed", 0, false},
		{"ran and failed", 1, false},
		{"ran and failed hard", 2, false},
		// 125 and below are ordinary statuses a sensor may legitimately return.
		{"just below the reserved range", 125, false},
		// 128+n is a signal death: the command did run, and was killed.
		{"killed by SIGKILL", 137, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CommandDidNotRun(c.code); got != c.want {
				t.Errorf("CommandDidNotRun(%d) = %v, want %v", c.code, got, c.want)
			}
		})
	}
}
