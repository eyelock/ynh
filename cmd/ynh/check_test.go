package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
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
