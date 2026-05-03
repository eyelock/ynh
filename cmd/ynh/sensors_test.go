package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sensorHarnessJSON = `{
  "name": "sh",
  "version": "0.1.0",
  "default_vendor": "claude",
  "focus": {
    "audit": {"prompt": "audit the diff"}
  },
  "sensors": {
    "build": {
      "category": "maintainability",
      "source": {"command": "make check"},
      "output": {"format": "text"}
    },
    "tests": {
      "category": "behaviour",
      "source": {"files": ["test-reports/**/*.xml"]},
      "output": {"format": "junit-xml"}
    },
    "sec": {
      "source": {"focus": "audit"},
      "output": {"format": "markdown"}
    },
    "judge": {
      "source": {"focus": {"prompt": "judge coverage"}},
      "output": {"format": "markdown"}
    }
  }
}`

func TestCmdSensors_Ls_JSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	installListTestHarness(t, home, "sh", sensorHarnessJSON)

	var stdout bytes.Buffer
	err := cmdSensorsTo([]string{"ls", "sh", "--format", "json"}, &stdout, io.Discard)
	if err != nil {
		t.Fatalf("cmdSensorsTo: %v", err)
	}
	var entries []sensorListEntry
	if err := json.Unmarshal(stdout.Bytes(), &entries); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, stdout.String())
	}
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}
	// Sorted alphabetically
	wantOrder := []string{"build", "judge", "sec", "tests"}
	for i, w := range wantOrder {
		if entries[i].Name != w {
			t.Errorf("entry[%d].Name = %q, want %q", i, entries[i].Name, w)
		}
	}
	for _, e := range entries {
		switch e.Name {
		case "build":
			if e.SourceKind != "command" || e.Format != "text" {
				t.Errorf("build entry = %+v", e)
			}
		case "tests":
			if e.SourceKind != "files" || e.Category != "behaviour" {
				t.Errorf("tests entry = %+v", e)
			}
		case "sec":
			if e.SourceKind != "focus" || e.InlineFocus {
				t.Errorf("sec entry = %+v (InlineFocus should be false)", e)
			}
		case "judge":
			if e.SourceKind != "focus" || !e.InlineFocus {
				t.Errorf("judge entry = %+v (InlineFocus should be true)", e)
			}
		}
	}
}

func TestCmdSensors_Ls_Text(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	installListTestHarness(t, home, "sh", sensorHarnessJSON)

	var stdout bytes.Buffer
	if err := cmdSensorsTo([]string{"ls", "sh"}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdSensorsTo: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "build") || !strings.Contains(out, "tests") {
		t.Errorf("expected sensor names in text output: %s", out)
	}
	if !strings.Contains(out, "* = inline focus") {
		t.Errorf("expected inline focus footnote, got: %s", out)
	}
}

func TestCmdSensors_Show_FocusReferenceExpanded(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	installListTestHarness(t, home, "sh", sensorHarnessJSON)

	var stdout bytes.Buffer
	if err := cmdSensorsTo([]string{"show", "sh", "sec"}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdSensorsTo: %v", err)
	}
	var entry sensorShowEntry
	if err := json.Unmarshal(stdout.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, stdout.String())
	}
	if entry.Source.Focus == nil {
		t.Fatal("expected focus in source")
	}
	if entry.Source.Focus.Inline {
		t.Error("string-ref focus should not be marked inline")
	}
	if entry.Source.Focus.Name != "audit" {
		t.Errorf("focus.name = %q, want audit", entry.Source.Focus.Name)
	}
	if entry.Source.Focus.Prompt != "audit the diff" {
		t.Errorf("focus.prompt = %q (expected expanded prompt)", entry.Source.Focus.Prompt)
	}
}

func TestCmdSensors_Show_InlineFocus(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	installListTestHarness(t, home, "sh", sensorHarnessJSON)

	var stdout bytes.Buffer
	if err := cmdSensorsTo([]string{"show", "sh", "judge"}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdSensorsTo: %v", err)
	}
	var entry sensorShowEntry
	if err := json.Unmarshal(stdout.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, stdout.String())
	}
	if entry.Source.Focus == nil || !entry.Source.Focus.Inline {
		t.Fatalf("expected inline focus, got %+v", entry.Source.Focus)
	}
	if entry.Source.Focus.Prompt != "judge coverage" {
		t.Errorf("inline prompt = %q", entry.Source.Focus.Prompt)
	}
}

func TestCmdSensors_Show_NotFound(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	installListTestHarness(t, home, "sh", sensorHarnessJSON)

	var stdout, stderr bytes.Buffer
	err := cmdSensorsTo([]string{"show", "sh", "nope", "--format", "json"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error for unknown sensor")
	}
	if !strings.Contains(stderr.String(), "not_found") {
		t.Errorf("expected not_found code in stderr, got: %s", stderr.String())
	}
}

func TestCmdSensors_Ls_NoSensors(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	installListTestHarness(t, home, "empty", `{"name":"empty","version":"0.1.0"}`)

	var stdout bytes.Buffer
	if err := cmdSensorsTo([]string{"ls", "empty"}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdSensorsTo: %v", err)
	}
	if !strings.Contains(stdout.String(), "no sensors declared") {
		t.Errorf("expected empty message, got: %s", stdout.String())
	}
}

func TestCmdSensors_UsageErrors(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{nil, "usage:"},
		{[]string{"unknown"}, "unknown sensors subcommand"},
		{[]string{"ls"}, "usage: ynh sensors ls"},
		{[]string{"show"}, "usage: ynh sensors show"},
		{[]string{"show", "h"}, "usage: ynh sensors show"},
	}
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, "_"), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := cmdSensorsTo(tt.args, &stdout, &stderr)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.want) && !strings.Contains(stderr.String(), tt.want) {
				t.Errorf("expected %q in error, got err=%q stderr=%q", tt.want, err.Error(), stderr.String())
			}
		})
	}
}

func TestCmdInfo_RendersSensorsSection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	installListTestHarness(t, home, "sh", sensorHarnessJSON)

	var stdout bytes.Buffer
	if err := cmdInfoTo([]string{"sh"}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdInfoTo: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "Sensors:") {
		t.Errorf("expected Sensors: section in info text output")
	}
	if !strings.Contains(out, "build") || !strings.Contains(out, "tests") {
		t.Errorf("expected sensor names in info output: %s", out)
	}
}

func TestCmdSensors_Run_Command(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	installListTestHarness(t, home, "sh", `{
		"name": "sh",
		"version": "0.1.0",
		"sensors": {
			"build": {
				"source": {"command": "echo hello && exit 0"},
				"output": {"format": "text"}
			},
			"fail": {
				"source": {"command": "exit 7"},
				"output": {"format": "text"}
			}
		}
	}`)

	t.Run("zero exit", func(t *testing.T) {
		var stdout bytes.Buffer
		if err := cmdSensorsTo([]string{"run", "sh", "build"}, &stdout, io.Discard); err != nil {
			t.Fatalf("cmdSensorsTo: %v", err)
		}
		var r sensorRunResult
		if err := json.Unmarshal(stdout.Bytes(), &r); err != nil {
			t.Fatalf("unmarshal: %v\noutput: %s", err, stdout.String())
		}
		if r.ExitCode != 0 {
			t.Errorf("ExitCode = %d, want 0", r.ExitCode)
		}
		if !strings.Contains(r.Output.Stdout, "hello") {
			t.Errorf("expected hello in stdout, got %q", r.Output.Stdout)
		}
		if r.Kind != "command" {
			t.Errorf("Kind = %q", r.Kind)
		}
	})

	t.Run("non-zero exit captured, no passed bool", func(t *testing.T) {
		var stdout bytes.Buffer
		if err := cmdSensorsTo([]string{"run", "sh", "fail"}, &stdout, io.Discard); err != nil {
			t.Fatalf("cmdSensorsTo: %v", err)
		}
		var r sensorRunResult
		if err := json.Unmarshal(stdout.Bytes(), &r); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if r.ExitCode != 7 {
			t.Errorf("ExitCode = %d, want 7", r.ExitCode)
		}
		// Verify there's no "passed" field by checking the raw JSON.
		if strings.Contains(stdout.String(), `"passed"`) {
			t.Errorf("expected no `passed` field in run output (loop-driver policy): %s", stdout.String())
		}
	})
}

func TestCmdSensors_Run_Files(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	installListTestHarness(t, home, "sh", `{
		"name": "sh",
		"version": "0.1.0",
		"sensors": {
			"reports": {
				"source": {"files": ["reports/*.txt"]},
				"output": {"format": "text"}
			}
		}
	}`)

	work := t.TempDir()
	if err := os.MkdirAll(filepath.Join(work, "reports"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "reports", "a.txt"), []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "reports", "b.txt"), []byte("bravo"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	if err := cmdSensorsTo([]string{"run", "sh", "reports", "--cwd", work}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdSensorsTo: %v", err)
	}
	var r sensorRunResult
	if err := json.Unmarshal(stdout.Bytes(), &r); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, stdout.String())
	}
	if len(r.Output.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(r.Output.Files))
	}
	if r.Output.Files[0].Content != "alpha" {
		t.Errorf("first file content = %q", r.Output.Files[0].Content)
	}

	// --no-content suppresses content but keeps size/path
	stdout.Reset()
	if err := cmdSensorsTo([]string{"run", "sh", "reports", "--cwd", work, "--no-content"}, &stdout, io.Discard); err != nil {
		t.Fatalf("--no-content: %v", err)
	}
	var r2 sensorRunResult
	if err := json.Unmarshal(stdout.Bytes(), &r2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r2.Output.Files[0].Content != "" {
		t.Errorf("expected no content with --no-content")
	}
	if r2.Output.Files[0].Size != 5 {
		t.Errorf("expected size = 5, got %d", r2.Output.Files[0].Size)
	}
}

func TestCmdSensors_Run_FocusReturnsResolvedPayload(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	installListTestHarness(t, home, "sh", sensorHarnessJSON)

	var stdout bytes.Buffer
	if err := cmdSensorsTo([]string{"run", "sh", "sec"}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdSensorsTo: %v", err)
	}
	var r sensorRunResult
	if err := json.Unmarshal(stdout.Bytes(), &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r.Kind != "focus" {
		t.Errorf("Kind = %q", r.Kind)
	}
	if r.Output.Focus == nil || r.Output.Focus.Prompt != "audit the diff" {
		t.Errorf("expected resolved focus, got %+v", r.Output.Focus)
	}
	if r.Output.Note == "" {
		t.Errorf("expected note explaining ynh does not invoke agent, got empty")
	}
}

func TestRoleField_ValidatesAndSurfacesInLs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	installListTestHarness(t, home, "sh", `{
		"name": "sh",
		"version": "0.1.0",
		"sensors": {
			"verify": {
				"role": "convergence-verifier",
				"source": {"command": "true"},
				"output": {"format": "text"}
			}
		}
	}`)

	var stdout bytes.Buffer
	if err := cmdSensorsTo([]string{"ls", "sh", "--format", "json"}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdSensorsTo: %v", err)
	}
	var entries []sensorListEntry
	if err := json.Unmarshal(stdout.Bytes(), &entries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(entries) != 1 || entries[0].Role != "convergence-verifier" {
		t.Errorf("expected role=convergence-verifier in ls output: %+v", entries)
	}
}
