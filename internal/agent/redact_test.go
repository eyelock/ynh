package agent

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestIsSecretName(t *testing.T) {
	secret := []string{
		"GITHUB_TOKEN", "github_token", "ANTHROPIC_API_KEY", "AWS_SECRET_ACCESS_KEY",
		"DB_PASSWORD", "CLAUDE_CODE_GH_PAT", "SSH_PRIVATE_KEY", "MY_CREDENTIAL",
		"AUTH_HEADER", "npm_apikey",
	}
	for _, n := range secret {
		if !IsSecretName(n) {
			t.Errorf("%q should be treated as a secret name", n)
		}
	}
	ordinary := []string{"PATH", "HOME", "LANG", "TERM", "YNH_HOME", "CI", "EDITOR"}
	for _, n := range ordinary {
		if IsSecretName(n) {
			t.Errorf("%q is not a credential and must not be redacted wholesale", n)
		}
	}
}

func TestRedactor_ReplacesKnownValuesAnywhere(t *testing.T) {
	r := NewRedactor([]string{
		"GITHUB_TOKEN=ghp_supersecretvalue123",
		"PATH=/usr/bin",
		"HOME=/Users/dev",
	})
	// The realistic leak: a sensor prints what it was given.
	in := `curl -H "Authorization: Bearer ghp_supersecretvalue123" failed with 401`
	got := r.Redact(in)
	if strings.Contains(got, "ghp_supersecretvalue123") {
		t.Errorf("the token survived redaction: %s", got)
	}
	if !strings.Contains(got, "[redacted:GITHUB_TOKEN]") {
		t.Errorf("the placeholder should name the variable, got: %s", got)
	}
	// Non-secret values must survive: redacting PATH would corrupt every line.
	if !strings.Contains(r.Redact("running in /usr/bin"), "/usr/bin") {
		t.Error("PATH is not a credential and must not be substituted")
	}
}

// A short value appears inside ordinary words and paths. Replacing every
// occurrence would corrupt the trajectory while protecting nothing.
func TestRedactor_IgnoresValuesTooShortToBeSecret(t *testing.T) {
	r := NewRedactor([]string{"API_KEY=abc"})
	if r.Len() != 0 {
		t.Errorf("a 3-character value should not be redacted, got %d entries", r.Len())
	}
	if got := r.Redact("abcdefg"); got != "abcdefg" {
		t.Errorf("short value corrupted unrelated text: %s", got)
	}
}

// Overlapping secrets: the longer match must win, or its tail is left behind.
func TestRedactor_LongestMatchWins(t *testing.T) {
	r := NewRedactor([]string{
		"SHORT_TOKEN=secretvalue",
		"LONG_TOKEN=secretvalue_with_more",
	})
	got := r.Redact("here is secretvalue_with_more end")
	if strings.Contains(got, "_with_more") {
		t.Errorf("the longer secret was only partly redacted: %s", got)
	}
}

// The trajectory is redacted after encoding, so a value containing a quote or
// backslash appears there escaped and would not match the raw form.
func TestRedactor_CoversTheJSONEscapedForm(t *testing.T) {
	const raw = `pa"ss\word12345`
	r := NewRedactor([]string{"DB_PASSWORD=" + raw})
	encoded, err := json.Marshal(map[string]string{"stdout": "login failed for " + raw})
	if err != nil {
		t.Fatal(err)
	}
	got := r.Redact(string(encoded))
	if strings.Contains(got, `pa\"ss\\word12345`) || strings.Contains(got, raw) {
		t.Errorf("the escaped form survived redaction: %s", got)
	}
}

// End to end: nothing a run was given may reach the trajectory file.
func TestTrajectory_RedactsSecretsItWasGiven(t *testing.T) {
	var buf bytes.Buffer
	tw := NewTrajectoryWriter(&buf)
	tw.SetRedactor(NewRedactor([]string{"ANTHROPIC_API_KEY=sk-ant-notarealkey0000"}))

	if err := tw.Emit(KindSensorResult, 1, SensorResultData{
		Name:    "smoke",
		Summary: "request failed: key sk-ant-notarealkey0000 rejected",
	}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "sk-ant-notarealkey0000") {
		t.Fatalf("a credential reached the trajectory: %s", out)
	}
	if !strings.Contains(out, "[redacted:ANTHROPIC_API_KEY]") {
		t.Errorf("expected a labelled placeholder, got: %s", out)
	}
	// The line must still be valid NDJSON — redaction cannot break consumers.
	var ev map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &ev); err != nil {
		t.Fatalf("redaction produced invalid JSON: %v\n%s", err, out)
	}
}

// A writer with no redactor must still emit valid, unmodified NDJSON.
func TestTrajectory_NoRedactorIsUnchanged(t *testing.T) {
	var buf bytes.Buffer
	tw := NewTrajectoryWriter(&buf)
	if err := tw.Emit(KindSensorRun, 1, "build"); err != nil {
		t.Fatal(err)
	}
	var ev map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &ev); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if ev["type"] != string(KindSensorRun) {
		t.Errorf("got %v", ev["type"])
	}
}
