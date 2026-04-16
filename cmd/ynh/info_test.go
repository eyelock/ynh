package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCmdInfoTextSuccess(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	installListTestHarness(t, home, "demo", `{
		"name": "demo",
		"version": "1.0.0",
		"description": "Demo harness",
		"default_vendor": "claude"
	}`)

	var stdout bytes.Buffer
	if err := cmdInfoTo([]string{"demo"}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdInfoTo: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "demo") {
		t.Errorf("expected name in output, got: %s", out)
	}
	if !strings.Contains(out, "claude") {
		t.Errorf("expected vendor in output, got: %s", out)
	}
}

func TestCmdInfoTextExplicit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	installListTestHarness(t, home, "demo", `{"name": "demo", "version": "0.1.0"}`)

	var defaultBuf, explicitBuf bytes.Buffer
	if err := cmdInfoTo([]string{"demo"}, &defaultBuf, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := cmdInfoTo([]string{"demo", "--format", "text"}, &explicitBuf, io.Discard); err != nil {
		t.Fatal(err)
	}
	if defaultBuf.String() != explicitBuf.String() {
		t.Errorf("default and --format text outputs differ")
	}
}

func TestCmdInfoJSONBasic(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	installListTestHarness(t, home, "test-harness", `{
		"name": "test-harness",
		"version": "2.0.0",
		"description": "A test harness",
		"default_vendor": "claude",
		"installed_from": {
			"source_type": "local",
			"source": "/tmp/test",
			"installed_at": "2026-04-16T10:00:00Z"
		}
	}`)

	var stdout bytes.Buffer
	if err := cmdInfoTo([]string{"test-harness", "--format", "json"}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdInfoTo: %v", err)
	}

	var got infoEntry
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, stdout.String())
	}

	if got.Name != "test-harness" {
		t.Errorf("name = %q, want test-harness", got.Name)
	}
	if got.Version != "2.0.0" {
		t.Errorf("version = %q, want 2.0.0", got.Version)
	}
	if got.Description != "A test harness" {
		t.Errorf("description = %q, want 'A test harness'", got.Description)
	}
	if got.DefaultVendor != "claude" {
		t.Errorf("default_vendor = %q, want claude", got.DefaultVendor)
	}
	if got.Path != filepath.Join(home, "harnesses", "test-harness") {
		t.Errorf("path = %q, want %s", got.Path, filepath.Join(home, "harnesses", "test-harness"))
	}

	// Provenance
	if got.InstalledFrom == nil {
		t.Fatal("installed_from is nil")
	}
	if got.InstalledFrom.SourceType != "local" {
		t.Errorf("source_type = %q, want local", got.InstalledFrom.SourceType)
	}
	if got.InstalledFrom.Source != "/tmp/test" {
		t.Errorf("source = %q, want /tmp/test", got.InstalledFrom.Source)
	}

	// Manifest must be present and valid JSON
	if got.Manifest == nil {
		t.Fatal("manifest is nil")
	}
	var manifest map[string]interface{}
	if err := json.Unmarshal(got.Manifest, &manifest); err != nil {
		t.Fatalf("manifest is not valid JSON: %v", err)
	}
	if manifest["name"] != "test-harness" {
		t.Errorf("manifest.name = %v, want test-harness", manifest["name"])
	}

	// Output must end with a newline
	if !strings.HasSuffix(stdout.String(), "\n") {
		t.Error("JSON output does not end with a newline")
	}
}

func TestCmdInfoJSONNoDescription(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	installListTestHarness(t, home, "bare", `{"name": "bare", "version": "0.1.0"}`)

	var stdout bytes.Buffer
	if err := cmdInfoTo([]string{"bare", "--format", "json"}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdInfoTo: %v", err)
	}

	// description should be omitted entirely
	if strings.Contains(stdout.String(), `"description"`) {
		t.Errorf("empty description should be omitted, got: %s", stdout.String())
	}
}

func TestCmdInfoJSONFormatBeforeName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	installListTestHarness(t, home, "demo", `{"name": "demo", "version": "0.1.0", "default_vendor": "claude"}`)

	// --format json before the harness name
	var stdout bytes.Buffer
	if err := cmdInfoTo([]string{"--format", "json", "demo"}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdInfoTo: %v", err)
	}

	var got infoEntry
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, stdout.String())
	}
	if got.Name != "demo" {
		t.Errorf("name = %q, want demo", got.Name)
	}
}

func TestCmdInfoJSONNotFound(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "harnesses"), 0o755); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := cmdInfoTo([]string{"nonexistent", "--format", "json"}, &stdout, &stderr)
	if !errors.Is(err, errStructuredReported) {
		t.Fatalf("expected errStructuredReported, got: %v", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout must be empty on error, got: %s", stdout.String())
	}

	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &env); err != nil {
		t.Fatalf("stderr is not valid JSON: %v\nraw: %s", err, stderr.String())
	}
	if env.Error.Code != errCodeNotFound {
		t.Errorf("expected code %q, got %q", errCodeNotFound, env.Error.Code)
	}
}

func TestCmdInfoNoArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := cmdInfoTo(nil, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error for no args")
	}
	if !strings.Contains(err.Error(), "usage") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCmdInfoUnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := cmdInfoTo([]string{"demo", "--nope"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
}

func TestCmdInfoInvalidFormat(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	var stdout, stderr bytes.Buffer
	err := cmdInfoTo([]string{"demo", "--format", "yaml"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
	if !strings.Contains(err.Error(), "yaml") {
		t.Errorf("error should mention the invalid value, got: %v", err)
	}
}

func TestCmdInfoJSONErrorEnvelope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	var stdout, stderr bytes.Buffer
	err := cmdInfoTo([]string{"--format", "json", "demo", "extra"}, &stdout, &stderr)
	if !errors.Is(err, errStructuredReported) {
		t.Fatalf("expected errStructuredReported, got: %v", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout must be empty on structured error, got: %s", stdout.String())
	}

	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &env); err != nil {
		t.Fatalf("stderr is not valid JSON: %v\nraw: %s", err, stderr.String())
	}
	if env.Error.Code != errCodeInvalidInput {
		t.Errorf("expected code %q, got %q", errCodeInvalidInput, env.Error.Code)
	}
}

func TestCmdInfoManifestPreservesAllFields(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	// Manifest with fields beyond what ynh normally parses
	hj := `{
		"name": "rich",
		"version": "3.0.0",
		"description": "Rich harness",
		"default_vendor": "claude",
		"includes": [
			{"git": "github.com/eyelock/assistants", "path": "skills/dev"}
		],
		"delegates_to": [
			{"git": "github.com/eyelock/delegate"}
		],
		"hooks": {
			"before_tool": [{"command": "echo hi"}]
		},
		"mcp_servers": {
			"test-server": {"command": "test-cmd"}
		}
	}`
	installListTestHarness(t, home, "rich", hj)

	var stdout bytes.Buffer
	if err := cmdInfoTo([]string{"rich", "--format", "json"}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdInfoTo: %v", err)
	}

	var got infoEntry
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// The manifest field should contain all the raw fields
	var manifest map[string]interface{}
	if err := json.Unmarshal(got.Manifest, &manifest); err != nil {
		t.Fatalf("manifest unmarshal: %v", err)
	}

	if _, ok := manifest["includes"]; !ok {
		t.Error("manifest should contain includes")
	}
	if _, ok := manifest["delegates_to"]; !ok {
		t.Error("manifest should contain delegates_to")
	}
	if _, ok := manifest["hooks"]; !ok {
		t.Error("manifest should contain hooks")
	}
	if _, ok := manifest["mcp_servers"]; !ok {
		t.Error("manifest should contain mcp_servers")
	}
}
