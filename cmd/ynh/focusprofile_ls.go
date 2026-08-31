package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/eyelock/ynh/internal/harness"
)

// List verbs for `ynh focus` and `ynh profile`.
//
// Both were editor-only — `add`, `remove`, `update` and nothing that reads. The
// capability existed, since `ynh info <name>` prints resolved focuses and
// profiles, but not where anyone looks for it, and not in a shape a loop driver
// can consume.
//
// The inconsistency was inside one command family: `ynh sensors ls` has a list
// verb, so `focus` and `profile` were the odd ones out. It also produced a
// documentation defect — `AGENTS.md` twice claimed these commands list what a
// harness offers, and had to be corrected to point at `ynh info` instead.

// focusListEntry is the summary shape for `ynh focus ls --format json`.
type focusListEntry struct {
	Name string `json:"name"`
	// Profile is the profile this focus applies, when it names one. A focus
	// with a profile is not interchangeable with one without: selecting it
	// also selects an overlay.
	Profile string `json:"profile,omitempty"`
	// Prompt is the focus's full prompt, not a summary. A loop driver picking
	// a focus needs the text it will send, and truncating here would mean
	// every consumer had to call something else to get it.
	Prompt string `json:"prompt"`
}

// profileListEntry is the summary shape for `ynh profile ls --format json`.
//
// Counts rather than contents: the question this answers is "which profiles
// exist and what do they touch". `ynh info` resolves one in full.
type profileListEntry struct {
	Name           string `json:"name"`
	Hooks          int    `json:"hooks"`
	MCPServers     int    `json:"mcp_servers"`
	Includes       int    `json:"includes"`
	EnvPassthrough int    `json:"env_passthrough"`
}

func buildFocusList(p *harness.Harness) []focusListEntry {
	names := make([]string, 0, len(p.Focuses))
	for n := range p.Focuses {
		names = append(names, n)
	}
	sort.Strings(names)

	out := make([]focusListEntry, 0, len(names))
	for _, n := range names {
		f := p.Focuses[n]
		out = append(out, focusListEntry{Name: n, Profile: f.Profile, Prompt: f.Prompt})
	}
	return out
}

func buildProfileList(p *harness.Harness) []profileListEntry {
	names := make([]string, 0, len(p.Profiles))
	for n := range p.Profiles {
		names = append(names, n)
	}
	sort.Strings(names)

	out := make([]profileListEntry, 0, len(names))
	for _, n := range names {
		pr := p.Profiles[n]
		hooks := 0
		for _, entries := range pr.Hooks {
			hooks += len(entries)
		}
		out = append(out, profileListEntry{
			Name:           n,
			Hooks:          hooks,
			MCPServers:     len(pr.MCPServers),
			Includes:       len(pr.Includes),
			EnvPassthrough: len(pr.EnvPassthrough),
		})
	}
	return out
}

// lsArgs parses the shared `<harness> [--format text|json]` shape.
func lsArgs(args []string, verb string) (harnessName, format string, err error) {
	format = "text"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--format":
			if i+1 >= len(args) {
				return "", "", fmt.Errorf("--format requires a value")
			}
			i++
			format = args[i]
		default:
			if strings.HasPrefix(args[i], "-") {
				return "", "", fmt.Errorf("unknown flag: %s", args[i])
			}
			if harnessName != "" {
				return "", "", fmt.Errorf("unexpected argument: %s", args[i])
			}
			harnessName = args[i]
		}
	}
	if harnessName == "" {
		return "", "", fmt.Errorf("usage: ynh %s ls <harness-name> [--format text|json]", verb)
	}
	if format != "text" && format != "json" {
		return "", "", fmt.Errorf("invalid --format value %q (want text or json)", format)
	}
	return harnessName, format, nil
}

func cmdFocusLs(args []string, stdout io.Writer) error {
	name, format, err := lsArgs(args, "focus")
	if err != nil {
		return err
	}
	p, err := harness.LoadQualified(name)
	if err != nil {
		return err
	}
	entries := buildFocusList(p)

	if format == "json" {
		data, mErr := json.MarshalIndent(entries, "", "  ")
		if mErr != nil {
			return fmt.Errorf("encoding focuses: %w", mErr)
		}
		_, wErr := fmt.Fprintln(stdout, string(data))
		return wErr
	}

	if len(entries) == 0 {
		_, wErr := fmt.Fprintf(stdout, "%s declares no focuses.\n", p.Name)
		return wErr
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	if _, wErr := fmt.Fprintln(tw, "NAME\tPROFILE\tPROMPT"); wErr != nil {
		return wErr
	}
	for _, e := range entries {
		profile := e.Profile
		if profile == "" {
			profile = "-"
		}
		// Text is for reading, so the prompt is trimmed to one line here.
		// --format json carries it whole.
		if _, wErr := fmt.Fprintf(tw, "%s\t%s\t%s\n", e.Name, profile, truncate(firstLine(e.Prompt), 60)); wErr != nil {
			return wErr
		}
	}
	return tw.Flush()
}

func cmdProfileLs(args []string, stdout io.Writer) error {
	name, format, err := lsArgs(args, "profile")
	if err != nil {
		return err
	}
	p, err := harness.LoadQualified(name)
	if err != nil {
		return err
	}
	entries := buildProfileList(p)

	if format == "json" {
		data, mErr := json.MarshalIndent(entries, "", "  ")
		if mErr != nil {
			return fmt.Errorf("encoding profiles: %w", mErr)
		}
		_, wErr := fmt.Fprintln(stdout, string(data))
		return wErr
	}

	if len(entries) == 0 {
		_, wErr := fmt.Fprintf(stdout, "%s declares no profiles.\n", p.Name)
		return wErr
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	if _, wErr := fmt.Fprintln(tw, "NAME\tHOOKS\tMCP\tINCLUDES\tENV"); wErr != nil {
		return wErr
	}
	for _, e := range entries {
		if _, wErr := fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\n",
			e.Name, e.Hooks, e.MCPServers, e.Includes, e.EnvPassthrough); wErr != nil {
			return wErr
		}
	}
	return tw.Flush()
}

// truncate shortens s to max runes, by rune so a multi-byte character is never
// split. Pairs with firstLine, which is already defined in this package.
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
