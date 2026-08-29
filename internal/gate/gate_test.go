package gate

import "testing"

// Gating is deliberately not "did it fail". Every case below is a result that
// looks like a failure from one angle and must not block from the gate's.
func TestResultGating(t *testing.T) {
	cases := []struct {
		name string
		r    Result
		want bool
	}{
		{"failing blocking command gates", Result{Kind: "command", Tolerance: "blocking", Status: StatusFail}, true},
		{"advisory failure does not", Result{Kind: "command", Tolerance: "advisory", Status: StatusFail}, false},
		{"report failure does not", Result{Kind: "command", Tolerance: "report", Status: StatusFail}, false},
		// Recorded debt is not a regression. Blocking on it makes the first
		// run unwinnable on any repository that is not already clean.
		{"baselined failure does not", Result{Kind: "command", Tolerance: "blocking", Status: StatusKnown}, false},
		// A files sensor has no derivable verdict and a focus sensor needs a
		// runtime ynh does not own, so neither gates whatever its tolerance.
		{"files sensor does not", Result{Kind: "files", Tolerance: "blocking", Status: StatusReported}, false},
		{"focus sensor does not", Result{Kind: "focus", Tolerance: "blocking", Status: StatusDeferred}, false},
		{"passing does not", Result{Kind: "command", Tolerance: "blocking", Status: StatusPass}, false},
		{"skipped does not", Result{Kind: "command", Tolerance: "blocking", Status: StatusSkipped}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.r.Gating(); got != c.want {
				t.Errorf("Gating() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestResultRan(t *testing.T) {
	if (Result{Status: StatusSkipped}).Ran() {
		t.Error("a skipped sensor did not run")
	}
	for _, s := range []string{StatusPass, StatusFail, StatusKnown, StatusReported, StatusDeferred} {
		if !(Result{Status: s}).Ran() {
			t.Errorf("status %q means the sensor ran", s)
		}
	}
}
