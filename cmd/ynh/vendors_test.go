package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestCmdVendorsOutput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := cmdVendorsTo(nil, &stdout, &stderr); err != nil {
		t.Fatalf("cmdVendorsTo: %v", err)
	}

	out := stdout.String()

	// Header must include all five columns
	if !strings.Contains(out, "NAME") {
		t.Error("missing NAME header")
	}
	if !strings.Contains(out, "DISPLAY NAME") {
		t.Error("missing DISPLAY NAME header")
	}
	if !strings.Contains(out, "CLI") {
		t.Error("missing CLI header")
	}
	if !strings.Contains(out, "CONFIG DIR") {
		t.Error("missing CONFIG DIR header")
	}
	if !strings.Contains(out, "AVAILABLE") {
		t.Error("missing AVAILABLE header")
	}

	// All three vendors must appear
	if !strings.Contains(out, "claude") {
		t.Error("missing claude vendor")
	}
	if !strings.Contains(out, "codex") {
		t.Error("missing codex vendor")
	}
	if !strings.Contains(out, "cursor") {
		t.Error("missing cursor vendor")
	}

	// Display names must appear
	if !strings.Contains(out, "Claude Code") {
		t.Error("missing Claude Code display name")
	}
	if !strings.Contains(out, "OpenAI Codex") {
		t.Error("missing OpenAI Codex display name")
	}

	// Available column must have true or false for each row
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 4 { // header + 3 vendors
		t.Fatalf("expected at least 4 lines, got %d", len(lines))
	}
	for _, line := range lines[1:] { // skip header
		if !strings.Contains(line, "true") && !strings.Contains(line, "false") {
			t.Errorf("vendor line missing availability: %s", line)
		}
	}
}

func TestCmdVendorsJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := cmdVendorsTo([]string{"--format", "json"}, &stdout, &stderr); err != nil {
		t.Fatalf("cmdVendorsTo JSON: %v", err)
	}

	var entries []vendorEntry
	if err := json.Unmarshal(stdout.Bytes(), &entries); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}

	if len(entries) < 3 {
		t.Fatalf("expected at least 3 vendors, got %d", len(entries))
	}

	names := map[string]bool{}
	for _, e := range entries {
		names[e.Name] = true
		if e.DisplayName == "" {
			t.Errorf("vendor %q missing display_name", e.Name)
		}
		if e.CLI == "" {
			t.Errorf("vendor %q missing cli", e.Name)
		}
		if e.ConfigDir == "" {
			t.Errorf("vendor %q missing config_dir", e.Name)
		}
	}

	if !names["claude"] {
		t.Error("missing claude vendor")
	}
	if !names["codex"] {
		t.Error("missing codex vendor")
	}
	if !names["cursor"] {
		t.Error("missing cursor vendor")
	}
}

func TestCmdVendorsJSON_AvailableIsBool(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := cmdVendorsTo([]string{"--format", "json"}, &stdout, &stderr); err != nil {
		t.Fatalf("cmdVendorsTo JSON: %v", err)
	}

	// Parse as generic to verify "available" is a boolean, not a string
	var raw []map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	for _, entry := range raw {
		avail, ok := entry["available"]
		if !ok {
			t.Error("missing available field")
			continue
		}
		if _, isBool := avail.(bool); !isBool {
			t.Errorf("available should be bool, got %T", avail)
		}
	}
}

func TestCmdVendors_InvalidFormat(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := cmdVendorsTo([]string{"--format", "yaml"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
}
