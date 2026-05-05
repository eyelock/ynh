package main

import (
	"fmt"
	"strings"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/registry"
	"github.com/eyelock/ynh/internal/sources"
)

// resolvedSource holds the result of disambiguating an install source.
// sourceType is always set and is the single source of truth for provenance.
// gitURL is only set when the source was resolved from a registry lookup.
type resolvedSource struct {
	gitURL       string // non-empty if resolved to a Git URL (from registry)
	path         string // monorepo subdir (from registry entry)
	ref          string // optional Git ref pin from a registry entry
	sha          string // optional commit SHA, verified against the fetched HEAD
	localPath    string // absolute path when resolved from a configured source
	sourceType   string // "local", "git", "registry", "source"
	registryName string // non-empty for registry lookups (user-declared label)
	namespace    string // non-empty for registry lookups (URL-derived "org/repo")
	sourceName   string // non-empty for source lookups (config source name)
	// nameHint is the trailing segment of a canonical id when we don't
	// know its in-repo path yet — e.g. for "github.com/org/repo/researcher"
	// the harness might live at the root, at "harnesses/researcher/", or
	// anywhere else under the cloned repo. Post-clone discovery scans for
	// a manifest with this name and sets path accordingly. Empty for refs
	// where the path is already known (registry lookups, 5+-segment ids).
	nameHint string
}

// resolveInstallSource applies disambiguation rules to determine the source type.
// Returns a resolvedSource if the source was resolved via registry lookup,
// or an empty resolvedSource if it should be handled as-is (local path or Git URL).
func resolveInstallSource(source, existingPath string, cfg *config.Config) (resolvedSource, error) {
	// Rule 1: local path — handled by isLocalPath() in caller
	if isLocalPath(source) {
		return resolvedSource{sourceType: "local"}, nil
	}

	// Rule 2: Git SSH URL
	if strings.HasPrefix(source, "git@") {
		return resolvedSource{sourceType: "git"}, nil
	}

	// Rule 3: Git HTTPS/HTTP/file URL
	if strings.HasPrefix(source, "https://") || strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "file://") {
		return resolvedSource{sourceType: "git"}, nil
	}

	// Rule 4: Contains @ → registry lookup as name@<namespace-or-label>.
	// Passes the full qualified ref through to LookupExact, which matches
	// against the registry's URL-derived namespace (e.g. "eyelock/assistants")
	// or its user-declared name (e.g. "eyelock-assistants").
	if strings.Contains(source, "@") {
		return lookupFromRegistry(source, cfg)
	}

	// Rule 5a: Canonical-id shape — "<host>/<org>/<repo>/<name>".
	// Strip the trailing harness-name segment(s) and synthesize the clone
	// URL from the first three segments. Schema 2's canonical id form
	// is path-shaped (Go-modules style), so users typing it directly to
	// `ynh install` need it normalised back to a real Git URL.
	//
	// "local/<name>" is rejected — it's not installable as a remote
	// source; users wanting a local install must pass a filesystem path.
	if cloneURL, withinRepoPath, isCanonID := canonicalIDToClone(source); isCanonID {
		if cloneURL == "" {
			return resolvedSource{}, fmt.Errorf(
				"%q is a local canonical id; install from a filesystem path instead "+
					"(e.g. 'ynh install ./path/to/harness')", source)
		}
		// Canonical id segments after host/org/repo decompose into either
		// an explicit subpath (5+ segments — e.g.
		// github.com/eyelock/assistants/e2e-fixtures/minimal → path
		// "e2e-fixtures/minimal") or a name hint (4 segments — e.g.
		// github.com/eyelock/assistants/researcher → "find harness named
		// researcher anywhere in the cloned repo"). The four-segment case
		// can't assume the harness lives at the repo root because monorepos
		// commonly nest harnesses under harnesses/<name>/ or similar; the
		// caller does post-clone discovery to resolve the actual subpath.
		var path, nameHint string
		if strings.Contains(withinRepoPath, "/") {
			path = withinRepoPath
		} else {
			nameHint = withinRepoPath
		}
		return resolvedSource{sourceType: "git", gitURL: cloneURL, path: path, nameHint: nameHint}, nil
	}

	// Rule 5b: Contains / → Git URL shorthand
	if strings.Contains(source, "/") {
		return resolvedSource{sourceType: "git"}, nil
	}

	// Rule 6: Plain word → check local sources first, then registry search
	if len(cfg.Sources) > 0 {
		if rs, found := searchFromSources(source, cfg); found {
			return rs, nil
		}
	}
	return searchFromRegistry(source, cfg)
}

func lookupFromRegistry(qualified string, cfg *config.Config) (resolvedSource, error) {
	if len(cfg.Registries) == 0 {
		return resolvedSource{}, fmt.Errorf("no registries configured. Add one with: ynh registry add <url>")
	}

	regs, err := registry.FetchAll(cfg.Registries)
	if err != nil {
		return resolvedSource{}, fmt.Errorf("fetching registries: %w", err)
	}

	results := registry.LookupExact(regs, qualified, "")
	if len(results) == 0 {
		return resolvedSource{}, fmt.Errorf("%q not found in any registry", qualified)
	}

	entry := results[0].Entry
	return resolvedSource{
		gitURL:       entry.Repo,
		path:         entry.Path,
		ref:          entry.Ref,
		sha:          entry.SHA,
		sourceType:   "registry",
		registryName: results[0].RegistryName,
		namespace:    entry.Namespace,
	}, nil
}

func searchFromRegistry(name string, cfg *config.Config) (resolvedSource, error) {
	if len(cfg.Registries) == 0 {
		return resolvedSource{}, fmt.Errorf(
			"no registries configured.\n  Add one with: ynh registry add <url>\n  Or specify a Git URL: ynh install github.com/user/%s",
			name,
		)
	}

	regs, err := registry.FetchAll(cfg.Registries)
	if err != nil {
		return resolvedSource{}, fmt.Errorf("fetching registries: %w", err)
	}

	results := registry.LookupExact(regs, name, "")
	if len(results) == 1 {
		entry := results[0].Entry
		return resolvedSource{
			gitURL:       entry.Repo,
			path:         entry.Path,
			ref:          entry.Ref,
			sha:          entry.SHA,
			sourceType:   "registry",
			registryName: results[0].RegistryName,
			namespace:    entry.Namespace,
		}, nil
	}

	if len(results) > 1 {
		msg := fmt.Sprintf("multiple matches for %q:\n", name)
		for _, r := range results {
			msg += fmt.Sprintf("  %s (from %s)\n", r.Entry.Name, r.RegistryName)
		}
		msg += fmt.Sprintf("Use: ynh install %s@<namespace>", name)
		return resolvedSource{}, fmt.Errorf("%s", msg)
	}

	// No exact match — try search
	searchResults := registry.Search(regs, name)
	if len(searchResults) > 0 {
		msg := fmt.Sprintf("%q not found. Similar results:\n", name)
		for _, r := range searchResults {
			msg += fmt.Sprintf("  %s - %s (from %s)\n", r.Entry.Name, r.Entry.Description, r.RegistryName)
		}
		return resolvedSource{}, fmt.Errorf("%s", msg)
	}

	return resolvedSource{}, fmt.Errorf(
		"%q not found in any registry.\n  Did you mean a Git URL? Try: ynh install github.com/user/%s",
		name, name,
	)
}

// searchFromSources looks for a harness by exact name in all configured
// local sources. Returns the first match (config order wins).
func searchFromSources(name string, cfg *config.Config) (resolvedSource, bool) {
	for _, s := range cfg.Sources {
		discovered, err := sources.Discover(s.Path, 2)
		if err != nil {
			continue
		}
		for _, h := range discovered {
			if strings.EqualFold(h.Name, name) {
				return resolvedSource{
					sourceType: "source",
					localPath:  h.Path,
					sourceName: s.Name,
				}, true
			}
		}
	}
	return resolvedSource{}, false
}

// canonicalIDToClone recognises a canonical-id-shaped install source
// and decomposes it into (cloneURL, withinRepoPath, isCanonID).
//
// Returns:
//   - ("https://<host>/<org>/<repo>", "<within>", true) for canonical ids
//     of the form "<host>/<org>/<repo>/<within...>" where host contains
//     a "." (real hostname). withinRepoPath is the path inside the repo
//     to the harness (last segment is the harness name; preceding
//     segments form an implicit --path for monorepos). For four-segment
//     ids ("host/org/repo/name"), withinRepoPath is the bare name and
//     gets treated as a top-level harness directory by the existing
//     loader — a no-op when the harness lives at the repo root.
//   - ("", "", true) for "local/<name>" — recognised as a canonical id
//     but not installable as a remote source.
//   - ("", "", false) for everything else (regular Git URLs, monorepo
//     shorthands like "myorg/myrepo", etc).
func canonicalIDToClone(source string) (cloneURL, withinRepoPath string, isCanonID bool) {
	parts := strings.Split(source, "/")
	if len(parts) < 2 {
		return "", "", false
	}
	if parts[0] == "local" {
		return "", "", true
	}
	if len(parts) < 4 || !strings.Contains(parts[0], ".") {
		return "", "", false
	}
	cloneURL = "https://" + parts[0] + "/" + parts[1] + "/" + parts[2]
	// Everything after host/org/repo is the within-repo path. For ids
	// with more than four segments (monorepo subdir), the leading
	// portion of withinRepoPath becomes an implicit --path; the trailing
	// segment is the harness name.
	withinRepoPath = strings.Join(parts[3:], "/")
	return cloneURL, withinRepoPath, true
}
