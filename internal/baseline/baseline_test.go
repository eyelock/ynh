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
	newFPs, known, fixed := b.Compare("lint", cur)
	if len(newFPs) != 2 || known != 0 || fixed != 0 {
		t.Errorf("nil baseline: got new=%d known=%d fixed=%d, want 2/0/0", len(newFPs), known, fixed)
	}
}

func TestCompare_PreExistingFailuresAreForgiven(t *testing.T) {
	b := &Baseline{Sensors: map[string]SensorBaseline{
		"lint": Record("fail", "old one\nold two", ""),
	}}
	newFPs, known, fixed := b.Compare("lint", Fingerprints("old one\nold two", ""))
	if len(newFPs) != 0 {
		t.Errorf("unchanged failures must not block, got %d new", len(newFPs))
	}
	if known != 2 || fixed != 0 {
		t.Errorf("got known=%d fixed=%d, want 2/0", known, fixed)
	}
}

func TestCompare_OnlyNewFailuresBlock(t *testing.T) {
	b := &Baseline{Sensors: map[string]SensorBaseline{
		"lint": Record("fail", "old one\nold two", ""),
	}}
	newFPs, known, _ := b.Compare("lint", Fingerprints("old one\nold two\nbrand new", ""))
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
	b := &Baseline{Sensors: map[string]SensorBaseline{
		"lint": Record("fail", "old one\nold two\nold three", ""),
	}}
	_, known, fixed := b.Compare("lint", Fingerprints("old one", ""))
	if known != 1 || fixed != 2 {
		t.Errorf("got known=%d fixed=%d, want 1/2", known, fixed)
	}
}

// A sensor absent from the baseline was passing when it was taken, so any
// failure it now reports is a regression and must block.
func TestCompare_UnknownSensorIsAllNew(t *testing.T) {
	b := &Baseline{Sensors: map[string]SensorBaseline{}}
	newFPs, known, _ := b.Compare("lint", Fingerprints("boom", ""))
	if len(newFPs) != 1 || known != 0 {
		t.Errorf("got new=%d known=%d, want 1/0", len(newFPs), known)
	}
}

func TestRecord_TruncatesRunawayOutput(t *testing.T) {
	var sb []byte
	for i := 0; i < maxFingerprints+50; i++ {
		sb = append(sb, []byte("issue number "+itoa(i)+"\n")...)
	}
	rec := Record("fail", string(sb), "")
	if !rec.Truncated {
		t.Error("want Truncated=true for output above the cap")
	}
	if len(rec.Fingerprints) != maxFingerprints {
		t.Errorf("stored %d fingerprints, want cap of %d", len(rec.Fingerprints), maxFingerprints)
	}
}

func TestLoadSave_RoundTrip(t *testing.T) {
	root := t.TempDir()
	if b, err := Load(root); b != nil || err != nil {
		t.Fatalf("missing baseline: got (%v, %v), want (nil, nil)", b, err)
	}

	want := &Baseline{Sensors: map[string]SensorBaseline{
		"lint": Record("fail", "one\ntwo", ""),
	}}
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
	if got.Sensors["lint"].Count != 2 {
		t.Errorf("count = %d, want 2", got.Sensors["lint"].Count)
	}
}

func TestLoad_RejectsFutureVersion(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, Dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(root), []byte(`{"version":99,"sensors":{}}`), 0o644); err != nil {
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
