package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/config"
)

func TestCmdVersion_Text(t *testing.T) {
	var out, errb bytes.Buffer
	if err := cmdVersionTo(nil, &out, &errb); err != nil {
		t.Fatalf("cmdVersionTo: %v", err)
	}
	got := strings.TrimSpace(out.String())
	if got != config.Version {
		t.Errorf("text output = %q, want %q", got, config.Version)
	}
}

func TestCmdVersion_JSON(t *testing.T) {
	var out, errb bytes.Buffer
	if err := cmdVersionTo([]string{"--format", "json"}, &out, &errb); err != nil {
		t.Fatalf("cmdVersionTo: %v", err)
	}

	var payload struct {
		Version      string `json:"version"`
		Capabilities string `json:"capabilities"`
	}
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v\nraw: %s", err, out.String())
	}

	if payload.Version != config.Version {
		t.Errorf("Version = %q, want %q", payload.Version, config.Version)
	}
	if payload.Capabilities != config.CapabilitiesVersion {
		t.Errorf("Capabilities = %q, want %q", payload.Capabilities, config.CapabilitiesVersion)
	}
	if payload.Capabilities == "" {
		t.Error("Capabilities must not be empty — TermQ relies on this field for gating")
	}
}

func TestCmdVersion_InvalidFormat(t *testing.T) {
	var out, errb bytes.Buffer
	err := cmdVersionTo([]string{"--format", "yaml"}, &out, &errb)
	if err == nil {
		t.Fatal("expected error for unknown format")
	}
}

func TestCmdVersion_UnknownFlag(t *testing.T) {
	var out, errb bytes.Buffer
	err := cmdVersionTo([]string{"--bogus"}, &out, &errb)
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
}

// TestCmdVersion_JSONShapeStability pins the JSON field names. Downstream
// consumers (TermQ) decode this into their own struct; renaming a field is
// a breaking contract change that must be accompanied by a CapabilitiesVersion
// bump and a coordinated TermQ release.
func TestCmdVersion_JSONShapeStability(t *testing.T) {
	var out, errb bytes.Buffer
	if err := cmdVersionTo([]string{"--format", "json"}, &out, &errb); err != nil {
		t.Fatal(err)
	}

	// Assert raw keys as strings to catch any rename.
	var raw map[string]any
	if err := json.Unmarshal(out.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"version", "capabilities"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("missing required key %q in version JSON", key)
		}
	}
}
