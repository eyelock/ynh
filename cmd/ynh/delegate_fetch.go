package main

import (
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/resolver"
)

// fetchNestedDelegates fetches the delegates that p's own delegates reach,
// the ones its delegates_to do not name, so the whole flattened graph is in
// the cache before a run and `ynh trust` can read every delegate's servers.
// Each source is checked against the allow-list first. p's own delegates
// must already be fetched. It returns what each nested delegate resolved to,
// for the install record.
func fetchNestedDelegates(p *harness.Harness, cfg *config.Config) ([]plugin.ResolvedSourceJSON, error) {
	var got []plugin.ResolvedSourceJSON
	_, err := resolver.FlattenDelegates(p.DelegatesTo, p.Dir, p.Name, func(gs harness.GitSource, dir string) (string, error) {
		if dir == p.Dir {
			// One of p's own: fetched already.
			base, _, err := resolver.ResolveGitSourceFromCache(gs, dir)
			return base, err
		}
		if err := cfg.CheckSource(gs.Git, dir); err != nil {
			return "", err
		}
		base, res, err := resolver.ResolveGitSource(gs, dir)
		if err == nil && res != nil {
			got = append(got, plugin.ResolvedSourceJSON{Git: gs.Git, Ref: res.ResolvedRef, Path: gs.Path, SHA: res.SHA})
		}
		return base, err
	})
	return got, err
}
