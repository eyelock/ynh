package delegation

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/eyelock/ynh/internal/presence"
)

// The delegation guard (docs/delegate-sessions.md, "Limits on delegation").
// A delegate reached over A2A is an ordinary new session, so nothing in the
// delegate's own harness says who started it: a delegates_to loop (A to B to
// A) would run until something outside ynh stopped it. These limits are
// checked by Start, the one place every delegation is launched from, so they
// hold on every vendor that can be a parent.
const (
	// ChainEnv carries a session's ancestry: the canonical ids of the
	// harnesses above it, root first, comma-separated. Start sets it in the
	// environment of every session it launches, and reads it back from its own
	// to know who it is being started under. It is not YNH_DELEGATE_*, which is
	// the parent's endpoints and is dropped from a delegate's environment
	// (ParentOnlyEnv).
	ChainEnv = "YNH_DELEGATION_CHAIN"
	// MaxDepthEnv sets how many harnesses may sit above a delegate.
	MaxDepthEnv = "YNH_DELEGATION_MAX_DEPTH"
	// MaxLiveEnv sets how many sessions may be live at once under one
	// YNH_HOME: every session ynh run has launched (see ynh ps), delegates
	// included.
	MaxLiveEnv = "YNH_DELEGATION_MAX_LIVE"

	// DefaultMaxDepth: a root harness's delegate is at depth 1, so a chain
	// of root, then four delegates deep is the longest allowed.
	DefaultMaxDepth = 4
	// DefaultMaxLive is the most sessions live under one YNH_HOME: the
	// sessions ynh run records (ynh ps) and the delegations started or running
	// (counted by whichever is larger, since a delegation has no presence record
	// until its run begins). A process tree per session makes this the bound on
	// how wide a chain can fan out.
	DefaultMaxLive = 16
)

// Chain is the ancestry of the session the current process runs in, from its
// environment: empty for a session no delegation started.
func Chain(get func(string) string) []string {
	var out []string
	for _, id := range strings.Split(get(ChainEnv), ",") {
		if id = strings.TrimSpace(id); id != "" {
			out = append(out, id)
		}
	}
	return out
}

// limit reads a positive whole-number limit from the environment.
func limit(get func(string) string, name string, def int) (int, error) {
	v := get(name)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s=%q: want a whole number above 0\n  Fix: set it to a number such as %d, or unset it for the default", name, v, def)
	}
	return n, nil
}

// Guard decides whether parent may start delegate, given the ancestry of
// parent's own session. It returns the ancestry the delegate's session
// carries (chain, then parent), or a refusal that names the chain and ends in
// a Fix line. The refusal is what the parent's delegate tool returns.
func Guard(parent, delegate string, chain []string, get func(string) string) ([]string, error) {
	maxDepth, err := limit(get, MaxDepthEnv, DefaultMaxDepth)
	if err != nil {
		return nil, err
	}
	maxLive, err := limit(get, MaxLiveEnv, DefaultMaxLive)
	if err != nil {
		return nil, err
	}
	anc := append(append([]string{}, chain...), parent)
	path := strings.Join(append(append([]string{}, anc...), delegate), " -> ")
	for _, id := range anc {
		if id == delegate {
			return nil, fmt.Errorf("delegation refused: this is a loop. %s returns to %s, which is already in the chain.\n  Fix: remove the delegates_to entry that closes the loop, or do this work in %s itself instead of delegating it back",
				path, delegate, parent)
		}
	}
	if len(anc) > maxDepth {
		return nil, fmt.Errorf("delegation refused: the chain is too deep. %s puts %s %d levels down; the limit is %d.\n  Fix: flatten the chain so fewer harnesses delegate onward, or raise the limit with %s=<n>",
			path, delegate, len(anc), maxDepth, MaxDepthEnv)
	}
	if live := LiveSessions(); live >= maxLive {
		return nil, fmt.Errorf("delegation refused: %d sessions are already live under this YNH_HOME; the limit is %d.\n  Fix: see them with ynh ps, end some with ynh stop <session> or ynh reap, or raise the limit with %s=<n>",
			live, maxLive, MaxLiveEnv)
	}
	return anc, nil
}

// LiveSessions counts every live session under this YNH_HOME: the live and
// orphaned sessions ynh ps lists, or the delegations not yet final when there
// are more of those (a delegation that has been started but whose run has not
// begun has no presence record yet).
func LiveSessions() int {
	n := Live()
	if es, err := presence.List(false); err == nil && len(es) > n {
		n = len(es)
	}
	return n
}

// Live counts the delegate sessions that are not final under this YNH_HOME,
// settling first any whose session process is gone.
func Live() int {
	paths, _ := filepath.Glob(filepath.Join(Dir(), "*", "*", "dlg-*.json"))
	n := 0
	for _, p := range paths {
		rec, err := Load(p)
		if err != nil || rec.Status.Final() {
			continue
		}
		if rec, err = Reconcile(p); err == nil && !rec.Status.Final() {
			n++
		}
	}
	return n
}

// chainEnvEntry is the environment entry that gives a launched session its
// ancestry.
func chainEnvEntry(anc []string) string { return ChainEnv + "=" + strings.Join(anc, ",") }
