package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/clischema"
)

func TestCmdAgentRun_FocusAndTaskExclusive(t *testing.T) {
	err := cmdAgentRunTo(t, []string{"--harness", "x", "--focus", "f", "--task", "t"})
	if err == nil || !strings.Contains(err.Error(), "--focus and --task") {
		t.Fatalf("expected --focus + --task rejection, got: %v", err)
	}
}

func TestCmdAgentRun_FocusAndProfileExclusive(t *testing.T) {
	err := cmdAgentRunTo(t, []string{"--harness", "x", "--focus", "f", "--profile", "p"})
	if err == nil || !strings.Contains(err.Error(), "--focus and --profile") {
		t.Fatalf("expected --focus + --profile rejection, got: %v", err)
	}
}

func TestCmdAgentRun_RequiresTaskOrFocus(t *testing.T) {
	err := cmdAgentRunTo(t, []string{"--harness", "x"})
	if err == nil || !strings.Contains(err.Error(), "--task or --focus is required") {
		t.Fatalf("expected --task-or-focus-required, got: %v", err)
	}
}

func TestCmdAgentRun_ResumeRequiresValue(t *testing.T) {
	err := cmdAgentRunTo(t, []string{"--resume"})
	if err == nil || !strings.Contains(err.Error(), "--resume requires a value") {
		t.Fatalf("expected --resume value-required, got: %v", err)
	}
}

func TestCmdAgentRun_ProfileFlagAccepted(t *testing.T) {
	err := cmdAgentRunTo(t, []string{"--harness", "x", "--profile"})
	if err == nil || !strings.Contains(err.Error(), "--profile requires a value") {
		t.Fatalf("expected --profile value-required, got: %v", err)
	}
}

func TestCmdAgentRun_FocusFlagAccepted(t *testing.T) {
	err := cmdAgentRunTo(t, []string{"--harness", "x", "--focus"})
	if err == nil || !strings.Contains(err.Error(), "--focus requires a value") {
		t.Fatalf("expected --focus value-required, got: %v", err)
	}
}

func TestCmdAgentRun_MaxPlanIterationsRequiresValue(t *testing.T) {
	err := cmdAgentRunTo(t, []string{"--harness", "x", "--task", "t", "--max-plan-iterations"})
	if err == nil || !strings.Contains(err.Error(), "--max-plan-iterations requires a value") {
		t.Fatalf("expected --max-plan-iterations value-required, got: %v", err)
	}
}

func TestCmdAgentRun_MaxPlanIterationsRejectsNonInt(t *testing.T) {
	err := cmdAgentRunTo(t, []string{"--harness", "x", "--task", "t", "--max-plan-iterations", "lots"})
	if err == nil || !strings.Contains(err.Error(), "non-negative integer") {
		t.Fatalf("expected non-integer rejection, got: %v", err)
	}
}

func TestCmdAgentRun_MaxPlanIterationsRejectsNegative(t *testing.T) {
	err := cmdAgentRunTo(t, []string{"--harness", "x", "--task", "t", "--max-plan-iterations", "-3"})
	if err == nil || !strings.Contains(err.Error(), "non-negative integer") {
		t.Fatalf("expected negative rejection, got: %v", err)
	}
}

func cmdAgentRunTo(t *testing.T, args []string) error {
	t.Helper()
	return cmdAgentRun(args, &bytes.Buffer{}, &bytes.Buffer{}, strings.NewReader(""))
}

func TestCmdAgentRun_AutoApproveRequiresValue(t *testing.T) {
	err := cmdAgentRunTo(t, []string{"--task", "t", "--auto-approve"})
	if err == nil || !strings.Contains(err.Error(), "--auto-approve requires a value") {
		t.Fatalf("expected --auto-approve value-required, got: %v", err)
	}
}

// A level the backend cannot honour fails before any run starts.
func TestCmdAgentRun_AutoApproveValidatedBeforeTheRun(t *testing.T) {
	tests := []struct {
		args    []string
		wantSub string
	}{
		{[]string{"--task", "t", "--auto-approve", "sometimes"}, "unknown --auto-approve level"},
		{[]string{"--task", "t", "--backend", "codex", "--auto-approve", "edits"}, "codex cannot auto-approve edits only"},
		{[]string{"--task", "t", "--backend", "cursor", "--auto-approve", "edits"}, "cursor cannot auto-approve edits only"},
		{[]string{"--task", "t", "--effort", "max"}, `unknown effort level "max"`},
		{[]string{"--task", "t", "--backend", "cursor", "--effort", "high"}, "cursor has no reasoning effort setting"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			err := cmdAgentRunTo(t, tt.args)
			if err == nil || !strings.Contains(err.Error(), tt.wantSub) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.wantSub)
			}
		})
	}
}

// A command line ynh agent run cannot accept is reported in the structured
// error envelope when --format json is given, wherever --format json appears
// relative to the bad argument (#513). It used to print plain text, so a loop
// driver parsing the output as JSON got nothing it could read.
func TestCmdAgentRun_ArgumentErrorsHonourFormatJSON(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	starts := vendorShims(t, "unused")
	missingTask := "@" + filepath.Join(t.TempDir(), "absent.md")
	jsonFlag := []string{"--format", "json"}
	tests := []struct {
		name     string
		args     []string
		jsonLast bool // --format json may follow the bad argument
		wantCode string
		wantSub  string
	}{
		{"unknown flag", []string{"--task", "t", "--bogus"}, true, errCodeInvalidInput, "unknown flag: --bogus"},
		{"unexpected argument", []string{"--task", "t", "stray"}, true, errCodeInvalidInput, "unexpected argument: stray"},
		{"missing value", []string{"--task"}, false, errCodeInvalidInput, "--task requires a value"},
		{"missing value of a numeric flag", []string{"--task", "t", "--max-turns"}, false, errCodeInvalidInput, "--max-turns requires a value"},
		{"max-turns not a number", []string{"--task", "t", "--max-turns", "lots"}, true, errCodeInvalidInput, "--max-turns must be a non-negative integer"},
		{"max-tokens negative", []string{"--task", "t", "--max-tokens", "-1"}, true, errCodeInvalidInput, "--max-tokens must be a non-negative integer"},
		{"max-wall not a duration", []string{"--task", "t", "--max-wall", "soon"}, true, errCodeInvalidInput, "--max-wall"},
		{"max-plan-iterations not a number", []string{"--task", "t", "--max-plan-iterations", "x"}, true, errCodeInvalidInput, "--max-plan-iterations must be a non-negative integer"},
		{"sensor overlay not JSON", []string{"--task", "t", "--sensor-overlay", "{"}, true, errCodeInvalidInput, "--sensor-overlay: invalid JSON"},
		{"unknown format", []string{"--task", "t", "--format", "yaml"}, true, errCodeInvalidInput, "unknown format: yaml"},
		{"task file unreadable", []string{"--task", missingTask}, true, errCodeIOError, "reading task file"},
		{"task with focus", []string{"--task", "t", "--focus", "f"}, true, errCodeInvalidInput, "cannot use --focus and --task together"},
		{"focus with profile", []string{"--focus", "f", "--profile", "p"}, true, errCodeInvalidInput, "cannot use --focus and --profile together"},
		{"neither task nor focus", []string{"--harness", "x"}, true, errCodeInvalidInput, "--task or --focus is required"},
	}
	schema, err := clischema.Get("error")
	if err != nil {
		t.Fatalf("Get error schema: %v", err)
	}
	for _, tt := range tests {
		orders := map[string][]string{"json first": append(append([]string{}, jsonFlag...), tt.args...)}
		if tt.jsonLast {
			orders["json last"] = append(append([]string{}, tt.args...), jsonFlag...)
		}
		for order, args := range orders {
			t.Run(tt.name+"/"+order, func(t *testing.T) {
				var stdout, stderr bytes.Buffer
				err := cmdAgentRun(args, &stdout, &stderr, strings.NewReader(""))
				if !errors.Is(err, errStructuredReported) {
					t.Fatalf("error not reported as an envelope: %v\nstderr: %s", err, stderr.String())
				}
				if got := processExitCode(err); got != 1 {
					t.Errorf("exit = %d, want 1", got)
				}
				if stdout.Len() != 0 {
					t.Errorf("stdout = %q, want empty", stdout.String())
				}
				dec := json.NewDecoder(&stderr)
				var env any
				if derr := dec.Decode(&env); derr != nil {
					t.Fatalf("stderr is not an envelope: %v", derr)
				}
				if dec.More() {
					t.Errorf("stderr holds more than one value")
				}
				if verr := schema.Validate(env); verr != nil {
					t.Errorf("envelope does not validate: %v", verr)
				}
				e, _ := env.(map[string]any)["error"].(map[string]any)
				if e["code"] != tt.wantCode {
					t.Errorf("code = %v, want %s", e["code"], tt.wantCode)
				}
				if msg, _ := e["message"].(string); !strings.Contains(msg, tt.wantSub) {
					t.Errorf("message = %q, want it to contain %q", msg, tt.wantSub)
				}
			})
		}
		t.Run(tt.name+"/text", func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := cmdAgentRun(tt.args, &stdout, &stderr, strings.NewReader(""))
			if err == nil || errors.Is(err, errStructuredReported) {
				t.Fatalf("text mode: want a plain error, got %v", err)
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantSub)
			}
			if got := processExitCode(err); got != 1 {
				t.Errorf("exit = %d, want 1", got)
			}
			if stdout.Len() != 0 || stderr.Len() != 0 {
				t.Errorf("text mode wrote stdout %q, stderr %q; main prints the error", stdout.String(), stderr.String())
			}
		})
	}
	if _, err := os.Stat(starts); err == nil {
		t.Error("an argument error started a worker")
	}
}

// The agent subcommand itself is checked the same way.
func TestCmdAgent_SubcommandErrorHonoursFormatJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := cmdAgentTo([]string{"bogus", "--format", "json"}, &stdout, &stderr, strings.NewReader(""))
	if !errors.Is(err, errStructuredReported) {
		t.Fatalf("error not reported as an envelope: %v", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	var env struct {
		Error struct{ Code, Message string } `json:"error"`
	}
	if jerr := json.Unmarshal(stderr.Bytes(), &env); jerr != nil || env.Error.Code != errCodeInvalidInput {
		t.Errorf("stderr = %q, want an invalid_input envelope (%v)", stderr.String(), jerr)
	}
}
