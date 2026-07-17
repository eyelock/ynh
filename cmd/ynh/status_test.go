package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/clischema"
)

func TestCmdStatus_TextEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	var out bytes.Buffer
	if err := cmdStatusTo(nil, &out, io.Discard); err != nil {
		t.Fatalf("cmdStatusTo: %v", err)
	}
	if !strings.Contains(out.String(), "No symlink installations") {
		t.Errorf("unexpected output: %s", out.String())
	}
}

func TestCmdStatus_JSONEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	var out bytes.Buffer
	if err := cmdStatusTo([]string{"--format", "json"}, &out, io.Discard); err != nil {
		t.Fatalf("cmdStatusTo: %v", err)
	}
	var got []any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\nraw: %s", err, out.String())
	}
	if len(got) != 0 {
		t.Errorf("expected empty array, got %d entries", len(got))
	}
}

func TestCmdStatus_JSONSchemaRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	var out bytes.Buffer
	if err := cmdStatusTo([]string{"--format", "json"}, &out, io.Discard); err != nil {
		t.Fatalf("cmdStatusTo: %v", err)
	}
	var v any
	if err := json.Unmarshal(out.Bytes(), &v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	schema, err := clischema.Get("status")
	if err != nil {
		t.Fatalf("Get status schema: %v", err)
	}
	if err := schema.Validate(v); err != nil {
		t.Errorf("status JSON does not validate: %v\noutput: %s", err, out.String())
	}
}

func TestCmdStatus_InvalidFormat(t *testing.T) {
	var out, errb bytes.Buffer
	err := cmdStatusTo([]string{"--format", "yaml"}, &out, &errb)
	if err == nil {
		t.Fatal("expected error")
	}
}
