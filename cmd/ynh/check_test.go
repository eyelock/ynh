package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func runCheck(t *testing.T, args ...string) (checkEnvelope, string, error) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	installListTestHarness(t, home, "ch", checkHarnessJSON)

	var stdout bytes.Buffer
	err := cmdCheck(append([]string{"local/ch"}, args...), &stdout, io.Discard)

	var env checkEnvelope
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
	byName := map[string]checkResult{}
	for _, r := range env.Sensors {
		byName[r.Name] = r
	}
	if got := byName["files"].Status; got != statusReported {
		t.Errorf("files status = %q, want %q", got, statusReported)
	}
	if got := byName["judged"].Status; got != statusDeferred {
		t.Errorf("judged status = %q, want %q", got, statusDeferred)
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
    }
  }
}`

// runBaselineCheck runs cmdCheck with cwd pointed at a scratch work dir whose
// issues.txt is the sensor's output, so a test can move the failure surface
// around the way a real edit would.
func runBaselineCheck(t *testing.T, home, work string, args ...string) (checkEnvelope, string, error) {
	t.Helper()
	t.Setenv("YNH_HOME", home)
	var stdout bytes.Buffer
	full := append([]string{"local/bl", "--cwd", work, "--format", "json"}, args...)
	err := cmdCheck(full, &stdout, io.Discard)
	var env checkEnvelope
	if stdout.Len() > 0 {
		if uErr := json.Unmarshal(stdout.Bytes(), &env); uErr != nil {
			t.Fatalf("decoding: %v\n%s", uErr, stdout.String())
		}
	}
	return env, stdout.String(), err
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
	if env.Sensors[0].NewCount != 2 {
		t.Errorf("new_count = %d, want 2", env.Sensors[0].NewCount)
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
	if env.Sensors[0].Status != statusKnown || env.Summary.Known != 1 {
		t.Errorf("status = %q known = %d, want known/1", env.Sensors[0].Status, env.Summary.Known)
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
	r := env.Sensors[0]
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
	if env.Sensors[0].Status != statusKnown {
		t.Errorf("status = %q, want known", env.Sensors[0].Status)
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
	t.Setenv("CI", "true")
	_, _, err := runBaselineCheck(t, home, work, "--update-baseline")
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
