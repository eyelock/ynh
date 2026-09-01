package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCmdValidateOutput_OK(t *testing.T) {
	input := `{"version": "0.3.1", "capabilities": "0.6.0"}`
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

// The path form is what makes this usable as a sensor over a project's own
// contracts rather than only over ynh's.
func TestValidateOutputSchemaByPath(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain.schema.json")
	if err := os.WriteFile(plain, []byte(`{"type":"object","required":["port"],
		"properties":{"port":{"type":"integer"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// A published contract usually carries an $id, and the compiler registers
	// by $id rather than by path when one is present.
	withID := filepath.Join(dir, "withid.schema.json")
	if err := os.WriteFile(withID, []byte(`{"$id":"https://example.com/c.json",
		"type":"object","required":["a"],"properties":{"a":{"type":"string"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		schema  string
		input   string
		wantErr bool
	}{
		{"path schema accepts a conforming document", plain, `{"port":8080}`, false},
		{"path schema rejects a wrong type", plain, `{"port":"8080"}`, true},
		{"path schema rejects a missing required field", plain, `{}`, true},
		{"schema with an $id still resolves", withID, `{"a":"x"}`, false},
		{"schema with an $id still rejects", withID, `{"a":1}`, true},
		{"published names keep working", "version", `{"version":"0.3.1","capabilities":"0.6.0"}`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			err := cmdValidateOutputTo([]string{"--schema", c.schema},
				strings.NewReader(c.input), &out, &errOut)
			if c.wantErr && err == nil {
				t.Errorf("expected a validation failure, got none (stdout %q)", out.String())
			}
			if !c.wantErr && err != nil {
				t.Errorf("unexpected error: %v (stderr %q)", err, errOut.String())
			}
		})
	}
}

// A name that is neither published nor a file must say so in terms of both.
// Reporting only "unknown schema" sent people looking for a typo when the
// real problem was a path that did not exist.
func TestValidateOutputUnknownSchemaNamesBothPossibilities(t *testing.T) {
	var out, errOut bytes.Buffer
	err := cmdValidateOutputTo([]string{"--schema", "definitely-not-here"},
		strings.NewReader(`{}`), &out, &errOut)
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"not a published schema name", "not a readable file", "published names:"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got: %v", want, err)
		}
	}
}

// A file that exists but is not a schema is a different failure from a file
// that is not there, and must not be reported as a validation failure of the
// document.
func TestValidateOutputRejectsUnparseableSchemaFile(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.schema.json")
	if err := os.WriteFile(bad, []byte(`not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	err := cmdValidateOutputTo([]string{"--schema", bad}, strings.NewReader(`{}`), &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "schema file") {
		t.Errorf("expected a schema-file error, got: %v", err)
	}
}
