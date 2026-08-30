// Package freshness decides whether a files sensor's artifact is entitled to
// be believed.
//
// A files sensor reads a result some other process left behind — an e2e run, a
// coverage report, a scan. ynh cannot judge what that result *says*: no verdict
// is derivable from arbitrary JSON, and deciding one would be inventing an
// opinion about a format ynh does not own.
//
// But ynh can decide something the artifact cannot say for itself: whether it
// still describes the tree in front of it. An artifact produced against
// different code is not a weaker observation, it is an observation of
// something else. Reporting it as current is the failure mode this package
// exists to close — a gate that goes green because it is reading last month's
// answer is worse than no gate, because it is trusted.
//
// # What "changed" means
//
// Not elapsed time. A coverage report from three weeks ago against code nobody
// touched is perfectly valid; one from five minutes ago against code edited two
// minutes ago is not. Freshness is a function of the inputs, never the clock.
//
// (Sensors that observe something *outside* the repository — a live service, a
// remote queue — genuinely do go stale with time, and nothing here helps them.
// That is a separate axis and deliberately not modelled yet.)
//
// # What counts as an input
//
// A sensor may declare `observes`, the paths its artifact actually depends on.
// When it does not, the whole tree is assumed. That default is deliberately
// strict: if the harness will not say what the artifact depends on, the only
// honest assumption is everything, and the cure is one line of configuration.
// A noisy default that pushes authors toward declaring the truth beats a quiet
// one that lets a stale artifact pass.
//
// Four things are always excluded, because including them makes the check
// invalidate itself:
//
//   - .ynh/ — writing a stamp would change the digest the stamp just recorded.
//   - the sensor's own artifact paths — producing the artifact would
//     immediately invalidate it.
//   - untracked and ignored files — otherwise `make build` writing to bin/
//     would invalidate every artifact in the repository.
//   - .git/ — implied by taking the tree from git in the first place.
//
// The tree is therefore git-tracked files: deterministic, cheap to enumerate,
// and the same set a developer means by "the tree".
package freshness

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// State is what ynh was able to conclude about an artifact.
type State string

const (
	// StateFresh means the artifact describes the tree as it stands.
	StateFresh State = "fresh"
	// StateStale means the inputs moved after the artifact was written. The
	// artifact is a real observation of a tree that no longer exists.
	StateStale State = "stale"
	// StateAbsent means a declared artifact is not there at all. This is the
	// cheapest and most important case: before this package existed, a files
	// sensor pointed at a missing file reported green.
	StateAbsent State = "absent"
	// StateUnknown means ynh could not tell. It is not "nothing was wrong" —
	// it is "no evidence either way", and it is treated as a failure because a
	// gate that cannot see is not a gate that passed.
	StateUnknown State = "unknown"
)

// Basis records what the conclusion rests on, so a consumer can tell a strong
// answer from a weak one. A verdict derived from file timestamps is not the
// same claim as one derived from a recorded digest, and a corpus graded across
// machines needs to know which it got.
type Basis string

const (
	// BasisMTime compared modification times. Correct on a working checkout,
	// unreliable anywhere timestamps are rewritten wholesale — fresh clones,
	// container builds, CI caches, `git worktree add`.
	BasisMTime Basis = "mtime"
	// BasisNone is set when no comparison was possible or none was needed.
	BasisNone Basis = ""
)

// Result is the conclusion plus enough context to explain it to a human.
type Result struct {
	State State
	Basis Basis
	// Reason is a single sentence naming the evidence, e.g. which input is
	// newer than the artifact. A gate that fails without saying why is a gate
	// people route around.
	Reason string
}

// Evaluate decides whether the artifacts matched by a files sensor still
// describe the tree at cwd.
//
// artifacts are the sensor's resolved `files` paths (already globbed, absolute).
// observes are the sensor's declared input globs, relative to cwd; empty means
// the whole tracked tree.
func Evaluate(cwd string, artifacts, observes []string) Result {
	if len(artifacts) == 0 {
		return Result{
			State:  StateAbsent,
			Basis:  BasisNone,
			Reason: "no file matched the sensor's declared paths",
		}
	}

	inputs, err := resolveInputs(cwd, artifacts, observes)
	if err != nil {
		return Result{
			State:  StateUnknown,
			Basis:  BasisNone,
			Reason: err.Error(),
		}
	}
	if len(inputs) == 0 {
		// Nothing to compare against is not the same as nothing having
		// changed. Saying "fresh" here would be the exact false green this
		// package exists to prevent.
		return Result{
			State:  StateUnknown,
			Basis:  BasisNone,
			Reason: "no input files to compare against; declare `observes` or run inside a git repository",
		}
	}

	return compareMTimes(artifacts, inputs)
}

// compareMTimes reports stale when any input is newer than the oldest
// artifact. The oldest is the right end to test: a sensor declaring several
// files is only as current as its least current one.
func compareMTimes(artifacts, inputs []string) Result {
	oldestArtifact, artifactPath, ok := extremeMTime(artifacts, true)
	if !ok {
		return Result{
			State:  StateUnknown,
			Basis:  BasisNone,
			Reason: "could not stat the sensor's artifacts",
		}
	}
	newestInput, inputPath, ok := extremeMTime(inputs, false)
	if !ok {
		return Result{
			State:  StateUnknown,
			Basis:  BasisNone,
			Reason: "could not stat any observed input",
		}
	}

	if newestInput.After(oldestArtifact) {
		return Result{
			State: StateStale,
			Basis: BasisMTime,
			Reason: filepath.Base(inputPath) + " changed after " +
				filepath.Base(artifactPath) + " was written",
		}
	}
	return Result{
		State:  StateFresh,
		Basis:  BasisMTime,
		Reason: "no observed input is newer than " + filepath.Base(artifactPath),
	}
}

// extremeMTime returns the oldest (or newest) modification time among paths,
// and the path that carried it. Unreadable paths are skipped rather than
// failing the whole comparison: a glob that matched a file which vanished
// mid-run should not turn into an unknown verdict for the others.
func extremeMTime(paths []string, oldest bool) (time.Time, string, bool) {
	var best time.Time
	var bestPath string
	found := false
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil || info.IsDir() {
			continue
		}
		mt := info.ModTime()
		if !found || (oldest && mt.Before(best)) || (!oldest && mt.After(best)) {
			best, bestPath, found = mt, p, true
		}
	}
	return best, bestPath, found
}

// resolveInputs expands the observed set to absolute paths, excluding anything
// that would make the check invalidate itself.
func resolveInputs(cwd string, artifacts, observes []string) ([]string, error) {
	exclude := make(map[string]bool, len(artifacts))
	for _, a := range artifacts {
		exclude[a] = true
	}

	var candidates []string
	if len(observes) > 0 {
		for _, pat := range observes {
			matches, err := expandObserved(cwd, pat)
			if err != nil {
				return nil, err
			}
			candidates = append(candidates, matches...)
		}
	} else {
		tracked, err := trackedFiles(cwd)
		if err != nil {
			return nil, err
		}
		candidates = tracked
	}

	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		if exclude[c] || underYnhDir(cwd, c) {
			continue
		}
		out = append(out, c)
	}
	sort.Strings(out)
	return out, nil
}

// expandObserved resolves one `observes` pattern to the files it names.
//
// It is not `filepath.Glob`, and the difference is load-bearing. Go's Match
// treats `**` as an ordinary `*`: it matches within one path element and never
// descends. So `services/**` returns the *directories* one level down and
// `services/**/*.go` silently misses everything deeper than one level — a
// pattern that looks recursive, reports no error, and quietly observes the
// wrong set. Worse for freshness specifically: directory matches carry mtimes
// that say nothing useful, so a harness declaring the obvious pattern would
// end up with no usable inputs at all and read as `unknown`.
//
// Two rules, both chosen so the obvious pattern does the obvious thing:
//
//   - `**` means "and everything beneath". A suffix after it filters the
//     files found, so `services/**/*.go` is every .go file at any depth.
//   - A pattern that resolves to a directory means that whole subtree. So
//     `services`, `services/*` and `services/**` all observe the same files,
//     rather than two of them observing nothing.
func expandObserved(cwd, pat string) ([]string, error) {
	abs := pat
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(cwd, pat)
	}

	if i := strings.Index(abs, "**"); i >= 0 {
		root := strings.TrimRight(abs[:i], string(filepath.Separator))
		if root == "" {
			root = string(filepath.Separator)
		}
		suffix := strings.TrimLeft(abs[i+2:], string(filepath.Separator))
		return walkFiles(root, suffix)
	}

	matches, err := filepath.Glob(abs)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, m := range matches {
		info, statErr := os.Stat(m)
		if statErr != nil {
			continue
		}
		if !info.IsDir() {
			out = append(out, m)
			continue
		}
		nested, wErr := walkFiles(m, "")
		if wErr != nil {
			return nil, wErr
		}
		out = append(out, nested...)
	}
	return out, nil
}

// walkFiles lists every file under root, optionally keeping only those whose
// path matches suffix. An unreadable subtree is skipped rather than failing
// the whole comparison — a permissions problem in one directory should not
// turn every sensor's verdict into `unknown`.
func walkFiles(root, suffix string) ([]string, error) {
	var out []string
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if suffix == "" {
			out = append(out, p)
			return nil
		}
		// Match the suffix against the tail of the path as well as the base
		// name, so both `**/*.go` and `**/testdata/*.json` behave.
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			rel = filepath.Base(p)
		}
		if ok, _ := filepath.Match(suffix, rel); ok {
			out = append(out, p)
			return nil
		}
		if ok, _ := filepath.Match(suffix, filepath.Base(p)); ok {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// underYnhDir reports whether p is inside cwd/.ynh — the gate's own state.
// Baselines and stamps change as a direct result of running the gate, so
// counting them as inputs would make every artifact stale the moment the gate
// recorded anything.
func underYnhDir(cwd, p string) bool {
	rel, err := filepath.Rel(cwd, p)
	if err != nil {
		return false
	}
	return rel == ".ynh" || strings.HasPrefix(rel, ".ynh"+string(filepath.Separator))
}

// trackedFiles lists the repository's tracked files at cwd.
//
// Tracked, not "everything on disk": build output, caches and node_modules are
// untracked, and counting them would mean any build would invalidate every artifact
// in the repository. It also excludes .git without needing a special case.
func trackedFiles(cwd string) ([]string, error) {
	cmd := exec.Command("git", "-C", cwd, "ls-files", "-z", "--cached")
	out, err := cmd.Output()
	if err != nil {
		return nil, errNotAGitRepo
	}
	parts := strings.Split(strings.TrimRight(string(out), "\x00"), "\x00")
	files := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		files = append(files, filepath.Join(cwd, p))
	}
	return files, nil
}

// errNotAGitRepo is the one case where ynh genuinely cannot see. It reports
// unknown rather than guessing, and unknown fails the gate.
var errNotAGitRepo = &noTreeError{}

type noTreeError struct{}

func (e *noTreeError) Error() string {
	return "no `observes` declared and cwd is not a git repository, so the input set is unknowable"
}
