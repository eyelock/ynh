package baseline

import (
	"regexp"
	"testing"
)

// golangci-lint before and after one genuine repair. Without a match, the
// summary lines are fingerprinted alongside the findings, so fixing one
// finding produces lines the baseline has never seen and the gate reports a
// correct repair as a regression (#338).
const lintBefore = `main.go:10:2: Error return value is not checked (errcheck)
util.go:20:2: Error return value is not checked (errcheck)
api.go:30:2: Error return value is not checked (errcheck)

3 issues:
* errcheck: 3
`

const lintAfter = `main.go:10:2: Error return value is not checked (errcheck)
util.go:20:2: Error return value is not checked (errcheck)

2 issues:
* errcheck: 2
`

// findingLine is what a sensor author would declare for golangci-lint.
var findingLine = regexp.MustCompile(`^[^ ]+\.go:[0-9]+:[0-9]+:`)

func novel(before, after []string) int {
	old := make(map[string]bool, len(before))
	for _, f := range before {
		old[f] = true
	}
	n := 0
	for _, f := range after {
		if !old[f] {
			n++
		}
	}
	return n
}

// The bug, stated as a test: without a match a repair introduces new
// fingerprints; with one it introduces none.
func TestMatch_RepairDoesNotRegisterAsNewFindings(t *testing.T) {
	t.Run("without match, the repair looks like a regression", func(t *testing.T) {
		got := novel(Fingerprints(lintBefore, "", nil), Fingerprints(lintAfter, "", nil))
		if got == 0 {
			t.Fatal("fixture no longer reproduces #338; the summary lines must differ between runs")
		}
		t.Logf("unfiltered: fixing one finding introduced %d new fingerprints", got)
	})

	t.Run("with match, the repair is only a removal", func(t *testing.T) {
		before := Fingerprints(lintBefore, "", findingLine)
		after := Fingerprints(lintAfter, "", findingLine)
		if n := novel(before, after); n != 0 {
			t.Errorf("a correct repair introduced %d new fingerprints; the gate would block on it", n)
		}
		if len(after) >= len(before) {
			t.Errorf("removing a finding should shrink the set: before=%d after=%d", len(before), len(after))
		}
	})
}

// The count ratchet must honour the same filter. It is the escape hatch for
// duplicate findings, and if it counted decoration there would be no ratchet
// left that ignores a tool's summary.
func TestMatch_CountLinesHonoursTheFilter(t *testing.T) {
	unfiltered := CountLines(lintBefore, nil)
	filtered := CountLines(lintBefore, findingLine)

	if filtered != 3 {
		t.Errorf("filtered count = %d, want 3 findings", filtered)
	}
	if unfiltered <= filtered {
		t.Errorf("unfiltered count %d should include the decoration the filter drops", unfiltered)
	}

	// And the count must fall by exactly one across the repair.
	if got := CountLines(lintAfter, findingLine); got != filtered-1 {
		t.Errorf("after one repair, filtered count = %d, want %d", got, filtered-1)
	}
}

// Its own fixture, because the shared one cannot show this.
//
// In lintBefore/lintAfter the summary keeps the same number of lines across the
// repair; only their text changes. That moves the fingerprints, which is what
// TestMatch_RepairDoesNotRegisterAsNewFindings proves, but it leaves the raw
// line count moving by exactly the number of findings removed. The test below
// used to run against that fixture, find the two deltas equal, and skip itself
// on "the fixture no longer demonstrates the point" -- so the count ratchet's
// justification went unproven in every run.
//
// A count ratchet is misled when the decoration changes in LENGTH. golangci-lint
// prints one summary line per linter that reported, so repairing the last
// finding of a linter removes its summary line as well.
const lintBeforeTwoLinters = `main.go:10:2: Error return value is not checked (errcheck)
util.go:20:2: Error return value is not checked (errcheck)
api.go:30:2: Error return value is not checked (errcheck)
main.go:12:5: printf: non-constant format string (govet)

4 issues:
* errcheck: 3
* govet: 1
`

const lintAfterTwoLinters = `main.go:10:2: Error return value is not checked (errcheck)
util.go:20:2: Error return value is not checked (errcheck)
api.go:30:2: Error return value is not checked (errcheck)

3 issues:
* errcheck: 3
`

// A count ratchet without a filter moves for the wrong reason: the summary
// lines change too, so the total shifts by more than the findings did.
func TestMatch_UnfilteredCountMovesForTheWrongReason(t *testing.T) {
	deltaFiltered := CountLines(lintBeforeTwoLinters, findingLine) - CountLines(lintAfterTwoLinters, findingLine)
	deltaRaw := CountLines(lintBeforeTwoLinters, nil) - CountLines(lintAfterTwoLinters, nil)

	if deltaFiltered != 1 {
		t.Fatalf("the fixture should remove exactly one finding, removed %d", deltaFiltered)
	}
	// Assert, do not skip. If a change to CountLines ever makes these equal,
	// the filter has stopped earning its place and that is a result worth
	// failing on, not stepping around.
	if deltaRaw <= deltaFiltered {
		t.Fatalf("unfiltered delta %d should exceed the %d finding(s) actually repaired; "+
			"without a filter the count ratchet would credit the repair with removing "+
			"the summary line too", deltaRaw, deltaFiltered)
	}
	t.Logf("one finding repaired: filtered delta %d, unfiltered delta %d", deltaFiltered, deltaRaw)
}

// Record threads the matcher into both Count and Total, so a recorded baseline
// carries findings rather than findings plus furniture.
func TestMatch_RecordUsesTheFilterForBothRatchets(t *testing.T) {
	rec := Record("fail", lintBefore, "", findingLine)
	if rec.Count != 3 {
		t.Errorf("Count = %d, want 3 findings", rec.Count)
	}
	if rec.Total != 3 {
		t.Errorf("Total = %d, want 3; the count ratchet must not include decoration", rec.Total)
	}

	raw := Record("fail", lintBefore, "", nil)
	if raw.Total <= rec.Total {
		t.Errorf("unfiltered Total %d should exceed filtered %d", raw.Total, rec.Total)
	}
}

// A nil matcher must behave exactly as before, or every existing baseline
// silently changes meaning on upgrade.
func TestMatch_NilIsUnchangedBehaviour(t *testing.T) {
	out := "a.go:1:1: one\nb.go:2:2: two\n\nsummary\n"
	if got, want := len(Fingerprints(out, "", nil)), 3; got != want {
		t.Errorf("nil matcher fingerprints = %d, want %d (every non-blank line)", got, want)
	}
	if got, want := CountLines(out, nil), 3; got != want {
		t.Errorf("nil matcher count = %d, want %d", got, want)
	}
}

// A pattern matching nothing yields nothing. The gate reports this rather than
// recording an empty baseline; asserted here at the mechanism level.
func TestMatch_SelectingNothingYieldsNothing(t *testing.T) {
	never := regexp.MustCompile(`^\x00never-matches`)
	if got := Fingerprints(lintBefore, "", never); len(got) != 0 {
		t.Errorf("expected no fingerprints, got %d", len(got))
	}
	if got := CountLines(lintBefore, never); got != 0 {
		t.Errorf("expected no counted lines, got %d", got)
	}
}
