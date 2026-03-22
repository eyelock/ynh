package main

import (
	"fmt"
	"strings"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/registry"
)

// resolvedSource holds the result of disambiguating an install source.
// sourceType is always set and is the single source of truth for provenance.
// gitURL is only set when the source was resolved from a registry lookup.
type resolvedSource struct {
	gitURL       string // non-empty if resolved to a Git URL (from registry)
	path         string // monorepo subdir (from registry entry)
	sourceType   string // "local", "git", "registry"
	registryName string // non-empty for registry lookups
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

	// Rule 4: Contains @ → registry lookup as name@registry-name
	if strings.Contains(source, "@") {
		parts := strings.SplitN(source, "@", 2)
		name := parts[0]
		regName := parts[1]
		return lookupFromRegistry(name, regName, cfg)
	}

	// Rule 5: Contains / → Git URL shorthand
	if strings.Contains(source, "/") {
		return resolvedSource{sourceType: "git"}, nil
	}

	// Rule 6: Plain word → registry search
	return searchFromRegistry(source, cfg)
}

func lookupFromRegistry(name, regName string, cfg *config.Config) (resolvedSource, error) {
	if len(cfg.Registries) == 0 {
		return resolvedSource{}, fmt.Errorf("no registries configured. Add one with: ynh registry add <url>")
	}

	regs, err := registry.FetchAll(cfg.Registries)
	if err != nil {
		return resolvedSource{}, err
	}

	results := registry.LookupExact(regs, name, regName)
	if len(results) == 0 {
		return resolvedSource{}, fmt.Errorf("%q not found in registry %q", name, regName)
	}

	entry := results[0].Entry
	return resolvedSource{
		gitURL:       entry.Repo,
		path:         entry.Path,
		sourceType:   "registry",
		registryName: regName,
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
		return resolvedSource{}, err
	}

	results := registry.LookupExact(regs, name, "")
	if len(results) == 1 {
		entry := results[0].Entry
		return resolvedSource{
			gitURL:       entry.Repo,
			path:         entry.Path,
			sourceType:   "registry",
			registryName: results[0].RegistryName,
		}, nil
	}

	if len(results) > 1 {
		msg := fmt.Sprintf("multiple matches for %q:\n", name)
		for _, r := range results {
			msg += fmt.Sprintf("  %s (from %s)\n", r.Entry.Name, r.RegistryName)
		}
		msg += fmt.Sprintf("Use: ynh install %s@<registry-name>", name)
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
