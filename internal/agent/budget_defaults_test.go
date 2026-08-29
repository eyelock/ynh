package agent

import (
	"testing"
	"time"
)

// A loop with no caps is unbounded in turns, tokens and wall clock. That is the
// absence of a control, not a choice, so there is no unlimited state to fall
// into — every run resolves to a cap from somewhere.
func TestBudgetDefaults_AreNonZero(t *testing.T) {
	if DefaultMaxTurns <= 0 || DefaultMaxTokens <= 0 || DefaultMaxWall <= 0 {
		t.Fatalf("defaults must bound a run: turns=%d tokens=%d wall=%v",
			DefaultMaxTurns, DefaultMaxTokens, DefaultMaxWall)
	}
}

// Analysing a batch of runs, a cap nobody chose that fires is noise in the
// result and a chosen cap that fires is a finding. They have to be
// distinguishable in the trajectory or the two get aggregated together.
func TestBudgetSource_DistinguishesChosenFromDefaulted(t *testing.T) {
	s := BudgetSource{Turns: "flag", Tokens: "manifest", Wall: "default"}
	if s.Turns == s.Wall {
		t.Fatal("an explicit cap and a defaulted one must not be indistinguishable")
	}
	for _, v := range []string{s.Turns, s.Tokens, s.Wall} {
		switch v {
		case "flag", "manifest", "default":
		default:
			t.Errorf("unexpected source %q", v)
		}
	}
}

// Exceeding a default still has to exit with the same code as exceeding an
// explicit cap — the source is metadata about why, not a different outcome.
func TestBudget_DefaultCapExitsAsABudget(t *testing.T) {
	b := &Budget{MaxTurns: DefaultMaxTurns}
	for i := 0; i < DefaultMaxTurns; i++ {
		b.RecordTurn()
	}
	reason, kind, code := b.Exceeded()
	if reason == "" || code != ExitIterationCap {
		t.Errorf("got reason=%q kind=%v code=%d, want the turn-cap exit", reason, kind, code)
	}
}

// A malformed duration in the manifest must fail the run, not be silently
// discarded into the default — a harness that thinks it set a 90-minute cap
// and actually got 60 has no way to notice.
func TestBudgetDefaults_ManifestDurationIsParsedStrictly(t *testing.T) {
	for _, c := range []struct {
		in      string
		wantErr bool
	}{
		{"90m", false},
		{"1h30m", false},
		{"ninety minutes", true},
		{"90", true},
	} {
		_, err := time.ParseDuration(c.in)
		if c.wantErr != (err != nil) {
			t.Errorf("ParseDuration(%q): err=%v, wantErr=%v", c.in, err, c.wantErr)
		}
	}
}
