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
