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
	"time"

	"github.com/eyelock/ynh/internal/baseline"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/freshness"
	"github.com/eyelock/ynh/internal/gate"
	"github.com/eyelock/ynh/internal/harness"
)

func cmdCheck(args []string, stdout, stderr io.Writer) error {
	structured := false
	cwd := ""
	var harnessName string
	var only []string
	var updateBaseline, ignoreBaseline, calibrate bool
	overlay := map[string]json.RawMessage{}

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--cwd":
			if i+1 >= len(args) {
				return checkExecErr(cliError(stderr, structured, errCodeInvalidInput, "--cwd requires a value"))
			}
			i++
			cwd = args[i]
		case "--calibrate":
			calibrate = true
		case "--update-baseline":
			updateBaseline = true
		case "--no-baseline":
			ignoreBaseline = true
		case "--sensor-overlay":
			// A JSON object keyed by sensor name, each value a partial sensor
			// declaration merged over the base before the sensor runs. The
			// agent loop uses it to substitute a faster inner-loop command
			// for the same declared sensor, and needs the substitution to go
			// through the gate rather than around it.
			if i+1 >= len(args) {
				return checkExecErr(cliError(stderr, structured, errCodeInvalidInput,
					"--sensor-overlay requires a value"))
			}
			i++
			if err := json.Unmarshal([]byte(args[i]), &overlay); err != nil {
				return checkExecErr(cliError(stderr, structured, errCodeInvalidInput,
					fmt.Sprintf("--sensor-overlay: invalid JSON: %v", err)))
			}
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

	// Nothing being gated may rewrite the gate's reference point.
	//
	// The CI guard alone protected the wrong process. An agent runs in a
	// worktree where CI is unset, so an agent that could not converge could
	// grant itself blanket amnesty and then converge — the exact "forgives
	// whatever it introduced" failure the CI check was written to prevent,
	// reached by the one path the check did not cover.
	// A baseline records what a declared sensor produces. Recording one while
	// a substitute command is running would file the proxy's output under the
	// real sensor's name, and every later run would compare the real command
	// against a fingerprint set it never produced.
	if updateBaseline && len(overlay) > 0 {
		return checkExecErr(cliError(stderr, structured, errCodeInvalidInput,
			"--update-baseline cannot be combined with --sensor-overlay: a baseline must record "+
				"what the declared sensor produces, not what a substitute command produces."))
	}

	if updateBaseline {
		if session := os.Getenv("YNH_AGENT_SESSION"); session != "" {
			recordBaselineWriteAttempt(session)
			return checkExecErr(cliError(stderr, structured, errCodeInvalidInput,
				"--update-baseline refuses to run inside an agent session: nothing being gated may "+
					"rewrite the gate's reference point. Ask a human to review the failures and "+
					"record the baseline deliberately."))
		}
		if os.Getenv("CI") != "" {
			return checkExecErr(cliError(stderr, structured, errCodeInvalidInput,
				"--update-baseline refuses to run in CI: the baseline is a repository decision, "+
					"not a side effect of a build. Run it locally and commit the result."))
		}
	}

	// Load once, and treat a load failure as fatal even under --no-baseline.
	//
	// This used to load twice and swallow the second error, which was silent
	// data loss waiting to happen: --no-baseline --update-baseline against an
	// unreadable file left `recording` empty, and saving an empty map prunes
	// every entry the load could not see — other sensors, other harnesses.
	// --no-baseline means "do not forgive", not "tolerate a corrupt ratchet".
	loaded, bErr := baseline.Load(cwd)
	if bErr != nil {
		return checkExecErr(cliError(stderr, structured, errCodeInvalidInput, bErr.Error()))
	}

	var base *baseline.Baseline
	if !ignoreBaseline {
		base = loaded
	}

	// Start from what is on disk, not from empty. --update-baseline must
	// refresh the sensors that actually ran and leave every other entry —
	// other sensors, other harnesses — untouched. Writing a freshly built map
	// would silently erase the forgiven debt of anything filtered out by
	// --only, and of every other harness sharing this repository.
	recording := loaded
	if recording == nil {
		recording = &baseline.Baseline{}
	}

	wanted := map[string]bool{}
	for _, n := range only {
		if _, ok := p.Sensors[n]; !ok {
			return checkExecErr(cliError(stderr, structured, errCodeNotFound,
				fmt.Sprintf("sensor %q not declared in harness %q", n, p.Name)))
		}
		wanted[n] = true
	}
	// An overlay for a sensor that does not exist is a typo that would
	// otherwise be silently ignored — the caller would believe it had
	// substituted a command and be gated on the original.
	for n := range overlay {
		if _, ok := p.Sensors[n]; !ok {
			return checkExecErr(cliError(stderr, structured, errCodeNotFound,
				fmt.Sprintf("--sensor-overlay names sensor %q, not declared in harness %q", n, p.Name)))
		}
	}

	// Calibration is a separate mode, not an extra step. `ynh check` stays
	// fast and never runs references: a gate that calibrates on every
	// invocation is a gate people disable, and then nothing is calibrated at
	// all. Baseline flags are meaningless here — a fixture has no debt to
	// ratchet — so they are rejected rather than silently ignored.
	if calibrate {
		if updateBaseline || ignoreBaseline {
			return checkExecErr(cliError(stderr, structured, errCodeInvalidInput,
				"--calibrate does not take baseline flags: a reference fixture has no recorded debt"))
		}
		if len(overlay) > 0 {
			return checkExecErr(cliError(stderr, structured, errCodeInvalidInput,
				"--calibrate does not take --sensor-overlay: calibrating a substituted command proves nothing about the declared one"))
		}
		return runCalibration(p, wanted, stdout, stderr, structured)
	}

	names := make([]string, 0, len(p.Sensors))
	for n := range p.Sensors {
		names = append(names, n)
	}
	sort.Strings(names)

	env := gate.Envelope{
		Capabilities: config.CapabilitiesVersion,
		YnhVersion:   config.Version,
		Harness:      p.Name,
		Verdict:      gate.VerdictPass,
	}

	var totalKnown, totalFixed int
	for _, name := range names {
		s := p.Sensors[name]
		_, overlaid := overlay[name]
		if raw, ok := overlay[name]; ok {
			merged, mergeErr := mergeSensorOverlay(s, raw)
			if mergeErr != nil {
				return checkExecErr(cliError(stderr, structured, errCodeInvalidInput,
					fmt.Sprintf("--sensor-overlay %q: %v", name, mergeErr)))
			}
			s = merged
		}
		res := gate.Result{
			Name:        name,
			Kind:        s.Source.Kind(),
			Category:    s.Category,
			Tolerance:   s.EffectiveTolerance(),
			ToolVersion: toolVersion(s.VersionCommand, cwd),
		}

		if len(wanted) > 0 && !wanted[name] {
			res.Status = gate.StatusSkipped
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
				res.Status = gate.StatusPass
				env.Summary.Passed++
				// Going fully green is the clearest signal the ratchet can be
				// tightened, so its recorded debt has to count as fixed here —
				// this path never reaches Compare.
				//
				// Unless a substitute command produced the green. `make fast`
				// passing says nothing about what `make check` records, and
				// telling the operator to lock that in would erase real debt
				// on the strength of a proxy.
				if !overlaid {
					totalFixed += base.RecordedCount(p.Name, name)
				}
				if updateBaseline {
					recording.Clear(p.Name, name)
				}
				break
			}

			raw := run.Output.Stdout + "\n" + run.Output.Stderr
			current := baseline.Fingerprints(raw, cwd)
			if updateBaseline {
				recording.Set(p.Name, name, baseline.Record(gate.StatusFail, raw, cwd))
			}

			// A count-ratchet sensor is measured on how many findings it
			// emits, not which ones. Fingerprints normalise line numbers and
			// deduplicate, so a second `//nolint` beside an existing one
			// changes neither the fingerprint set nor the distinct-line count.
			// For a sensor whose quantity *is* the finding, that would forgive
			// exactly the thing it exists to catch.
			var cmp baseline.Comparison
			if s.EffectiveRatchet() == "count" {
				cmp = base.CompareTotals(p.Name, name, baseline.CountLines(raw))
				res.CountDelta = cmp.CountDelta
			} else {
				cmp = base.Compare(p.Name, name, current, len(current))
			}
			res.NewCount = len(cmp.New)
			res.KnownCount = cmp.Known
			totalKnown += cmp.Known
			// Forgiveness still applies under an overlay — a substitute
			// command is normally a subset of the declared one, and its
			// shared findings fingerprint identically. Absence of a finding
			// under a subset is not evidence it was fixed, so Fixed is not.
			if !overlaid {
				totalFixed += cmp.Fixed
			}

			// A sensor whose failures are all already recorded is debt, not a
			// regression. The test is that the baseline has an entry for it —
			// not that some fingerprint matched, because a sensor that fails
			// with no output produces none and could otherwise never be
			// baselined at all.
			forgiven := base.Has(p.Name, name) && len(cmp.New) == 0 && !cmp.Regressed
			if forgiven {
				res.Status = gate.StatusKnown
				env.Summary.Known++
				if cmp.Approximate {
					res.Note = "baseline is count-based for this sensor; comparison is approximate"
				}
				break
			}

			res.Status = gate.StatusFail
			env.Summary.Failed++
			if base != nil {
				res.NewOutput = selectLines(raw, cwd, cmp.New)
				if cmp.Approximate {
					res.Note = "baseline is count-based for this sensor; new-failure detection is approximate"
				}
			}
			if res.Tolerance == "blocking" {
				env.Summary.Blocking++
				env.Verdict = gate.VerdictBlocked
			}
		case "files":
			// What the artifact *says* is still not ynh's to judge — no
			// verdict is derivable from arbitrary JSON. But whether it is
			// entitled to be believed is decidable, and was previously
			// nobody's job: a sensor pointed at a missing file reported green.
			//
			// So content still only ever reports; freshness can gate.
			paths := make([]string, 0, len(run.Output.Files))
			for _, f := range run.Output.Files {
				paths = append(paths, f.Path)
			}
			fr := freshness.Evaluate(cwd, paths, s.Observes)
			res.Freshness = string(fr.State)
			res.FreshnessBasis = string(fr.Basis)

			if fr.State == freshness.StateFresh {
				res.Status = gate.StatusForKind("files", 0)
				env.Summary.Reported++
				break
			}

			res.Status = gate.StatusFail
			res.Note = fr.Reason
			env.Summary.Failed++
			if res.Tolerance == "blocking" {
				env.Summary.Blocking++
				env.Verdict = gate.VerdictBlocked
			}
		case "focus":
			// Resolving a focus needs an agent runtime ynh does not own.
			res.Status = gate.StatusForKind("focus", 0)
			res.Note = "focus sensors require a loop driver; ynh resolves the declaration only"
			env.Summary.Deferred++
		}
		env.Sensors = append(env.Sensors, res)
	}
	env.Summary.Total = len(env.Sensors)
	if base != nil {
		env.Baseline = &gate.BaselineInfo{
			RecordedAt: base.OldestRecordedAt(),
			Known:      totalKnown,
			Fixed:      totalFixed,
			Stale:      totalFixed > 0,
		}
	}

	if updateBaseline {
		if err := baseline.Save(cwd, recording); err != nil {
			return checkExecErr(cliError(stderr, structured, errCodeIOError, err.Error()))
		}
		// Recording is an explicit act of accepting current state, so it
		// reports what it accepted rather than gating on it. Both output modes
		// must say something: a structured consumer that gets empty stdout
		// cannot tell success from a crash.
		env.Verdict = gate.VerdictPass
		env.Baseline = &gate.BaselineInfo{RecordedAt: recording.OldestRecordedAt()}
		if structured {
			data, encErr := json.MarshalIndent(env, "", "  ")
			if encErr != nil {
				return fmt.Errorf("encoding check: %w", encErr)
			}
			_, wErr := fmt.Fprintln(stdout, string(data))
			return wErr
		}
		if _, wErr := fmt.Fprintf(stdout, "\nbaseline recorded under %s — commit it\n",
			filepath.Join(baseline.Dir, baseline.SubDir)); wErr != nil {
			return wErr
		}
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

	if env.Verdict == gate.VerdictBlocked {
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

// recordBaselineWriteAttempt appends the refusal to the agent's session
// directory.
//
// Blocking the write is the safety property; recording it is the useful one.
// An agent reaching for --update-baseline when it cannot converge is a direct
// measurement of how often the loop tries to buy its way past the gate rather
// than satisfy it — which is the number that sizes how much containment the
// rest of the system needs. Best effort: failing the check because a
// measurement could not be written would be the wrong trade.
func recordBaselineWriteAttempt(session string) {
	dir := os.Getenv("YNH_AGENT_SESSION_DIR")
	if dir == "" {
		return
	}
	line, err := json.Marshal(map[string]string{
		"at":      time.Now().UTC().Format(time.RFC3339),
		"session": session,
		"attempt": "check --update-baseline",
	})
	if err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "gate-write-attempts.jsonl"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = f.Write(append(line, '\n'))
}

const maxSensorOutput = 4096

func truncateOutput(s string) string {
	if len(s) <= maxSensorOutput {
		return s
	}
	return s[:maxSensorOutput] + "\n… truncated"
}

func writeCheckText(w io.Writer, env gate.Envelope) error {
	if len(env.Sensors) == 0 {
		_, err := fmt.Fprintf(w, "%s: no sensors declared\n", env.Harness)
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, r := range env.Sensors {
		// The inner loop runs filtered on every turn, so a skipped sensor
		// must cost nothing to read. It stays in the JSON payload, where a
		// consumer may want the full declared set.
		if r.Status == gate.StatusSkipped {
			continue
		}
		mark := "·"
		switch r.Status {
		case gate.StatusPass:
			mark = "✓"
		case gate.StatusFail:
			mark = "✗"
		case gate.StatusKnown:
			mark = "~"
		}
		detail := r.Status
		switch {
		case r.Status == gate.StatusKnown:
			detail = fmt.Sprintf("known (%d)", r.KnownCount)
		case r.Status == gate.StatusFail && r.NewCount > 0 && r.KnownCount > 0:
			detail = fmt.Sprintf("%d new, %d known", r.NewCount, r.KnownCount)
		case r.Status == gate.StatusFail && r.NewCount > 0:
			detail = fmt.Sprintf("%d new", r.NewCount)
		case r.Status == gate.StatusFail && r.Freshness != "":
			// A files sensor never fails on content, so "fail" alone tells the
			// operator nothing about what to do. The state is the finding.
			detail = r.Freshness
		}
		if r.Status == gate.StatusFail && r.Tolerance != "blocking" {
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
		if r.Status != gate.StatusFail {
			continue
		}
		// With a baseline in play, show only what this change introduced.
		body := strings.TrimSpace(r.NewOutput)
		if body == "" {
			body = strings.TrimSpace(r.Stdout + "\n" + r.Stderr)
		}
		if body == "" {
			// A files sensor failing on freshness produces no output at all —
			// the note *is* the finding. Skipping it here would fail the gate
			// without saying why, which is how gates get switched off.
			if r.Note != "" {
				if _, err := fmt.Fprintf(w, "\n%s:\n%s\n", r.Name, r.Note); err != nil {
					return err
				}
			}
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

	if env.Verdict == gate.VerdictBlocked {
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
func writeBaselineFooter(w io.Writer, env gate.Envelope) error {
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
