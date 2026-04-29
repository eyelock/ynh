package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
)

// sensorListEntry is the summary shape for `ynh sensors ls --format json`.
type sensorListEntry struct {
	Name        string `json:"name"`
	Category    string `json:"category,omitempty"`
	SourceKind  string `json:"source_kind"`
	Format      string `json:"format"`
	InlineFocus bool   `json:"inline_focus,omitempty"`
}

// sensorShowEntry is the resolved shape for `ynh sensors show --format json`.
// Inline focuses appear under source.focus.inline; named focuses are expanded
// from the harness's top-level focus map so consumers get a self-contained
// payload.
type sensorShowEntry struct {
	Name     string           `json:"name"`
	Category string           `json:"category,omitempty"`
	Source   sensorShowSource `json:"source"`
	Output   sensorShowOutput `json:"output"`
}

type sensorShowSource struct {
	Files   []string         `json:"files,omitempty"`
	Command string           `json:"command,omitempty"`
	Focus   *sensorShowFocus `json:"focus,omitempty"`
}

type sensorShowFocus struct {
	Name    string `json:"name,omitempty"`
	Profile string `json:"profile,omitempty"`
	Prompt  string `json:"prompt"`
	Inline  bool   `json:"inline"`
}

type sensorShowOutput struct {
	Format  string `json:"format"`
	Channel string `json:"channel,omitempty"`
	Path    string `json:"path,omitempty"`
}

func cmdSensors(args []string) error {
	return cmdSensorsTo(args, os.Stdout, os.Stderr)
}

func cmdSensorsTo(args []string, stdout, stderr io.Writer) error {
	if len(args) < 1 {
		return cliError(stderr, false, errCodeInvalidInput,
			"usage: ynh sensors <ls|show> [args]")
	}
	switch args[0] {
	case "ls", "list":
		return cmdSensorsLs(args[1:], stdout, stderr)
	case "show":
		return cmdSensorsShow(args[1:], stdout, stderr)
	default:
		return cliError(stderr, false, errCodeInvalidInput,
			fmt.Sprintf("unknown sensors subcommand: %s", args[0]))
	}
}

func cmdSensorsLs(args []string, stdout, stderr io.Writer) error {
	structured := detectJSONFormat(args)
	format := "text"
	var harnessName string
	for i := 0; i < len(args); i++ {
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
			if harnessName != "" {
				return cliError(stderr, structured, errCodeInvalidInput,
					fmt.Sprintf("unexpected argument: %s", args[i]))
			}
			harnessName = args[i]
		}
	}
	if harnessName == "" {
		return cliError(stderr, structured, errCodeInvalidInput,
			"usage: ynh sensors ls <harness-name> [--format text|json]")
	}

	p, err := harness.LoadQualified(harnessName)
	if err != nil {
		return cliError(stderr, structured, errCodeNotFound, err.Error())
	}

	entries := buildSensorList(p)

	switch format {
	case "text":
		return printSensorListText(stdout, entries)
	case "json":
		data, err := json.MarshalIndent(entries, "", "  ")
		if err != nil {
			return fmt.Errorf("encoding sensors: %w", err)
		}
		_, err = fmt.Fprintln(stdout, string(data))
		return err
	default:
		return cliError(stderr, structured, errCodeInvalidInput,
			fmt.Sprintf("invalid --format value %q (want text or json)", format))
	}
}

func cmdSensorsShow(args []string, stdout, stderr io.Writer) error {
	structured := detectJSONFormat(args)
	format := "json" // show defaults to JSON — the deep view is intended for machine consumption
	var harnessName, sensorName string
	for i := 0; i < len(args); i++ {
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
			switch {
			case harnessName == "":
				harnessName = args[i]
			case sensorName == "":
				sensorName = args[i]
			default:
				return cliError(stderr, structured, errCodeInvalidInput,
					fmt.Sprintf("unexpected argument: %s", args[i]))
			}
		}
	}
	if harnessName == "" || sensorName == "" {
		return cliError(stderr, structured, errCodeInvalidInput,
			"usage: ynh sensors show <harness-name> <sensor-name> [--format text|json]")
	}

	p, err := harness.LoadQualified(harnessName)
	if err != nil {
		return cliError(stderr, structured, errCodeNotFound, err.Error())
	}

	entry, ok := buildSensorShow(p, sensorName)
	if !ok {
		return cliError(stderr, structured, errCodeNotFound,
			fmt.Sprintf("sensor %q not declared in harness %q", sensorName, p.Name))
	}

	switch format {
	case "json":
		data, err := json.MarshalIndent(entry, "", "  ")
		if err != nil {
			return fmt.Errorf("encoding sensor: %w", err)
		}
		_, err = fmt.Fprintln(stdout, string(data))
		return err
	case "text":
		return printSensorShowText(stdout, entry)
	default:
		return cliError(stderr, structured, errCodeInvalidInput,
			fmt.Sprintf("invalid --format value %q (want text or json)", format))
	}
}

func buildSensorList(p *harness.Harness) []sensorListEntry {
	entries := make([]sensorListEntry, 0, len(p.Sensors))
	names := make([]string, 0, len(p.Sensors))
	for n := range p.Sensors {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		s := p.Sensors[n]
		e := sensorListEntry{
			Name:       n,
			Category:   s.Category,
			SourceKind: s.Source.Kind(),
			Format:     s.Output.Format,
		}
		if s.Source.Focus != nil && s.Source.Focus.Inline != nil {
			e.InlineFocus = true
		}
		entries = append(entries, e)
	}
	return entries
}

func buildSensorShow(p *harness.Harness, name string) (sensorShowEntry, bool) {
	s, ok := p.Sensors[name]
	if !ok {
		return sensorShowEntry{}, false
	}
	entry := sensorShowEntry{
		Name:     name,
		Category: s.Category,
		Output: sensorShowOutput{
			Format:  s.Output.Format,
			Channel: s.Output.Channel,
			Path:    s.Output.Path,
		},
	}
	entry.Source.Files = s.Source.Files
	entry.Source.Command = s.Source.Command
	if s.Source.Focus != nil {
		entry.Source.Focus = resolveSensorFocus(p, s.Source.Focus)
	}
	return entry, true
}

// resolveSensorFocus expands a string focus reference against the harness's
// top-level focuses so consumers get a self-contained payload. Inline focuses
// pass through unchanged.
func resolveSensorFocus(p *harness.Harness, fr *plugin.FocusRef) *sensorShowFocus {
	if fr.Inline != nil {
		return &sensorShowFocus{
			Profile: fr.Inline.Profile,
			Prompt:  fr.Inline.Prompt,
			Inline:  true,
		}
	}
	resolved, ok := p.Focuses[fr.Name]
	if !ok {
		// Validation should have caught this, but emit a stub so the consumer
		// can still see what reference was unresolvable.
		return &sensorShowFocus{Name: fr.Name}
	}
	return &sensorShowFocus{
		Name:    fr.Name,
		Profile: resolved.Profile,
		Prompt:  resolved.Prompt,
	}
}

func printSensorListText(w io.Writer, entries []sensorListEntry) error {
	if len(entries) == 0 {
		_, err := fmt.Fprintln(w, "(no sensors declared)")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tCATEGORY\tSOURCE\tFORMAT")
	for _, e := range entries {
		cat := e.Category
		if cat == "" {
			cat = "-"
		}
		kind := e.SourceKind
		if e.InlineFocus {
			kind = "focus*"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", e.Name, cat, kind, e.Format)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if hasInline(entries) {
		_, _ = fmt.Fprintln(w, "\n* = inline focus")
	}
	return nil
}

func hasInline(entries []sensorListEntry) bool {
	for _, e := range entries {
		if e.InlineFocus {
			return true
		}
	}
	return false
}

func printSensorShowText(w io.Writer, entry sensorShowEntry) error {
	_, _ = fmt.Fprintf(w, "Name:      %s\n", entry.Name)
	if entry.Category != "" {
		_, _ = fmt.Fprintf(w, "Category:  %s\n", entry.Category)
	}
	_, _ = fmt.Fprintln(w, "Source:")
	switch {
	case len(entry.Source.Files) > 0:
		_, _ = fmt.Fprintf(w, "  files:   %s\n", strings.Join(entry.Source.Files, ", "))
	case entry.Source.Command != "":
		_, _ = fmt.Fprintf(w, "  command: %s\n", entry.Source.Command)
	case entry.Source.Focus != nil:
		f := entry.Source.Focus
		label := "(inline)"
		if !f.Inline && f.Name != "" {
			label = "→ focus " + f.Name
		}
		_, _ = fmt.Fprintf(w, "  focus:   %s\n", label)
		if f.Profile != "" {
			_, _ = fmt.Fprintf(w, "    profile: %s\n", f.Profile)
		}
		_, _ = fmt.Fprintf(w, "    prompt:  %q\n", f.Prompt)
	}
	_, _ = fmt.Fprintln(w, "Output:")
	_, _ = fmt.Fprintf(w, "  format:  %s\n", entry.Output.Format)
	if entry.Output.Channel != "" {
		_, _ = fmt.Fprintf(w, "  channel: %s\n", entry.Output.Channel)
	}
	if entry.Output.Path != "" {
		_, _ = fmt.Fprintf(w, "  path:    %s\n", entry.Output.Path)
	}
	return nil
}
