package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
)

// sensorListEntry is the summary shape for `ynh sensors ls --format json`.
type sensorListEntry struct {
	Name        string `json:"name"`
	Category    string `json:"category,omitempty"`
	Role        string `json:"role,omitempty"`
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
	Role     string           `json:"role,omitempty"`
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
	case "run":
		return cmdSensorsRun(args[1:], stdout, stderr)
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
			Role:       s.Role,
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
		Role:     s.Role,
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

// sensorRunResult is the payload returned by `ynh sensors run`.
//
// Contract notes for loop drivers consuming this:
//   - There is NO `passed` field. ynh runs the sensor mechanically and
//     returns raw signal (exit_code, output, files). Whether a result counts
//     as pass or fail is loop-driver policy — per-team thresholds, severity
//     filters, and convergence judgments belong above ynh, not inside it.
//   - For focus-sourced sensors ynh resolves the focus declaration and
//     returns it under output.focus. The loop driver invokes the agent
//     runtime; ynh owns no agent-invocation surface.
type sensorRunResult struct {
	Name       string          `json:"name"`
	Kind       string          `json:"kind"` // files | command | focus
	Role       string          `json:"role,omitempty"`
	Category   string          `json:"category,omitempty"`
	ExitCode   int             `json:"exit_code"`
	DurationMS int64           `json:"duration_ms"`
	Output     sensorRunOutput `json:"output"`
}

type sensorRunOutput struct {
	Format  string           `json:"format"`
	Channel string           `json:"channel,omitempty"`
	Stdout  string           `json:"stdout,omitempty"`
	Stderr  string           `json:"stderr,omitempty"`
	Files   []sensorRunFile  `json:"files,omitempty"`
	Focus   *sensorShowFocus `json:"focus,omitempty"`
	Note    string           `json:"note,omitempty"`
}

type sensorRunFile struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	Content string `json:"content,omitempty"`
}

func cmdSensorsRun(args []string, stdout, stderr io.Writer) error {
	structured := true // run only emits JSON
	cwd := ""
	includeContent := true
	var harnessName, sensorName string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--cwd":
			if i+1 >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--cwd requires a value")
			}
			i++
			cwd = args[i]
		case "--no-content":
			includeContent = false
		case "--format":
			if i+1 >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--format requires a value")
			}
			i++
			if args[i] != "json" {
				return cliError(stderr, structured, errCodeInvalidInput,
					"ynh sensors run only supports --format json")
			}
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
			"usage: ynh sensors run <harness-name> <sensor-name> [--cwd dir] [--no-content]")
	}

	p, err := harness.LoadQualified(harnessName)
	if err != nil {
		return cliError(stderr, structured, errCodeNotFound, err.Error())
	}
	s, ok := p.Sensors[sensorName]
	if !ok {
		return cliError(stderr, structured, errCodeNotFound,
			fmt.Sprintf("sensor %q not declared in harness %q", sensorName, p.Name))
	}

	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	result, err := runSensor(p, sensorName, s, cwd, includeContent)
	if err != nil {
		return cliError(stderr, structured, errCodeIOError, err.Error())
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding sensor result: %w", err)
	}
	_, err = fmt.Fprintln(stdout, string(data))
	return err
}

func runSensor(p *harness.Harness, name string, s plugin.Sensor, cwd string, includeContent bool) (*sensorRunResult, error) {
	r := &sensorRunResult{
		Name:     name,
		Kind:     s.Source.Kind(),
		Role:     s.Role,
		Category: s.Category,
		Output: sensorRunOutput{
			Format:  s.Output.Format,
			Channel: s.Output.Channel,
		},
	}
	start := time.Now()
	defer func() { r.DurationMS = time.Since(start).Milliseconds() }()

	switch s.Source.Kind() {
	case "command":
		if r.Output.Channel == "" {
			r.Output.Channel = "stdout+exit"
		}
		var stdoutBuf, stderrBuf bytes.Buffer
		cmd := exec.Command("/bin/sh", "-c", s.Source.Command)
		cmd.Dir = cwd
		cmd.Stdout = &stdoutBuf
		cmd.Stderr = &stderrBuf
		runErr := cmd.Run()
		r.Output.Stdout = stdoutBuf.String()
		r.Output.Stderr = stderrBuf.String()
		if runErr != nil {
			if ee, ok := runErr.(*exec.ExitError); ok {
				r.ExitCode = ee.ExitCode()
			} else {
				// Couldn't even start the command — treat as a runner failure
				// and surface via stderr; loop driver decides what to do.
				r.ExitCode = -1
				r.Output.Stderr = runErr.Error()
			}
		}

	case "files":
		if r.Output.Channel == "" {
			r.Output.Channel = "files"
		}
		for _, pat := range s.Source.Files {
			abs := pat
			if !filepath.IsAbs(abs) {
				abs = filepath.Join(cwd, pat)
			}
			matches, err := filepath.Glob(abs)
			if err != nil {
				return nil, fmt.Errorf("glob %q: %w", pat, err)
			}
			sort.Strings(matches)
			for _, m := range matches {
				info, err := os.Stat(m)
				if err != nil || info.IsDir() {
					continue
				}
				f := sensorRunFile{Path: m, Size: info.Size()}
				if includeContent {
					content, err := os.ReadFile(m)
					if err != nil {
						return nil, fmt.Errorf("reading %s: %w", m, err)
					}
					f.Content = string(content)
				}
				r.Output.Files = append(r.Output.Files, f)
			}
		}

	case "focus":
		if r.Output.Channel == "" {
			r.Output.Channel = "stdout"
		}
		r.Output.Focus = resolveSensorFocus(p, s.Source.Focus)
		r.Output.Note = "ynh resolves the focus declaration; the loop driver invokes the agent runtime"

	default:
		return nil, fmt.Errorf("sensor %q has no source variant set", name)
	}

	return r, nil
}
