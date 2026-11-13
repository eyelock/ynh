package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/eyelock/ynh/internal/a2aserve"
	"github.com/eyelock/ynh/internal/harness"
)

// openDelegateEndpoints opens one localhost A2A endpoint for each delegates_to
// entry of p whose transport resolves to a2a, and puts what the parent needs
// to reach them in this process's environment, which the vendor session
// inherits: YNH_DELEGATES and, per delegate, YNH_DELEGATE_<NAME>_CARD and
// _AUTH; with exactly one, also the a2a CLI's own A2ACLI_AGENT_CARD and
// A2ACLI_AUTH (docs/delegate-sessions.md, "Reaching a delegate"). The delegate's
// vendor session starts only on an endpoint's first message. The returned
// function closes the endpoints; sessions already running are detached and go on.
//
// An entry that cannot be resolved (not installed, not trusted) refuses the
// run, with the command to fix it: the parent must not start believing it has a
// delegate it cannot reach.
func openDelegateEndpoints(p *harness.Harness, parentID, folder string, stderr io.Writer) (func(), error) {
	return openDelegateEndpointsWith(p, parentID, folder, nil, stderr)
}

// openDelegateEndpointsWith is openDelegateEndpoints with the trust store given
// (nil reads the user's), for tests.
func openDelegateEndpointsWith(p *harness.Harness, parentID, folder string, store *trustStore, stderr io.Writer) (func(), error) {
	scrubParentEndpoints()
	var entries []harness.Delegate
	for _, d := range p.DelegatesTo {
		if p.TransportOf(d) == harness.TransportA2A {
			entries = append(entries, d)
		}
	}
	if len(entries) == 0 {
		return func() {}, nil
	}
	if store == nil {
		s, err := loadTrustStore()
		if err != nil {
			return nil, err
		}
		store = &s
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("finding the ynh binary: %w", err)
	}
	if parentID == "" {
		parentID = "local/" + p.Name
	}
	var eps []*a2aserve.Endpoint
	closeAll := func() {
		for _, e := range eps {
			_ = e.Close()
		}
	}
	for _, d := range entries {
		rd, err := resolveDelegate(p, d, *store)
		if err != nil {
			closeAll()
			return nil, err
		}
		label := d.Label()
		if src, serr := delegateEntrySource(p, d); serr == nil {
			label = src.String()
		}
		e, err := a2aserve.Open(a2aserve.Options{
			Name: rd.Harness.Name, Label: label, Parent: parentID, Delegate: rd.ID,
			Profile: rd.Profile, Focus: rd.Focus, Folder: folder,
			Supervisor: []string{exe, "delegate-run", "supervise"},
		})
		if err != nil {
			closeAll()
			return nil, err
		}
		eps = append(eps, e)
	}
	env, err := a2aserve.Env(eps)
	if err != nil {
		closeAll()
		return nil, err
	}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		if err := os.Setenv(k, v); err != nil {
			closeAll()
			return nil, err
		}
	}
	_, _ = fmt.Fprintf(stderr, "Opened %d A2A delegate endpoint(s): %s\n", len(eps), os.Getenv("YNH_DELEGATES"))
	return closeAll, nil
}

// scrubParentEndpoints removes the endpoint settings this process inherited
// from an enclosing session, so a run never offers its vendor another
// session's delegates, or lets the a2a CLI pick one of them by default. It
// clears only what a run itself sets: YNH_DELEGATES, YNH_DELEGATE_* and the two
// a2a CLI variables that pick an agent. It leaves every other A2ACLI_* alone, since
// those are the user's own settings for the CLI (a timeout, say) and the
// parent should keep them; a delegate session, by contrast, drops all of
// A2ACLI_* (delegation.ParentOnlyEnv), because it is a separate agent that must
// not carry the parent's CLI configuration, a credential helper that names
// the parent's endpoints for one.
func scrubParentEndpoints() {
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if k == "YNH_DELEGATES" || strings.HasPrefix(k, "YNH_DELEGATE_") || k == "A2ACLI_AGENT_CARD" || k == "A2ACLI_AUTH" {
			_ = os.Unsetenv(k)
		}
	}
}
