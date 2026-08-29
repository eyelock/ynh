package baseline

import (
	"os"
	"path/filepath"
	"testing"
)

// The property that makes a baseline usable at all: editing a file must not
// make every issue below the edit look new. Linters report positions, and
// positions move.
func TestFingerprints_StableAcrossLineMoves(t *testing.T) {
	before := "internal/foo.go:12:5: exported func Bar should have comment"
	after := "internal/foo.go:47:5: exported func Bar should have comment"

	a := Fingerprints(before, "")
	b := Fingerprints(after, "")
	if len(a) != 1 || len(b) != 1 {
		t.Fatalf("want 1 fingerprint each, got %d and %d", len(a), len(b))
	}
	if a[0] != b[0] {
		t.Errorf("fingerprint changed when only the line number moved:\n %s\n %s", a[0], b[0])
	}
}

// A genuinely different message on the same line must not be forgiven.
func TestFingerprints_DistinguishesMessages(t *testing.T) {
	a := Fingerprints("foo.go:1: unused variable x", "")
	b := Fingerprints("foo.go:1: unused variable y", "")
	if a[0] == b[0] {
		t.Error("different messages produced the same fingerprint")
	}
}

// A baseline recorded on one machine has to match on another, so absolute
// paths are made relative to the repository root before hashing.
func TestFingerprints_RootRelative(t *testing.T) {
	a := Fingerprints("/home/alice/repo/foo.go:1: bad", "/home/alice/repo")
	b := Fingerprints("/build/ci/repo/foo.go:1: bad", "/build/ci/repo")
	if a[0] != b[0] {
		t.Error("same issue fingerprinted differently under different checkout paths")
	}
}

func TestFingerprints_IgnoresBlankLinesAndOrder(t *testing.T) {
	a := Fingerprints("one\n\n  \ntwo\n", "")
	b := Fingerprints("two\none", "")
	if len(a) != 2 {
		t.Fatalf("want 2 fingerprints, got %d", len(a))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatal("fingerprint set is order-dependent; it must not be")
		}
	}
}

func TestCompare_NoBaselineMeansEverythingIsNew(t *testing.T) {
	var b *Baseline
	cur := Fingerprints("a\nb", "")
	c := b.Compare("h", "lint", cur, len(cur))
	if len(c.New) != 2 || c.Known != 0 || c.Fixed != 0 {
		t.Errorf("nil baseline: got new=%d known=%d fixed=%d, want 2/0/0", len(c.New), c.Known, c.Fixed)
	}
}

func TestCompare_PreExistingFailuresAreForgiven(t *testing.T) {
	b := newBL("h", "lint", Record("fail", "old one\nold two", ""))
	c := cmpOf(b, "lint", Fingerprints("old one\nold two", ""))
	newFPs, known, fixed := c.New, c.Known, c.Fixed
	if len(newFPs) != 0 {
		t.Errorf("unchanged failures must not block, got %d new", len(newFPs))
	}
	if known != 2 || fixed != 0 {
		t.Errorf("got known=%d fixed=%d, want 2/0", known, fixed)
	}
}

func TestCompare_OnlyNewFailuresBlock(t *testing.T) {
	b := newBL("h", "lint", Record("fail", "old one\nold two", ""))
	c := cmpOf(b, "lint", Fingerprints("old one\nold two\nbrand new", ""))
	newFPs, known := c.New, c.Known
	if len(newFPs) != 1 {
		t.Fatalf("want exactly 1 new fingerprint, got %d", len(newFPs))
	}
	if known != 2 {
		t.Errorf("known = %d, want 2", known)
	}
}

// Debt paid off is what lets the ratchet tighten; the caller needs to know
// it happened so it can offer to narrow the baseline.
func TestCompare_ReportsFixedDebt(t *testing.T) {
	b := newBL("h", "lint", Record("fail", "old one\nold two\nold three", ""))
	c := cmpOf(b, "lint", Fingerprints("old one", ""))
	known, fixed := c.Known, c.Fixed
	if known != 1 || fixed != 2 {
		t.Errorf("got known=%d fixed=%d, want 1/2", known, fixed)
	}
}

// A sensor absent from the baseline was passing when it was taken, so any
// failure it now reports is a regression and must block.
func TestCompare_UnknownSensorIsAllNew(t *testing.T) {
	b := &Baseline{Harnesses: map[string]HarnessBaseline{}}
	c := cmpOf(b, "lint", Fingerprints("boom", ""))
	newFPs, known := c.New, c.Known
	if len(newFPs) != 1 || known != 0 {
		t.Errorf("got new=%d known=%d, want 1/0", len(newFPs), known)
	}
}

func TestLoadSave_RoundTrip(t *testing.T) {
	root := t.TempDir()
	if b, err := Load(root); b != nil || err != nil {
		t.Fatalf("missing baseline: got (%v, %v), want (nil, nil)", b, err)
	}

	want := newBL("h", "lint", Record("fail", "one\ntwo", ""))
	if err := Save(root, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, Dir, File)); err != nil {
		t.Fatalf("baseline not written: %v", err)
	}

	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Version != Version || got.RecordedAt == "" {
		t.Errorf("version/timestamp not persisted: %+v", got)
	}
	if got.Harnesses["h"].Sensors["lint"].Count != 2 {
		t.Errorf("count = %d, want 2", got.Harnesses["h"].Sensors["lint"].Count)
	}
}

func TestLoad_RejectsFutureVersion(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, Dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(root), []byte(`{"version":99,"harnesses":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil {
		t.Error("a baseline from a newer ynh must be rejected, not silently ignored")
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func newBL(harness, sensor string, sb SensorBaseline) *Baseline {
	b := &Baseline{}
	b.Set(harness, sensor, sb)
	return b
}

func cmpOf(b *Baseline, sensor string, current []string) Comparison {
	return b.Compare("h", sensor, current, len(current))
}

// --- regressions ---

// Sensor names are unique only within a harness. One repo-wide map keyed by
// sensor name meant checking harness B erased harness A's forgiven debt.
func TestCompare_HarnessesAreIsolated(t *testing.T) {
	b := &Baseline{}
	b.Set("harness-a", "lint", Record("fail", "a-issue", ""))
	b.Set("harness-b", "lint", Record("fail", "b-issue", ""))

	if c := b.Compare("harness-a", "lint", Fingerprints("a-issue", ""), 1); len(c.New) != 0 {
		t.Errorf("harness-a should forgive its own issue, got %d new", len(c.New))
	}
	if c := b.Compare("harness-b", "lint", Fingerprints("a-issue", ""), 1); len(c.New) != 1 {
		t.Errorf("harness-b must not forgive harness-a's issue, got %d new", len(c.New))
	}
}

// Truncation used to keep whichever 2000 hashes sorted first — unrelated to
// content — so every line outside that subset was permanently new and
// permanently blocking. A truncated sensor now keeps no fingerprints and
// ratchets on count alone, and says the comparison is approximate.
func TestCompare_TruncatedRatchetsOnCountOnly(t *testing.T) {
	var big []byte
	for i := 0; i < maxFingerprints+50; i++ {
		big = append(big, []byte("issue "+itoa(i)+"\n")...)
	}
	rec := Record("fail", string(big), "")
	if len(rec.Fingerprints) != 0 {
		t.Fatalf("a truncated sensor must keep no arbitrary subset, kept %d", len(rec.Fingerprints))
	}
	if rec.Count != maxFingerprints+50 {
		t.Fatalf("count = %d, want the true line count", rec.Count)
	}

	b := newBL("h", "lint", rec)
	same := b.Compare("h", "lint", nil, rec.Count)
	if same.Regressed || !same.Approximate {
		t.Errorf("same count must not regress; got regressed=%v approximate=%v", same.Regressed, same.Approximate)
	}
	worse := b.Compare("h", "lint", nil, rec.Count+1)
	if !worse.Regressed {
		t.Error("more failing lines than recorded must regress")
	}
	better := b.Compare("h", "lint", nil, rec.Count-10)
	if better.Fixed != 10 {
		t.Errorf("fixed = %d, want 10", better.Fixed)
	}
}

// A sensor that fails with no output produces no fingerprints. Gating
// forgiveness on a non-zero known count made it impossible to ever baseline.
func TestHas_SilentFailureCanBeBaselined(t *testing.T) {
	b := newBL("h", "quiet", Record("fail", "", ""))
	if !b.Has("h", "quiet") {
		t.Fatal("a silently failing sensor must still record an entry")
	}
	c := b.Compare("h", "quiet", nil, 0)
	if len(c.New) != 0 || c.Fixed != 0 {
		t.Errorf("got new=%d fixed=%d, want 0/0 — nothing changed, so nothing is new or fixed",
			len(c.New), c.Fixed)
	}
}

func TestRecordedCount_ReportsDebtForClearedSensor(t *testing.T) {
	b := newBL("h", "lint", Record("fail", "one\ntwo\nthree", ""))
	if got := b.RecordedCount("h", "lint"); got != 3 {
		t.Errorf("RecordedCount = %d, want 3", got)
	}
	if got := b.RecordedCount("h", "absent"); got != 0 {
		t.Errorf("absent sensor RecordedCount = %d, want 0", got)
	}
}
