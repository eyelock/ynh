package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/resolver"
)

// Registry represents a parsed registry.json from a Git repo.
type Registry struct {
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	Entries     []Entry `json:"entries"`
}

// Entry describes one harness or plugin in a registry.
type Entry struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Keywords    []string `json:"keywords,omitempty"`
	Repo        string   `json:"repo"`
	Path        string   `json:"path,omitempty"`
	Vendors     []string `json:"vendors,omitempty"`
	Version     string   `json:"version,omitempty"`
}

// SearchResult combines a registry entry with its source registry name.
type SearchResult struct {
	Entry        Entry
	RegistryName string
}

// FetchAll loads and parses all configured registries, returning them keyed by name.
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

// Fetch clones/updates a single registry repo and parses its registry.json.
func Fetch(src config.RegistrySource) (Registry, error) {
	gs := harness.GitSource{
		Git: src.URL,
		Ref: src.Ref,
	}

	result, err := resolver.EnsureRepo(gs.Git, gs.Ref)
	if err != nil {
		return Registry{}, fmt.Errorf("fetching registry: %w", err)
	}

	return LoadFromDir(result.Path)
}

// LoadFromDir parses a registry.json from a local directory.
func LoadFromDir(dir string) (Registry, error) {
	data, err := os.ReadFile(filepath.Join(dir, "registry.json"))
	if err != nil {
		return Registry{}, fmt.Errorf("reading registry.json: %w", err)
	}

	var reg Registry
	if err := json.Unmarshal(data, &reg); err != nil {
		return Registry{}, fmt.Errorf("parsing registry.json: %w", err)
	}

	return reg, nil
}

// Search matches a query against all entries across multiple registries.
// Matching is case-insensitive substring against name, description, and keywords.
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

// LookupExact finds an entry by exact name, optionally scoped to a specific registry.
// registryName can be empty to search all registries.
func LookupExact(registries []Registry, name string, registryName string) []SearchResult {
	var results []SearchResult

	for _, reg := range registries {
		if registryName != "" && reg.Name != registryName {
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
