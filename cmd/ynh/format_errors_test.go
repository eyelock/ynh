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

// formatErrorCase is one command that accepts --format, called the way main
// dispatches it, with the arguments that come before the representative bad
// flag (a subcommand, a harness name).
type formatErrorCase struct {
	// name is "<help topic>" or "<help topic> <subcommand>". The first word
	// is what TestFormatErrorContractCoversEveryFormatCommand matches against
	// commandHelp.
	name   string
	run    func(args []string, stdout, stderr io.Writer) error
	prefix []string
	// jsonOnly marks a command with no text mode: it emits JSON whatever the
	// flags say, so its errors are always the envelope.
	jsonOnly bool
}

// formatErrorCases lists every ynh command that accepts --format. Adding a
// command with --format to commandHelp without a row here fails
// TestFormatErrorContractCoversEveryFormatCommand.
func formatErrorCases() []formatErrorCase {
	return []formatErrorCase{
		{name: "ls", run: cmdListTo},
		{name: "info", run: cmdInfoTo, prefix: []string{"local/nope"}},
		{name: "installed", run: cmdInstalledTo, prefix: []string{"local/nope"}},
		{name: "schema", run: cmdSchemaTo, prefix: []string{"version"}},
		{name: "vendors", run: cmdVendorsTo},
		{name: "sources list", run: cmdSourcesTo, prefix: []string{"list"}},
		{name: "paths", run: cmdPathsTo},
		{name: "status", run: cmdStatusTo},
		{name: "search", run: cmdSearchTo, prefix: []string{"term"}},
		{name: "registry list", run: cmdRegistryTo, prefix: []string{"list"}},
		{name: "backend list", run: cmdBackendTo, prefix: []string{"list"}},
		{name: "fork", run: cmdForkTo, prefix: []string{"local/nope"}},
		{name: "focus ls", run: cmdFocusLs, prefix: []string{"local/nope"}},
		{name: "profile ls", run: cmdProfileLs, prefix: []string{"local/nope"}},
		{name: "doctor", run: cmdDoctorTo},
		{name: "sensors ls", run: cmdSensorsTo, prefix: []string{"ls", "local/nope"}},
		{name: "sensors show", run: cmdSensorsTo, prefix: []string{"show", "local/nope", "s"}},
		{name: "sensors run", run: cmdSensorsTo, prefix: []string{"run", "local/nope", "s"}, jsonOnly: true},
		{name: "check", run: cmdCheck, prefix: []string{"local/nope"}},
		{name: "baseline", run: cmdBaseline, prefix: []string{"local/nope"}},
		{
			name: "agent run",
			run: func(args []string, stdout, stderr io.Writer) error {
				return cmdAgentTo(args, stdout, stderr, strings.NewReader(""))
			},
			prefix: []string{"run", "--task", "t"},
		},
		{name: "migrate", run: cmdMigrateTo},
		{name: "quarantine list", run: cmdQuarantineTo, prefix: []string{"list"}},
		{name: "trust ls", run: cmdTrustTo, prefix: []string{"ls"}},
		{name: "trust show", run: cmdTrustTo, prefix: []string{"show", "local/nope"}},
		{name: "trust accept", run: cmdTrustTo, prefix: []string{"accept", "local/nope"}},
		{name: "version", run: cmdVersionTo},
	}
}

// TestFormatErrorContract pins the error contract in docs/cli-structured.md
// for every command that accepts --format, using an unknown flag as the
// representative argument error:
//
//   - with --format json, before or after the bad flag: one error envelope on
//     stderr validating against the error schema, code invalid_input, stdout
//     empty, and errStructuredReported so main prints nothing more and exits
//     non-zero;
//   - in text mode: a plain error for main to print as "Error: ...", nothing
//     on stdout or stderr from the command itself.
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
					assertOneErrorEnvelope(t, schema, errb.Bytes(), errCodeInvalidInput, "--bogus")
				})
			}

			t.Run("text", func(t *testing.T) {
				var out, errb bytes.Buffer
				err := tc.run(append(append([]string{}, tc.prefix...), "--bogus"), &out, &errb)
				if err == nil {
					t.Fatal("expected an error for --bogus")
				}
				if out.Len() != 0 {
					t.Errorf("stdout must be empty, got: %s", out.String())
				}
				if tc.jsonOnly {
					if !errors.Is(err, errStructuredReported) {
						t.Fatalf("err = %v, want errStructuredReported for a JSON-only command", err)
					}
					assertOneErrorEnvelope(t, schema, errb.Bytes(), errCodeInvalidInput, "--bogus")
					return
				}
				if errors.Is(err, errStructuredReported) {
					t.Fatalf("text mode emitted the JSON envelope: %s", errb.String())
				}
				if !strings.Contains(err.Error(), "unknown flag: --bogus") {
					t.Errorf("err = %q, want it to name the unknown flag", err)
				}
				if errb.Len() != 0 {
					t.Errorf("text mode wrote to stderr itself, want main to print the error: %s", errb.String())
				}
			})
		})
	}
}

func assertOneErrorEnvelope(t *testing.T, schema interface{ Validate(any) error }, raw []byte, code, inMessage string) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	var env any
	if err := dec.Decode(&env); err != nil {
		t.Fatalf("stderr is not a JSON value: %v\nstderr: %s", err, raw)
	}
	if dec.More() {
		t.Fatalf("stderr holds more than one JSON value: %s", raw)
	}
	if err := schema.Validate(env); err != nil {
		t.Errorf("envelope does not validate against the error schema: %v\nstderr: %s", err, raw)
	}
	var got struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if got.Error.Code != code {
		t.Errorf("code = %q, want %q", got.Error.Code, code)
	}
	if !strings.Contains(got.Error.Message, inMessage) {
		t.Errorf("message = %q, want it to mention %q", got.Error.Message, inMessage)
	}
}

// A command whose help documents --format must have a row in
// formatErrorCases, so a new structured command is held to the contract from
// its first commit.
func TestFormatErrorContractCoversEveryFormatCommand(t *testing.T) {
	covered := map[string]bool{}
	for _, tc := range formatErrorCases() {
		covered[strings.Fields(tc.name)[0]] = true
	}
	for _, topic := range helpTopics() {
		if strings.Contains(commandHelp[topic], "--format") && !covered[topic] {
			t.Errorf("ynh %s documents --format but has no row in formatErrorCases", topic)
		}
	}
}
