package main

import (
	"fmt"
	"io"
	"sort"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/permission"
	"github.com/eyelock/ynh/internal/resolver"
)

// A parent's acceptance covers its a2a delegates (ynh #1058, N1). A delegate
// is its own harness with its own trust record at its own commit, so a change
// to one re-gates only it; what the parent's acceptance adds is that it lists
// each one, and accepts each one, in the same step. Native delegates are part
// of the parent's own digest and are not handled here.

// a2aEntries are p's delegates_to entries that resolve to the a2a transport.
func a2aEntries(p *harness.Harness) []harness.Delegate {
	var out []harness.Delegate
	for _, d := range p.DelegatesTo {
		if p.TransportOf(d) == harness.TransportA2A {
			out = append(out, d)
		}
	}
	return out
}

// unpinnedA2ADelegates lists, one line each, the a2a delegates_to entries of p
// that come from a remote source not pinned to a full SHA. Like a native
// entry, one naming the parent's own repository or a local folder is judged
// by the parent's own pin or is the operator's own files.
func unpinnedA2ADelegates(p *harness.Harness) []string {
	var out []string
	for _, d := range a2aEntries(p) {
		if d.Git == "" || resolver.IsOwnRepoSource(d.GitSource, p.Dir) {
			continue
		}
		if why := notPinned(d.Ref); why != "" {
			out = append(out, fmt.Sprintf("a2a delegate %s: %s", d.Git, why))
		}
	}
	return out
}

// installedMatch is an installed harness and its id.
type installedMatch struct {
	ID string
	H  *harness.Harness
}

// installedMatchesOf lists the installed harnesses that were installed from
// src, smallest id first, as resolveDelegate does.
func installedMatchesOf(src delegateSource) ([]installedMatch, error) {
	entries, err := harness.ListAll()
	if err != nil {
		return nil, err
	}
	var out []installedMatch
	for _, e := range entries {
		id := canonicalIDForEntry(e)
		h, lerr := harness.LoadByID(id)
		if lerr != nil || !installedMatches(h, src) {
			continue
		}
		out = append(out, installedMatch{id, h})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// installArgsFor is the `ynh install` argument list for a delegates_to entry
// whose source is not installed.
func installArgsFor(parent *harness.Harness, d harness.Delegate, src delegateSource) []string {
	if src.local != "" {
		return []string{src.local}
	}
	args := []string{src.url}
	if src.path != "" {
		args = append(args, "--path", src.path)
	}
	ref := d.Ref
	if ref == "" && harnessHasRemoteSource(parent) && parent.InstalledFrom != nil {
		// An entry naming the parent's own repository follows the parent's
		// commit.
		ref = parent.InstalledFrom.SHA
	}
	if ref != "" {
		args = append(args, "--ref", ref)
	}
	return args
}

// installDelegate installs the harness a delegates_to entry points at, the
// way `ynh install` would, so it has an id and a record of its own. A
// variable so tests stand it aside, set in init because cmdInstall reaches
// this file through install --trust.
var installDelegate func(parent *harness.Harness, d harness.Delegate, src delegateSource) error

func init() {
	installDelegate = func(parent *harness.Harness, d harness.Delegate, src delegateSource) error {
		return cmdInstall(installArgsFor(parent, d, src))
	}
}

// acceptA2ADelegates records an acceptance of each a2a delegate of p, each
// under its own id at the commit it is installed at, installing one that is
// not installed yet. It returns the ids it accepted.
func acceptA2ADelegates(p *harness.Harness, via string, stdout io.Writer) ([]string, error) {
	var ids []string
	for _, d := range a2aEntries(p) {
		src, err := delegateEntrySource(p, d)
		if err != nil {
			return ids, fmt.Errorf("delegate of %s: %w", p.Name, err)
		}
		matches, err := installedMatchesOf(src)
		if err != nil {
			return ids, fmt.Errorf("delegate %s: listing installed harnesses: %w", src, err)
		}
		if len(matches) == 0 {
			if err := installDelegate(p, d, src); err != nil {
				return ids, fmt.Errorf("delegate %s of %s: installing it: %w", src, p.Name, err)
			}
			if matches, err = installedMatchesOf(src); err != nil {
				return ids, err
			}
		}
		if len(matches) == 0 {
			return ids, fmt.Errorf("delegate %s of %s is not installed, and installing it did not make it so", src, p.Name)
		}
		m := matches[0]
		if err := recordAcceptance(m.ID, m.H, via, stdout); err != nil {
			return ids, fmt.Errorf("delegate %s of %s: %w", src, p.Name, err)
		}
		ids = append(ids, m.ID)
	}
	return ids, nil
}

// a2aDelegateReport is one a2a delegate as `trust show` lists it: where it
// points, the id it is installed under, its own trust state, and the level it
// opens at when run unattended.
type a2aDelegateReport struct {
	Delegate    string `json:"delegate"`
	ID          string `json:"id,omitempty"`
	State       string `json:"state"`
	AutoApprove string `json:"auto_approve,omitempty"`
	From        string `json:"auto_approve_from,omitempty"`
}

// a2aDelegateReports lists each a2a delegate of p with its own state. A
// delegate that is not installed reads "not-installed": accepting the parent
// installs and accepts it.
func a2aDelegateReports(p *harness.Harness, store trustStore) []a2aDelegateReport {
	var out []a2aDelegateReport
	for _, d := range a2aEntries(p) {
		r := a2aDelegateReport{Delegate: d.Label(), State: "not-installed"}
		if src, err := delegateEntrySource(p, d); err == nil {
			r.Delegate = src.String()
			if ms, err := installedMatchesOf(src); err == nil && len(ms) > 0 {
				m := ms[0]
				r.ID = m.ID
				r.State = trustStatusFor(m.ID, m.H, store, nil).State
				level, from := m.H.AutoApproveOf(), m.H.AutoApproveOrigin()
				if d.AutoApprove != "" {
					// An entry may only narrow the delegate's own level (D2).
					if e := permission.Level(d.AutoApprove); e.Narrower(level) {
						level, from = e, harness.AutoApproveFromEntry
					}
				}
				r.AutoApprove, r.From = string(level), from
			}
		}
		out = append(out, r)
	}
	return out
}
