package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

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
)

// checkEnvelope is the `ynh check --format json` payload.
type checkEnvelope struct {
	Capabilities string        `json:"capabilities"`
	YnhVersion   string        `json:"ynh_version"`
	Harness      string        `json:"harness"`
	Verdict      string        `json:"verdict"` // pass | blocked
	Summary      checkSummary  `json:"summary"`
	Sensors      []checkResult `json:"sensors"`
}

type checkSummary struct {
	Total    int `json:"total"`
	Passed   int `json:"passed"`
	Failed   int `json:"failed"`
	Blocking int `json:"blocking"` // failures that caused a block
	Reported int `json:"reported"`
	Deferred int `json:"deferred"`
	Skipped  int `json:"skipped"`
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
}

func cmdCheck(args []string, stdout, stderr io.Writer) error {
	structured := false
	cwd := ""
	var harnessName string
	var only []string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--cwd":
			if i+1 >= len(args) {
				return checkExecErr(cliError(stderr, structured, errCodeInvalidInput, "--cwd requires a value"))
			}
			i++
			cwd = args[i]
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
			} else {
				res.Status = statusFail
				env.Summary.Failed++
				if res.Tolerance == "blocking" {
					env.Summary.Blocking++
					env.Verdict = "blocked"
				}
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
		}
		detail := r.Status
		if r.Status == statusFail && r.Tolerance != "blocking" {
			detail = r.Status + " (" + r.Tolerance + ")"
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
		body := strings.TrimSpace(r.Stdout + "\n" + r.Stderr)
		if body == "" {
			continue
		}
		if _, err := fmt.Fprintf(w, "\n%s:\n%s\n", r.Name, body); err != nil {
			return err
		}
	}

	if env.Verdict == "blocked" {
		_, err := fmt.Fprintf(w, "\nblocked: %d of %d sensors failed\n", env.Summary.Blocking, env.Summary.Total)
		return err
	}
	_, err := fmt.Fprintf(w, "\nok: %d passed\n", env.Summary.Passed)
	return err
}
