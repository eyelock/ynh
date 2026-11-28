package main

import (
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/resolver"
)

// floatingPin is one floating entry that `ynh trust accept` accepted at the
// commit it found, a remote include or a native delegate entry with no full
// SHA ref. Commit is "" when none could be told.
type floatingPin struct {
	Entry  string `json:"entry"`
	Why    string `json:"why"`
	Commit string `json:"commit,omitempty"`
}

// floatingPins lists what `ynh trust accept` accepts although it floats, each
// with the commit it is at now. Acceptance is the operator's explicit act of
// reviewing what is installed, so it covers the commit installed, and a floating
// entry no longer refuses it (ynh #1002, the same rule as a floating own ref,
// ynh #1023). Only `--trust` on install, run, update and include, which accept
// without a prior review, still need a full SHA.
func floatingPins(p *harness.Harness, remote []remoteInclude) []floatingPin {
	var out []floatingPin
	for _, r := range remote {
		if why := notPinned(includeRef(r.Key)); why != "" {
			out = append(out, floatingPin{Entry: r.Key, Why: why, Commit: r.Commit})
		}
	}
	for _, d := range p.DelegatesTo {
		if p.TransportOf(d) != harness.TransportNative || d.Git == "" || resolver.IsOwnRepoSource(d.GitSource, p.Dir) {
			continue
		}
		if why := notPinned(d.Ref); why != "" {
			out = append(out, floatingPin{Entry: "native delegate " + d.Git, Why: why, Commit: delegateCommit(p, d)})
		}
	}
	return out
}

// delegateCommit is the commit a native delegate entry is at now: the HEAD of
// a local git folder it names, or the full SHA its installed copy recorded.
// "" when neither can be told.
func delegateCommit(parent *harness.Harness, d harness.Delegate) string {
	src, err := delegateEntrySource(parent, d)
	if err != nil {
		return ""
	}
	if src.local != "" {
		out, err := exec.Command("git", "-C", src.local, "rev-parse", "HEAD").Output()
		if err != nil {
			return ""
		}
		if sha := strings.TrimSpace(string(out)); isFullSHA(sha) {
			return sha
		}
		return ""
	}
	entries, err := harness.ListAll()
	if err != nil {
		return ""
	}
	for _, e := range entries {
		h, lerr := harness.LoadByID(canonicalIDForEntry(e))
		if lerr == nil && installedMatches(h, src) && isFullSHA(h.InstalledFrom.SHA) {
			return h.InstalledFrom.SHA
		}
	}
	return ""
}

// describeFloating is what `ynh trust accept` prints for what it accepted
// although it floats.
func describeFloating(pins []floatingPin) string {
	var b strings.Builder
	for _, pin := range pins {
		if pin.Commit != "" {
			fmt.Fprintf(&b, "  %s: %s; accepted at commit %s, the one installed\n", pin.Entry, pin.Why, pin.Commit)
		} else {
			fmt.Fprintf(&b, "  %s: %s; no commit could be told, so none is recorded\n", pin.Entry, pin.Why)
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return "Floating, accepted at what is installed now (what it executes reads as changed if it moves):\n" + b.String()
}

func printFloating(w io.Writer, pins []floatingPin) error {
	_, err := io.WriteString(w, describeFloating(pins))
	return err
}

// floatingFor is the pins of key, for the record of one include.
func floatingFor(pins []floatingPin, key string) []floatingPin {
	var out []floatingPin
	for _, p := range pins {
		if p.Entry == key {
			out = append(out, p)
		}
	}
	return out
}
