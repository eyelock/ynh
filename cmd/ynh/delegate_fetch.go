package main

import (
	"errors"
	"path/filepath"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/resolver"
)

// errNestedRefused marks an allow-list refusal, which callers treat as an
// error, unlike a fetch failure.
type errNestedRefused struct{ err error }

func (e *errNestedRefused) Error() string { return e.err.Error() }
func (e *errNestedRefused) Unwrap() error { return e.err }

// isNestedRefusal reports whether err is an allow-list refusal from
// fetchNestedDelegates.
func isNestedRefusal(err error) bool {
	var r *errNestedRefused
	return errors.As(err, &r)
}

// sameDir reports whether a and b name the same directory.
func sameDir(a, b string) bool {
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return ra == rb
}

// fetchNestedDelegates fetches the delegates that p's own delegates reach,
// the ones its delegates_to do not name, so the whole flattened graph is in
// the cache before a run and `ynh trust` can read every delegate's servers.
// Each source is checked against the allow-list first. p's own delegates
// must already be fetched. It returns what each nested delegate resolved to,
// for the install record, each anchored to the delegate that declares it
// (never a bare relative string). A source that is the root itself is not
// recorded. An allow-list refusal comes back as an error for which
// isNestedRefusal is true.
func fetchNestedDelegates(p *harness.Harness, cfg *config.Config) ([]plugin.ResolvedSourceJSON, error) {
	var got []plugin.ResolvedSourceJSON
	_, err := resolver.FlattenDelegates(p.DelegatesTo, p.Dir, p.Name, func(gs harness.GitSource, dir string) (string, error) {
		if dir == p.Dir {
			// One of p's own: fetched already.
			base, _, err := resolver.ResolveGitSourceFromCache(gs, dir)
			return base, err
		}
		if err := cfg.CheckSource(gs.Git, dir); err != nil {
			return "", &errNestedRefused{err}
		}
		url := resolver.GitSourceURL(gs.Git, dir)
		base, res, err := resolver.ResolveGitSource(gs, dir)
		if err == nil && res != nil {
			// The walk skips the root, but still resolves it, so it is
			// fetched (the cache-only trust walk needs it); it is not
			// recorded, since the root is no resolved source of itself.
			if gs.Path == "" && sameDir(url, p.Dir) {
				return base, nil
			}
			if l, lerr := harness.LoadDir(base); lerr == nil && p.Name != "" && l.Name == p.Name {
				return base, nil
			}
			got = append(got, plugin.ResolvedSourceJSON{Git: url, Ref: res.ResolvedRef, Path: gs.Path, SHA: res.SHA})
		}
		return base, err
	})
	return got, err
}
