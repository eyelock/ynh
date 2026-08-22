package vendor

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Resume support, in one place because the shape is the same for every vendor:
// find the id of the last session recorded for a directory, then hand it to the
// vendor's own resume invocation.
//
// Two rules hold across all adapters:
//
//  1. Session ids come from the vendor's on-disk session store, never from
//     scraping the resume banner the CLI prints on exit. That banner is
//     undocumented UI; the store holds the same id in a stable form.
//
//  2. A bare resume flag is never emitted. On claude, copilot, codex and cursor
//     alike, a bare --resume opens an interactive *picker* rather than resuming
//     the most recent session — which would hang an unattended relaunch. Every
//     invocation is either an explicit id or the vendor's continue-last form.

// vendorHomeDir returns the user's real home directory, where vendor CLIs keep
// their session stores. Deliberately not YNH_HOME: these are the vendors' own
// directories, not ynh's. Honouring $HOME (as os.UserHomeDir does on unix) is
// also what makes these lookups testable via t.Setenv("HOME", t.TempDir()).
func vendorHomeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return home, nil
}

// sessionCandidate is one entry in a vendor's session store, reduced to what
// resume resolution needs.
type sessionCandidate struct {
	id      string
	modTime time.Time
}

// newestCandidate returns the id of the most recently modified candidate that
// is not older than notBefore, or ErrNoResumableSession if none qualifies.
func newestCandidate(candidates []sessionCandidate, notBefore time.Time) (string, error) {
	filtered := candidates[:0:0]
	for _, c := range candidates {
		if !notBefore.IsZero() && c.modTime.Before(notBefore) {
			continue
		}
		filtered = append(filtered, c)
	}
	if len(filtered) == 0 {
		return "", ErrNoResumableSession
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].modTime.After(filtered[j].modTime)
	})
	return filtered[0].id, nil
}

// dirEntriesByModTimeDesc lists dir's entries newest-first. A missing directory
// is not an error — it just means the vendor has recorded no sessions yet.
func dirEntriesByModTimeDesc(dir string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoResumableSession
		}
		return nil, fmt.Errorf("reading session store %s: %w", dir, err)
	}

	type timed struct {
		entry os.DirEntry
		mod   time.Time
	}
	timedEntries := make([]timed, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			// Session directories churn; one vanishing mid-scan is normal.
			continue
		}
		timedEntries = append(timedEntries, timed{entry: e, mod: info.ModTime()})
	}
	sort.Slice(timedEntries, func(i, j int) bool {
		return timedEntries[i].mod.After(timedEntries[j].mod)
	})

	ordered := make([]os.DirEntry, 0, len(timedEntries))
	for _, t := range timedEntries {
		ordered = append(ordered, t.entry)
	}
	return ordered, nil
}

// dirCandidates returns the forms of dir a vendor might have recorded, most
// likely first: the symlink-resolved path, then the path as given.
//
// This exists because the two ends disagree in practice. Go's os.Getwd honours
// $PWD, so ynh sees the logical path a shell reports (/tmp/x), while a vendor
// CLI resolves it before recording (/private/tmp/x on macOS, where /tmp is a
// symlink to /private/tmp). A lookup keyed on only one form silently finds
// nothing and the launch falls back to a cold session.
func dirCandidates(dir string) []string {
	clean := filepath.Clean(dir)
	candidates := []string{}

	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		candidates = append(candidates, resolved)
	}
	for _, existing := range candidates {
		if existing == clean {
			return candidates
		}
	}
	return append(candidates, clean)
}

// sameDir reports whether two paths refer to the same directory, after
// normalising separators and resolving symlinks where possible. Vendor stores
// record the cwd as the CLI saw it, which may differ textually from the path
// ynh resolves (e.g. /tmp vs /private/tmp on macOS).
func sameDir(a, b string) bool {
	if a == b {
		return true
	}
	cleanA, cleanB := filepath.Clean(a), filepath.Clean(b)
	if cleanA == cleanB {
		return true
	}
	resolvedA, errA := filepath.EvalSymlinks(cleanA)
	resolvedB, errB := filepath.EvalSymlinks(cleanB)
	if errA != nil || errB != nil {
		return false
	}
	return resolvedA == resolvedB
}
