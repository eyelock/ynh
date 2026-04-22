package registry

import (
	"fmt"
	"strings"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/migration"
	"github.com/eyelock/ynh/internal/namespace"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/resolver"
)

// Registry represents a parsed registry (marketplace.json or legacy registry.json).
type Registry struct {
	Name        string
	Description string
	Namespace   string // derived from registry URL
	Entries     []Entry
}

// Entry describes one harness in a registry.
type Entry struct {
	Name        string
	Description string
	Keywords    []string
	Namespace   string // registry namespace
	// Source fields (from marketplace.json or mapped from legacy)
	Repo    string // owner/repo or full URL
	Path    string // subdirectory within repo
	Version string
	Vendors []string
}

// SearchResult combines a registry entry with its source registry name.
type SearchResult struct {
	Entry        Entry
	RegistryName string
}

// FetchAll loads and parses all configured registries.
func FetchAll(registries []config.RegistrySource) ([]Registry, error) {
	var results []Registry
	for _, src := range registries {
		reg, err := Fetch(src)
		if err != nil {
			return nil, fmt.Errorf("registry %s: %w", src.URL, err)
		}
		results = append(results, reg)
	}
	return results, nil
}

// Fetch clones/updates a single registry repo and parses its index.
func Fetch(src config.RegistrySource) (Registry, error) {
	gs := harness.GitSource{
		Git: src.URL,
		Ref: src.Ref,
	}

	result, err := resolver.EnsureRepo(gs.Git, gs.Ref)
	if err != nil {
		return Registry{}, fmt.Errorf("fetching registry: %w", err)
	}

	reg, err := LoadFromDir(result.Path)
	if err != nil {
		return Registry{}, err
	}

	// Derive and set namespace from the registry URL
	reg.Namespace = namespace.DeriveFromURL(src.URL)
	for i := range reg.Entries {
		reg.Entries[i].Namespace = reg.Namespace
	}
	return reg, nil
}

// LoadFromDir parses a registry from a local directory.
// Runs the migration chain first (registry.json → .ynh-plugin/marketplace.json),
// then reads marketplace.json. Callers never see the old format.
func LoadFromDir(dir string) (Registry, error) {
	if _, err := migration.FormatChain().Run(dir); err != nil {
		return Registry{}, fmt.Errorf("migrating registry: %w", err)
	}

	mj, err := plugin.LoadMarketplaceJSON(dir)
	if err != nil {
		return Registry{}, err
	}

	reg := Registry{
		Name: mj.Name,
	}
	if mj.Metadata != nil {
		reg.Description = mj.Metadata.Description
	}
	if mj.Owner != nil && reg.Name == "" {
		reg.Name = mj.Owner.Name
	}

	harnessRoot := ""
	if mj.Metadata != nil {
		harnessRoot = mj.Metadata.HarnessRoot
	}

	for _, h := range mj.Harnesses {
		e := Entry{
			Name:        h.Name,
			Description: h.Description,
			Keywords:    h.Keywords,
			Version:     h.Version,
		}

		if path, ok := h.SourcePath(); ok {
			// Relative path: resolve against harnessRoot if set
			if harnessRoot != "" && !strings.HasPrefix(path, harnessRoot) {
				path = strings.TrimSuffix(harnessRoot, "/") + "/" + strings.TrimPrefix(path, "./")
			}
			e.Path = path
		} else if src, ok := h.SourceRemote(); ok {
			e.Repo = src.Repo
			if e.Repo == "" {
				e.Repo = src.URL
			}
			e.Path = src.Path
		}

		reg.Entries = append(reg.Entries, e)
	}

	return reg, nil
}

// Search matches a query against all entries across multiple registries.
func Search(registries []Registry, query string) []SearchResult {
	q := strings.ToLower(query)
	var results []SearchResult

	for _, reg := range registries {
		for _, entry := range reg.Entries {
			if matchesQuery(entry, q) {
				results = append(results, SearchResult{
					Entry:        entry,
					RegistryName: reg.Name,
				})
			}
		}
	}

	return results
}

// LookupExact finds an entry by exact name, optionally scoped to a registry.
// Accepts plain "name" or qualified "name@org/repo".
func LookupExact(registries []Registry, ref string, registryName string) []SearchResult {
	name, ns, _ := namespace.ParseQualified(ref)

	var results []SearchResult
	for _, reg := range registries {
		if registryName != "" && reg.Name != registryName {
			continue
		}
		if ns != "" && reg.Namespace != ns {
			continue
		}
		for _, entry := range reg.Entries {
			if strings.EqualFold(entry.Name, name) {
				results = append(results, SearchResult{
					Entry:        entry,
					RegistryName: reg.Name,
				})
			}
		}
	}

	return results
}

func matchesQuery(entry Entry, query string) bool {
	if strings.Contains(strings.ToLower(entry.Name), query) {
		return true
	}
	if strings.Contains(strings.ToLower(entry.Description), query) {
		return true
	}
	for _, kw := range entry.Keywords {
		if strings.Contains(strings.ToLower(kw), query) {
			return true
		}
	}
	return false
}
