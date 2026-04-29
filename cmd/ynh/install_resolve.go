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

	// Rule 3: Git HTTPS/HTTP URL
	if strings.HasPrefix(source, "https://") || strings.HasPrefix(source, "http://") {
		return resolvedSource{sourceType: "git"}, nil
	}

	// Rule 4: Contains @ → registry lookup as name@<namespace-or-label>.
	// Passes the full qualified ref through to LookupExact, which matches
	// against the registry's URL-derived namespace (e.g. "eyelock/assistants")
	// or its user-declared name (e.g. "eyelock-assistants").
	if strings.Contains(source, "@") {
		return lookupFromRegistry(source, cfg)
	}

	// Rule 5: Contains / → Git URL shorthand
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
