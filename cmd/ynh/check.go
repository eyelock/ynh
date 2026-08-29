package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/eyelock/ynh/internal/baseline"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
)

// Sensor statuses reported by `ynh check`.
//
// ynh deliberately owns only the thinnest possible pass/fail policy: a
// command sensor passes when it exits 0. Anything richer — thresholds,
// severity filters, convergence judgments — still belongs to a loop driver.
// Sensors whose result cannot be reduced to pass/fail mechanically say so
// rather than guessing, and never gate.
const (
	statusPass     = "pass"     // command sensor exited 0
	statusFail     = "fail"     // command sensor exited non-zero
	statusReported = "reported" // files sensor: content surfaced, no verdict derivable
	statusDeferred = "deferred" // focus sensor: needs an agent runtime ynh does not own
	statusSkipped  = "skipped"  // filtered out by --only
	statusKnown    = "known"    // failing, but every failure is in the baseline
)

// checkEnvelope is the `ynh check --format json` payload.
type checkEnvelope struct {
	Capabilities string        `json:"capabilities"`
	YnhVersion   string        `json:"ynh_version"`
	Harness      string        `json:"harness"`
	Verdict      string        `json:"verdict"` // pass | blocked
	Summary      checkSummary  `json:"summary"`
	Sensors      []checkResult `json:"sensors"`
	Baseline     *baselineInfo `json:"baseline,omitempty"`
}

// baselineInfo tells a consumer whether a ratchet is in play and whether it
// could be tightened. Absent when no baseline has been recorded.
type baselineInfo struct {
	RecordedAt string `json:"recorded_at"`
	Known      int    `json:"known"` // pre-existing failures forgiven this run
	Fixed      int    `json:"fixed"` // baseline entries no longer failing
	Stale      bool   `json:"stale"` // true when Fixed > 0: the baseline can be narrowed
}

type checkSummary struct {
	Total    int `json:"total"`
	Passed   int `json:"passed"`
	Failed   int `json:"failed"`
	Blocking int `json:"blocking"` // failures that caused a block
	Reported int `json:"reported"`
	Deferred int `json:"deferred"`
	Skipped  int `json:"skipped"`
	Known    int `json:"known"` // sensors failing only in ways the baseline records
}

type checkResult struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	Category   string `json:"category,omitempty"`
	Tolerance  string `json:"tolerance"`
	Status     string `json:"status"`
	ExitCode   int    `json:"exit_code"`
	DurationMS int64  `json:"duration_ms"`
	Stdout     string `json:"stdout,omitempty"`
	Stderr     string `json:"stderr,omitempty"`
	Note       string `json:"note,omitempty"`
	// NewCount and KnownCount are set for failing command sensors when a
	// baseline exists. NewOutput carries only the lines not in the baseline —
	// the ones the author is actually being asked to fix.
	NewCount   int    `json:"new_count,omitempty"`
	KnownCount int    `json:"known_count,omitempty"`
	NewOutput  string `json:"new_output,omitempty"`
}

func cmdCheck(args []string, stdout, stderr io.Writer) error {
	structured := false
	cwd := ""
	var harnessName string
	var only []string
	var updateBaseline, ignoreBaseline bool

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--cwd":
			if i+1 >= len(args) {
				return checkExecErr(cliError(stderr, structured, errCodeInvalidInput, "--cwd requires a value"))
			}
			i++
			cwd = args[i]
		case "--update-baseline":
			updateBaseline = true
		case "--no-baseline":
			ignoreBaseline = true
		case "--only":
			if i+1 >= len(args) {
				return checkExecErr(cliError(stderr, structured, errCodeInvalidInput, "--only requires a value"))
			}
			i++
			for _, n := range strings.Split(args[i], ",") {
				if n = strings.TrimSpace(n); n != "" {
					only = append(only, n)
				}
			}
		case "--format":
			if i+1 >= len(args) {
				return checkExecErr(cliError(stderr, structured, errCodeInvalidInput, "--format requires a value"))
			}
			i++
			switch args[i] {
			case "json":
				structured = true
			case "text":
				structured = false
			default:
				return checkExecErr(cliError(stderr, structured, errCodeInvalidInput,
					fmt.Sprintf("unknown format: %s (want text or json)", args[i])))
			}
		default:
			if strings.HasPrefix(args[i], "-") {
				return checkExecErr(cliError(stderr, structured, errCodeInvalidInput,
					fmt.Sprintf("unknown flag: %s", args[i])))
			}
			if harnessName != "" {
				return checkExecErr(cliError(stderr, structured, errCodeInvalidInput,
					fmt.Sprintf("unexpected argument: %s", args[i])))
			}
			harnessName = args[i]
		}
	}

	if harnessName == "" {
		return checkExecErr(cliError(stderr, structured, errCodeInvalidInput,
			"usage: ynh check <harness-name> [--only a,b] [--cwd dir] [--format text|json]"))
	}

	p, err := harness.LoadQualified(harnessName)
	if err != nil {
		return checkExecErr(cliError(stderr, structured, errCodeNotFound, err.Error()))
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	// CI must never write the ratchet. A gate that rewrites its own reference
	// point from a feature branch forgives whatever that branch introduced,
	// which is exactly backwards.
	if updateBaseline && os.Getenv("CI") != "" {
		return checkExecErr(cliError(stderr, structured, errCodeInvalidInput,
			"--update-baseline refuses to run in CI: the baseline is a repository decision, "+
				"not a side effect of a build. Run it locally and commit the result."))
	}

	var base *baseline.Baseline
	if !ignoreBaseline {
		loaded, bErr := baseline.Load(cwd)
		if bErr != nil {
			return checkExecErr(cliError(stderr, structured, errCodeInvalidInput, bErr.Error()))
		}
		base = loaded
	}
	recording := &baseline.Baseline{Sensors: map[string]baseline.SensorBaseline{}}

	wanted := map[string]bool{}
	for _, n := range only {
		if _, ok := p.Sensors[n]; !ok {
			return checkExecErr(cliError(stderr, structured, errCodeNotFound,
				fmt.Sprintf("sensor %q not declared in harness %q", n, p.Name)))
		}
		wanted[n] = true
	}

	names := make([]string, 0, len(p.Sensors))
	for n := range p.Sensors {
		names = append(names, n)
	}
	sort.Strings(names)

	env := checkEnvelope{
		Capabilities: config.CapabilitiesVersion,
		YnhVersion:   config.Version,
		Harness:      p.Name,
		Verdict:      "pass",
	}

	var totalKnown, totalFixed int
	for _, name := range names {
		s := p.Sensors[name]
		res := checkResult{
			Name:      name,
			Kind:      s.Source.Kind(),
			Category:  s.Category,
			Tolerance: s.EffectiveTolerance(),
		}

		if len(wanted) > 0 && !wanted[name] {
			res.Status = statusSkipped
			env.Summary.Skipped++
			env.Sensors = append(env.Sensors, res)
			continue
		}

		run, runErr := runSensor(p, name, s, cwd, false)
		if runErr != nil {
			return checkExecErr(cliError(stderr, structured, errCodeIOError,
				fmt.Sprintf("sensor %q: %v", name, runErr)))
		}
		res.ExitCode = run.ExitCode
		res.DurationMS = run.DurationMS
		res.Stdout = truncateOutput(run.Output.Stdout)
		res.Stderr = truncateOutput(run.Output.Stderr)
		res.Note = run.Output.Note

		switch s.Source.Kind() {
		case "command":
			if run.ExitCode == 0 {
				res.Status = statusPass
				env.Summary.Passed++
				break
			}

			raw := run.Output.Stdout + "\n" + run.Output.Stderr
			current := baseline.Fingerprints(raw, cwd)
			if updateBaseline {
				recording.Sensors[name] = baseline.Record(statusFail, raw, cwd)
			}

			newFPs, known, fixed := base.Compare(name, current)
			res.NewCount = len(newFPs)
			res.KnownCount = known
			totalKnown += known
			totalFixed += fixed

			// A sensor whose every failure is already recorded is debt, not a
			// regression. Reporting it as a failure on every run is what makes
			// a gate feel like noise and gets it switched off.
			if base != nil && len(newFPs) == 0 && known > 0 {
				res.Status = statusKnown
				env.Summary.Known++
				break
			}

			res.Status = statusFail
			env.Summary.Failed++
			if base != nil {
				res.NewOutput = selectLines(raw, cwd, newFPs)
				if base.Truncated(name) {
					res.Note = "baseline was truncated for this sensor; new-failure detection is approximate"
				}
			}
			if res.Tolerance == "blocking" {
				env.Summary.Blocking++
				env.Verdict = "blocked"
			}
		case "files":
			// No verdict is mechanically derivable from a file glob, so a
			// files sensor reports and never gates, whatever its tolerance.
			res.Status = statusReported
			env.Summary.Reported++
		case "focus":
			// Resolving a focus needs an agent runtime ynh does not own.
			res.Status = statusDeferred
			res.Note = "focus sensors require a loop driver; ynh resolves the declaration only"
			env.Summary.Deferred++
		}
		env.Sensors = append(env.Sensors, res)
	}
	env.Summary.Total = len(env.Sensors)
	if base != nil {
		env.Baseline = &baselineInfo{
			RecordedAt: base.RecordedAt,
			Known:      totalKnown,
			Fixed:      totalFixed,
			Stale:      totalFixed > 0,
		}
	}

	if updateBaseline {
		if err := baseline.Save(cwd, recording); err != nil {
			return checkExecErr(cliError(stderr, structured, errCodeIOError, err.Error()))
		}
		if !structured {
			if _, wErr := fmt.Fprintf(stdout, "\nbaseline recorded at %s — commit it\n",
				filepath.Join(baseline.Dir, baseline.File)); wErr != nil {
				return wErr
			}
		}
		// Recording is an explicit act of accepting current state, so it
		// reports what it accepted rather than gating on it.
		return nil
	}

	if structured {
		data, encErr := json.MarshalIndent(env, "", "  ")
		if encErr != nil {
			return fmt.Errorf("encoding check: %w", encErr)
		}
		if _, wErr := fmt.Fprintln(stdout, string(data)); wErr != nil {
			return wErr
		}
	} else if wErr := writeCheckText(stdout, env); wErr != nil {
		return wErr
	}

	if env.Verdict == "blocked" {
		return errCheckBlocked
	}
	return nil
}

// errCheckBlocked signals exit code 1: a blocking sensor failed. Distinct
// from an execution error, which exits 2 — a consumer must be able to tell
// "your code is failing" from "ynh could not run".
var errCheckBlocked = fmt.Errorf("blocked")

// errCheckExec signals exit code 2: ynh could not run the check at all.
// A consumer must be able to tell "your code is failing" (exit 1) from
// "the gate itself is broken" (exit 2) — conflating them makes a red CI
// job ambiguous exactly when it matters.
var errCheckExec = fmt.Errorf("check execution failed")

func checkExecErr(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", errCheckExec, err)
}

// selectLines returns only those output lines whose fingerprint is in want.
// Showing an author the twelve issues they did not introduce alongside the one
// they did is how a useful gate becomes an ignored one.
func selectLines(raw, root string, want []string) string {
	if len(want) == 0 {
		return ""
	}
	wanted := make(map[string]bool, len(want))
	for _, fp := range want {
		wanted[fp] = true
	}
	var keep []string
	for _, line := range strings.Split(raw, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fps := baseline.Fingerprints(line, root)
		if len(fps) == 1 && wanted[fps[0]] {
			keep = append(keep, strings.TrimSpace(line))
		}
	}
	return strings.Join(keep, "\n")
}

const maxSensorOutput = 4096

func truncateOutput(s string) string {
	if len(s) <= maxSensorOutput {
		return s
	}
	return s[:maxSensorOutput] + "\n… truncated"
}

func writeCheckText(w io.Writer, env checkEnvelope) error {
	if len(env.Sensors) == 0 {
		_, err := fmt.Fprintf(w, "%s: no sensors declared\n", env.Harness)
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, r := range env.Sensors {
		// The inner loop runs filtered on every turn, so a skipped sensor
		// must cost nothing to read. It stays in the JSON payload, where a
		// consumer may want the full declared set.
		if r.Status == statusSkipped {
			continue
		}
		mark := "·"
		switch r.Status {
		case statusPass:
			mark = "✓"
		case statusFail:
			mark = "✗"
		case statusKnown:
			mark = "~"
		}
		detail := r.Status
		switch {
		case r.Status == statusKnown:
			detail = fmt.Sprintf("known (%d)", r.KnownCount)
		case r.Status == statusFail && r.NewCount > 0 && r.KnownCount > 0:
			detail = fmt.Sprintf("%d new, %d known", r.NewCount, r.KnownCount)
		case r.Status == statusFail && r.NewCount > 0:
			detail = fmt.Sprintf("%d new", r.NewCount)
		}
		if r.Status == statusFail && r.Tolerance != "blocking" {
			detail += " (" + r.Tolerance + ")"
		}
		if _, err := fmt.Fprintf(tw, "  %s\t%s\t%s\t%dms\n", mark, r.Name, detail, r.DurationMS); err != nil {
			return err
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	// Failing output is the remediation the agent acts on, so it goes to
	// the operator verbatim rather than being summarised away.
	for _, r := range env.Sensors {
		if r.Status != statusFail {
			continue
		}
		// With a baseline in play, show only what this change introduced.
		body := strings.TrimSpace(r.NewOutput)
		if body == "" {
			body = strings.TrimSpace(r.Stdout + "\n" + r.Stderr)
		}
		if body == "" {
			continue
		}
		header := r.Name
		if r.KnownCount > 0 {
			header = fmt.Sprintf("%s — %d new (%d pre-existing not shown)", r.Name, r.NewCount, r.KnownCount)
		}
		if _, err := fmt.Fprintf(w, "\n%s:\n%s\n", header, body); err != nil {
			return err
		}
		if r.Note != "" {
			if _, err := fmt.Fprintf(w, "note: %s\n", r.Note); err != nil {
				return err
			}
		}
	}

	if env.Verdict == "blocked" {
		// Count what actually ran. Reporting "1 of 4" when --only filtered
		// three of them out invites the reader to go looking for failures
		// that were never evaluated.
		ran := env.Summary.Total - env.Summary.Skipped
		if _, err := fmt.Fprintf(w, "\nblocked: %d of %d %s failed\n",
			env.Summary.Blocking, ran, plural(ran, "sensor")); err != nil {
			return err
		}
		return writeBaselineFooter(w, env)
	}
	tail := fmt.Sprintf("\nok: %d passed", env.Summary.Passed)
	if env.Summary.Known > 0 {
		tail += fmt.Sprintf(", %d known", env.Summary.Known)
	}
	if _, err := fmt.Fprintln(w, tail); err != nil {
		return err
	}
	return writeBaselineFooter(w, env)
}

// writeBaselineFooter surfaces a ratchet that has slack in it. Debt paid off
// stays forgiven until someone narrows the baseline, so the gate says when
// that is worth doing rather than waiting to be asked.
func writeBaselineFooter(w io.Writer, env checkEnvelope) error {
	if env.Baseline == nil || !env.Baseline.Stale {
		return nil
	}
	n := env.Baseline.Fixed
	verb := "are"
	if n == 1 {
		verb = "is"
	}
	_, err := fmt.Fprintf(w,
		"\nbaseline: %d recorded %s %s now fixed — `ynh check --update-baseline` to lock that in\n",
		n, plural(n, "failure"), verb)
	return err
}

// plural returns word or its plural, so counted output reads as English.
func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}
