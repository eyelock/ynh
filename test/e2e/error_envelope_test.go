//go:build e2e

package e2e

import (
	"encoding/json"
	"errors"
	"os/exec"
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

// TestErrorEnvelope_ArgumentErrorsAtTheBinary checks, through the real
// binaries, the part of the contract unit tests cannot see: main exits
// non-zero and adds nothing after the envelope, so stderr is exactly one JSON
// object, and text mode keeps its plain "Error: ..." line. The rows are the
// commands #516 found off-contract; cmd/ynh and cmd/ynd each hold a unit test
// over every --format command.
func TestErrorEnvelope_ArgumentErrorsAtTheBinary(t *testing.T) {
	s := newSandbox(t)
	cases := []struct {
		bin  string
		args []string
		exit int
	}{
		{"ynh", []string{"check", "local/nope"}, 2},
		{"ynh", []string{"baseline", "local/nope"}, 1},
		{"ynh", []string{"backend", "list"}, 1},
		{"ynh", []string{"focus", "ls", "local/nope"}, 1},
		{"ynh", []string{"profile", "ls", "local/nope"}, 1},
		{"ynh", []string{"registry", "list"}, 1},
		{"ynh", []string{"search", "term"}, 1},
		{"ynh", []string{"version"}, 1},
		{"ynd", []string{"compose", "./nope"}, 1},
		{"ynd", []string{"version"}, 1},
	}
	run := func(t *testing.T, bin string, args []string) (string, string, int) {
		t.Helper()
		var stdout, stderr string
		var err error
		if bin == "ynh" {
			stdout, stderr, err = s.runYnh(t, args...)
		} else {
			stdout, stderr, err = runYnd(t, args...)
		}
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("%s %s: want a non-zero exit, got err=%v", bin, strings.Join(args, " "), err)
		}
		return stdout, stderr, exitErr.ExitCode()
	}

	for _, tc := range cases {
		name := tc.bin + " " + strings.Join(tc.args, " ")
		for _, tail := range [][]string{{"--format", "json", "--bogus"}, {"--bogus", "--format", "json"}} {
			t.Run(name+" "+strings.Join(tail, " "), func(t *testing.T) {
				stdout, stderr, code := run(t, tc.bin, append(append([]string{}, tc.args...), tail...))
				if code != tc.exit {
					t.Errorf("exit = %d, want %d", code, tc.exit)
				}
				if stdout != "" {
					t.Errorf("stdout must be empty, got:\n%s", stdout)
				}
				dec := json.NewDecoder(strings.NewReader(stderr))
				var env struct {
					Error struct {
						Code    string `json:"code"`
						Message string `json:"message"`
					} `json:"error"`
				}
				if err := dec.Decode(&env); err != nil {
					t.Fatalf("stderr is not a JSON envelope: %v\n%s", err, stderr)
				}
				if dec.More() {
					t.Errorf("stderr holds more than the envelope:\n%s", stderr)
				}
				if env.Error.Code != "invalid_input" || !strings.Contains(env.Error.Message, "--bogus") {
					t.Errorf("envelope = %+v, want invalid_input naming --bogus", env.Error)
				}
			})
		}
		t.Run(name+" --bogus", func(t *testing.T) {
			stdout, stderr, code := run(t, tc.bin, append(append([]string{}, tc.args...), "--bogus"))
			if code != tc.exit {
				t.Errorf("exit = %d, want %d", code, tc.exit)
			}
			if stdout != "" {
				t.Errorf("stdout must be empty, got:\n%s", stdout)
			}
			if !strings.HasPrefix(stderr, "Error: ") || !strings.Contains(stderr, "unknown flag: --bogus") {
				t.Errorf("text mode stderr = %q, want a plain Error: line", stderr)
			}
		})
	}
}
