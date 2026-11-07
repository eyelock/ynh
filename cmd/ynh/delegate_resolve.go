package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/resolver"
)

// resolvedDelegate is a delegates_to entry matched to the installed, trusted
// harness it runs as. A delegate is its own ynh session under its own trust
// grant, so the parent's digest does not cover its commands.
type resolvedDelegate struct {
	// ID is the installed harness's canonical id, the key of its trust grant.
	ID string
	// Harness is the installed harness the entry resolved to.
	Harness *harness.Harness
	// Profile and Focus are the entry's selection, carried through to the
	// launch. At most one is set.
	Profile string
	Focus   string
}

// delegateSource is where a delegates_to entry points, in a form comparable
// with an install's recorded source: a remote URL and path, or a local folder.
type delegateSource struct {
	url   string // normalised remote URL, empty for a local folder
	path  string // subdirectory within the remote
	ref   string
	local string // absolute folder, empty for a remote
}

func (s delegateSource) String() string {
	if s.local != "" {
		return s.local
	}
	if s.path != "" {
		return s.url + " (path " + s.path + ")"
	}
	return s.url
}

// normalizeSourceURL reduces a Git URL to host/path so the https, ssh and
// shorthand spellings of one repository compare equal.
func normalizeSourceURL(u string) string {
	s := strings.TrimSpace(u)
	if rest, ok := strings.CutPrefix(s, "git@"); ok {
		s = strings.Replace(rest, ":", "/", 1)
	}
	for _, p := range []string{"https://", "http://", "ssh://git@", "ssh://"} {
		s = strings.TrimPrefix(s, p)
	}
	s = strings.TrimSuffix(strings.TrimSuffix(s, "/"), ".git")
	return s
}

func cleanSubpath(p string) string {
	p = filepath.ToSlash(filepath.Clean(p))
	if p == "." {
		return ""
	}
	return p
}

func realDir(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// delegateEntrySource says where parent's entry d points. A relative git
// source ("." for the parent's own repo) is the parent's folder when the
// parent runs from a source tree, and the parent's own remote when the
// parent is a git install, whose copy is not a repository.
func delegateEntrySource(parent *harness.Harness, d harness.Delegate) (delegateSource, error) {
	src := delegateSource{ref: d.Ref, path: cleanSubpath(d.Path)}
	switch {
	case d.Git != "" && strings.HasPrefix(d.Git, "."):
		if harnessHasRemoteSource(parent) && cleanSubpath(d.Git) == "" {
			pr := parent.InstalledFrom
			src.url = normalizeSourceURL(pr.Source)
			src.path = cleanSubpath(filepath.Join(pr.Path, d.Path))
			return src, nil
		}
		src.local = realDir(filepath.Join(resolver.GitSourceURL(d.Git, parent.Dir), d.Path))
	case d.Git != "":
		src.url = normalizeSourceURL(d.Git)
	case d.Local != "":
		l := d.Local
		if !filepath.IsAbs(l) {
			l = filepath.Join(parent.Dir, l)
		}
		src.local = realDir(filepath.Join(l, d.Path))
	default:
		return src, fmt.Errorf("delegates_to entry names no git URL or local folder")
	}
	return src, nil
}

// installedMatches reports whether the installed harness h was installed
// from src.
func installedMatches(h *harness.Harness, src delegateSource) bool {
	pr := h.InstalledFrom
	if src.local != "" {
		if pr == nil || (pr.SourceType != "local" && pr.SourceType != "source") {
			return false
		}
		return realDir(h.Dir) == src.local
	}
	if pr == nil || (pr.SourceType != "git" && pr.SourceType != "registry") || pr.Source == "" {
		return false
	}
	if normalizeSourceURL(pr.Source) != src.url || cleanSubpath(pr.Path) != src.path {
		return false
	}
	return src.ref == "" || pr.Ref == "" || src.ref == pr.Ref
}

// resolveDelegate finds the installed harness whose source matches the
// delegates_to entry d of parent and checks it carries a current trust grant.
// It installs and trusts nothing: when no install matches, or the match is
// not trusted, the error names the command to run.
func resolveDelegate(parent *harness.Harness, d harness.Delegate, store trustStore) (*resolvedDelegate, error) {
	src, err := delegateEntrySource(parent, d)
	if err != nil {
		return nil, fmt.Errorf("delegate of %s: %w", parent.Name, err)
	}
	entries, err := harness.ListAll()
	if err != nil {
		return nil, fmt.Errorf("delegate %s: listing installed harnesses: %w", src, err)
	}
	// Several installs can match one entry (one source installed by git and
	// by registry, or at two refs). The result must not depend on listing
	// order: the trusted match with the smallest id wins, and when none is
	// trusted the smallest id's refusal is the one reported.
	type match struct {
		id string
		h  *harness.Harness
	}
	var matches []match
	for _, e := range entries {
		id := canonicalIDForEntry(e)
		h, lerr := harness.LoadByID(id)
		if lerr != nil || !installedMatches(h, src) {
			continue
		}
		matches = append(matches, match{id, h})
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].id < matches[j].id })
	var refusal error
	for _, m := range matches {
		id, h := m.id, m.h
		st := trustStatusFor(id, h, store, nil)
		switch st.State {
		case trustAccepted, trustNoCommands:
			return &resolvedDelegate{ID: id, Harness: h, Profile: d.Profile, Focus: d.Focus}, nil
		case trustUnavailable:
			if refusal == nil {
				refusal = fmt.Errorf("delegate %s of %s is installed as %s but cannot be read in full: %s\n  Run: ynh update %s",
					src, parent.Name, id, strings.Join(st.Problems, "; "), id)
			}
		case trustChanged:
			if refusal == nil {
				refusal = fmt.Errorf("delegate %s of %s is installed as %s but its commands changed since you accepted them\n  Review with 'ynh trust show %s', then run: ynh trust accept %s",
					src, parent.Name, id, id, id)
			}
		default:
			if refusal == nil {
				refusal = fmt.Errorf("delegate %s of %s is installed as %s but not trusted\n  Review with 'ynh trust show %s', then run: ynh trust accept %s",
					src, parent.Name, id, id, id)
			}
		}
	}
	if refusal != nil {
		return nil, refusal
	}
	return nil, fmt.Errorf("delegate %s of %s is not installed\n  Run: %s, then ynh trust accept <the installed id>",
		src, parent.Name, installHint(d, src))
}

// installHint is the ynh install command for an entry's source.
func installHint(d harness.Delegate, src delegateSource) string {
	cmd := "ynh install " + src.local
	if src.local == "" {
		cmd = "ynh install " + d.Git
		if strings.HasPrefix(d.Git, ".") {
			cmd = "ynh install " + src.url
		}
		if src.path != "" {
			cmd += " --path " + src.path
		}
		if d.Ref != "" {
			cmd += " --ref " + d.Ref
		}
	}
	return cmd
}
