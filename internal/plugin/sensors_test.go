package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePluginJSON(dir, content string) error {
	if err := os.MkdirAll(filepath.Join(dir, PluginDir), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, PluginDir, PluginFile), []byte(content), 0o644)
}

func TestSensorSource_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
		wantKnd string
	}{
		{"files only", `{"files":["a","b"]}`, "", "files"},
		{"command only", `{"command":"make check"}`, "", "command"},
		{"focus string", `{"focus":"infer-vulns"}`, "", "focus"},
		{"focus inline", `{"focus":{"prompt":"x"}}`, "", "focus"},
		{"empty", `{}`, "exactly one", ""},
		{"two set", `{"files":["a"],"command":"x"}`, "exactly one", ""},
		{"three set", `{"files":["a"],"command":"x","focus":"y"}`, "exactly one", ""},
		{"unknown field", `{"command":"x","junk":1}`, "unknown field", ""},
		{"empty focus string", `{"focus":""}`, "must not be empty", ""},
		{"inline focus no prompt", `{"focus":{"profile":"ci"}}`, "non-empty prompt", ""},
		{"inline focus unknown field", `{"focus":{"prompt":"x","junk":1}}`, "unknown field", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s SensorSource
			err := json.Unmarshal([]byte(tt.input), &s)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if s.Kind() != tt.wantKnd {
					t.Errorf("Kind = %q, want %q", s.Kind(), tt.wantKnd)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestFocusRef_UnmarshalRoundTrip(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		var f FocusRef
		if err := json.Unmarshal([]byte(`"my-focus"`), &f); err != nil {
			t.Fatal(err)
		}
		if f.Name != "my-focus" || f.Inline != nil {
			t.Errorf("got %+v", f)
		}
		out, _ := json.Marshal(f)
		if string(out) != `"my-focus"` {
			t.Errorf("round-trip = %s", out)
		}
	})
	t.Run("inline", func(t *testing.T) {
		var f FocusRef
		if err := json.Unmarshal([]byte(`{"profile":"ci","prompt":"do x"}`), &f); err != nil {
			t.Fatal(err)
		}
		if f.Inline == nil || f.Inline.Prompt != "do x" || f.Inline.Profile != "ci" {
			t.Errorf("got %+v", f.Inline)
		}
		out, _ := json.Marshal(f)
		if !strings.Contains(string(out), `"prompt":"do x"`) {
			t.Errorf("round-trip = %s", out)
		}
	})
}

func TestValidateSensors(t *testing.T) {
	profiles := map[string]bool{"ci": true}
	focuses := map[string]bool{"known": true}

	tests := []struct {
		name      string
		sensors   map[string]Sensor
		wantIssue string // empty = expect no issues
	}{
		{
			name: "valid files sensor",
			sensors: map[string]Sensor{
				"coverage": {
					Category: "maintainability",
					Source:   SensorSource{Files: []string{"coverage/*.json"}},
					Output:   SensorOutput{Format: "lcov-summary"},
				},
			},
		},
		{
			name: "valid command sensor",
			sensors: map[string]Sensor{
				"build": {
					Source: SensorSource{Command: "make check"},
					Output: SensorOutput{Format: "text"},
				},
			},
		},
		{
			name: "valid string focus ref",
			sensors: map[string]Sensor{
				"sec": {
					Source: SensorSource{Focus: &FocusRef{Name: "known"}},
					Output: SensorOutput{Format: "markdown"},
				},
			},
		},
		{
			name: "valid inline focus",
			sensors: map[string]Sensor{
				"sec": {
					Source: SensorSource{Focus: &FocusRef{Inline: &InlineFocus{Profile: "ci", Prompt: "audit"}}},
					Output: SensorOutput{Format: "markdown"},
				},
			},
		},
		{
			name: "bad category",
			sensors: map[string]Sensor{
				"x": {
					Category: "wrong",
					Source:   SensorSource{Command: "true"},
					Output:   SensorOutput{Format: "text"},
				},
			},
			wantIssue: `category "wrong"`,
		},
		{
			name: "missing format",
			sensors: map[string]Sensor{
				"x": {
					Source: SensorSource{Command: "true"},
					Output: SensorOutput{},
				},
			},
			wantIssue: "output.format must not be empty",
		},
		{
			name: "unknown focus reference",
			sensors: map[string]Sensor{
				"x": {
					Source: SensorSource{Focus: &FocusRef{Name: "nope"}},
					Output: SensorOutput{Format: "markdown"},
				},
			},
			wantIssue: `references undefined focus "nope"`,
		},
		{
			name: "inline focus references unknown profile",
			sensors: map[string]Sensor{
				"x": {
					Source: SensorSource{Focus: &FocusRef{Inline: &InlineFocus{Profile: "missing", Prompt: "x"}}},
					Output: SensorOutput{Format: "markdown"},
				},
			},
			wantIssue: `unknown profile "missing"`,
		},
		{
			name: "no source set",
			sensors: map[string]Sensor{
				"x": {
					Output: SensorOutput{Format: "text"},
				},
			},
			wantIssue: "source must have exactly one",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issues := ValidateSensors(tt.sensors, profiles, focuses)
			if tt.wantIssue == "" {
				if len(issues) > 0 {
					t.Errorf("expected no issues, got: %v", issues)
				}
				return
			}
			found := false
			for _, i := range issues {
				if strings.Contains(i, tt.wantIssue) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected issue containing %q, got: %v", tt.wantIssue, issues)
			}
		})
	}
}

func TestLoadPluginJSON_WithSensors(t *testing.T) {
	dir := t.TempDir()
	if err := writePluginJSON(dir, `{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "h",
  "version": "0.1.0",
  "sensors": {
    "build": {
      "source": {"command": "make check"},
      "output": {"format": "text"}
    },
    "tests": {
      "category": "behaviour",
      "source": {"files": ["test-reports/**/*.xml"]},
      "output": {"format": "junit-xml"}
    }
  }
}`); err != nil {
		t.Fatal(err)
	}
	hj, err := LoadPluginJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(hj.Sensors) != 2 {
		t.Fatalf("expected 2 sensors, got %d", len(hj.Sensors))
	}
	if hj.Sensors["build"].Source.Kind() != "command" {
		t.Errorf("build kind = %q", hj.Sensors["build"].Source.Kind())
	}
	if hj.Sensors["tests"].Source.Kind() != "files" {
		t.Errorf("tests kind = %q", hj.Sensors["tests"].Source.Kind())
	}
}
