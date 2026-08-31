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

// A count ratchet without a filter moves for the wrong reason: the summary
// lines change too, so the total shifts by more than the findings did.
func TestMatch_UnfilteredCountMovesForTheWrongReason(t *testing.T) {
	deltaFiltered := CountLines(lintBefore, findingLine) - CountLines(lintAfter, findingLine)
	deltaRaw := CountLines(lintBefore, nil) - CountLines(lintAfter, nil)

	if deltaFiltered != 1 {
		t.Fatalf("the fixture should remove exactly one finding, removed %d", deltaFiltered)
	}
	if deltaRaw == deltaFiltered {
		t.Skip("this tool's decoration happens not to change; fixture no longer demonstrates the point")
	}
	t.Logf("one finding removed: filtered delta %d, unfiltered delta %d", deltaFiltered, deltaRaw)
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
