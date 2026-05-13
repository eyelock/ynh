package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestCmdSchema_PrintsByName(t *testing.T) {
	var out bytes.Buffer
	if err := cmdSchemaTo([]string{"version"}, &out, io.Discard); err != nil {
		t.Fatalf("cmdSchemaTo: %v", err)
	}
	if !strings.Contains(out.String(), `"$id"`) {
		t.Errorf("output should contain $id, got: %s", out.String())
	}
}

func TestCmdSchema_UnknownName(t *testing.T) {
	var out, errb bytes.Buffer
	err := cmdSchemaTo([]string{"--format", "json", "nope"}, &out, &errb)
	if err == nil {
		t.Fatal("expected error for unknown schema")
	}
}

func TestCmdSchema_AllManifest(t *testing.T) {
	var out bytes.Buffer
	if err := cmdSchemaTo([]string{"--all", "--format", "json"}, &out, io.Discard); err != nil {
		t.Fatalf("cmdSchemaTo: %v", err)
	}
	var manifest struct {
		Capabilities string                     `json:"capabilities"`
		YnhVersion   string                     `json:"ynh_version"`
		Schemas      map[string]json.RawMessage `json:"schemas"`
	}
	if err := json.Unmarshal(out.Bytes(), &manifest); err != nil {
		t.Fatalf("unmarshal manifest: %v\noutput: %s", err, out.String())
	}
	if manifest.Capabilities == "" {
		t.Error("manifest missing capabilities")
	}
	// Expect at least the high-value command schemas.
	for _, want := range []string{"cli/version", "cli/list", "cli/info", "cli/error"} {
		if _, ok := manifest.Schemas[want]; !ok {
			t.Errorf("manifest missing %q", want)
		}
	}
}

func TestCmdSchema_AllText(t *testing.T) {
	var out bytes.Buffer
	if err := cmdSchemaTo([]string{"--all"}, &out, io.Discard); err != nil {
		t.Fatalf("cmdSchemaTo: %v", err)
	}
	if !strings.Contains(out.String(), "cli/version") {
		t.Errorf("text manifest should list cli/version, got: %s", out.String())
	}
}

func TestCmdSchema_NoArgs(t *testing.T) {
	var out, errb bytes.Buffer
	err := cmdSchemaTo(nil, &out, &errb)
	if err == nil {
		t.Fatal("expected error for no args")
	}
}
