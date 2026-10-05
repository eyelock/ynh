package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/clischema"
)

// formatErrorCase is one ynd command whose --format selects text or JSON
// output. export and marketplace also take --format, but there it names a
// package layout, not an output format, so they are not held to this.
type formatErrorCase struct {
	name   string
	run    func(args []string, stdout, stderr io.Writer) error
	prefix []string
}

// formatErrorCases lists every ynd command with a text|json --format. A
// command whose help documents one without a row here fails
// TestFormatErrorContractCoversEveryFormatCommand.
func formatErrorCases() []formatErrorCase {
	return []formatErrorCase{
		{name: "compose", run: cmdComposeTo, prefix: []string{"./nope"}},
		{name: "version", run: cmdVersionTo},
	}
}

// TestFormatErrorContract is ynd's half of the contract in
// docs/cli-structured.md; cmd/ynh has the same test for its commands. With
// --format json before or after the bad flag, one error envelope on stderr
// and nothing on stdout; in text mode, a plain error for main to print.
func TestFormatErrorContract(t *testing.T) {
	schema, err := clischema.Get("error")
	if err != nil {
		t.Fatalf("Get error schema: %v", err)
	}

	for _, tc := range formatErrorCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("YNH_HOME", t.TempDir())
			t.Chdir(t.TempDir())

			orders := map[string][]string{
				"json first": {"--format", "json", "--bogus"},
				"json last":  {"--bogus", "--format", "json"},
			}
			for order, tail := range orders {
				t.Run(order, func(t *testing.T) {
					var out, errb bytes.Buffer
					err := tc.run(append(append([]string{}, tc.prefix...), tail...), &out, &errb)
					if !errors.Is(err, errStructuredReported) {
						t.Fatalf("err = %v, want errStructuredReported\nstderr: %s", err, errb.String())
					}
					if out.Len() != 0 {
						t.Errorf("stdout must be empty, got: %s", out.String())
					}
					dec := json.NewDecoder(bytes.NewReader(errb.Bytes()))
					var env any
					if err := dec.Decode(&env); err != nil {
						t.Fatalf("stderr is not a JSON value: %v\nstderr: %s", err, errb.String())
					}
					if dec.More() {
						t.Fatalf("stderr holds more than one JSON value: %s", errb.String())
					}
					if err := schema.Validate(env); err != nil {
						t.Errorf("envelope does not validate against the error schema: %v", err)
					}
					var got struct {
						Error struct {
							Code    string `json:"code"`
							Message string `json:"message"`
						} `json:"error"`
					}
					if err := json.Unmarshal(errb.Bytes(), &got); err != nil {
						t.Fatalf("decode envelope: %v", err)
					}
					if got.Error.Code != errCodeInvalidInput || !strings.Contains(got.Error.Message, "--bogus") {
						t.Errorf("envelope = %+v, want invalid_input naming --bogus", got.Error)
					}
				})
			}

			t.Run("text", func(t *testing.T) {
				var out, errb bytes.Buffer
				err := tc.run(append(append([]string{}, tc.prefix...), "--bogus"), &out, &errb)
				if err == nil || errors.Is(err, errStructuredReported) {
					t.Fatalf("err = %v, want a plain error\nstderr: %s", err, errb.String())
				}
				if !strings.Contains(err.Error(), "unknown flag: --bogus") {
					t.Errorf("err = %q, want it to name the unknown flag", err)
				}
				if out.Len() != 0 || errb.Len() != 0 {
					t.Errorf("text mode wrote output itself; stdout=%q stderr=%q", out.String(), errb.String())
				}
			})
		})
	}
}

func TestFormatErrorContractCoversEveryFormatCommand(t *testing.T) {
	covered := map[string]bool{}
	for _, tc := range formatErrorCases() {
		covered[tc.name] = true
	}
	for _, topic := range helpTopics() {
		if strings.Contains(commandHelp[topic], "--format <text|json>") && !covered[topic] {
			t.Errorf("ynd %s documents --format <text|json> but has no row in formatErrorCases", topic)
		}
	}
}
