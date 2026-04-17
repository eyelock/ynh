package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/sources"
)

func cmdSources(args []string) error {
	return cmdSourcesTo(args, os.Stdout, os.Stderr)
}

func cmdSourcesTo(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ynh sources <add|list|remove>")
	}

	switch args[0] {
	case "add":
		return cmdSourcesAdd(args[1:], stdout)
	case "list":
		return cmdSourcesListTo(args[1:], stdout, stderr)
	case "remove":
		return cmdSourcesRemove(args[1:], stdout)
	default:
		return fmt.Errorf("unknown sources subcommand: %s\nUsage: ynh sources <add|list|remove>", args[0])
	}
}

func cmdSourcesAdd(args []string, stdout io.Writer) error {
	var name, description string
	var remaining []string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--name":
			if i+1 >= len(args) {
				return fmt.Errorf("--name requires a value")
			}
			i++
			name = args[i]
		case "--description":
			if i+1 >= len(args) {
				return fmt.Errorf("--description requires a value")
			}
			i++
			description = args[i]
		default:
			remaining = append(remaining, args[i])
		}
	}

	if len(remaining) != 1 {
		return fmt.Errorf("usage: ynh sources add <path> [--name <n>] [--description <d>]")
	}

	sourcePath := remaining[0]

	// Resolve to absolute path
	absPath, err := filepath.Abs(sourcePath)
	if err != nil {
		return fmt.Errorf("resolving path: %w", err)
	}

	// Validate path exists and is a directory
	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("path %q does not exist", absPath)
	}
	if !info.IsDir() {
		return fmt.Errorf("path %q is not a directory", absPath)
	}

	// Derive name from final segment if not given
	if name == "" {
		name = filepath.Base(absPath)
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Check for duplicate name
	for _, s := range cfg.Sources {
		if s.Name == name {
			return fmt.Errorf("source %q already exists (path: %s)", name, s.Path)
		}
	}

	cfg.Sources = append(cfg.Sources, config.Source{
		Name:        name,
		Path:        absPath,
		Description: description,
	})

	if err := cfg.Save(); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	// Count discoverable harnesses
	discovered, _ := sources.Discover(absPath, 2)
	_, _ = fmt.Fprintf(stdout, "Added source %q (%s) — %d harness(es) found\n", name, absPath, len(discovered))
	return nil
}

// sourceListEntry is the JSON shape for a single source in `ynh sources list`.
type sourceListEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Description string `json:"description,omitempty"`
	Harnesses   int    `json:"harnesses"`
}

func cmdSourcesListTo(args []string, stdout, stderr io.Writer) error {
	structured := detectJSONFormat(args)

	format := "text"
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
			if strings.HasPrefix(args[i], "-") {
				return cliError(stderr, structured, errCodeInvalidInput,
					fmt.Sprintf("unknown flag: %s", args[i]))
			}
			return cliError(stderr, structured, errCodeInvalidInput,
				fmt.Sprintf("unexpected argument: %s", args[i]))
		}
		i++
	}

	switch format {
	case "text":
		return printSourcesText(stdout)
	case "json":
		return printSourcesJSON(stdout)
	default:
		return cliError(stderr, structured, errCodeInvalidInput,
			fmt.Sprintf("invalid --format value %q (want text or json)", format))
	}
}

func printSourcesText(w io.Writer) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if len(cfg.Sources) == 0 {
		_, _ = fmt.Fprintln(w, "No sources configured.")
		_, _ = fmt.Fprintln(w, "Add one with: ynh sources add <path>")
		return nil
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tPATH\tDESCRIPTION\tHARNESSES")

	for _, s := range cfg.Sources {
		discovered, _ := sources.Discover(s.Path, 2)

		desc := s.Description
		if desc == "" {
			desc = "-"
		}

		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%d\n", s.Name, s.Path, desc, len(discovered))
	}

	return tw.Flush()
}

func printSourcesJSON(w io.Writer) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	entries := make([]sourceListEntry, 0, len(cfg.Sources))
	for _, s := range cfg.Sources {
		discovered, _ := sources.Discover(s.Path, 2)
		entries = append(entries, sourceListEntry{
			Name:        s.Name,
			Path:        s.Path,
			Description: s.Description,
			Harnesses:   len(discovered),
		})
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding sources: %w", err)
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}

func cmdSourcesRemove(args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: ynh sources remove <name>")
	}

	name := args[0]

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	found := false
	remaining := make([]config.Source, 0, len(cfg.Sources))
	for _, s := range cfg.Sources {
		if s.Name == name {
			found = true
			continue
		}
		remaining = append(remaining, s)
	}

	if !found {
		return fmt.Errorf("source %q not found", name)
	}

	cfg.Sources = remaining
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	_, _ = fmt.Fprintf(stdout, "Removed source %q\n", name)
	return nil
}
