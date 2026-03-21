package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/registry"
)

func cmdSearch(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: ynh search <term>")
	}

	query := strings.Join(args, " ")

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if len(cfg.Registries) == 0 {
		return fmt.Errorf("no registries configured. Add one with: ynh registry add <url>")
	}

	regs, err := registry.FetchAll(cfg.Registries)
	if err != nil {
		return err
	}

	results := registry.Search(regs, query)
	if len(results) == 0 {
		fmt.Printf("No results for %q\n", query)
		return nil
	}

	printSearchResults(results)
	return nil
}

func printSearchResults(results []registry.SearchResult) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tDESCRIPTION\tREPO\tVENDORS\tREGISTRY")
	for _, r := range results {
		vendors := strings.Join(r.Entry.Vendors, ",")
		if vendors == "" {
			vendors = "-"
		}
		repo := r.Entry.Repo
		if r.Entry.Path != "" {
			repo += " (" + r.Entry.Path + ")"
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			r.Entry.Name,
			r.Entry.Description,
			repo,
			vendors,
			r.RegistryName,
		)
	}
	_ = w.Flush()
}
