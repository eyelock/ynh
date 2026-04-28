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

	var env infoEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, stdout.String())
	}
	got := env.Harness

	if env.Capabilities == "" {
		t.Errorf("envelope missing capabilities; output: %s", stdout.String())
	}
	if got.Name != "test-harness" {
		t.Errorf("name = %q, want test-harness", got.Name)
	}
	if got.VersionInstalled != "2.0.0" {
		t.Errorf("version_installed = %q, want 2.0.0", got.VersionInstalled)
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

	var env infoEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, stdout.String())
	}
	if env.Harness.Name != "demo" {
		t.Errorf("name = %q, want demo", env.Harness.Name)
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

	var env infoEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := env.Harness

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

func TestCmdInfoTextRichHarness(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	hj := `{
		"name": "rich",
		"version": "2.0.0",
		"description": "Rich harness",
		"default_vendor": "claude",
		"includes": [
			{"git": "github.com/eyelock/assistants", "path": "skills/dev", "ref": "v2"}
		],
		"delegates_to": [
			{"git": "github.com/eyelock/delegate", "path": "harnesses/helper", "ref": "main"}
		],
		"hooks": {
			"before_tool": [{"command": "echo before", "matcher": "Write"}],
			"after_tool": [{"command": "echo after"}]
		},
		"mcp_servers": {
			"db-server": {"command": "db-mcp", "args": ["--port", "5432"]},
			"remote-api": {"url": "https://api.example.com/mcp"}
		},
		"profiles": {
			"staging": {
				"hooks": {"before_tool": [{"command": "echo staging"}]},
				"mcp_servers": {"staging-db": {"command": "staging-mcp"}}
			}
		},
		"focus": {
			"quick": {"prompt": "Be concise"},
			"review": {"profile": "staging", "prompt": "Review mode"}
		},
		"installed_from": {
			"source_type": "git",
			"source": "github.com/eyelock/rich",
			"path": "harnesses/rich",
			"registry_name": "eyelock",
			"installed_at": "2026-04-16T10:00:00Z"
		}
	}`
	installListTestHarness(t, home, "rich", hj)

	// Add artifacts
	skillDir := filepath.Join(home, "harnesses", "rich", "skills", "greet")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: greet\n---\nHello"), 0o644); err != nil {
		t.Fatal(err)
	}
	agentDir := filepath.Join(home, "harnesses", "rich", "agents")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "reviewer.md"), []byte("Review agent"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	if err := cmdInfoTo([]string{"rich"}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdInfoTo: %v", err)
	}

	out := stdout.String()

	// Identity
	if !strings.Contains(out, "rich") {
		t.Error("expected harness name")
	}
	if !strings.Contains(out, "claude") {
		t.Error("expected vendor")
	}

	// Provenance
	if !strings.Contains(out, "github.com/eyelock/rich") {
		t.Error("expected source in provenance")
	}
	if !strings.Contains(out, "harnesses/rich") {
		t.Error("expected path in provenance")
	}
	if !strings.Contains(out, "eyelock") {
		t.Error("expected registry name in provenance")
	}

	// Artifacts
	if !strings.Contains(out, "greet") {
		t.Error("expected skill name in artifacts")
	}
	if !strings.Contains(out, "reviewer") {
		t.Error("expected agent name in artifacts")
	}

	// Includes
	if !strings.Contains(out, "github.com/eyelock/assistants") {
		t.Error("expected include git URL")
	}
	if !strings.Contains(out, "path=skills/dev") {
		t.Error("expected include path")
	}
	if !strings.Contains(out, "ref=v2") {
		t.Error("expected include ref")
	}

	// Delegates
	if !strings.Contains(out, "github.com/eyelock/delegate") {
		t.Error("expected delegate git URL")
	}
	if !strings.Contains(out, "path=harnesses/helper") {
		t.Error("expected delegate path")
	}

	// Hooks
	if !strings.Contains(out, "before_tool") {
		t.Error("expected before_tool hook event")
	}
	if !strings.Contains(out, "echo before") {
		t.Error("expected hook command")
	}
	if !strings.Contains(out, "matcher=Write") {
		t.Error("expected hook matcher")
	}
	if !strings.Contains(out, "after_tool") {
		t.Error("expected after_tool hook event")
	}

	// MCP Servers
	if !strings.Contains(out, "db-server") {
		t.Error("expected MCP server name (command)")
	}
	if !strings.Contains(out, "db-mcp") {
		t.Error("expected MCP server command")
	}
	if !strings.Contains(out, "remote-api") {
		t.Error("expected MCP server name (URL)")
	}
	if !strings.Contains(out, "https://api.example.com/mcp") {
		t.Error("expected MCP server URL")
	}

	// Profiles
	if !strings.Contains(out, "staging") {
		t.Error("expected profile name")
	}

	// Focuses
	if !strings.Contains(out, "quick") {
		t.Error("expected focus name")
	}
	if !strings.Contains(out, "Be concise") {
		t.Error("expected focus prompt")
	}
	if !strings.Contains(out, "review") {
		t.Error("expected focus name review")
	}
	if !strings.Contains(out, "profile=staging") {
		t.Error("expected focus profile")
	}
}

func TestCmdInfoTextNoProvenance(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	installListTestHarness(t, home, "bare", `{"name": "bare", "version": "0.1.0"}`)

	var stdout bytes.Buffer
	if err := cmdInfoTo([]string{"bare"}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdInfoTo: %v", err)
	}

	out := stdout.String()
	// Should show dashes for missing provenance
	if !strings.Contains(out, "Installed:    -") {
		t.Error("expected dash for missing install date")
	}
	if !strings.Contains(out, "Source:       -") {
		t.Error("expected dash for missing source")
	}
	// Should show (none) for empty sections
	if !strings.Contains(out, "(none)") {
		t.Error("expected (none) for empty sections")
	}
}

func TestCmdInfoJSONNamespacedPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	installListTestHarnessNS(t, home, "eyelock/assistants", "planner",
		`{"name":"planner","version":"1.0.0","default_vendor":"claude"}`)

	var stdout bytes.Buffer
	if err := cmdInfoTo([]string{"planner", "--format", "json"}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdInfoTo: %v", err)
	}

	var env infoEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, stdout.String())
	}

	wantPath := filepath.Join(home, "harnesses", "eyelock--assistants", "planner")
	if env.Harness.Path != wantPath {
		t.Errorf("path = %q, want %q", env.Harness.Path, wantPath)
	}
	if env.Harness.Manifest == nil {
		t.Fatal("manifest is nil")
	}
}

func TestCmdInfoTextNoVendor(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	installListTestHarness(t, home, "no-vendor", `{"name": "no-vendor", "version": "0.1.0"}`)

	var stdout bytes.Buffer
	if err := cmdInfoTo([]string{"no-vendor"}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdInfoTo: %v", err)
	}

	if !strings.Contains(stdout.String(), "Vendor:       -") {
		t.Error("expected dash for missing vendor")
	}
}
