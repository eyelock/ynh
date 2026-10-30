// Package workspace decides which extra folders a harness may have granted to
// the vendor CLI it launches.
//
// A harness requests folders (the manifest's "workspace" array); the operator
// grants them. A request on its own grants nothing. The decision is made in
// three layers, in this order:
//
//  1. Refusal: a pure predicate (Refusal) naming why a path must never be
//     granted, whatever the operator has said. Nothing is deleted or written
//     here, but the shape follows .claude/rules/destructive-operations.md: the
//     dangerous paths are only ever handed to the function that decides.
//  2. The operator allow-list (workspace_roots, YNH_WORKSPACE_ROOTS), which is
//     a boundary. A path outside it is refused, and trust cannot override it.
//  3. With no allow-list, trust: the harness must be accepted with a digest
//     covering its current workspace entries.
package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnvRoots is the environment variable that replaces the configured
// allow-list for one run. Colon-separated.
const EnvRoots = "YNH_WORKSPACE_ROOTS"

// Expand turns a requested entry into an absolute, cleaned path without
// touching the filesystem: "~" and "~/x" expand against home, a relative path
// resolves against harnessDir, an absolute one stays.
func Expand(entry, harnessDir, home string) string {
	switch {
	case entry == "~":
		return filepath.Clean(home)
	case strings.HasPrefix(entry, "~/"):
		return filepath.Join(home, entry[2:])
	case filepath.IsAbs(entry):
		return filepath.Clean(entry)
	default:
		return filepath.Join(harnessDir, entry)
	}
}

// contains reports whether outer is inner or an ancestor of it. Both must be
// absolute, cleaned and symlink-free.
func contains(outer, inner string) bool {
	if outer == inner {
		return true
	}
	if outer == string(filepath.Separator) {
		return true
	}
	return strings.HasPrefix(inner, outer+string(filepath.Separator))
}

// resolveDir evaluates symlinks and returns the real path, or the cleaned path
// unchanged when it cannot be resolved (a base that does not exist cannot be
// contained by anything).
func resolveDir(p string) string {
	if p == "" {
		return ""
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// Refusal returns why path must not be granted, or "". path is an absolute
// path as requested; symlinks are resolved here, so a link whose target lands
// on a forbidden directory is refused as that directory. It reads the
// filesystem (stat, symlink targets) and writes nothing.
//
// Refused: a path that does not exist or is not a directory; the filesystem
// root; home itself; and any directory that is, or contains, the harness
// directory or the project directory.
func Refusal(path, harnessDir, projectDir, home string) string {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "does not exist"
		}
		return fmt.Sprintf("cannot be resolved: %v", err)
	}
	info, err := os.Stat(real)
	if err != nil {
		return fmt.Sprintf("cannot be read: %v", err)
	}
	if !info.IsDir() {
		return "is not a directory"
	}
	via := ""
	if real != filepath.Clean(path) {
		via = " (the path resolves through a symlink to " + real + ")"
	}
	if real == string(filepath.Separator) {
		return "is the filesystem root" + via
	}
	if home != "" && real == resolveDir(home) {
		return "is your home directory" + via
	}
	if hd := resolveDir(harnessDir); hd != "" && contains(real, hd) {
		return "contains the harness directory " + hd + via
	}
	if pd := resolveDir(projectDir); pd != "" && contains(real, pd) {
		return "contains the project directory " + pd + via
	}
	return ""
}

// Resolve returns the symlink-free absolute path for an entry, or the cleaned
// expansion when it does not resolve.
func Resolve(entry, harnessDir, home string) string {
	return resolveDir(Expand(entry, harnessDir, home))
}

// Allowlist is the operator's boundary.
type Allowlist struct {
	// Set is true when an allow-list is in force. An empty list that is not
	// Set means "no allow-list", which falls back to trust.
	Set bool
	// Source is "env" or "config" when Set.
	Source string
	// Roots are the resolved, absolute roots.
	Roots []string
	// Notes are entries that could not be used (relative, missing), for the
	// launch to report.
	Notes []string
}

// LoadAllowlist builds the allow-list. YNH_WORKSPACE_ROOTS, when set and
// non-empty, replaces configRoots for the run. Entries expand "~/"; a
// relative entry is refused because it would mean a different folder from each
// directory ynh happens to be run in. A root that does not exist can contain
// nothing and is noted.
func LoadAllowlist(configRoots []string, envValue string, envSet bool, home string) Allowlist {
	var raw []string
	var a Allowlist
	if envSet && envValue != "" {
		raw, a.Source = filepath.SplitList(envValue), "env"
	} else if len(configRoots) > 0 {
		raw, a.Source = configRoots, "config"
	} else {
		return a
	}
	a.Set = true
	for _, r := range raw {
		if r == "" {
			continue
		}
		exp := r
		switch {
		case r == "~":
			exp = home
		case strings.HasPrefix(r, "~/"):
			exp = filepath.Join(home, r[2:])
		}
		if !filepath.IsAbs(exp) {
			a.Notes = append(a.Notes, fmt.Sprintf("allow-list entry %q is not an absolute path and was ignored", r))
			continue
		}
		real, err := filepath.EvalSymlinks(exp)
		if err != nil {
			a.Notes = append(a.Notes, fmt.Sprintf("allow-list entry %q does not exist and was ignored", r))
			continue
		}
		a.Roots = append(a.Roots, real)
	}
	return a
}

// Covers reports whether a resolved path lies inside one of the roots.
func (a Allowlist) Covers(resolved string) bool {
	for _, r := range a.Roots {
		if contains(r, resolved) {
			return true
		}
	}
	return false
}

// Request is everything the grant decision needs, gathered by the caller.
type Request struct {
	Entries    []string
	HarnessDir string
	ProjectDir string
	Home       string
	// Remote is true for a harness installed from a remote source. Such a
	// harness has every entry refused, whatever the allow-list or trust say.
	Remote bool
	Allow  Allowlist
	// Trusted is true when the harness is trust-accepted with a digest that
	// covers its current workspace entries. Consulted only with no allow-list.
	Trusted bool
	// TrustHint is the sentence that says how to get trusted, used in the
	// warning when Trusted is false.
	TrustHint string
}

// Entry is the outcome for one requested folder.
type Entry struct {
	Requested string
	Resolved  string
	Granted   bool
	// Reason is why a folder was refused, or how it was granted.
	Reason string
}

// Decide returns one Entry per requested folder, in order.
func Decide(r Request) []Entry {
	out := make([]Entry, 0, len(r.Entries))
	for _, e := range r.Entries {
		out = append(out, decideOne(r, e))
	}
	return out
}

func decideOne(r Request, entry string) Entry {
	res := Entry{Requested: entry, Resolved: Resolve(entry, r.HarnessDir, r.Home)}
	if r.Remote {
		res.Reason = "this harness was installed from a remote source, and a remote harness is never granted workspace folders"
		return res
	}
	if why := Refusal(Expand(entry, r.HarnessDir, r.Home), r.HarnessDir, r.ProjectDir, r.Home); why != "" {
		res.Reason = res.Resolved + " " + why
		return res
	}
	if r.Allow.Set {
		if !r.Allow.Covers(res.Resolved) {
			res.Reason = fmt.Sprintf("%s is outside the %s allow-list (%s)", res.Resolved, allowName(r.Allow.Source), strings.Join(r.Allow.Roots, ", "))
			return res
		}
		res.Granted, res.Reason = true, "inside the "+allowName(r.Allow.Source)+" allow-list"
		return res
	}
	if !r.Trusted {
		res.Reason = r.TrustHint
		return res
	}
	res.Granted, res.Reason = true, "harness trust-accepted"
	return res
}

func allowName(source string) string {
	if source == "env" {
		return EnvRoots
	}
	return "workspace_roots"
}
