package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/eyelock/ynh/internal/baseline"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/gate"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
)

// cmdBaseline reports what a harness's ratchet is currently forgiving.
//
// The ratchet had no read surface at all: the only way to learn what it was
// letting through was to read the JSON by hand, and the JSON stores twelve
// character hashes rather than findings. So "what does this gate forgive" —
// the question an auditor actually asks — had no answer.
//
// Two levels. Without --explain it reports the recorded shape: when each
// sensor's debt was accepted, how much of it there is, and whether it was
// truncated. With --explain it runs the sensors and resolves the hashes
// against live output, which is what turns a list of fingerprints into a list
// of findings. That costs a run, so it is opt-in.
func cmdBaseline(args []string, stdout, stderr io.Writer) error {
	structured := false
	explain := false
	cwd := ""
	var harnessName string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--format":
			i++
			if i >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--format requires a value")
			}
			switch args[i] {
			case "json":
				structured = true
			case "text":
			default:
				return cliError(stderr, structured, errCodeInvalidInput,
					fmt.Sprintf("unknown format: %s (want text or json)", args[i]))
			}
		case "--explain":
			explain = true
		case "--cwd":
			i++
			if i >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--cwd requires a value")
			}
			cwd = args[i]
		default:
			if harnessName == "" {
				harnessName = args[i]
			}
		}
	}
	if harnessName == "" {
		return cliError(stderr, structured, errCodeInvalidInput,
			"usage: ynh baseline <harness> [--explain] [--cwd <dir>] [--format text|json]")
	}

	p, err := harness.LoadQualified(harnessName)
	if err != nil {
		return cliError(stderr, structured, errCodeNotFound, err.Error())
	}
	if cwd == "" {
		if cwd, err = os.Getwd(); err != nil {
			return cliError(stderr, structured, errCodeIOError, err.Error())
		}
	}
	base, err := baseline.Load(cwd)
	if err != nil {
		return cliError(stderr, structured, errCodeIOError, err.Error())
	}

	env := gate.BaselineReport{
		Capabilities: config.CapabilitiesVersion,
		YnhVersion:   config.Version,
		Harness:      p.Name,
		Sensors:      []gate.BaselineSensor{},
	}

	names := make([]string, 0, len(p.Sensors))
	for n := range p.Sensors {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		s := p.Sensors[name]
		rec, ok := base.Entry(p.Name, name)
		row := gate.BaselineSensor{Name: name, Kind: s.Source.Kind(), Ratchet: s.EffectiveRatchet()}
		if !ok {
			// No entry means nothing is forgiven — which is different from an
			// entry recording zero, and worth saying rather than omitting.
			row.Recorded = false
			env.Sensors = append(env.Sensors, row)
			env.Summary.Unrecorded++
			continue
		}
		row.Recorded = true
		row.RecordedAt = rec.RecordedAt
		row.Status = rec.Status
		row.Forgiven = rec.Count
		row.Total = rec.Total
		row.Truncated = rec.Truncated
		env.Summary.Recorded++
		env.Summary.Forgiven += rec.Count

		if explain {
			row.Findings = explainForgiven(p, name, s, cwd, rec)
			if rec.Truncated {
				row.Note = "recorded without fingerprints — too many findings to store, so only the count is ratcheted"
			}
		}
		env.Sensors = append(env.Sensors, row)
	}
	env.Summary.Total = len(env.Sensors)

	if structured {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(env)
	}
	printBaselineText(stdout, env, explain)
	return nil
}

// explainForgiven resolves recorded fingerprints back to the lines that
// produce them, by running the sensor and matching.
//
// This is the part that makes forgiveness auditable. A fingerprint is a hash
// of a normalised line, so it cannot be reversed — but the current output can
// be hashed and matched, which answers "what is this gate letting through"
// with the findings themselves rather than twelve hex characters.
//
// A finding that no longer appears is not listed: it has been fixed, and the
// baseline can be narrowed.
func explainForgiven(p *harness.Harness, name string, s plugin.Sensor, cwd string, rec baseline.SensorBaseline) []string {
	if rec.Truncated || len(rec.Fingerprints) == 0 {
		return nil
	}
	run, err := runSensor(p, name, s, cwd, false)
	if err != nil {
		return nil
	}
	recorded := make(map[string]bool, len(rec.Fingerprints))
	for _, fp := range rec.Fingerprints {
		recorded[fp] = true
	}
	raw := run.Output.Stdout + "\n" + run.Output.Stderr
	var out []string
	seen := map[string]bool{}
	for _, line := range splitLines(raw) {
		for _, fp := range baseline.Fingerprints(line, cwd) {
			if recorded[fp] && !seen[fp] {
				seen[fp] = true
				out = append(out, line)
			}
		}
	}
	sort.Strings(out)
	return out
}

func printBaselineText(w io.Writer, env gate.BaselineReport, explain bool) {
	_, _ = fmt.Fprintf(w, "Baseline for %s\n\n", env.Harness)
	for _, r := range env.Sensors {
		if !r.Recorded {
			_, _ = fmt.Fprintf(w, "  · %-24s nothing recorded — no failures are forgiven\n", r.Name)
			continue
		}
		_, _ = fmt.Fprintf(w, "  ● %-24s %d forgiven, accepted %s", r.Name, r.Forgiven, r.RecordedAt)
		if r.Ratchet == "count" {
			_, _ = fmt.Fprintf(w, " (count ratchet, total %d)", r.Total)
		}
		if r.Truncated {
			_, _ = fmt.Fprint(w, " [truncated]")
		}
		_, _ = fmt.Fprintln(w)
		for _, f := range r.Findings {
			_, _ = fmt.Fprintf(w, "      %s\n", f)
		}
		if r.Note != "" {
			_, _ = fmt.Fprintf(w, "      note: %s\n", r.Note)
		}
	}
	s := env.Summary
	_, _ = fmt.Fprintf(w, "\n%d sensors: %d with recorded debt (%d findings forgiven), %d with none\n",
		s.Total, s.Recorded, s.Forgiven, s.Unrecorded)
	if !explain && s.Recorded > 0 {
		_, _ = fmt.Fprint(w,
			"\nRun with --explain to resolve the recorded fingerprints into the findings\nthey forgive. That runs the sensors, so it is not the default.\n")
	}
}

// splitLines splits raw sensor output into non-empty trimmed lines.
func splitLines(raw string) []string {
	var out []string
	for _, l := range strings.Split(raw, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			out = append(out, t)
		}
	}
	return out
}
