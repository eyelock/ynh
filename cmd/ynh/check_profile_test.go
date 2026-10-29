package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/gate"
)

const profileCheckHarnessJSON = `{
  "name": "pc",
  "version": "0.1.0",
  "default_vendor": "claude",
  "profiles": {
    "factory": {"sensor_overrides": {"red": {"tolerance": "advisory"}, "green": {"tolerance": "report"}}},
    "hardened": {"sensor_overrides": {"soft": {"tolerance": "blocking"}}},
    "ratchety": {"sensor_overrides": {"red": {"tolerance": "advisory", "ratchet": "count"}}},
    "ghostly":  {"sensor_overrides": {"ghost": {"tolerance": "advisory"}}},
    "verifying": {"sensor_overrides": {"verify": {"tolerance": "advisory"}}}
  },
  "sensors": {
    "green":  {"source": {"command": "exit 0"}, "output": {"format": "text"}},
    "red":    {"source": {"command": "echo boom >&2; exit 3"}, "output": {"format": "text"}},
    "soft":   {"tolerance": "advisory", "source": {"command": "exit 1"}, "output": {"format": "text"}},
    "verify": {"role": "convergence-verifier", "source": {"command": "exit 0"}, "output": {"format": "text"}}
  }
}`

// runProfileCheck installs the profile fixture and runs `ynh check` over it,
// returning stdout, stderr, the decoded envelope when stdout is JSON, and the
// error.
func runProfileCheck(t *testing.T, args ...string) (gate.Envelope, string, string, error) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	t.Setenv("CI", "")
	t.Setenv("YNH_AGENT_SESSION", "")
	t.Setenv("YNH_PROFILE", "")
	installListTestHarness(t, home, "pc", profileCheckHarnessJSON)
	return runProfileCheckIn(t, t.TempDir(), args...)
}

func runProfileCheckIn(t *testing.T, cwd string, args ...string) (gate.Envelope, string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := cmdCheck(append([]string{"local/pc", "--cwd", cwd}, args...), &stdout, &stderr)
	var env gate.Envelope
	if strings.Contains(strings.Join(args, " "), "json") && stdout.Len() > 0 {
		if uErr := json.Unmarshal(stdout.Bytes(), &env); uErr != nil {
			t.Fatalf("decoding: %v\n%s", uErr, stdout.String())
		}
	}
	return env, stdout.String(), stderr.String(), err
}

// Without the profile the failing blocking sensor gates; with it the sensor is
// advisory, the exit is clean, and both outputs say why.
func TestCheck_ProfileSoftensAFailingBlockingSensor(t *testing.T) {
	env, _, _, err := runProfileCheck(t, "--format", "json", "--only", "red")
	if !errors.Is(err, errCheckBlocked) {
		t.Fatalf("without a profile want errCheckBlocked, got %v", err)
	}
	if got := sensorByName(t, env, "red"); got.ToleranceSource != "" || env.Profile != "" {
		t.Errorf("declared tolerance should carry no source or profile; got %q, %q", got.ToleranceSource, env.Profile)
	}

	env, _, _, err = runProfileCheck(t, "--format", "json", "--only", "red", "--profile", "factory")
	if err != nil {
		t.Fatalf("with the profile want exit 0, got %v", err)
	}
	red := sensorByName(t, env, "red")
	if env.Verdict != "pass" || red.Status != gate.StatusFail || red.Tolerance != "advisory" || red.ToleranceSource != "profile" || env.Profile != "factory" {
		t.Errorf("verdict %q, red %+v, profile %q", env.Verdict, red, env.Profile)
	}

	_, text, _, err := runProfileCheck(t, "--only", "red,green", "--profile", "factory")
	if err != nil {
		t.Fatalf("text run: %v", err)
	}
	for _, want := range []string{"(advisory, profile factory)", "(report, profile factory)"} {
		if !strings.Contains(text, want) {
			t.Errorf("text output missing %q:\n%s", want, text)
		}
	}
}

// A profile can harden too, and an untouched sensor carries no mark.
func TestCheck_ProfileCanHardenAndLeavesOthersUnmarked(t *testing.T) {
	env, text, _, err := runProfileCheck(t, "--format", "text", "--only", "soft,green", "--profile", "hardened")
	if !errors.Is(err, errCheckBlocked) {
		t.Fatalf("want errCheckBlocked, got %v\n%s", err, text)
	}
	_ = env
	if !strings.Contains(text, "(blocking, profile hardened)") {
		t.Errorf("hardened sensor not marked:\n%s", text)
	}
	if strings.Contains(text, "green") && strings.Contains(strings.SplitN(strings.SplitAfter(text, "green")[1], "\n", 2)[0], "profile") {
		t.Errorf("an untouched sensor was marked:\n%s", text)
	}
}

func TestCheck_ProfileRefusals(t *testing.T) {
	cases := []struct {
		profile string
		want    []string
	}{
		{"nope", []string{`profile "nope" not defined`, "factory"}},
		{"ratchety", []string{`profile "ratchety"`, `"red"`, "ratchet", "baseline"}},
		{"ghostly", []string{`profile "ghostly"`, `"ghost"`}},
		{"verifying", []string{`profile "verifying"`, `"verify"`, "convergence verifier"}},
	}
	for _, c := range cases {
		t.Run(c.profile, func(t *testing.T) {
			_, _, stderr, err := runProfileCheck(t, "--profile", c.profile)
			if !errors.Is(err, errCheckExec) {
				t.Fatalf("want errCheckExec (exit 2), got %v", err)
			}
			for _, w := range c.want {
				if !strings.Contains(stderr+err.Error(), w) {
					t.Errorf("output %q missing %q", stderr+err.Error(), w)
				}
			}
		})
	}
}

// YNH_PROFILE does not soften `ynh check`: only the flag does.
func TestCheck_IgnoresYNHProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	t.Setenv("CI", "")
	t.Setenv("YNH_AGENT_SESSION", "")
	t.Setenv("YNH_PROFILE", "factory")
	installListTestHarness(t, home, "pc", profileCheckHarnessJSON)
	_, _, _, err := runProfileCheckIn(t, t.TempDir(), "--format", "json", "--only", "red")
	if !errors.Is(err, errCheckBlocked) {
		t.Fatalf("YNH_PROFILE must not soften the gate; got %v", err)
	}
}

func TestCheck_ProfileFlagNeedsAValue(t *testing.T) {
	var stderr bytes.Buffer
	err := cmdCheck([]string{"local/pc", "--profile"}, io.Discard, &stderr)
	if !errors.Is(err, errCheckExec) {
		t.Fatalf("want errCheckExec, got %v", err)
	}
}

// Recording a baseline never depends on tolerance, so --profile is allowed and
// records exactly what the plain run records.
func TestCheck_UpdateBaselineWithProfileRecordsTheSame(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	t.Setenv("CI", "")
	t.Setenv("YNH_AGENT_SESSION", "")
	installListTestHarness(t, home, "pc", profileCheckHarnessJSON)

	cwd := t.TempDir()
	if _, _, stderr, err := runProfileCheckIn(t, cwd, "--only", "red", "--profile", "factory", "--update-baseline"); err != nil {
		t.Fatalf("update-baseline under a profile: %v\n%s", err, stderr)
	}
	// The plain run, blocking again, finds the failure already recorded.
	env, _, _, err := runProfileCheckIn(t, cwd, "--format", "json", "--only", "red")
	if err != nil {
		t.Fatalf("plain run after recording under the profile: %v", err)
	}
	if red := sensorByName(t, env, "red"); red.Status != gate.StatusKnown || red.Tolerance != "blocking" {
		t.Errorf("red = %+v, want known and blocking", red)
	}
}
