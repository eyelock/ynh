//go:build e2e

package e2e

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestErrorEnvelope_JSON locks the structured-error contract documented in
// docs/cli-structured.md: when --format=json is passed and the command
// fails, stderr carries a single JSON object {error:{code,message}} and
// stdout is empty. Consumers (CI tooling, IDE plugins) parse this shape.
//
// Pinning ynh info on a missing harness as the canary — exercises the
// not_found code path through cliError.
func TestErrorEnvelope_JSON(t *testing.T) {
	s := newSandbox(t)

	stdout, stderr, err := s.runYnh(t, "info", "does-not-exist", "--format", "json")
	if err == nil {
		t.Fatalf("expected info on missing harness to fail, got success\nstdout:\n%s", stdout)
	}
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("structured error should not write to stdout, got:\n%s", stdout)
	}

	// stderr may contain trailing text from main.go but the envelope must be
	// parseable from one of the lines.
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	var found bool
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		if jerr := json.Unmarshal([]byte(line), &env); jerr == nil && env.Error.Code != "" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("could not find error envelope in stderr:\n%s", stderr)
	}
	if env.Error.Code == "" {
		t.Errorf("error.code is empty")
	}
	if env.Error.Message == "" {
		t.Errorf("error.message is empty")
	}
}
