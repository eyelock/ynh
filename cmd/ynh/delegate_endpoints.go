package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/eyelock/ynh/internal/a2acli"
	"github.com/eyelock/ynh/internal/a2aserve"
	"github.com/eyelock/ynh/internal/harness"
)

// openDelegateEndpoints opens one localhost A2A endpoint for each delegates_to
// entry of p whose transport resolves to a2a, and puts what the parent needs
// to reach them in this process's environment, which the vendor session
// inherits: YNH_DELEGATES and, per delegate, YNH_DELEGATE_<NAME>_CARD and
// _AUTH; with exactly one, also the a2a CLI's own A2ACLI_AGENT_CARD and
// A2ACLI_AUTH, and always the a2a CLI's credential helper (docs/delegate-sessions.md, "Reaching a delegate"). The delegate's
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
	o, err := openEndpointsWith(p, parentID, folder, store, stderr)
	if err != nil {
		return nil, err
	}
	return o.close, nil
}

// openedEndpoints is what a run holds of its open endpoints: the endpoints,
// which the MCP server for the delegates is given, and how to close them.
type openedEndpoints struct {
	eps   []*a2aserve.Endpoint
	close func()
}

// parentIDOf is the id a run's delegations are recorded under: the harness's
// canonical id, or local/<name> for one run from a folder.
func parentIDOf(p *harness.Harness, parentID string) string {
	if parentID == "" {
		return "local/" + p.Name
	}
	return parentID
}

// openEndpointsWith is openDelegateEndpointsWith, also returning the endpoints.
func openEndpointsWith(p *harness.Harness, parentID, folder string, store *trustStore, stderr io.Writer) (*openedEndpoints, error) {
	scrubParentEndpoints()
	var entries []harness.Delegate
	for _, d := range p.DelegatesTo {
		if p.TransportOf(d) == harness.TransportA2A {
			entries = append(entries, d)
		}
	}
	if len(entries) == 0 {
		return &openedEndpoints{close: func() {}}, nil
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
	parentID = parentIDOf(p, parentID)
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
	// The a2a CLI's credential helper gives each endpoint its own token, so the
	// parent's command names a delegate by its card address alone, with no
	// variable in it (Claude Code's Bash(a2a:*) rule refuses one).
	var creds []a2acli.Credential
	for _, e := range eps {
		creds = append(creds, a2acli.Credential{Card: e.CardURL, Token: e.Token})
	}
	helper, rmHelper, err := a2acli.WriteCredentialHelper(creds)
	if err != nil {
		closeAll()
		return nil, err
	}
	closeAll = func(prev func()) func() {
		return func() { prev(); rmHelper() }
	}(closeAll)
	if err := os.Setenv(a2acli.CredentialHelperEnv, "127.0.0.1="+helper); err != nil {
		closeAll()
		return nil, err
	}
	_, _ = fmt.Fprintf(stderr, "Opened %d A2A delegate endpoint(s): %s\n", len(eps), os.Getenv("YNH_DELEGATES"))
	return &openedEndpoints{eps: eps, close: closeAll}, nil
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
		if k == "YNH_DELEGATES" || strings.HasPrefix(k, "YNH_DELEGATE_") || k == "A2ACLI_AGENT_CARD" || k == "A2ACLI_AUTH" ||
			(k == a2acli.CredentialHelperEnv && strings.Contains(kv, a2acli.HelperMarker)) {
			_ = os.Unsetenv(k)
		}
	}
}
