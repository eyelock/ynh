package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/gate"
)

const baselineReadHarness = `{
  "name": "blr", "version": "0.1.0", "default_vendor": "claude",
  "sensors": {
    "lint":  {"tolerance": "blocking",
              "source": {"command": "printf 'a.go:1: unused x\\nb.go:3: shadowed\\n'; exit 1"},
              "output": {"format": "text"}},
    "clean": {"tolerance": "blocking", "source": {"command": "exit 0"}, "output": {"format": "text"}}
  }
}`

func baselineFixture(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	t.Setenv("CI", "")
	t.Setenv("YNH_AGENT_SESSION", "")
	installListTestHarness(t, home, "blr", baselineReadHarness)
	work := t.TempDir()
	if err := cmdCheck([]string{"local/blr", "--cwd", work, "--update-baseline"}, io.Discard, io.Discard); err != nil {
		t.Fatalf("recording the baseline: %v", err)
	}
	return work
}

func readBaseline(t *testing.T, work string, extra ...string) gate.BaselineReport {
	t.Helper()
	var out bytes.Buffer
	args := append([]string{"local/blr", "--cwd", work, "--format", "json"}, extra...)
	if err := cmdBaseline(args, &out, io.Discard); err != nil {
		t.Fatalf("cmdBaseline: %v", err)
	}
	var env gate.BaselineReport
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("decoding: %v\n%s", err, out.String())
	}
	return env
}

// The ratchet had no read surface at all: learning what it forgave meant
// reading the JSON by hand.
func TestBaseline_ReportsRecordedDebt(t *testing.T) {
	env := readBaseline(t, baselineFixture(t))
	if env.Summary.Total != 2 || env.Summary.Recorded != 1 || env.Summary.Unrecorded != 1 {
		t.Errorf("summary = %+v", env.Summary)
	}
	if env.Summary.Forgiven != 2 {
		t.Errorf("forgiven = %d, want 2", env.Summary.Forgiven)
	}
	byName := map[string]gate.BaselineSensor{}
	for _, s := range env.Sensors {
		byName[s.Name] = s
	}
	// A sensor absent from the baseline was passing when it was taken. That is
	// different from one recording zero, and collapsing them would hide it.
	if byName["clean"].Recorded {
		t.Error("a passing sensor forgives nothing and must not read as recorded")
	}
	if !byName["lint"].Recorded || byName["lint"].RecordedAt == "" {
		t.Errorf("lint = %+v, want recorded with a timestamp", byName["lint"])
	}
}

// The point of the command: a fingerprint cannot be reversed, but current
// output can be hashed and matched — which turns twelve hex characters into
// the finding it forgives.
func TestBaseline_ExplainResolvesFingerprintsToFindings(t *testing.T) {
	work := baselineFixture(t)

	plain := readBaseline(t, work)
	for _, s := range plain.Sensors {
		if len(s.Findings) != 0 {
			t.Errorf("%s: findings present without --explain — that costs a sensor run", s.Name)
		}
	}

	explained := readBaseline(t, work, "--explain")
	var lint gate.BaselineSensor
	for _, s := range explained.Sensors {
		if s.Name == "lint" {
			lint = s
		}
	}
	if len(lint.Findings) != 2 {
		t.Fatalf("findings = %v, want the 2 lines the hashes forgive", lint.Findings)
	}
	joined := strings.Join(lint.Findings, "\n")
	for _, want := range []string{"unused x", "shadowed"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in %v", want, lint.Findings)
		}
	}
}

func TestBaseline_RequiresAHarness(t *testing.T) {
	if err := cmdBaseline([]string{"--format", "json"}, io.Discard, io.Discard); err == nil {
		t.Error("a harness name is required")
	}
}
