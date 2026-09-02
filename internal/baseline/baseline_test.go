package baseline

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The property that makes a baseline usable at all: editing a file must not
// make every issue below the edit look new. Linters report positions, and
// positions move.
func TestFingerprints_StableAcrossLineMoves(t *testing.T) {
	before := "internal/foo.go:12:5: exported func Bar should have comment"
	after := "internal/foo.go:47:5: exported func Bar should have comment"

	a := Fingerprints(before, "", nil)
	b := Fingerprints(after, "", nil)
	if len(a) != 1 || len(b) != 1 {
		t.Fatalf("want 1 fingerprint each, got %d and %d", len(a), len(b))
	}
	if a[0] != b[0] {
		t.Errorf("fingerprint changed when only the line number moved:\n %s\n %s", a[0], b[0])
	}
}

// A genuinely different message on the same line must not be forgiven.
func TestFingerprints_DistinguishesMessages(t *testing.T) {
	a := Fingerprints("foo.go:1: unused variable x", "", nil)
	b := Fingerprints("foo.go:1: unused variable y", "", nil)
	if a[0] == b[0] {
		t.Error("different messages produced the same fingerprint")
	}
}

// A baseline recorded on one machine has to match on another, so absolute
// paths are made relative to the repository root before hashing.
func TestFingerprints_RootRelative(t *testing.T) {
	a := Fingerprints("/home/alice/repo/foo.go:1: bad", "/home/alice/repo", nil)
	b := Fingerprints("/build/ci/repo/foo.go:1: bad", "/build/ci/repo", nil)
	if a[0] != b[0] {
		t.Error("same issue fingerprinted differently under different checkout paths")
	}
}

func TestFingerprints_IgnoresBlankLinesAndOrder(t *testing.T) {
	a := Fingerprints("one\n\n  \ntwo\n", "", nil)
	b := Fingerprints("two\none", "", nil)
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
	cur := Fingerprints("a\nb", "", nil)
	c := b.Compare("h", "lint", cur, len(cur))
	if len(c.New) != 2 || c.Known != 0 || c.Fixed != 0 {
		t.Errorf("nil baseline: got new=%d known=%d fixed=%d, want 2/0/0", len(c.New), c.Known, c.Fixed)
	}
}

func TestCompare_PreExistingFailuresAreForgiven(t *testing.T) {
	b := newBL("h", "lint", Record("fail", "old one\nold two", "", nil))
	c := cmpOf(b, "lint", Fingerprints("old one\nold two", "", nil))
	newFPs, known, fixed := c.New, c.Known, c.Fixed
	if len(newFPs) != 0 {
		t.Errorf("unchanged failures must not block, got %d new", len(newFPs))
	}
	if known != 2 || fixed != 0 {
		t.Errorf("got known=%d fixed=%d, want 2/0", known, fixed)
	}
}

func TestCompare_OnlyNewFailuresBlock(t *testing.T) {
	b := newBL("h", "lint", Record("fail", "old one\nold two", "", nil))
	c := cmpOf(b, "lint", Fingerprints("old one\nold two\nbrand new", "", nil))
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
	b := newBL("h", "lint", Record("fail", "old one\nold two\nold three", "", nil))
	c := cmpOf(b, "lint", Fingerprints("old one", "", nil))
	known, fixed := c.Known, c.Fixed
	if known != 1 || fixed != 2 {
		t.Errorf("got known=%d fixed=%d, want 1/2", known, fixed)
	}
}

// A sensor absent from the baseline was passing when it was taken, so any
// failure it now reports is a regression and must block.
func TestCompare_UnknownSensorIsAllNew(t *testing.T) {
	b := &Baseline{Harnesses: map[string]HarnessBaseline{}}
	c := cmpOf(b, "lint", Fingerprints("boom", "", nil))
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

	want := newBL("h", "lint", Record("fail", "one\ntwo", "", nil))
	if err := Save(root, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(Path(root, "h", "lint")); err != nil {
		t.Fatalf("baseline not written: %v", err)
	}

	got, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if e := got.Harnesses["h"].Sensors["lint"]; e.Version != Version || e.RecordedAt == "" {
		t.Errorf("version/timestamp not persisted: %+v", e)
	}
	if got.Harnesses["h"].Sensors["lint"].Count != 2 {
		t.Errorf("count = %d, want 2", got.Harnesses["h"].Sensors["lint"].Count)
	}
}

func TestLoad_RejectsFutureVersion(t *testing.T) {
	root := t.TempDir()
	writeSensorFile(t, root, "h", "lint", `{"version":99,"harness":"h","sensor":"lint"}`)
	if _, err := Load(root); err == nil {
		t.Error("a baseline from a newer ynh must be rejected, not silently ignored")
	}
}

func writeSensorFile(t *testing.T, root, harness, sensor, body string) {
	t.Helper()
	path := Path(root, harness, sensor)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
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
	b.Set("harness-a", "lint", Record("fail", "a-issue", "", nil))
	b.Set("harness-b", "lint", Record("fail", "b-issue", "", nil))

	if c := b.Compare("harness-a", "lint", Fingerprints("a-issue", "", nil), 1); len(c.New) != 0 {
		t.Errorf("harness-a should forgive its own issue, got %d new", len(c.New))
	}
	if c := b.Compare("harness-b", "lint", Fingerprints("a-issue", "", nil), 1); len(c.New) != 1 {
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
	rec := Record("fail", string(big), "", nil)
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
	b := newBL("h", "quiet", Record("fail", "", "", nil))
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
	b := newBL("h", "lint", Record("fail", "one\ntwo\nthree", "", nil))
	if got := b.RecordedCount("h", "lint"); got != 3 {
		t.Errorf("RecordedCount = %d, want 3", got)
	}
	if got := b.RecordedCount("h", "absent"); got != 0 {
		t.Errorf("absent sensor RecordedCount = %d, want 0", got)
	}
}

// One file per sensor is the whole point of the layout: two branches touching
// different sensors must not touch the same bytes. A single repo-wide file of
// sorted hash arrays conflicts on every concurrent change, and every natural
// resolution of such a conflict widens the amnesty.
func TestSave_OneFilePerSensor(t *testing.T) {
	root := t.TempDir()
	b := &Baseline{}
	b.Set("h", "lint", Record("fail", "a\nb", "", nil))
	b.Set("h", "vet", Record("fail", "c", "", nil))
	if err := Save(root, b); err != nil {
		t.Fatalf("Save: %v", err)
	}
	for _, s := range []string{"lint", "vet"} {
		if _, err := os.Stat(Path(root, "h", s)); err != nil {
			t.Errorf("%s has no file of its own: %v", s, err)
		}
	}
}

// Rewriting an untouched sensor's file turns an unrelated branch's no-op into
// a merge conflict. RecordedAt therefore tracks when the failures were
// accepted, not when the file was last written.
func TestSave_UntouchedSensorIsByteIdentical(t *testing.T) {
	root := t.TempDir()
	b := &Baseline{}
	b.Set("h", "lint", Record("fail", "a", "", nil))
	b.Set("h", "vet", Record("fail", "c", "", nil))
	if err := Save(root, b); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(Path(root, "h", "vet"))
	if err != nil {
		t.Fatal(err)
	}

	reloaded, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	// Model what `ynh check --update-baseline` actually does: re-Set every
	// failing sensor, not only the changed one. A Set that refreshed the
	// timestamp unconditionally would rewrite every file on every run, and
	// turn an unrelated branch's no-op into a merge conflict.
	reloaded.Set("h", "lint", Record("fail", "a\nb", "", nil)) // findings changed
	reloaded.Set("h", "vet", Record("fail", "c", "", nil))     // findings identical
	if err := Save(root, reloaded); err != nil {
		t.Fatal(err)
	}

	after, err := os.ReadFile(Path(root, "h", "vet"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("an untouched sensor's file was rewritten:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// Sensor names are free-form map keys — the schema constrains harness names
// but not these — so "../../etc/passwd" is a legal declaration. A file-per-
// sensor layout must not turn that into a write outside the baseline
// directory.
func TestPath_UnsafeSensorNameCannotEscape(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"../../etc/passwd", "..", ".", "", "a/b", `c\d`} {
		p := Path(root, "h", name)
		rel, err := filepath.Rel(Root(root), p)
		if err != nil || strings.HasPrefix(rel, "..") || filepath.Dir(rel) != "h" {
			t.Errorf("sensor %q escapes the baseline directory: %s", name, p)
		}
	}
}

// Two unsafe names that sanitise to the same characters must not share a file,
// or one sensor's forgiveness would silently become another's.
func TestPath_UnsafeNamesDoNotCollide(t *testing.T) {
	root := t.TempDir()
	if Path(root, "h", "a/b") == Path(root, "h", `a\b`) {
		t.Error("distinct sensor names must not map to one file")
	}
}

// Every automatic resolution of a baseline conflict forgives more than either
// branch intended, so the only safe behaviour is to stop. "invalid character
// '<'" would tell the reader nothing about what to do.
func TestLoad_RefusesConflictedFile(t *testing.T) {
	root := t.TempDir()
	writeSensorFile(t, root, "h", "lint", "<<<<<<< HEAD\n{\"version\":2}\n=======\n{}\n>>>>>>> other\n")

	_, err := Load(root)
	if err == nil {
		t.Fatal("a conflicted baseline must not load")
	}
	if !errors.Is(err, ErrConflicted) {
		t.Errorf("error should identify the conflict, got %v", err)
	}
	if !strings.Contains(err.Error(), "agent") {
		t.Errorf("the error must say a conflict is not an agent's to resolve, got %q", err)
	}
}

// `ynh check` never shipped the single-file layout, but a working copy from
// earlier in development may carry one. Silently ignoring it would drop a
// ratchet the developer believes is in force.
func TestLoad_RejectsLegacySingleFile(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, Dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, Dir, "baseline.json"),
		[]byte(`{"version":1,"harnesses":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil {
		t.Error("a pre-split baseline must be reported, not silently ignored")
	}
}

// A sensor that now passes must stop being forgiven, or the ratchet only ever
// loosens. With a file per sensor that means deleting the file.
func TestSave_PrunesClearedSensors(t *testing.T) {
	root := t.TempDir()
	b := &Baseline{}
	b.Set("h", "lint", Record("fail", "a", "", nil))
	b.Set("h", "vet", Record("fail", "c", "", nil))
	if err := Save(root, b); err != nil {
		t.Fatal(err)
	}

	b.Clear("h", "vet")
	if err := Save(root, b); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(Path(root, "h", "vet")); err == nil {
		t.Error("a cleared sensor's file must be removed, or its debt stays forgiven")
	}
	if _, err := os.Stat(Path(root, "h", "lint")); err != nil {
		t.Errorf("the remaining sensor must survive the prune: %v", err)
	}

	b.Clear("h", "lint")
	if err := Save(root, b); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(Root(root), "h")); err == nil {
		t.Error("an emptied harness directory should not be left behind")
	}
}

// A ratchet is only as tight as its oldest forgiveness. Reporting the most
// recent write would hide the entry nobody has revisited.
func TestOldestRecordedAt(t *testing.T) {
	b := &Baseline{}
	b.Set("h", "old", SensorBaseline{Status: "fail", RecordedAt: "2026-01-02T00:00:00Z"})
	b.Set("h", "new", SensorBaseline{Status: "fail", RecordedAt: "2026-08-02T00:00:00Z"})
	if got := b.OldestRecordedAt(); got != "2026-01-02T00:00:00Z" {
		t.Errorf("OldestRecordedAt() = %q, want the earliest entry", got)
	}
	if got := (*Baseline)(nil).OldestRecordedAt(); got != "" {
		t.Errorf("no baseline should report no timestamp, got %q", got)
	}
}

// Fingerprint is what makes "the agent may not rewrite its own gate"
// enforceable rather than merely refused: --update-baseline declines inside an
// agent session, but nothing stops a worker editing the JSON directly.
func TestFingerprint_MovesOnlyWhenForgivenessMoves(t *testing.T) {
	root := t.TempDir()
	b := &Baseline{}
	b.Set("h", "lint", Record("fail", "a\nb", "", nil))
	if err := Save(root, b); err != nil {
		t.Fatal(err)
	}
	first, err := Fingerprint(root)
	if err != nil {
		t.Fatal(err)
	}

	// Re-saving identical state must not move it, or every run would look
	// like tampering.
	if err := Save(root, b); err != nil {
		t.Fatal(err)
	}
	if again, fErr := Fingerprint(root); fErr != nil || again != first {
		t.Errorf("an unchanged baseline must fingerprint the same: %q vs %q (%v)", first, again, fErr)
	}

	b.Set("h", "lint", Record("fail", "a\nb\nc", "", nil))
	if err := Save(root, b); err != nil {
		t.Fatal(err)
	}
	after, err := Fingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	if after == first {
		t.Error("forgiving one more failure must change the fingerprint")
	}
}

func TestFingerprint_EmptyIsStable(t *testing.T) {
	root := t.TempDir()
	fp, err := Fingerprint(root)
	if err != nil {
		t.Fatalf("no baseline is not an error: %v", err)
	}
	if fp == "" {
		t.Error("an absent baseline should still fingerprint to something comparable")
	}
}

// `ynh check --update-baseline` re-Sets every failing sensor, not only the
// ones whose findings moved. If Set refreshed the timestamp unconditionally,
// every run would rewrite every file — turning an unrelated branch's no-op
// into a merge conflict, and destroying the age signal OldestRecordedAt reads.
//
// Asserted directly rather than through Save: RFC3339 has second granularity,
// so a test that compares two writes inside the same second passes whether or
// not the timestamp was preserved.
func TestSet_KeepsTimestampWhenFailuresUnchanged(t *testing.T) {
	const then = "2026-01-02T03:04:05Z"
	b := &Baseline{}
	rec := Record("fail", "a\nb", "", nil)
	rec.RecordedAt = then
	b.Set("h", "lint", rec)

	b.Set("h", "lint", Record("fail", "a\nb", "", nil)) // same failures, no timestamp
	if got := b.Harnesses["h"].Sensors["lint"].RecordedAt; got != then {
		t.Errorf("re-recording identical failures moved the timestamp: %q, want %q", got, then)
	}

	b.Set("h", "lint", Record("fail", "a\nb\nc", "", nil)) // a new failure accepted
	if got := b.Harnesses["h"].Sensors["lint"].RecordedAt; got == then {
		t.Error("accepting a new failure is a new acceptance and must be timestamped as one")
	}
}

func TestCountLines_CountsDuplicatesSeparately(t *testing.T) {
	// The whole point: two identical findings count twice. Fingerprints
	// deduplicate them into one.
	out := "a.go:12: nolint\na.go:99: nolint\n\n  \nb.go:1: nolint\n"
	if got := CountLines(out, nil); got != 3 {
		t.Errorf("CountLines = %d, want 3 (blank and whitespace lines ignored)", got)
	}
	if got := len(Fingerprints(out, "", nil)); got != 2 {
		t.Errorf("Fingerprints = %d, want 2 — this is the gap CountLines closes", got)
	}
}

// The gaming vector for a ratchet is suppression, not relocation. An agent
// that adds //nolint beside an existing one must not pass.
func TestCompareTotals(t *testing.T) {
	b := &Baseline{Harnesses: map[string]HarnessBaseline{}}
	b.Set("h", "sup", SensorBaseline{Status: "fail", Total: 3})

	t.Run("unchanged is forgiven", func(t *testing.T) {
		c := b.CompareTotals("h", "sup", 3)
		if c.Regressed || c.CountDelta != 0 || c.Known != 3 {
			t.Errorf("got %+v", c)
		}
	})
	t.Run("one more is a regression, and says how much", func(t *testing.T) {
		c := b.CompareTotals("h", "sup", 4)
		if !c.Regressed || c.CountDelta != 1 {
			t.Errorf("got %+v, want regressed with delta 1", c)
		}
	})
	t.Run("fewer is progress", func(t *testing.T) {
		c := b.CompareTotals("h", "sup", 1)
		if c.Regressed {
			t.Error("removing suppressions must not be a regression")
		}
		if c.Fixed != 2 || c.CountDelta != -2 {
			t.Errorf("got %+v, want 2 fixed and delta -2", c)
		}
	})
	t.Run("unrecorded sensor forgives nothing", func(t *testing.T) {
		c := b.CompareTotals("h", "never-seen", 2)
		if !c.Regressed || c.CountDelta != 2 {
			t.Errorf("got %+v — a sensor absent from the baseline was passing when it was taken", c)
		}
	})
	t.Run("unrecorded and clean is fine", func(t *testing.T) {
		if c := b.CompareTotals("h", "never-seen", 0); c.Regressed {
			t.Errorf("got %+v", c)
		}
	})
}

// Record must capture the raw total, not only the distinct count, or a
// count-ratchet sensor has nothing to measure against.
func TestRecord_CapturesTheRawTotal(t *testing.T) {
	sb := Record("fail", "a.go:12: nolint\na.go:99: nolint\n", "", nil)
	if sb.Total != 2 {
		t.Errorf("Total = %d, want 2", sb.Total)
	}
	if sb.Count != 1 {
		t.Errorf("Count = %d, want 1 — distinct lines, which is why Total is needed", sb.Count)
	}
}
