package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestCmdValidateOutput_OK(t *testing.T) {
	input := `{"version": "0.3.1", "capabilities": "0.4.0"}`
	var out, errb bytes.Buffer
	err := cmdValidateOutputTo([]string{"--schema", "version"}, strings.NewReader(input), &out, &errb)
	if err != nil {
		t.Fatalf("validate: %v (stderr: %s)", err, errb.String())
	}
	if !strings.Contains(out.String(), "ok") {
		t.Errorf("expected 'ok', got: %s", out.String())
	}
}

func TestCmdValidateOutput_BadJSON(t *testing.T) {
	var out, errb bytes.Buffer
	err := cmdValidateOutputTo([]string{"--schema", "version"}, strings.NewReader("not json"), &out, &errb)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCmdValidateOutput_ValidationFails(t *testing.T) {
	// Missing required "capabilities" field.
	input := `{"version": "0.3.1"}`
	var out, errb bytes.Buffer
	err := cmdValidateOutputTo([]string{"--schema", "version"}, strings.NewReader(input), &out, &errb)
	if err == nil {
		t.Fatal("expected validation failure")
	}
}

func TestCmdValidateOutput_UnknownSchema(t *testing.T) {
	var out, errb bytes.Buffer
	err := cmdValidateOutputTo([]string{"--schema", "nope"}, strings.NewReader("{}"), &out, &errb)
	if err == nil {
		t.Fatal("expected unknown-schema error")
	}
}

func TestCmdValidateOutput_NoSchemaFlag(t *testing.T) {
	var out, errb bytes.Buffer
	err := cmdValidateOutputTo(nil, strings.NewReader("{}"), &out, &errb)
	if err == nil {
		t.Fatal("expected usage error")
	}
}

func TestCmdValidateOutput_StdinFromFile(t *testing.T) {
	// Same case as OK but verifying we accept any io.Reader.
	input := `{"version": "x", "capabilities": "y"}`
	var out, errb bytes.Buffer
	if err := cmdValidateOutputTo([]string{"--schema", "version"}, strings.NewReader(input), &out, &errb); err != nil {
		t.Fatalf("validate: %v", err)
	}
	_ = io.Discard
}
