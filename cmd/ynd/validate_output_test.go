// Tests for `ynd validate --schema <name>` schema-mode (formerly the
// standalone `ynd validate-output` command). The file name is kept to
// preserve `go test -run` muscle memory; tests exercise the folded surface
// via validateFromStdin.
package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestValidateSchema_OK(t *testing.T) {
	input := `{"version": "0.3.1", "capabilities": "0.4.0"}`
	var out, errb bytes.Buffer
	err := validateFromStdin(strings.NewReader(input), &out, &errb, "version")
	if err != nil {
		t.Fatalf("validate: %v (stderr: %s)", err, errb.String())
	}
	if !strings.Contains(out.String(), "ok") {
		t.Errorf("expected 'ok', got: %s", out.String())
	}
}

func TestValidateSchema_BadJSON(t *testing.T) {
	var out, errb bytes.Buffer
	err := validateFromStdin(strings.NewReader("not json"), &out, &errb, "version")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateSchema_ValidationFails(t *testing.T) {
	input := `{"version": "0.3.1"}`
	var out, errb bytes.Buffer
	err := validateFromStdin(strings.NewReader(input), &out, &errb, "version")
	if err == nil {
		t.Fatal("expected validation failure")
	}
}

func TestValidateSchema_UnknownSchema(t *testing.T) {
	var out, errb bytes.Buffer
	err := validateFromStdin(strings.NewReader("{}"), &out, &errb, "nope")
	if err == nil {
		t.Fatal("expected unknown-schema error")
	}
}
