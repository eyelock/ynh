package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

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
	source := src.git
	if source == "" {
		source = src.url
	}
	args := []string{source}
	if src.path != "" {
		args = append(args, "--path", src.path)
	}
	ref := d.Ref
	if ref == "" && harnessHasRemoteSource(parent) && parent.InstalledFrom != nil {
		// An entry naming the parent's own repository follows the parent's
		// commit.
		ref = parent.InstalledFrom.SHA
		if ref == "" {
			ref = parent.InstalledFrom.Ref
		}
	}
	if ref != "" {
		args = append(args, "--ref", ref)
	}
	return args
}

// installDelegate installs the harness a delegates_to entry points at, the
// way `ynh install` would, so it has an id and a record of its own. Its
// output goes to stderr, so a command that writes one JSON document to stdout
// stays one document. A variable so tests stand it aside, set in init because
// cmdInstall reaches this file through install --trust.
var installDelegate func(parent *harness.Harness, d harness.Delegate, src delegateSource) error

func init() {
	installDelegate = func(parent *harness.Harness, d harness.Delegate, src delegateSource) error {
		out := os.Stdout
		os.Stdout = os.Stderr
		defer func() { os.Stdout = out }()
		return cmdInstall(installArgsFor(parent, d, src))
	}
}

// a2aNode is one a2a delegate in the tree below a parent: the harness it
// resolves to, installed or, when it is not installed yet, read from the
// cache, and what it runs.
type a2aNode struct {
	Label     string // where it points
	Parent    string // the harness that declares it
	ParentH   *harness.Harness
	Depth     int
	Entry     harness.Delegate
	Src       delegateSource
	ID        string // the installed id, "" until installed
	H         *harness.Harness
	Installed bool
	Set       trustSet
	Level     permission.Level
	From      permission.Source
	Problem   string // why what it runs could not be read
}

// planA2ATree lists every a2a delegate below p, nested ones included, each
// once, in declaration order, with what each runs. It installs and records
// nothing: `trust show` shows it and `trust accept` accepts exactly it.
func planA2ATree(p *harness.Harness) []a2aNode {
	var out []a2aNode
	seen := map[string]bool{}
	var walk func(h *harness.Harness, depth int, parent string)
	walk = func(h *harness.Harness, depth int, parent string) {
		for _, d := range a2aEntries(h) {
			n := a2aNode{Label: d.Label(), Parent: parent, ParentH: h, Depth: depth, Entry: d}
			src, err := delegateEntrySource(h, d)
			if err != nil {
				n.Problem = err.Error()
				out = append(out, n)
				continue
			}
			n.Src, n.Label = src, src.String()
			if seen[n.Label] {
				continue
			}
			seen[n.Label] = true
			if ms, err := installedMatchesOf(src); err == nil && len(ms) > 0 {
				n.ID, n.H, n.Installed = ms[0].ID, ms[0].H, true
			} else if dh, lerr := loadDelegateFromCache(h, d, src); lerr == nil {
				n.H = dh
			} else {
				n.Problem = fmt.Sprintf("it is not installed and cannot be read from the cache (%v), so what it runs cannot be shown", lerr)
			}
			if n.H != nil {
				n.Set = harnessExecutables(n.H)
				if len(n.Set.Problems) > 0 {
					n.Problem = strings.Join(n.Set.Problems, "; ")
				} else if b := scriptBlockers(n.Set.Sensors, n.Set.Hooks, n.Set.MCPServers); len(b) > 0 {
					n.Problem = strings.Join(b, "; ")
				}
				n.Level, n.From = n.H.AutoApproveOf(), n.H.AutoApproveOrigin()
				// An entry may only narrow the delegate's own level (D2).
				if e := permission.Level(d.AutoApprove); e != "" && e.Narrower(n.Level) {
					n.Level, n.From = e, permission.SourceEntry
				}
			}
			out = append(out, n)
			if n.H != nil {
				walk(n.H, depth+1, n.Label)
			}
		}
	}
	walk(p, 0, p.Name)
	return out
}

// loadDelegateFromCache reads the harness an entry points at from the local
// folder or the include cache, without fetching.
func loadDelegateFromCache(parent *harness.Harness, d harness.Delegate, src delegateSource) (*harness.Harness, error) {
	if src.local != "" {
		return harness.LoadDir(src.local)
	}
	gs := d.GitSource
	if pr := parent.InstalledFrom; pr != nil && strings.HasPrefix(d.Git, ".") && cleanSubpath(d.Git) == "" && harnessHasRemoteSource(parent) {
		// An entry naming the declaring harness's own repository is that
		// repository at the commit the declaring harness is at, never the
		// cache folder it was read from.
		gs = harness.GitSource{Git: pr.Source, Path: cleanSubpath(filepath.Join(pr.Path, d.Path)), Ref: d.Ref}
		if gs.Ref == "" {
			gs.Ref = pr.SHA
			if gs.Ref == "" {
				gs.Ref = pr.Ref
			}
		}
	}
	base, _, err := resolver.ResolveGitSourceFromCache(gs, parent.Dir)
	if err != nil {
		return nil, err
	}
	dh, err := harness.LoadDir(base)
	if err != nil {
		return nil, err
	}
	dh.InstalledFrom = cacheProvenance(src)
	return dh, nil
}

// cacheProvenance is the provenance of a delegate read from the cache rather
// than installed, so an entry of its own naming its repository ("." or
// "./sub") resolves to that repository at the commit it was read at
// (delegateEntrySource), not to the cache folder as if it were a local one.
func cacheProvenance(src delegateSource) *harness.Provenance {
	source := src.git
	if source == "" {
		source = src.url
	}
	pr := &harness.Provenance{SourceType: "git", Source: source, Path: src.path, Ref: src.ref}
	if isFullSHA(src.ref) {
		pr.SHA = src.ref
	}
	return pr
}

// acceptedDelegate is what acceptA2ATree recorded for one delegate.
type acceptedDelegate struct {
	ID     string `json:"id"`
	Digest string `json:"digest"`
}

// acceptA2ATree is the one function every acceptance route calls to accept
// the a2a delegates of a parent (ynh #1058, N1): the whole tree below p,
// nested delegates included, each with its own record under its own id at the
// commit it is installed at, and the level it opens at.
//
// Nothing is accepted unseen: every delegate's commands, hooks, MCP servers
// and level are written to w before anything is recorded, and a delegate that
// cannot be read in full refuses the whole acceptance. A delegate not
// installed yet is installed (its output goes to stderr) and must match what
// was shown.
//
// All or nothing: extra holds the records the caller accepts with the tree,
// the parent's own and a folder run's includes, and they are written in the
// same store write as the delegates', after every delegate has been read and
// installed. If any delegate cannot be accepted nothing is recorded, and the
// error names the delegate and why. rootID is the harness whose acceptance
// this is, noted on each delegate's record (trust ls shows "with <rootID>").
func acceptA2ATree(p *harness.Harness, rootID, via string, extra map[string]trustRecord, w io.Writer) ([]acceptedDelegate, error) {
	nodes := planA2ATree(p)
	// Every --trust route needs the whole tree pinned to full SHAs, and says
	// so before anything is read further, installed or recorded; trust accept,
	// which a person has reviewed, accepts at the commit it is at now.
	if via != viaAccept {
		if bad := unpinnedInTree(p, nodes); len(bad) > 0 {
			return nil, refuseUnpinned(rootID, "--trust", bad)
		}
	}
	for _, n := range nodes {
		if n.Problem != "" {
			return nil, fmt.Errorf("nothing was accepted: delegate %s of %s cannot be accepted: %s", n.Label, n.Parent, n.Problem)
		}
	}
	for _, n := range nodes {
		_, _ = fmt.Fprintf(w, "A2A delegate %s of %s, opens at %s when run unattended (%s):\n", n.Label, n.Parent, n.Level, n.From)
		_, _ = fmt.Fprint(w, listExecutables(n.Set.Sensors, n.Set.Hooks, n.Set.MCPServers))
	}
	// Install the ones that are not, then check each is what was shown.
	for i := range nodes {
		n := &nodes[i]
		if n.Installed {
			continue
		}
		if err := installDelegate(n.ParentH, n.Entry, n.Src); err != nil {
			return nil, fmt.Errorf("nothing was accepted: delegate %s of %s: installing it: %w", n.Label, n.Parent, err)
		}
		ms, err := installedMatchesOf(n.Src)
		if err != nil || len(ms) == 0 {
			return nil, fmt.Errorf("nothing was accepted: delegate %s of %s is not installed, and installing it did not make it so", n.Label, n.Parent)
		}
		shown := executablesDigest(n.Set)
		n.ID, n.H, n.Installed = ms[0].ID, ms[0].H, true
		n.Set = harnessExecutables(n.H)
		if got := executablesDigest(n.Set); got != shown {
			return nil, fmt.Errorf("nothing was accepted: delegate %s of %s installed as %s runs something other than what was shown (%s, then %s)", n.Label, n.Parent, n.ID, shown, got)
		}
	}
	store, err := loadTrustStore()
	if err != nil {
		return nil, err
	}
	var done []acceptedDelegate
	now := time.Now().UTC().Format(time.RFC3339)
	for _, n := range nodes {
		rec := newAcceptance(n.H, n.Set, via, now)
		rec.Parent = rootID
		store.Harnesses[n.ID] = rec
		done = append(done, acceptedDelegate{ID: n.ID, Digest: rec.Digest})
	}
	for id, rec := range extra {
		store.Harnesses[id] = rec
	}
	if err := saveTrustStore(store); err != nil {
		return nil, err
	}
	for _, n := range nodes {
		_, _ = fmt.Fprintf(w, "Accepted %s (%s, with %s): %d command(s), %s, opens at %s when run unattended\n",
			n.ID, via, rootID, n.Set.count(), store.Harnesses[n.ID].Digest, n.Level)
	}
	return done, nil
}

// a2aDelegateReport is one a2a delegate as `trust show` lists it: where it
// points, who declares it, the id it is installed under, its own trust state,
// what it runs, and the level it opens at when run unattended.
type a2aDelegateReport struct {
	Delegate    string       `json:"delegate"`
	Parent      string       `json:"parent"`
	Depth       int          `json:"depth"`
	ID          string       `json:"id,omitempty"`
	State       string       `json:"state"`
	Digest      string       `json:"digest,omitempty"`
	AutoApprove string       `json:"auto_approve,omitempty"`
	From        string       `json:"auto_approve_from,omitempty"`
	Executables []executable `json:"executables,omitempty"`
	Hooks       []hookExec   `json:"hooks,omitempty"`
	MCPServers  []mcpExec    `json:"mcp_servers,omitempty"`
	Workspace   []string     `json:"workspace,omitempty"`
	Problem     string       `json:"problem,omitempty"`
}

// a2aDelegateReports lists the whole tree of a2a delegates below p with its
// own state and what each runs. A delegate that is not installed reads
// "not-installed": accepting the parent installs and accepts it.
func a2aDelegateReports(p *harness.Harness, store trustStore) []a2aDelegateReport {
	var out []a2aDelegateReport
	for _, n := range planA2ATree(p) {
		r := a2aDelegateReport{Delegate: n.Label, Parent: n.Parent, Depth: n.Depth, State: "not-installed", Problem: n.Problem}
		if n.H != nil {
			r.Digest = executablesDigest(n.Set)
			r.Executables, r.Hooks, r.MCPServers, r.Workspace = n.Set.Sensors, n.Set.Hooks, n.Set.MCPServers, n.Set.Workspace
			r.AutoApprove, r.From = string(n.Level), string(n.From)
		}
		if n.Installed {
			r.ID = n.ID
			r.State = trustStatusFor(n.ID, n.H, store, nil).State
		}
		out = append(out, r)
	}
	return out
}

// unpinnedInTree lists, one line each, every floating ref anywhere in the tree
// below p, naming where it sits: each harness's a2a and native delegates_to
// entries and its remote includes (ynh #1058, N1). Only a full commit SHA pins.
func unpinnedInTree(p *harness.Harness, nodes []a2aNode) []string {
	var out []string
	seen := map[*harness.Harness]bool{}
	check := func(h *harness.Harness, where string) {
		if h == nil || seen[h] {
			return
		}
		seen[h] = true
		for _, l := range unpinnedDelegates(h) {
			out = append(out, l+" (declared by "+where+")")
		}
		if h != p {
			if remote, err := remoteIncludes(h, "accept"); err == nil {
				for _, l := range unpinnedIncludes(remote) {
					out = append(out, "include "+l+" (of delegate "+where+")")
				}
			}
		}
	}
	check(p, p.Name)
	for _, n := range nodes {
		check(n.H, n.Label)
	}
	return out
}
