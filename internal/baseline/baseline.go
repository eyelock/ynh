// Package baseline records which sensor failures a repository already had, so
// a gate can block on new problems without demanding the old ones be fixed
// first.
//
// The problem it solves: a sensor is an arbitrary command that exits 0 or
// non-zero. There is no violation count to compare against. Blocking on the
// exit code alone makes the very first run unwinnable on any repository that
// is not already clean — which is every real repository — and a gate nobody
// can satisfy is a gate everybody disables.
//
// So a baseline stores a fingerprint per output line. A failure whose
// fingerprints are all present in the baseline is pre-existing and does not
// block; anything new does, and only the new lines are shown.
package baseline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Dir is the per-repository directory holding gate state.
const Dir = ".ynh"

// File is the baseline filename within Dir. It is meant to be committed:
// the ratchet is a property of the repository, not of one developer.
const File = "baseline.json"

// Version is the on-disk format version.
const Version = 1

// maxFingerprints caps what a single sensor may record. A sensor emitting
// more distinct lines than this is producing noise rather than a violation
// list, and storing it all would bloat a committed file for no benefit.
const maxFingerprints = 2000

// Baseline is the recorded state at the moment it was taken, scoped by
// harness id.
//
// The scoping is not cosmetic. One repository may be checked by more than one
// harness, and sensor names are only unique within a harness — two harnesses
// each declaring "lint" would otherwise share, and overwrite, one entry.
type Baseline struct {
	Version    int                        `json:"version"`
	RecordedAt string                     `json:"recorded_at"`
	Harnesses  map[string]HarnessBaseline `json:"harnesses"`
}

// HarnessBaseline is one harness's recorded sensor state.
type HarnessBaseline struct {
	Sensors map[string]SensorBaseline `json:"sensors"`
}

// SensorBaseline is one sensor's recorded failure surface.
type SensorBaseline struct {
	// Status is the sensor's status when recorded. Only failing sensors are
	// worth recording; a passing sensor has nothing to forgive.
	Status string `json:"status"`
	// Fingerprints are the normalised, sorted hashes of each output line.
	Fingerprints []string `json:"fingerprints"`
	// Count is the number of distinct output lines recorded. For a truncated
	// sensor it is the true count even though Fingerprints is empty, because
	// it is the only thing left to ratchet against.
	Count int `json:"count"`
	// Truncated records that the sensor emitted more distinct lines than
	// maxFingerprints.
	//
	// No fingerprints are stored in that case. Keeping an arbitrary subset
	// would be worse than keeping none: Fingerprints returns hashes in sorted
	// order, so a subset is whichever hashes happen to sort first — unrelated
	// to content — and every line outside it would be permanently new and
	// permanently blocking. A truncated sensor ratchets on count alone, and
	// callers must report that the comparison is approximate.
	Truncated bool `json:"truncated,omitempty"`
}

// Comparison is the result of measuring a sensor's current failures against
// what was recorded.
type Comparison struct {
	// New holds fingerprints absent from the baseline. Always empty for a
	// truncated sensor, which has no fingerprints to compare.
	New []string
	// Known counts current fingerprints the baseline already had.
	Known int
	// Fixed counts baseline entries no longer failing — debt paid off.
	Fixed int
	// Approximate is set for a truncated sensor: the verdict came from
	// counting lines, not identifying them.
	Approximate bool
	// Regressed is the verdict for a truncated sensor — more distinct
	// failing lines than were recorded.
	Regressed bool
}

// lineNumbers matches the ":12" and ":12:34" position suffixes compilers and
// linters attach to a path. They are stripped before hashing: an issue does
// not become a different issue because someone inserted a line above it, and
// a baseline that thought otherwise would report the whole file as new after
// any edit.
var lineNumbers = regexp.MustCompile(`:\d+(:\d+)?`)

// Fingerprints reduces raw sensor output to a sorted, deduplicated set of
// hashes. Blank lines are dropped, absolute paths under root are made
// relative so a baseline recorded on one machine matches on another, and
// line/column positions are collapsed.
func Fingerprints(output, root string) []string {
	seen := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if root != "" {
			line = strings.ReplaceAll(line, root+string(filepath.Separator), "")
			line = strings.ReplaceAll(line, root, "")
		}
		line = lineNumbers.ReplaceAllString(line, ":N")
		sum := sha256.Sum256([]byte(line))
		seen[hex.EncodeToString(sum[:])[:12]] = true
	}
	out := make([]string, 0, len(seen))
	for fp := range seen {
		out = append(out, fp)
	}
	sort.Strings(out)
	return out
}

// Record builds a SensorBaseline from raw output.
func Record(status, output, root string) SensorBaseline {
	fps := Fingerprints(output, root)
	sb := SensorBaseline{Status: status, Count: len(fps)}
	if len(fps) > maxFingerprints {
		// Store none rather than an arbitrary subset — see Truncated.
		sb.Truncated = true
		return sb
	}
	sb.Fingerprints = fps
	return sb
}

// Compare measures a sensor's current failures against what was recorded.
//
// A nil baseline, or a sensor with no recorded entry, means nothing is
// forgiven: every current failure is new. A sensor absent from the baseline
// was passing when it was taken, so anything it reports now is a regression.
func (b *Baseline) Compare(harness, sensor string, current []string, currentCount int) Comparison {
	sb, ok := b.entry(harness, sensor)
	if !ok {
		return Comparison{New: current}
	}

	if sb.Truncated {
		// No fingerprints were kept, so the only honest comparison is how
		// many distinct lines there are now versus then.
		return Comparison{
			Approximate: true,
			Regressed:   currentCount > sb.Count,
			Known:       min(currentCount, sb.Count),
			Fixed:       max(0, sb.Count-currentCount),
		}
	}

	recorded := make(map[string]bool, len(sb.Fingerprints))
	for _, fp := range sb.Fingerprints {
		recorded[fp] = true
	}
	var c Comparison
	cur := make(map[string]bool, len(current))
	for _, fp := range current {
		cur[fp] = true
		if recorded[fp] {
			c.Known++
		} else {
			c.New = append(c.New, fp)
		}
	}
	for fp := range recorded {
		if !cur[fp] {
			c.Fixed++
		}
	}
	return c
}

// Has reports whether a sensor has a recorded entry. This is the test for
// "was this failure already accepted", not a non-zero known count: a sensor
// that fails silently produces no fingerprints, and gating on a count would
// make it impossible to ever baseline.
func (b *Baseline) Has(harness, sensor string) bool {
	_, ok := b.entry(harness, sensor)
	return ok
}

// RecordedCount returns how many distinct failing lines were recorded for a
// sensor, so a caller can report debt cleared when the sensor now passes.
func (b *Baseline) RecordedCount(harness, sensor string) int {
	sb, ok := b.entry(harness, sensor)
	if !ok {
		return 0
	}
	return sb.Count
}

// Truncated reports whether the sensor's baseline was capped, meaning its
// ratchet is approximate.
func (b *Baseline) Truncated(harness, sensor string) bool {
	sb, ok := b.entry(harness, sensor)
	return ok && sb.Truncated
}

func (b *Baseline) entry(harness, sensor string) (SensorBaseline, bool) {
	if b == nil {
		return SensorBaseline{}, false
	}
	hb, ok := b.Harnesses[harness]
	if !ok {
		return SensorBaseline{}, false
	}
	sb, ok := hb.Sensors[sensor]
	return sb, ok
}

// Clear removes a sensor's entry, used when it now passes and has no debt
// left to forgive.
func (b *Baseline) Clear(harness, sensor string) {
	hb, ok := b.Harnesses[harness]
	if !ok {
		return
	}
	delete(hb.Sensors, sensor)
	b.Harnesses[harness] = hb
}

// Set records one sensor's state, creating the harness scope if needed.
func (b *Baseline) Set(harness, sensor string, sb SensorBaseline) {
	if b.Harnesses == nil {
		b.Harnesses = map[string]HarnessBaseline{}
	}
	hb, ok := b.Harnesses[harness]
	if !ok || hb.Sensors == nil {
		hb = HarnessBaseline{Sensors: map[string]SensorBaseline{}}
	}
	hb.Sensors[sensor] = sb
	b.Harnesses[harness] = hb
}

// Path returns the baseline file path for a repository root.
func Path(root string) string {
	return filepath.Join(root, Dir, File)
}

// Load reads the baseline for root. A missing file is not an error — it
// means no baseline has been taken, and every failure is new. Callers get a
// nil Baseline, which Compare treats as "nothing is forgiven".
func Load(root string) (*Baseline, error) {
	data, err := os.ReadFile(Path(root))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading baseline: %w", err)
	}
	var b Baseline
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", Path(root), err)
	}
	if b.Version != Version {
		return nil, fmt.Errorf("%s has version %d, this ynh understands %d", Path(root), b.Version, Version)
	}
	if b.Harnesses == nil {
		b.Harnesses = map[string]HarnessBaseline{}
	}
	return &b, nil
}

// Save writes the baseline, creating .ynh if needed.
func Save(root string, b *Baseline) error {
	b.Version = Version
	if b.RecordedAt == "" {
		b.RecordedAt = time.Now().UTC().Format(time.RFC3339)
	}
	dir := filepath.Join(root, Dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding baseline: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(Path(root), data, 0o644); err != nil {
		return fmt.Errorf("writing baseline: %w", err)
	}
	return nil
}
