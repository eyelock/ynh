package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/registry"
	"github.com/eyelock/ynh/internal/sources"
)

func cmdSearch(args []string) error {
	return cmdSearchTo(args, os.Stdout, os.Stderr)
}

func cmdSearchTo(args []string, stdout, stderr io.Writer) error {
	structured := detectJSONFormat(args)

	format := "text"
	var queryParts []string
	i := 0
	for i < len(args) {
		switch args[i] {
		case "--format":
			if i+1 >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--format requires a value")
			}
			i++
			format = args[i]
		default:
			queryParts = append(queryParts, args[i])
		}
		i++
	}

	query := strings.Join(queryParts, " ")

	cfg, err := config.Load()
	if err != nil {
		return cliError(stderr, structured, errCodeConfigError,
			fmt.Sprintf("loading config: %v", err))
	}

	// Gather results from registries and local sources
	results, err := unifiedSearch(cfg, query)
	if err != nil {
		return cliError(stderr, structured, errCodeIOError, err.Error())
	}

	switch format {
	case "text":
		return printSearchText(stdout, query, results)
	case "json":
		return printSearchJSON(stdout, results)
	default:
		return cliError(stderr, structured, errCodeInvalidInput,
			fmt.Sprintf("invalid --format value %q (want text or json)", format))
	}
}

// searchResultEntry is a unified result from both registries and local sources.
type searchResultEntry struct {
	Name string `json:"name"`
	// Namespace is the URL-derived "<org>/<repo>" for registry results.
	// Empty for local-source results. Lets consumers preview the
	// namespace they'll see post-install without a separate lookup.
	Namespace   string     `json:"namespace,omitempty"`
	Description string     `json:"description,omitempty"`
	Keywords    []string   `json:"keywords,omitempty"`
	Repo        string     `json:"repo,omitempty"`
	Path        string     `json:"path,omitempty"`
	Version     string     `json:"version,omitempty"`
	From        searchFrom `json:"from"`
}

type searchFrom struct {
	Type string `json:"type"` // "registry" or "source"
	Name string `json:"name"`
}

// unifiedSearch searches both registries and local sources.
func unifiedSearch(cfg *config.Config, query string) ([]searchResultEntry, error) {
	var results []searchResultEntry

	// Search registries
	if len(cfg.Registries) > 0 {
		regs, err := registry.FetchAll(cfg.Registries)
		if err != nil {
			return nil, fmt.Errorf("fetching registries: %w", err)
		}
		for _, r := range registry.Search(regs, query) {
			entry := searchResultEntry{
				Name:        r.Entry.Name,
				Namespace:   r.Entry.Namespace,
				Description: r.Entry.Description,
				Keywords:    r.Entry.Keywords,
				Repo:        r.Entry.Repo,
				Path:        r.Entry.Path,
				Version:     r.Entry.Version,
				From:        searchFrom{Type: "registry", Name: r.RegistryName},
			}
			results = append(results, entry)
		}
	}

	// Search local sources
	q := strings.ToLower(query)
	for _, s := range cfg.Sources {
		discovered, err := sources.Discover(s.Path, 2)
		if err != nil {
			continue
		}
		for _, h := range discovered {
			if matchesSourceQuery(h, q) {
				entry := searchResultEntry{
					Name:        h.Name,
					Description: h.Description,
					Repo:        h.Path,
					Version:     h.Version,
					From:        searchFrom{Type: "source", Name: s.Name},
				}
				if len(h.Keywords) > 0 {
					entry.Keywords = h.Keywords
				}
				results = append(results, entry)
			}
		}
	}

	return results, nil
}

// matchesSourceQuery does case-insensitive substring matching against
// a discovered harness's name, description, and keywords.
func matchesSourceQuery(h sources.DiscoveredHarness, query string) bool {
	if strings.Contains(strings.ToLower(h.Name), query) {
		return true
	}
	if strings.Contains(strings.ToLower(h.Description), query) {
		return true
	}
	for _, kw := range h.Keywords {
		if strings.Contains(strings.ToLower(kw), query) {
			return true
		}
	}
	return false
}

func printSearchText(w io.Writer, query string, results []searchResultEntry) error {
	if len(results) == 0 {
		if query == "" {
			_, _ = fmt.Fprintln(w, "No harnesses found")
		} else {
			_, _ = fmt.Fprintf(w, "No results for %q\n", query)
		}
		return nil
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tDESCRIPTION\tREPO\tFROM")
	for _, r := range results {
		repo := r.Repo
		if r.Path != "" {
			repo += " (" + r.Path + ")"
		}
		from := r.From.Name + " (" + r.From.Type + ")"
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			r.Name, r.Description, repo, from)
	}
	return tw.Flush()
}

func printSearchJSON(w io.Writer, results []searchResultEntry) error {
	// Ensure empty array is [] not null
	if results == nil {
		results = []searchResultEntry{}
	}
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding search results: %w", err)
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}
