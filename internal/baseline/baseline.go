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
//
// # On-disk layout
//
// One file per sensor, under .ynh/baseline/<harness>/<sensor>.json.
//
// A single repository-wide file of sorted hash arrays is maximally
// conflict-prone and minimally mergeable: with several branches in flight
// against one repository, every one of them touches the same lines. That
// matters more than it looks, because **every natural resolution of a baseline
// conflict widens the amnesty** — union keeps both sides' forgiveness, `-X
// ours` keeps one branch's, and regenerating accepts whatever is failing right
// now. A ratchet is monotonic only if nothing concurrent quietly loosens it.
//
// Splitting per sensor means two branches touching different sensors never
// conflict at all, and a conflict that does happen is scoped to one sensor and
// legible in the diff. It cannot make a conflict resolve itself correctly —
// nothing can — so conflicts are surfaced and refused rather than merged. See
// ErrConflicted.
package baseline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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

// SubDir is the directory within Dir holding one file per sensor. It is meant
// to be committed: the ratchet is a property of the repository, not of one
// developer.
const SubDir = "baseline"

// legacyFile is the single-file layout this format replaced. `ynh check` has
// never been released, so nothing in the wild carries one — but a working copy
// from earlier in development might, and silently ignoring it would drop a
// ratchet the developer believes is in force.
const legacyFile = "baseline.json"

// Version is the on-disk format version of a single sensor file.
const Version = 2

// maxFingerprints caps what a single sensor may record. A sensor emitting
// more distinct lines than this is producing noise rather than a violation
// list, and storing it all would bloat a committed file for no benefit.
const maxFingerprints = 2000

// ErrConflicted reports a baseline file left with git conflict markers.
//
// It is deliberately fatal rather than best-effort. Every way of resolving a
// baseline conflict automatically forgives more than either side intended, so
// the only safe behaviour is to stop and say so. Resolving one is a human
// decision about which failures a repository accepts — never an agent's.
var ErrConflicted = errors.New("baseline file contains unresolved merge conflict markers")

// Baseline is the recorded state, scoped by harness id.
//
// The scoping is not cosmetic. One repository may be checked by more than one
// harness, and sensor names are only unique within a harness — two harnesses
// each declaring "lint" would otherwise share, and overwrite, one entry.
type Baseline struct {
	Harnesses map[string]HarnessBaseline
}

// HarnessBaseline is one harness's recorded sensor state.
type HarnessBaseline struct {
	Sensors map[string]SensorBaseline
}

// SensorBaseline is one sensor's recorded failure surface — the content of one
// file on disk.
type SensorBaseline struct {
	Version int    `json:"version"`
	Harness string `json:"harness"`
	Sensor  string `json:"sensor"`
	// RecordedAt is when this sensor's debt was accepted, not when the file
	// was last written. It only moves when the recorded failures actually
	// change, so an untouched sensor produces byte-identical output on every
	// save — which is what keeps unrelated branches from conflicting.
	RecordedAt string `json:"recorded_at"`
	// Status is the sensor's status when recorded. Only failing sensors are
	// worth recording; a passing sensor has nothing to forgive.
	Status string `json:"status"`
	// Count is the number of distinct output lines recorded. For a truncated
	// sensor it is the true count even though Fingerprints is empty, because
	// it is the only thing left to ratchet against.
	Count int `json:"count"`
	// Total is every emitted line, not only the distinct ones. A count-ratchet
	// sensor is measured against this: fingerprints normalise line numbers and
	// deduplicate, so a second identical finding in a file that already has
	// one changes neither Fingerprints nor Count. For a sensor whose quantity
	// is the finding — suppression directives above all — that is the wrong
	// answer, and Total is the only field that moves.
	Total int `json:"total,omitempty"`
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
	// Fingerprints are the normalised, sorted hashes of each output line. One
	// per line in the encoded file, so a diff shows which findings a branch
	// accepted rather than one unreadable hunk.
	Fingerprints []string `json:"fingerprints"`
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
	// CountDelta is how far a count-ratchet sensor has moved: positive is new
	// suppressions, negative is removed ones. Stating the number means a
	// report says how much worse, not merely that it is worse.
	CountDelta int
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
func Fingerprints(output, root string, match *regexp.Regexp) []string {
	seen := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// A sensor that declares output.match records only its findings.
		// Without it, a tool's headers and summary counts become accepted
		// debt, and fixing a real finding changes the summary into a line the
		// baseline has never seen (#338).
		if match != nil && !match.MatchString(line) {
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

// Record builds a SensorBaseline from raw output. RecordedAt is left empty;
// Set fills it, so an unchanged entry keeps the timestamp it already had.
func Record(status, output, root string, match *regexp.Regexp) SensorBaseline {
	fps := Fingerprints(output, root, match)
	sb := SensorBaseline{Status: status, Count: len(fps), Total: CountLines(output, match)}
	if len(fps) > maxFingerprints {
		// Store none rather than an arbitrary subset — see Truncated.
		sb.Truncated = true
		return sb
	}
	sb.Fingerprints = fps
	return sb
}

// CountLines counts every non-empty line, without normalising or
// deduplicating. Two identical findings count twice — which is the whole point
// for a sensor whose quantity is the finding.
func CountLines(output string, match *regexp.Regexp) int {
	n := 0
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// The count ratchet must honour the same filter as the fingerprint
		// ratchet. It is the escape hatch for duplicate findings, and if it
		// counted decoration too there would be no ratchet left that ignores
		// a tool's summary lines (#338).
		if match != nil && !match.MatchString(line) {
			continue
		}
		n++
	}
	return n
}

// CompareTotals ratchets a sensor on how many findings it emits rather than
// which ones.
//
// A nil baseline or an unrecorded sensor means nothing is forgiven. Otherwise
// any increase is a regression, and a decrease is progress worth recording —
// which is what stops a ratchet from silently permitting the count to creep
// back up to an old high-water mark.
func (b *Baseline) CompareTotals(harness, sensor string, currentTotal int) Comparison {
	sb, ok := b.entry(harness, sensor)
	if !ok {
		if currentTotal == 0 {
			return Comparison{}
		}
		return Comparison{Regressed: true, CountDelta: currentTotal}
	}
	return Comparison{
		Known:      min(currentTotal, sb.Total),
		Fixed:      max(0, sb.Total-currentTotal),
		Regressed:  currentTotal > sb.Total,
		CountDelta: currentTotal - sb.Total,
	}
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

// OldestRecordedAt returns the earliest timestamp across every recorded
// sensor, or "" when nothing is recorded.
//
// Oldest rather than newest: a ratchet is only as tight as its oldest
// forgiveness, and the entry nobody has revisited since spring is the one
// most likely to be forgiving something that was fixed long ago. Reporting
// the most recent write would hide exactly that.
func (b *Baseline) OldestRecordedAt() string {
	if b == nil {
		return ""
	}
	oldest := ""
	for _, hb := range b.Harnesses {
		for _, sb := range hb.Sensors {
			if sb.RecordedAt == "" {
				continue
			}
			if oldest == "" || sb.RecordedAt < oldest {
				oldest = sb.RecordedAt
			}
		}
	}
	return oldest
}

// Entry returns a sensor's recorded baseline, for callers that need to report
// what is forgiven rather than merely apply it.
func (b *Baseline) Entry(harness, sensor string) (SensorBaseline, bool) {
	return b.entry(harness, sensor)
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
//
// RecordedAt is carried over from any existing entry whose recorded failures
// are identical. Refreshing it on every save would rewrite every file on every
// `--update-baseline`, turning an unrelated branch's no-op into a conflict —
// and would erase the age signal OldestRecordedAt depends on.
func (b *Baseline) Set(harness, sensor string, sb SensorBaseline) {
	if b.Harnesses == nil {
		b.Harnesses = map[string]HarnessBaseline{}
	}
	hb, ok := b.Harnesses[harness]
	if !ok || hb.Sensors == nil {
		hb = HarnessBaseline{Sensors: map[string]SensorBaseline{}}
	}
	sb.Version = Version
	sb.Harness = harness
	sb.Sensor = sensor
	if prev, had := hb.Sensors[sensor]; had && prev.RecordedAt != "" && sameFailures(prev, sb) {
		sb.RecordedAt = prev.RecordedAt
	} else if sb.RecordedAt == "" {
		sb.RecordedAt = time.Now().UTC().Format(time.RFC3339)
	}
	hb.Sensors[sensor] = sb
	b.Harnesses[harness] = hb
}

func sameFailures(a, b SensorBaseline) bool {
	if a.Status != b.Status || a.Count != b.Count || a.Truncated != b.Truncated {
		return false
	}
	if len(a.Fingerprints) != len(b.Fingerprints) {
		return false
	}
	for i := range a.Fingerprints {
		if a.Fingerprints[i] != b.Fingerprints[i] {
			return false
		}
	}
	return true
}

// Root returns the baseline directory for a repository root.
func Root(root string) string { return filepath.Join(root, Dir, SubDir) }

// Path returns the file a sensor's baseline is stored in.
func Path(root, harness, sensor string) string {
	return filepath.Join(Root(root), safeName(harness), safeName(sensor)+".json")
}

// safeName maps a harness or sensor name to a filename component that cannot
// escape the baseline directory or collide with another name.
//
// Harness names are constrained by the schema, but sensor names are not: they
// are free-form map keys, so "../../etc/passwd" is a legal declaration. The
// name is not read back from the path — every file records its own harness and
// sensor — so this only has to be safe and unique, not reversible. A name that
// is already safe is used verbatim, because a readable path is what makes a
// baseline diff worth reviewing.
func safeName(name string) string {
	safe := name != "" && name != "." && name != ".."
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			safe = false
			b.WriteByte('-')
		}
	}
	if safe {
		return name
	}
	sum := sha256.Sum256([]byte(name))
	return b.String() + "-" + hex.EncodeToString(sum[:])[:8]
}

// Load reads every sensor baseline under root. A missing directory is not an
// error — it means no baseline has been taken, and every failure is new.
// Callers get a nil Baseline, which Compare treats as "nothing is forgiven".
func Load(root string) (*Baseline, error) {
	if _, err := os.Stat(filepath.Join(root, Dir, legacyFile)); err == nil {
		return nil, fmt.Errorf("%s predates the per-sensor baseline layout: delete it and "+
			"re-record with `ynh check <harness> --update-baseline`",
			filepath.Join(Dir, legacyFile))
	}

	dir := Root(root)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading baseline: %w", err)
	}

	b := &Baseline{Harnesses: map[string]HarnessBaseline{}}
	found := false
	for _, hd := range entries {
		if !hd.IsDir() {
			continue
		}
		files, rErr := os.ReadDir(filepath.Join(dir, hd.Name()))
		if rErr != nil {
			return nil, fmt.Errorf("reading baseline: %w", rErr)
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
				continue
			}
			path := filepath.Join(dir, hd.Name(), f.Name())
			sb, lErr := loadSensor(path)
			if lErr != nil {
				return nil, lErr
			}
			b.Set(sb.Harness, sb.Sensor, sb)
			found = true
		}
	}
	if !found {
		return nil, nil
	}
	return b, nil
}

func loadSensor(path string) (SensorBaseline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SensorBaseline{}, fmt.Errorf("reading %s: %w", path, err)
	}
	// Check before parsing: a conflicted file is usually still invalid JSON,
	// but "invalid character '<'" tells the reader nothing about what to do.
	if hasConflictMarkers(data) {
		return SensorBaseline{}, fmt.Errorf("%s: %w — resolve it by deciding which failures this "+
			"repository accepts. Do not take either side wholesale, and do not let an agent "+
			"resolve it: every automatic resolution forgives more than either branch intended",
			path, ErrConflicted)
	}
	var sb SensorBaseline
	if err := json.Unmarshal(data, &sb); err != nil {
		return SensorBaseline{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	if sb.Version != Version {
		return SensorBaseline{}, fmt.Errorf("%s has version %d, this ynh understands %d",
			path, sb.Version, Version)
	}
	if sb.Harness == "" || sb.Sensor == "" {
		return SensorBaseline{}, fmt.Errorf("%s names no harness or sensor", path)
	}
	return sb, nil
}

func hasConflictMarkers(data []byte) bool {
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "<<<<<<< ") || strings.HasPrefix(line, ">>>>>>> ") {
			return true
		}
	}
	return false
}

// Save writes one file per recorded sensor and removes files for sensors no
// longer recorded, so a sensor that went green stops being forgiven.
func Save(root string, b *Baseline) error {
	dir := Root(root)
	want := map[string]bool{}

	for harness, hb := range b.Harnesses {
		for sensor, sb := range hb.Sensors {
			path := Path(root, harness, sensor)
			want[path] = true
			sb.Version = Version
			sb.Harness = harness
			sb.Sensor = sensor
			if sb.RecordedAt == "" {
				sb.RecordedAt = time.Now().UTC().Format(time.RFC3339)
			}
			if sb.Fingerprints == nil {
				sb.Fingerprints = []string{}
			}
			data, err := json.MarshalIndent(sb, "", "  ")
			if err != nil {
				return fmt.Errorf("encoding baseline for %s/%s: %w", harness, sensor, err)
			}
			data = append(data, '\n')
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
			}
			if err := os.WriteFile(path, data, 0o644); err != nil {
				return fmt.Errorf("writing %s: %w", path, err)
			}
		}
	}

	return pruneStale(dir, want)
}

// pruneStale removes baseline files no longer wanted, and any harness
// directory left empty. A sensor that now passes must stop being forgiven, or
// the ratchet only ever loosens.
func pruneStale(dir string, want map[string]bool) error {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading baseline: %w", err)
	}
	for _, hd := range entries {
		if !hd.IsDir() {
			continue
		}
		hdir := filepath.Join(dir, hd.Name())
		files, rErr := os.ReadDir(hdir)
		if rErr != nil {
			return fmt.Errorf("reading baseline: %w", rErr)
		}
		remaining := 0
		for _, f := range files {
			path := filepath.Join(hdir, f.Name())
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".json") {
				remaining++
				continue
			}
			if want[path] {
				remaining++
				continue
			}
			if err := os.Remove(path); err != nil {
				return fmt.Errorf("removing %s: %w", path, err)
			}
		}
		if remaining == 0 {
			if err := os.Remove(hdir); err != nil {
				return fmt.Errorf("removing %s: %w", hdir, err)
			}
		}
	}
	return nil
}

// Fingerprint returns a stable hash over every recorded baseline file, so a
// caller can tell whether the ratchet moved while it was not looking.
//
// This is what makes "the agent may not rewrite its own gate" enforceable
// rather than merely refused: `ynh check --update-baseline` declines inside an
// agent session, but nothing stops a worker editing the JSON directly.
// Comparing this across a run detects that.
func Fingerprint(root string) (string, error) {
	b, err := Load(root)
	if err != nil {
		return "", err
	}
	if b == nil {
		return "empty", nil
	}
	harnesses := make([]string, 0, len(b.Harnesses))
	for h := range b.Harnesses {
		harnesses = append(harnesses, h)
	}
	sort.Strings(harnesses)

	var sb strings.Builder
	for _, h := range harnesses {
		sensors := make([]string, 0, len(b.Harnesses[h].Sensors))
		for s := range b.Harnesses[h].Sensors {
			sensors = append(sensors, s)
		}
		sort.Strings(sensors)
		for _, s := range sensors {
			e := b.Harnesses[h].Sensors[s]
			// RecordedAt is deliberately excluded: it moves only when the
			// recorded failures move, so including it would add nothing, and
			// excluding it keeps the fingerprint a statement about what is
			// forgiven rather than about when.
			fmt.Fprintf(&sb, "%s\x00%s\x00%s\x00%d\x00%t\x00%s\n",
				h, s, e.Status, e.Count, e.Truncated, strings.Join(e.Fingerprints, ","))
		}
	}
	sum := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(sum[:])[:16], nil
}
