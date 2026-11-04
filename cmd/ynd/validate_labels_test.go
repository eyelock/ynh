package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// The manifest's labels are checked against plugin.schema.json with the same
// limits ynh enforces at run time (ynr ADR-006 rule 10).
func TestValidateHarnessLabels(t *testing.T) {
	hr := filepath.Join(t.TempDir(), "seat")
	mkdirAll(t, hr)
	tests := []struct {
		name    string
		labels  string
		invalid bool
	}{
		{"one label", `{"wheelhouse":"ynw"}`, false},
		{"a path-like value", `{"repo":"eyelock/ynh"}`, false},
		{"upper-case key", `{"Wheelhouse":"ynw"}`, true},
		{"key starts with a digit", `{"1a":"ynw"}`, true},
		{"value with a space", `{"wheelhouse":"y n w"}`, true},
		{"value with an @", `{"owner":"me@example.com"}`, true},
		{"empty value", `{"wheelhouse":""}`, true},
		{"value too long", `{"wheelhouse":"` + strings.Repeat("v", 65) + `"}`, true},
		{"not an object", `["wheelhouse:ynw"]`, true},
		{"not strings", `{"n":7}`, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			writeFile(t, filepath.Join(hr, ".agents/harness", "plugin.json"),
				[]byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"seat","version":"0.1.0","labels":`+tc.labels+`}`))
			out, err := validateOutput(t, hr)
			if (err != nil) != tc.invalid {
				t.Fatalf("invalid = %v, want %v\n%s", err != nil, tc.invalid, out)
			}
		})
	}
}
