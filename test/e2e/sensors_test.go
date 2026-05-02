//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sensorListShape mirrors cmd/ynh.sensorListEntry for assertions.
type sensorListShape struct {
	Name        string `json:"name"`
	Category    string `json:"category,omitempty"`
	Role        string `json:"role,omitempty"`
	SourceKind  string `json:"source_kind"`
	Format      string `json:"format"`
	InlineFocus bool   `json:"inline_focus,omitempty"`
}

// sensorShowShape mirrors cmd/ynh.sensorShowEntry — only the fields the
// suite asserts on, kept local so tests don't import internal/*.
type sensorShowShape struct {
	Name     string `json:"name"`
	Category string `json:"category,omitempty"`
	Role     string `json:"role,omitempty"`
	Source   struct {
		Files   []string `json:"files,omitempty"`
		Command string   `json:"command,omitempty"`
		Focus   *struct {
			Name   string `json:"name,omitempty"`
			Prompt string `json:"prompt"`
			Inline bool   `json:"inline"`
		} `json:"focus,omitempty"`
	} `json:"source"`
	Output struct {
		Format string `json:"format"`
	} `json:"output"`
}

// sensorRunShape mirrors cmd/ynh.sensorRunResult.
type sensorRunShape struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Role     string `json:"role,omitempty"`
	Category string `json:"category,omitempty"`
	ExitCode int    `json:"exit_code"`
	Output   struct {
		Format string `json:"format"`
		Stdout string `json:"stdout,omitempty"`
		Stderr string `json:"stderr,omitempty"`
		Files  []struct {
			Path    string `json:"path"`
			Size    int64  `json:"size"`
			Content string `json:"content,omitempty"`
		} `json:"files,omitempty"`
		Focus *struct {
			Name   string `json:"name,omitempty"`
			Prompt string `json:"prompt"`
			Inline bool   `json:"inline"`
		} `json:"focus,omitempty"`
	} `json:"output"`
}

// writeSensorHarness writes a plugin.json into a fresh tmpdir and returns
// the harness directory. Used by every sensors E2E test that needs an
// installable fixture.
func writeSensorHarness(t *testing.T, manifest string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "sensor-harness")
	if err := os.MkdirAll(filepath.Join(dir, ".ynh-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, ".ynh-plugin", "plugin.json"),
		[]byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestSensors_Install_PreservesAllSourceVariants installs a manifest with
// every sensor source variant — files, command, focus-ref, inline-focus —
// and asserts all four survive the install path unchanged.
//
// Behavioural backstop for the sensors flow through plugin → harness →
// install → discovery CLI.
func TestSensors_Install_PreservesAllSourceVariants(t *testing.T) {
	s := newSandbox(t)

	manifest := `{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "sensor-demo",
  "version": "0.1.0",
  "default_vendor": "claude",
  "focus": {
    "audit-vulns": { "prompt": "Audit the diff for vulnerabilities." }
  },
  "sensors": {
    "coverage": {
      "category": "maintainability",
      "source": { "files": ["coverage/lcov.info"] },
      "output": { "format": "lcov-summary" }
    },
    "build": {
      "source": { "command": "make check" },
      "output": { "format": "text" }
    },
    "security": {
      "source": { "focus": "audit-vulns" },
      "output": { "format": "markdown" }
    },
    "coverage-judge": {
      "role": "convergence-verifier",
      "source": {
        "focus": { "prompt": "Is coverage adequate for the changed surface?" }
      },
      "output": { "format": "markdown" }
    }
  }
}`

	src := writeSensorHarness(t, manifest)
	s.mustRunYnh(t, "install", src)

	out, _ := s.mustRunYnh(t, "sensors", "ls", "sensor-demo", "--format", "json")
	var got []sensorListShape
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("parsing sensors ls JSON: %v\n%s", err, out)
	}

	want := map[string]sensorListShape{
		"coverage":       {Name: "coverage", Category: "maintainability", SourceKind: "files", Format: "lcov-summary"},
		"build":          {Name: "build", SourceKind: "command", Format: "text"},
		"security":       {Name: "security", SourceKind: "focus", Format: "markdown"},
		"coverage-judge": {Name: "coverage-judge", Role: "convergence-verifier", SourceKind: "focus", Format: "markdown", InlineFocus: true},
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d sensors, got %d: %+v", len(want), len(got), got)
	}
	for _, g := range got {
		w, ok := want[g.Name]
		if !ok {
			t.Errorf("unexpected sensor %q in ls output", g.Name)
			continue
		}
		assertEqual(t, g.Name+".category", g.Category, w.Category)
		assertEqual(t, g.Name+".role", g.Role, w.Role)
		assertEqual(t, g.Name+".source_kind", g.SourceKind, w.SourceKind)
		assertEqual(t, g.Name+".format", g.Format, w.Format)
		assertEqual(t, g.Name+".inline_focus", g.InlineFocus, w.InlineFocus)
	}
}

// TestSensors_Show_ResolvesFocusReference verifies that `ynh sensors show`
// expands a string focus reference inline so the consumer gets a
// self-contained payload — the entire point of show vs ls.
func TestSensors_Show_ResolvesFocusReference(t *testing.T) {
	s := newSandbox(t)

	manifest := `{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "sensor-show",
  "version": "0.1.0",
  "default_vendor": "claude",
  "focus": {
    "audit-vulns": { "prompt": "Audit the diff for vulnerabilities." }
  },
  "sensors": {
    "security": {
      "source": { "focus": "audit-vulns" },
      "output": { "format": "markdown" }
    }
  }
}`

	src := writeSensorHarness(t, manifest)
	s.mustRunYnh(t, "install", src)

	out, _ := s.mustRunYnh(t, "sensors", "show", "sensor-show", "security", "--format", "json")
	var got sensorShowShape
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("parsing sensors show JSON: %v\n%s", err, out)
	}

	if got.Source.Focus == nil {
		t.Fatalf("expected source.focus to be populated, got: %+v", got)
	}
	assertEqual(t, "focus.name", got.Source.Focus.Name, "audit-vulns")
	assertEqual(t, "focus.prompt", got.Source.Focus.Prompt, "Audit the diff for vulnerabilities.")
	assertEqual(t, "focus.inline", got.Source.Focus.Inline, false)
}

// TestSensors_Run_CommandCapturesExitAndStreams runs a command sensor that
// exits non-zero with output on both streams. Asserts ynh emits raw signal
// (exit_code, stdout, stderr) and — critically — does not invent a
// `passed` boolean. Pass/fail is loop-driver policy.
func TestSensors_Run_CommandCapturesExitAndStreams(t *testing.T) {
	s := newSandbox(t)

	manifest := `{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "sensor-run-cmd",
  "version": "0.1.0",
  "default_vendor": "claude",
  "sensors": {
    "noisy": {
      "source": { "command": "echo OUT; echo ERR 1>&2; exit 7" },
      "output": { "format": "text" }
    }
  }
}`

	src := writeSensorHarness(t, manifest)
	s.mustRunYnh(t, "install", src)

	// runYnh, not mustRunYnh — sensor command exits non-zero. ynh itself
	// must still exit 0; the non-zero is reported via JSON.
	out, _, err := s.runYnh(t, "sensors", "run", "sensor-run-cmd", "noisy")
	if err != nil {
		t.Fatalf("ynh sensors run unexpectedly failed: %v\n%s", err, out)
	}

	var got sensorRunShape
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("parsing sensors run JSON: %v\n%s", err, out)
	}

	assertEqual(t, "kind", got.Kind, "command")
	assertEqual(t, "exit_code", got.ExitCode, 7)
	if !strings.Contains(got.Output.Stdout, "OUT") {
		t.Errorf("expected stdout to contain OUT, got %q", got.Output.Stdout)
	}
	if !strings.Contains(got.Output.Stderr, "ERR") {
		t.Errorf("expected stderr to contain ERR, got %q", got.Output.Stderr)
	}

	// Contract check: the JSON must NOT contain a "passed" key. Loop
	// drivers turn raw signal into pass/fail, not ynh.
	if strings.Contains(out, `"passed"`) {
		t.Errorf("ynh sensors run JSON must not include a 'passed' field:\n%s", out)
	}
}

// TestSensors_Run_FilesGlobAndNoContent exercises the files-source path:
// glob expands to multiple matches, default invocation includes content,
// --no-content omits it but keeps path/size.
func TestSensors_Run_FilesGlobAndNoContent(t *testing.T) {
	s := newSandbox(t)

	manifest := `{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "sensor-run-files",
  "version": "0.1.0",
  "default_vendor": "claude",
  "sensors": {
    "results": {
      "source": { "files": ["results/*.txt"] },
      "output": { "format": "text" }
    }
  }
}`

	src := writeSensorHarness(t, manifest)
	s.mustRunYnh(t, "install", src)

	// Lay down two matching files in a per-test cwd that sensor run will
	// glob against via --cwd.
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cwd, "results"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(cwd, "results", name),
			[]byte("payload-"+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// First run: include content.
	out, _ := s.mustRunYnh(t, "sensors", "run", "sensor-run-files", "results", "--cwd", cwd)
	var withContent sensorRunShape
	if err := json.Unmarshal([]byte(out), &withContent); err != nil {
		t.Fatalf("parsing run JSON: %v\n%s", err, out)
	}
	assertEqual(t, "kind", withContent.Kind, "files")
	if len(withContent.Output.Files) != 2 {
		t.Fatalf("expected 2 glob matches, got %d: %+v", len(withContent.Output.Files), withContent.Output.Files)
	}
	for _, f := range withContent.Output.Files {
		if f.Content == "" {
			t.Errorf("expected content for %s in default mode, got empty", f.Path)
		}
		if f.Size <= 0 {
			t.Errorf("expected nonzero size for %s, got %d", f.Path, f.Size)
		}
	}

	// Second run: --no-content.
	out2, _ := s.mustRunYnh(t, "sensors", "run", "sensor-run-files", "results", "--cwd", cwd, "--no-content")
	var withoutContent sensorRunShape
	if err := json.Unmarshal([]byte(out2), &withoutContent); err != nil {
		t.Fatalf("parsing run JSON: %v\n%s", err, out2)
	}
	if len(withoutContent.Output.Files) != 2 {
		t.Fatalf("expected 2 glob matches with --no-content, got %d", len(withoutContent.Output.Files))
	}
	for _, f := range withoutContent.Output.Files {
		if f.Content != "" {
			t.Errorf("expected empty content with --no-content for %s, got %q", f.Path, f.Content)
		}
		if f.Size <= 0 {
			t.Errorf("expected size preserved with --no-content for %s, got %d", f.Path, f.Size)
		}
	}
}

// TestSensors_InlineFocus_NotInTopLevelFocus locks the scoping rule:
// inline focuses declared inside a sensor must NOT leak into the
// top-level focus map. Top-level focus is the public surface; inline
// focuses belong to their sensor.
func TestSensors_InlineFocus_NotInTopLevelFocus(t *testing.T) {
	s := newSandbox(t)

	manifest := `{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "sensor-inline",
  "version": "0.1.0",
  "default_vendor": "claude",
  "focus": {
    "audit-vulns": { "prompt": "Audit the diff." }
  },
  "sensors": {
    "judge": {
      "source": {
        "focus": { "prompt": "Inline-only — must not leak to top-level focus." }
      },
      "output": { "format": "markdown" }
    }
  }
}`

	src := writeSensorHarness(t, manifest)
	s.mustRunYnh(t, "install", src)

	// Inspect top-level focus via info text — info JSON does not break
	// out focus as a discrete field, but the text rendering does.
	out, _ := s.mustRunYnh(t, "info", "sensor-inline")
	if !strings.Contains(out, "audit-vulns") {
		t.Errorf("expected top-level focus 'audit-vulns' in info output:\n%s", out)
	}
	// The inline focus has no name — it should appear under Sensors,
	// never under Focus. Assert by checking that nothing under Focus
	// references the inline prompt.
	if focusBlock := extractInfoBlock(out, "Focus:"); strings.Contains(focusBlock, "Inline-only") {
		t.Errorf("inline-focus prompt leaked into top-level Focus block:\n%s", focusBlock)
	}
	// And the Sensors block should mention judge with its inline marker.
	if sensorsBlock := extractInfoBlock(out, "Sensors:"); !strings.Contains(sensorsBlock, "judge") {
		t.Errorf("expected 'judge' under Sensors block, got:\n%s", sensorsBlock)
	}
}

// extractInfoBlock returns the lines under a `ynh info` section header
// (e.g. "Focus:") up to the next blank line or top-level header. Used by
// inline-focus scoping assertions.
func extractInfoBlock(info, header string) string {
	lines := strings.Split(info, "\n")
	var out []string
	in := false
	for _, line := range lines {
		if !in {
			if strings.TrimSpace(line) == header {
				in = true
			}
			continue
		}
		// Section ends at blank line or new top-level header (no leading space).
		if line == "" || (len(line) > 0 && line[0] != ' ' && line[0] != '\t' && strings.HasSuffix(strings.TrimSpace(line), ":")) {
			break
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// TestSensors_Validate_OneOfSourceViolation locks the dual-error contract
// for an invalid sensor source: schema-level oneOf rejection AND the
// cross-field validator each emit one line. Loop-driver authors and
// harness writers both rely on this dual signal.
func TestSensors_Validate_OneOfSourceViolation(t *testing.T) {
	manifest := `{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "sensor-bad",
  "version": "0.1.0",
  "sensors": {
    "broken": {
      "source": { "command": "x", "files": ["a"] },
      "output": { "format": "text" }
    }
  }
}`

	dir := writeSensorHarness(t, manifest)

	stdout, errOut, err := runYnd(t, "validate", dir)
	if err == nil {
		t.Fatalf("expected validate to fail on multi-field source, got success\nstdout:\n%s\nstderr:\n%s", stdout, errOut)
	}
	combined := stdout + errOut
	if !strings.Contains(combined, "oneOf") {
		t.Errorf("expected schema-level 'oneOf' line in stderr, got:\n%s", combined)
	}
	if !strings.Contains(combined, "exactly one of files, command, focus") {
		t.Errorf("expected cross-field source rule line in stderr, got:\n%s", combined)
	}
}
