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

// Baseline is the recorded state of every sensor at the moment it was taken.
type Baseline struct {
	Version    int                       `json:"version"`
	RecordedAt string                    `json:"recorded_at"`
	Sensors    map[string]SensorBaseline `json:"sensors"`
}

// SensorBaseline is one sensor's recorded failure surface.
type SensorBaseline struct {
	// Status is the sensor's status when recorded. Only failing sensors are
	// worth recording; a passing sensor has nothing to forgive.
	Status string `json:"status"`
	// Fingerprints are the normalised, sorted hashes of each output line.
	Fingerprints []string `json:"fingerprints"`
	// Count is len(Fingerprints), stored so a human reading the file can see
	// the size of the debt without counting hashes.
	Count int `json:"count"`
	// Truncated records that the sensor emitted more distinct lines than
	// maxFingerprints. Such a sensor cannot ratchet precisely, and callers
	// must say so rather than silently under-reporting new failures.
	Truncated bool `json:"truncated,omitempty"`
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
	sb := SensorBaseline{Status: status}
	if len(fps) > maxFingerprints {
		sb.Truncated = true
		fps = fps[:maxFingerprints]
	}
	sb.Fingerprints = fps
	sb.Count = len(fps)
	return sb
}

// Compare classifies a sensor's current failure against the baseline.
//
// known is the count of current fingerprints the baseline already had.
// fixed is the count the baseline had and the current run does not — debt
// that has been paid off, which is what lets the ratchet tighten.
func (b *Baseline) Compare(sensor string, current []string) (newFPs []string, known, fixed int) {
	if b == nil {
		return current, 0, 0
	}
	sb, ok := b.Sensors[sensor]
	if !ok {
		return current, 0, 0
	}
	recorded := map[string]bool{}
	for _, fp := range sb.Fingerprints {
		recorded[fp] = true
	}
	cur := map[string]bool{}
	for _, fp := range current {
		cur[fp] = true
		if recorded[fp] {
			known++
		} else {
			newFPs = append(newFPs, fp)
		}
	}
	for fp := range recorded {
		if !cur[fp] {
			fixed++
		}
	}
	return newFPs, known, fixed
}

// Truncated reports whether the sensor's baseline was capped, meaning its
// ratchet is approximate.
func (b *Baseline) Truncated(sensor string) bool {
	if b == nil {
		return false
	}
	return b.Sensors[sensor].Truncated
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
	if b.Sensors == nil {
		b.Sensors = map[string]SensorBaseline{}
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
