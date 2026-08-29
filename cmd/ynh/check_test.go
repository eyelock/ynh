package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/gate"
)

const checkHarnessJSON = `{
  "name": "ch",
  "version": "0.1.0",
  "default_vendor": "claude",
  "focuses": {
    "audit": {"prompt": "audit the diff"}
  },
  "sensors": {
    "green": {
      "category": "maintainability",
      "source": {"command": "exit 0"},
      "output": {"format": "text"}
    },
    "red": {
      "category": "behaviour",
      "source": {"command": "echo boom >&2; exit 3"},
      "output": {"format": "text"}
    },
    "soft": {
      "tolerance": "advisory",
      "source": {"command": "exit 1"},
      "output": {"format": "text"}
    },
    "files": {
      "source": {"files": ["nope/**/*.xml"]},
      "output": {"format": "junit-xml"}
    },
    "judged": {
      "source": {"focus": "audit"},
      "output": {"format": "markdown"}
    }
  }
}`

func runCheck(t *testing.T, args ...string) (gate.Envelope, string, error) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	installListTestHarness(t, home, "ch", checkHarnessJSON)

	// Always run against a scratch directory. Without --cwd, cmdCheck falls
	// back to os.Getwd() — the package directory — so the suite would read
	// the repository's own .ynh/baseline.json, and one --update-baseline
	// would leave a file behind that silently changed every later result.
	// It did: a stray entry turned an advisory failure into "known" and the
	// suite passed or failed depending on what had run before.
	if !slices.Contains(args, "--cwd") {
		args = append(args, "--cwd", t.TempDir())
	}

	var stdout bytes.Buffer
	err := cmdCheck(append([]string{"local/ch"}, args...), &stdout, io.Discard)

	var env gate.Envelope
	// An execution error reports on stderr and writes no payload, so only
	// decode when there is one.
	if strings.Contains(strings.Join(args, " "), "json") && stdout.Len() > 0 {
		if uErr := json.Unmarshal(stdout.Bytes(), &env); uErr != nil {
			t.Fatalf("decoding check output: %v\n%s", uErr, stdout.String())
		}
	}
	return env, stdout.String(), err
}

func TestCheck_BlocksOnFailingBlockingSensor(t *testing.T) {
	env, _, err := runCheck(t, "--format", "json")
	if !errors.Is(err, errCheckBlocked) {
		t.Fatalf("want errCheckBlocked, got %v", err)
	}
	if env.Verdict != "blocked" {
		t.Fatalf("verdict = %q, want blocked", env.Verdict)
	}
	if env.Summary.Blocking != 1 {
		t.Fatalf("blocking = %d, want 1 (only \"red\" blocks)", env.Summary.Blocking)
	}
}

func TestCheck_AdvisoryFailureDoesNotBlock(t *testing.T) {
	env, _, _ := runCheck(t, "--format", "json", "--only", "soft")
	if env.Verdict != "pass" {
		t.Fatalf("verdict = %q, want pass — advisory failures must not gate", env.Verdict)
	}
	if env.Summary.Failed != 1 {
		t.Fatalf("failed = %d, want 1", env.Summary.Failed)
	}
}

func TestCheck_PassesWhenOnlyGreen(t *testing.T) {
	env, _, err := runCheck(t, "--format", "json", "--only", "green")
	if err != nil {
		t.Fatalf("cmdCheck: %v", err)
	}
	if env.Verdict != "pass" || env.Summary.Passed != 1 {
		t.Fatalf("verdict=%q passed=%d, want pass/1", env.Verdict, env.Summary.Passed)
	}
}

// Only command sensors can produce a verdict. Files and focus sensors must
// report honestly rather than guessing a pass, and must never gate.
func TestCheck_NonCommandSensorsNeverGate(t *testing.T) {
	env, _, err := runCheck(t, "--format", "json", "--only", "files,judged")
	if err != nil {
		t.Fatalf("cmdCheck: %v", err)
	}
	if env.Verdict != "pass" {
		t.Fatalf("verdict = %q, want pass", env.Verdict)
	}
	byName := map[string]gate.Result{}
	for _, r := range env.Sensors {
		byName[r.Name] = r
	}
	if got := byName["files"].Status; got != gate.StatusReported {
		t.Errorf("files status = %q, want %q", got, gate.StatusReported)
	}
	if got := byName["judged"].Status; got != gate.StatusDeferred {
		t.Errorf("judged status = %q, want %q", got, gate.StatusDeferred)
	}
}

func TestCheck_OnlyFiltersAndMarksSkipped(t *testing.T) {
	env, _, _ := runCheck(t, "--format", "json", "--only", "green")
	if env.Summary.Skipped != 4 {
		t.Fatalf("skipped = %d, want 4", env.Summary.Skipped)
	}
}

func TestCheck_UnknownSensorIsExecError(t *testing.T) {
	_, _, err := runCheck(t, "--format", "json", "--only", "nosuch")
	if !errors.Is(err, errCheckExec) {
		t.Fatalf("want errCheckExec (exit 2), got %v", err)
	}
	if errors.Is(err, errCheckBlocked) {
		t.Fatal("a missing sensor must not read as a blocked gate")
	}
}

// Failing output is the remediation the agent acts on, so it has to reach
// the operator verbatim rather than being summarised away.
func TestCheck_TextIncludesFailureOutput(t *testing.T) {
	_, out, err := runCheck(t)
	if !errors.Is(err, errCheckBlocked) {
		t.Fatalf("want errCheckBlocked, got %v", err)
	}
	if !strings.Contains(out, "boom") {
		t.Errorf("failing sensor output missing from report:\n%s", out)
	}
	if !strings.Contains(out, "blocked:") {
		t.Errorf("verdict line missing:\n%s", out)
	}
}

func TestCheck_RequiresHarnessName(t *testing.T) {
	err := cmdCheck(nil, io.Discard, io.Discard)
	if !errors.Is(err, errCheckExec) {
		t.Fatalf("want errCheckExec, got %v", err)
	}
}

// --- baseline ---

const baselineHarnessJSON = `{
  "name": "bl",
  "version": "0.1.0",
  "default_vendor": "claude",
  "sensors": {
    "lint": {
      "tolerance": "blocking",
      "source": {"command": "cat issues.txt >&2; exit 1"},
      "output": {"format": "text"}
    },
    "quiet": {
      "tolerance": "blocking",
      "source": {"command": "exit 1"},
      "output": {"format": "text"}
    }
  }
}`

// runBaselineCheck runs cmdCheck with cwd pointed at a scratch work dir whose
// issues.txt is the sensor's output, so a test can move the failure surface
// around the way a real edit would.
func runBaselineCheck(t *testing.T, home, work string, args ...string) (gate.Envelope, string, error) {
	t.Helper()
	t.Setenv("YNH_HOME", home)
	// Neutralise the guards that read the ambient environment, so a test
	// asserts the behaviour it names rather than the behaviour of the machine
	// it runs on. Without this every --update-baseline test passes locally and
	// fails in CI — where CI is set and the guard correctly refuses — which is
	// the one place nobody was looking until stacked PRs started running CI.
	//
	// A test whose subject *is* the ambient environment uses
	// runBaselineCheckInEnv instead: this helper runs after the caller's own
	// t.Setenv and would undo it.
	t.Setenv("CI", "")
	t.Setenv("YNH_AGENT_SESSION", "")
	return runBaselineCheckInEnv(t, home, work, args...)
}

// runBaselineCheckInEnv is runBaselineCheck without the environment
// neutralisation, for the guard tests that need the ambient environment they
// set to survive.
func runBaselineCheckInEnv(t *testing.T, home, work string, args ...string) (checkEnvelope, string, error) {
	t.Helper()
	t.Setenv("YNH_HOME", home)
	var stdout bytes.Buffer
	full := append([]string{"local/bl", "--cwd", work, "--format", "json"}, args...)
	err := cmdCheck(full, &stdout, io.Discard)
	var env gate.Envelope
	if stdout.Len() > 0 {
		if uErr := json.Unmarshal(stdout.Bytes(), &env); uErr != nil {
			t.Fatalf("decoding: %v\n%s", uErr, stdout.String())
		}
	}
	return env, stdout.String(), err
}

// sensorNamed finds a result by name. Indexing is fragile: results are sorted
// and --only leaves filtered sensors in the payload as skipped.
func sensorNamed(t *testing.T, env gate.Envelope, name string) gate.Result {
	t.Helper()
	for _, r := range env.Sensors {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("sensor %q not in result", name)
	return gate.Result{}
}

func setupBaselineRepo(t *testing.T, issues string) (home, work string) {
	t.Helper()
	home = t.TempDir()
	work = t.TempDir()
	t.Setenv("YNH_HOME", home)
	installListTestHarness(t, home, "bl", baselineHarnessJSON)
	writeIssues(t, work, issues)
	return home, work
}

func writeIssues(t *testing.T, work, issues string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(work, "issues.txt"), []byte(issues), 0o644); err != nil {
		t.Fatal(err)
	}
}

const preExisting = `src/legacy.go:12:5: exported func Old should have comment
src/util.go:8:2: unused variable tmp
`

// The trap this feature exists to remove: without a baseline, inheriting a
// repo that already fails means turn one is unwinnable.
func TestCheck_WithoutBaselineDirtyRepoBlocks(t *testing.T) {
	home, work := setupBaselineRepo(t, preExisting)
	env, _, err := runBaselineCheck(t, home, work)
	if !errors.Is(err, errCheckBlocked) {
		t.Fatalf("want blocked, got %v", err)
	}
	if got := sensorNamed(t, env, "lint").NewCount; got != 2 {
		t.Errorf("lint new_count = %d, want 2", got)
	}
}

func TestCheck_BaselineForgivesPreExistingFailures(t *testing.T) {
	home, work := setupBaselineRepo(t, preExisting)
	if _, _, err := runBaselineCheck(t, home, work, "--update-baseline"); err != nil {
		t.Fatalf("--update-baseline: %v", err)
	}
	env, _, err := runBaselineCheck(t, home, work)
	if err != nil {
		t.Fatalf("want pass after baseline, got %v", err)
	}
	if env.Verdict != "pass" {
		t.Errorf("verdict = %q, want pass", env.Verdict)
	}
	if got := sensorNamed(t, env, "lint").Status; got != gate.StatusKnown {
		t.Errorf("lint status = %q, want known", got)
	}
	if env.Summary.Known != 2 {
		t.Errorf("known = %d, want 2 (lint and quiet both baselined)", env.Summary.Known)
	}
}

// The load-bearing case: a new issue must still block, and the report must
// show only it — not the debt the author did not introduce.
func TestCheck_NewFailureBlocksAndIsolated(t *testing.T) {
	home, work := setupBaselineRepo(t, preExisting)
	if _, _, err := runBaselineCheck(t, home, work, "--update-baseline"); err != nil {
		t.Fatal(err)
	}
	writeIssues(t, work, preExisting+"src/feature.go:3:1: exported func New should have comment\n")

	env, _, err := runBaselineCheck(t, home, work)
	if !errors.Is(err, errCheckBlocked) {
		t.Fatalf("a new failure must block, got %v", err)
	}
	r := sensorNamed(t, env, "lint")
	if r.NewCount != 1 || r.KnownCount != 2 {
		t.Errorf("new=%d known=%d, want 1/2", r.NewCount, r.KnownCount)
	}
	if !strings.Contains(r.NewOutput, "feature.go") {
		t.Errorf("new output missing the new issue: %q", r.NewOutput)
	}
	if strings.Contains(r.NewOutput, "legacy.go") {
		t.Errorf("new output leaked pre-existing debt, which is what makes a gate ignorable: %q", r.NewOutput)
	}
}

// Without position normalisation this is the bug that would make baselines
// useless in practice: inserting lines above an issue would report the whole
// file as new on every edit.
func TestCheck_LineNumberDriftIsNotANewFailure(t *testing.T) {
	home, work := setupBaselineRepo(t, preExisting)
	if _, _, err := runBaselineCheck(t, home, work, "--update-baseline"); err != nil {
		t.Fatal(err)
	}
	writeIssues(t, work, `src/legacy.go:112:5: exported func Old should have comment
src/util.go:88:2: unused variable tmp
`)
	env, _, err := runBaselineCheck(t, home, work)
	if err != nil {
		t.Fatalf("moved line numbers must not block, got %v", err)
	}
	if got := sensorNamed(t, env, "lint").Status; got != gate.StatusKnown {
		t.Errorf("lint status = %q, want known", got)
	}
}

func TestCheck_ReportsFixedDebtSoRatchetCanTighten(t *testing.T) {
	home, work := setupBaselineRepo(t, preExisting)
	if _, _, err := runBaselineCheck(t, home, work, "--update-baseline"); err != nil {
		t.Fatal(err)
	}
	writeIssues(t, work, "src/legacy.go:12:5: exported func Old should have comment\n")

	env, _, err := runBaselineCheck(t, home, work)
	if err != nil {
		t.Fatalf("want pass, got %v", err)
	}
	if env.Baseline == nil || env.Baseline.Fixed != 1 || !env.Baseline.Stale {
		t.Errorf("baseline info = %+v, want fixed=1 stale=true", env.Baseline)
	}
}

// A gate that rewrites its own reference point from a feature branch forgives
// whatever that branch introduced.
func TestCheck_UpdateBaselineRefusedInCI(t *testing.T) {
	home, work := setupBaselineRepo(t, preExisting)
	t.Setenv("YNH_AGENT_SESSION", "")
	t.Setenv("CI", "true")
	_, _, err := runBaselineCheckInEnv(t, home, work, "--update-baseline")
	if !errors.Is(err, errCheckExec) {
		t.Fatalf("want errCheckExec, got %v", err)
	}
}

func TestCheck_NoBaselineFlagShowsUnratchetedTruth(t *testing.T) {
	home, work := setupBaselineRepo(t, preExisting)
	if _, _, err := runBaselineCheck(t, home, work, "--update-baseline"); err != nil {
		t.Fatal(err)
	}
	env, _, err := runBaselineCheck(t, home, work, "--no-baseline")
	if !errors.Is(err, errCheckBlocked) {
		t.Fatalf("--no-baseline must ignore the ratchet, got %v", err)
	}
	if env.Baseline != nil {
		t.Errorf("baseline info should be absent with --no-baseline, got %+v", env.Baseline)
	}
}

// --- regressions ---

// --update-baseline used to write a freshly built map, so any sensor filtered
// out by --only lost its recorded debt. Silent, and unrecoverable without git.
func TestCheck_UpdateBaselineWithOnlyPreservesOtherSensors(t *testing.T) {
	home, work := setupBaselineRepo(t, preExisting)
	if _, _, err := runBaselineCheck(t, home, work, "--update-baseline"); err != nil {
		t.Fatal(err)
	}

	// Re-record only a sensor that is not "lint".
	if _, _, err := runBaselineCheck(t, home, work, "--only", "quiet", "--update-baseline"); err != nil {
		t.Fatalf("scoped update: %v", err)
	}

	// lint's forgiveness must survive.
	env, _, err := runBaselineCheck(t, home, work, "--only", "lint")
	if err != nil {
		t.Fatalf("lint should still be forgiven after a scoped update, got %v", err)
	}
	if got := sensorNamed(t, env, "lint").Status; got != gate.StatusKnown {
		t.Errorf("lint status = %q, want known — its baseline entry was erased", got)
	}
}

// A sensor that fails with no output produces no fingerprints, so forgiveness
// keyed on a non-zero known count could never apply to it.
func TestCheck_SilentFailureCanBeBaselined(t *testing.T) {
	home, work := setupBaselineRepo(t, preExisting)
	if _, _, err := runBaselineCheck(t, home, work, "--update-baseline"); err != nil {
		t.Fatal(err)
	}
	env, _, err := runBaselineCheck(t, home, work, "--only", "quiet")
	if err != nil {
		t.Fatalf("a baselined silent failure must not gate, got %v", err)
	}
	if got := sensorNamed(t, env, "quiet").Status; got != gate.StatusKnown {
		t.Errorf("quiet status = %q, want known", got)
	}
	if env.Baseline != nil && env.Baseline.Stale {
		t.Error("nothing changed, so the baseline must not report fixed debt")
	}
}

// A sensor going fully green is the clearest signal the ratchet can tighten,
// but that path returns before Compare, so its debt was never counted.
func TestCheck_GreenSensorReportsClearedDebt(t *testing.T) {
	home, work := setupBaselineRepo(t, preExisting)
	if _, _, err := runBaselineCheck(t, home, work, "--update-baseline"); err != nil {
		t.Fatal(err)
	}
	writeIssues(t, work, "") // lint now passes entirely

	env, _, err := runBaselineCheck(t, home, work, "--only", "lint")
	if err != nil {
		t.Fatalf("want pass, got %v", err)
	}
	if env.Baseline == nil || !env.Baseline.Stale || env.Baseline.Fixed != 2 {
		t.Errorf("baseline = %+v, want fixed=2 stale=true", env.Baseline)
	}
}

// An empty stdout leaves a structured consumer unable to tell success from a
// crash.
func TestCheck_UpdateBaselineEmitsJSON(t *testing.T) {
	home, work := setupBaselineRepo(t, preExisting)
	_, out, err := runBaselineCheck(t, home, work, "--update-baseline")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("--update-baseline --format json wrote nothing to stdout")
	}
	var env gate.Envelope
	if uErr := json.Unmarshal([]byte(out), &env); uErr != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", uErr, out)
	}
	if env.Verdict != "pass" {
		t.Errorf("verdict = %q, want pass", env.Verdict)
	}
}

// The CI guard alone protected the wrong process: an agent runs in a worktree
// where CI is unset, so an agent that could not converge could grant itself
// blanket amnesty and then converge.
func TestCheck_UpdateBaselineRefusedInsideAgentSession(t *testing.T) {
	home, work := setupBaselineRepo(t, preExisting)
	t.Setenv("CI", "")
	t.Setenv("YNH_AGENT_SESSION", "sess-123")
	_, _, err := runBaselineCheckInEnv(t, home, work, "--update-baseline")
	if !errors.Is(err, errCheckExec) {
		t.Fatalf("want refusal, got %v", err)
	}
}

// Blocking is the safety property; recording is the measurement. An agent
// reaching for --update-baseline when it cannot converge is a direct signal of
// how often the loop tries to buy past the gate rather than satisfy it.
func TestCheck_BaselineWriteAttemptIsRecorded(t *testing.T) {
	home, work := setupBaselineRepo(t, preExisting)
	sessionDir := t.TempDir()
	t.Setenv("CI", "")
	t.Setenv("YNH_AGENT_SESSION", "sess-abc")
	t.Setenv("YNH_AGENT_SESSION_DIR", sessionDir)

	if _, _, err := runBaselineCheckInEnv(t, home, work, "--update-baseline"); err == nil {
		t.Fatal("expected the write to be refused")
	}

	data, err := os.ReadFile(filepath.Join(sessionDir, "gate-write-attempts.jsonl"))
	if err != nil {
		t.Fatalf("attempt not recorded: %v", err)
	}
	if !strings.Contains(string(data), "sess-abc") {
		t.Errorf("record does not identify the session: %s", data)
	}
}

// A refusal must not depend on a writable session directory — failing the
// check because a measurement could not be written is the wrong trade.
func TestCheck_RefusalWorksWithoutASessionDir(t *testing.T) {
	home, work := setupBaselineRepo(t, preExisting)
	t.Setenv("CI", "")
	t.Setenv("YNH_AGENT_SESSION", "sess-nodir")
	if _, _, err := runBaselineCheckInEnv(t, home, work, "--update-baseline"); err == nil {
		t.Fatal("expected refusal even with no session dir set")
	}
}

// A human on their own machine is the intended caller and must not be blocked.
func TestCheck_UpdateBaselineAllowedForAHuman(t *testing.T) {
	home, work := setupBaselineRepo(t, preExisting)
	t.Setenv("YNH_AGENT_SESSION", "")
	t.Setenv("CI", "")
	if _, _, err := runBaselineCheck(t, home, work, "--update-baseline"); err != nil {
		t.Fatalf("a human recording a baseline must be allowed: %v", err)
	}
}

// The agent loop substitutes a faster command for the same declared sensor on
// its inner turns. That substitution has to go through the gate, not around
// it — a loop that ran `ynh sensors run` directly to get an overlay would be
// applying its own policy again, which is the split B1 closed.
func TestCheck_SensorOverlayReplacesTheCommand(t *testing.T) {
	env, _, err := runCheck(t, "--only", "red", "--format", "json",
		"--sensor-overlay", `{"red":{"source":{"command":"exit 0"}}}`)
	if err != nil {
		t.Fatalf("overlay made red pass, so the gate should not block: %v", err)
	}
	if env.Verdict != gate.VerdictPass {
		t.Errorf("verdict = %q, want pass once the overlay replaced the failing command", env.Verdict)
	}
	for _, s := range env.Sensors {
		if s.Name == "red" && s.Status != gate.StatusPass {
			t.Errorf("red status = %q, want pass — the overlay did not take effect", s.Status)
		}
	}
}

// An overlay naming a sensor the harness does not declare is a typo. Ignoring
// it silently would leave the caller believing it had substituted a command
// while being gated on the original.
func TestCheck_SensorOverlayForUnknownSensorIsAnError(t *testing.T) {
	_, _, err := runCheck(t, "--format", "json",
		"--sensor-overlay", `{"rde":{"source":{"command":"exit 0"}}}`)
	if !errors.Is(err, errCheckExec) {
		t.Fatalf("want exec error for an overlay naming no declared sensor, got %v", err)
	}
}

func TestCheck_SensorOverlayRejectsInvalidJSON(t *testing.T) {
	_, _, err := runCheck(t, "--format", "json", "--sensor-overlay", `{`)
	if !errors.Is(err, errCheckExec) {
		t.Fatalf("want exec error for malformed overlay JSON, got %v", err)
	}
}

// A baseline records what a declared sensor produces. Writing one while a
// substitute command runs would file the proxy's output under the real
// sensor's name, and every later run would compare the real command against
// fingerprints it never produced.
func TestCheck_UpdateBaselineRejectsAnOverlay(t *testing.T) {
	_, _, err := runCheck(t, "--update-baseline",
		"--sensor-overlay", `{"red":{"source":{"command":"exit 0"}}}`)
	if !errors.Is(err, errCheckExec) {
		t.Fatalf("recording a baseline from a substitute command must be refused, got %v", err)
	}
}

// A substitute command passing says nothing about what the declared one
// records. Reporting its debt as fixed would invite the operator to narrow the
// ratchet — erasing real findings on the strength of a proxy.
func TestCheck_OverlayDoesNotClaimDebtIsFixed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	installListTestHarness(t, home, "ch", checkHarnessJSON)
	cwd := t.TempDir()

	var rec bytes.Buffer
	if err := cmdCheck([]string{"local/ch", "--only", "red", "--cwd", cwd, "--update-baseline"},
		&rec, io.Discard); err != nil {
		t.Fatalf("recording baseline: %v", err)
	}

	var out bytes.Buffer
	err := cmdCheck([]string{"local/ch", "--only", "red", "--cwd", cwd, "--format", "json",
		"--sensor-overlay", `{"red":{"source":{"command":"exit 0"}}}`}, &out, io.Discard)
	if err != nil {
		t.Fatalf("overlay made red pass: %v", err)
	}
	var env gate.Envelope
	if uErr := json.Unmarshal(out.Bytes(), &env); uErr != nil {
		t.Fatalf("decoding: %v", uErr)
	}
	if env.Baseline == nil {
		t.Fatal("a baseline was recorded, so the envelope should report one")
	}
	if env.Baseline.Stale || env.Baseline.Fixed != 0 {
		t.Errorf("a proxy command passing is not evidence the recorded debt is fixed: fixed=%d stale=%v",
			env.Baseline.Fixed, env.Baseline.Stale)
	}
}
