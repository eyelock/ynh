package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestCmdVendorsOutput(t *testing.T) {
	var stdout bytes.Buffer
	if err := cmdVendorsTo(&stdout); err != nil {
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
