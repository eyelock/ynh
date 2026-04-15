package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/eyelock/ynh/internal/config"
)

// resolvedPaths holds every path root ynh resolves for the current environment.
// Field order in the struct drives both JSON key order and tabwriter row order,
// so consumers see a stable shape.
type resolvedPaths struct {
	Home      string `json:"home"`
	Config    string `json:"config"`
	Harnesses string `json:"harnesses"`
	Symlinks  string `json:"symlinks"`
	Cache     string `json:"cache"`
	Run       string `json:"run"`
	Bin       string `json:"bin"`
}

// cmdPaths reports every path root ynh resolves for the current environment.
// Useful for scripting, CI, shell completion, and troubleshooting — the same
// values ynh uses internally, without guessing at $YNH_HOME or platform defaults.
func cmdPaths(args []string) error {
	return cmdPathsTo(args, os.Stdout, os.Stderr)
}

func cmdPathsTo(args []string, stdout, stderr io.Writer) error {
	// Detect structured mode in a pre-pass so errors emitted during argument
	// parsing honour the conventions doc's JSON error envelope.
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

	p := resolvedPaths{
		Home:      config.HomeDir(),
		Config:    config.ConfigPath(),
		Harnesses: config.HarnessesDir(),
		Symlinks:  config.SymlinksPath(),
		Cache:     config.CacheDir(),
		Run:       config.RunDir(),
		Bin:       config.BinDir(),
	}

	switch format {
	case "text":
		return printPathsText(stdout, p)
	case "json":
		return printPathsJSON(stdout, p)
	default:
		return cliError(stderr, structured, errCodeInvalidInput,
			fmt.Sprintf("invalid --format value %q (want text or json)", format))
	}
}

// detectJSONFormat scans args for "--format json" without doing full parsing.
// Used to decide which error shape to emit when parsing itself fails.
func detectJSONFormat(args []string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--format" && args[i+1] == "json" {
			return true
		}
	}
	return false
}

func printPathsText(w io.Writer, p resolvedPaths) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(tw, "home\t%s\n", p.Home)
	_, _ = fmt.Fprintf(tw, "config\t%s\n", p.Config)
	_, _ = fmt.Fprintf(tw, "harnesses\t%s\n", p.Harnesses)
	_, _ = fmt.Fprintf(tw, "symlinks\t%s\n", p.Symlinks)
	_, _ = fmt.Fprintf(tw, "cache\t%s\n", p.Cache)
	_, _ = fmt.Fprintf(tw, "run\t%s\n", p.Run)
	_, _ = fmt.Fprintf(tw, "bin\t%s\n", p.Bin)
	return tw.Flush()
}

func printPathsJSON(w io.Writer, p resolvedPaths) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding paths: %w", err)
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}
