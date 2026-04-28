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

// installListTestHarness creates a harness with custom JSON in the harnesses dir.
func installListTestHarness(t *testing.T, home, name, harnessJSON string) {
	t.Helper()
	dir := filepath.Join(home, "harnesses", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".harness.json"), []byte(harnessJSON), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCmdListTextEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "harnesses"), 0o755); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	if err := cmdListTo(nil, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdListTo: %v", err)
	}

	if !strings.Contains(stdout.String(), "No harnesses installed") {
		t.Errorf("expected empty message, got: %s", stdout.String())
	}
}

func TestCmdListTextPopulated(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	installListTestHarness(t, home, "demo", `{
		"name": "demo",
		"version": "0.1.0",
		"description": "Demo harness",
		"default_vendor": "claude"
	}`)

	var stdout bytes.Buffer
	if err := cmdListTo(nil, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdListTo: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "demo") {
		t.Errorf("expected harness name in output, got: %s", out)
	}
	if !strings.Contains(out, "claude") {
		t.Errorf("expected vendor in output, got: %s", out)
	}
}

func TestCmdListJSONEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "harnesses"), 0o755); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	if err := cmdListTo([]string{"--format", "json"}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdListTo: %v", err)
	}

	var got listEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, stdout.String())
	}
	if got.Capabilities == "" {
		t.Errorf("envelope missing capabilities; output: %s", stdout.String())
	}
	if got.Harnesses == nil {
		t.Errorf("envelope harnesses is null, expected []; output: %s", stdout.String())
	}
	if len(got.Harnesses) != 0 {
		t.Errorf("expected empty harnesses, got %d", len(got.Harnesses))
	}
	if !strings.Contains(stdout.String(), `"harnesses": []`) {
		t.Errorf("expected literal \"harnesses\": [], got: %s", stdout.String())
	}
}

func TestCmdListJSONPopulated(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	hj := `{
		"name": "test-harness",
		"version": "1.2.3",
		"description": "A test harness",
		"default_vendor": "claude",
		"includes": [
			{"git": "github.com/eyelock/assistants", "path": "skills/dev", "pick": ["go", "testing"]}
		],
		"delegates_to": [
			{"git": "github.com/eyelock/delegate", "path": "harnesses/helper"}
		],
		"installed_from": {
			"source_type": "local",
			"source": "/tmp/test",
			"installed_at": "2026-04-15T12:00:00Z"
		}
	}`
	installListTestHarness(t, home, "test-harness", hj)

	// Add a skill artifact
	skillDir := filepath.Join(home, "harnesses", "test-harness", "skills", "greet")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: greet\n---\nHello"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	if err := cmdListTo([]string{"--format", "json"}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdListTo: %v", err)
	}

	var got listEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, stdout.String())
	}

	if len(got.Harnesses) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(got.Harnesses))
	}

	e := got.Harnesses[0]
	if e.Name != "test-harness" {
		t.Errorf("name = %q, want test-harness", e.Name)
	}
	if e.VersionInstalled != "1.2.3" {
		t.Errorf("version_installed = %q, want 1.2.3", e.VersionInstalled)
	}
	if e.Description != "A test harness" {
		t.Errorf("description = %q, want 'A test harness'", e.Description)
	}
	if e.DefaultVendor != "claude" {
		t.Errorf("default_vendor = %q, want claude", e.DefaultVendor)
	}
	if e.Path != filepath.Join(home, "harnesses", "test-harness") {
		t.Errorf("path = %q, want %s", e.Path, filepath.Join(home, "harnesses", "test-harness"))
	}

	// Provenance
	if e.InstalledFrom == nil {
		t.Fatal("installed_from is nil")
	}
	if e.InstalledFrom.SourceType != "local" {
		t.Errorf("source_type = %q, want local", e.InstalledFrom.SourceType)
	}
	if e.InstalledFrom.Source != "/tmp/test" {
		t.Errorf("source = %q, want /tmp/test", e.InstalledFrom.Source)
	}
	if e.InstalledFrom.InstalledAt != "2026-04-15T12:00:00Z" {
		t.Errorf("installed_at = %q, want 2026-04-15T12:00:00Z", e.InstalledFrom.InstalledAt)
	}

	// Artifacts
	if e.Artifacts.Skills != 1 {
		t.Errorf("artifacts.skills = %d, want 1", e.Artifacts.Skills)
	}
	if e.Artifacts.Agents != 0 {
		t.Errorf("artifacts.agents = %d, want 0", e.Artifacts.Agents)
	}

	// Includes
	if len(e.Includes) != 1 {
		t.Fatalf("includes: got %d, want 1", len(e.Includes))
	}
	if e.Includes[0].Git != "github.com/eyelock/assistants" {
		t.Errorf("include git = %q", e.Includes[0].Git)
	}
	if e.Includes[0].Path != "skills/dev" {
		t.Errorf("include path = %q", e.Includes[0].Path)
	}
	if len(e.Includes[0].Pick) != 2 {
		t.Errorf("include pick = %v, want [go testing]", e.Includes[0].Pick)
	}

	// Delegates
	if len(e.DelegatesTo) != 1 {
		t.Fatalf("delegates_to: got %d, want 1", len(e.DelegatesTo))
	}
	if e.DelegatesTo[0].Git != "github.com/eyelock/delegate" {
		t.Errorf("delegate git = %q", e.DelegatesTo[0].Git)
	}

	// Output must end with a newline
	if !strings.HasSuffix(stdout.String(), "\n") {
		t.Error("JSON output does not end with a newline")
	}
}

func TestCmdListJSONNoDescription(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	installListTestHarness(t, home, "bare", `{"name": "bare", "version": "0.1.0"}`)

	var stdout bytes.Buffer
	if err := cmdListTo([]string{"--format", "json"}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdListTo: %v", err)
	}

	// description should be omitted entirely, not empty string
	if strings.Contains(stdout.String(), `"description"`) {
		t.Errorf("empty description should be omitted, got: %s", stdout.String())
	}

	// includes and delegates_to should be present as empty arrays
	if !strings.Contains(stdout.String(), `"includes": []`) {
		t.Errorf("expected includes: [], got: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"delegates_to": []`) {
		t.Errorf("expected delegates_to: [], got: %s", stdout.String())
	}
}

func TestCmdListExplicitText(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "harnesses"), 0o755); err != nil {
		t.Fatal(err)
	}

	var defaultBuf, explicitBuf bytes.Buffer
	if err := cmdListTo(nil, &defaultBuf, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := cmdListTo([]string{"--format", "text"}, &explicitBuf, io.Discard); err != nil {
		t.Fatal(err)
	}
	if defaultBuf.String() != explicitBuf.String() {
		t.Errorf("default and --format text outputs differ")
	}
}

func TestCmdListInvalidFormat(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	var stdout, stderr bytes.Buffer
	err := cmdListTo([]string{"--format", "yaml"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
	if !strings.Contains(err.Error(), "yaml") {
		t.Errorf("error should mention the invalid value, got: %v", err)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr should be empty in text mode, got: %s", stderr.String())
	}
}

func TestCmdListInvalidFormatJSONEnvelope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	var stdout, stderr bytes.Buffer
	err := cmdListTo([]string{"--format", "json", "extra"}, &stdout, &stderr)
	if !errors.Is(err, errStructuredReported) {
		t.Fatalf("expected errStructuredReported, got: %v", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout must be empty on structured error, got: %s", stdout.String())
	}

	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &env); err != nil {
		t.Fatalf("stderr is not valid JSON envelope: %v\nraw: %s", err, stderr.String())
	}
	if env.Error.Code != errCodeInvalidInput {
		t.Errorf("expected code %q, got %q", errCodeInvalidInput, env.Error.Code)
	}
}

func TestCmdListUnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := cmdListTo([]string{"--nope"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
}

func TestCmdListMissingFormatValue(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := cmdListTo([]string{"--format"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error for missing --format value")
	}
}

// installListTestHarnessNS creates a harness under a namespace subdirectory,
// mirroring what `ynh install` does for registry harnesses.
func installListTestHarnessNS(t *testing.T, home, ns, name, harnessJSON string) {
	t.Helper()
	fsNS := strings.ReplaceAll(ns, "/", "--")
	dir := filepath.Join(home, "harnesses", fsNS, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".harness.json"), []byte(harnessJSON), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCmdListJSONNamespacedPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	installListTestHarnessNS(t, home, "eyelock/assistants", "planner",
		`{"name":"planner","version":"1.0.0","default_vendor":"claude"}`)

	var stdout bytes.Buffer
	if err := cmdListTo([]string{"--format", "json"}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdListTo: %v", err)
	}

	var env listEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, stdout.String())
	}
	if len(env.Harnesses) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(env.Harnesses))
	}

	wantPath := filepath.Join(home, "harnesses", "eyelock--assistants", "planner")
	if env.Harnesses[0].Path != wantPath {
		t.Errorf("path = %q, want %q", env.Harnesses[0].Path, wantPath)
	}
}

func TestCmdListMultipleHarnesses(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	installListTestHarness(t, home, "alpha", `{"name": "alpha", "version": "1.0.0", "default_vendor": "claude"}`)
	installListTestHarness(t, home, "beta", `{"name": "beta", "version": "2.0.0", "default_vendor": "codex"}`)

	var stdout bytes.Buffer
	if err := cmdListTo([]string{"--format", "json"}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdListTo: %v", err)
	}

	var got listEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Harnesses) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got.Harnesses))
	}

	// Entries are sorted by name (harness.List reads directory entries)
	names := []string{got.Harnesses[0].Name, got.Harnesses[1].Name}
	if names[0] != "alpha" || names[1] != "beta" {
		t.Errorf("names = %v, want [alpha beta]", names)
	}
	if got.Harnesses[1].DefaultVendor != "codex" {
		t.Errorf("beta vendor = %q, want codex", got.Harnesses[1].DefaultVendor)
	}
}

func TestCmdListJSONEnvelopeShape(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "harnesses"), 0o755); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	if err := cmdListTo([]string{"--format", "json"}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdListTo: %v", err)
	}

	out := stdout.String()
	for _, key := range []string{`"capabilities"`, `"ynh_version"`, `"harnesses"`} {
		if !strings.Contains(out, key) {
			t.Errorf("envelope missing %s; output: %s", key, out)
		}
	}
}

func TestCmdListJSONIsPinnedSemantics(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	// Include with a SHA-style ref → pinned. Include with a tag-style ref → floating.
	hj := `{
		"name": "demo",
		"version": "1.0.0",
		"default_vendor": "claude",
		"includes": [
			{"git": "github.com/example/a", "ref": "abc1234567890abcdef1234567890abcdef12345"},
			{"git": "github.com/example/b", "ref": "v1.2.3"},
			{"git": "github.com/example/c", "ref": "main"}
		]
	}`
	installListTestHarness(t, home, "demo", hj)

	var stdout bytes.Buffer
	if err := cmdListTo([]string{"--format", "json"}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdListTo: %v", err)
	}

	var env listEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, stdout.String())
	}
	if len(env.Harnesses) != 1 {
		t.Fatalf("expected 1 harness, got %d", len(env.Harnesses))
	}
	incs := env.Harnesses[0].Includes
	if len(incs) != 3 {
		t.Fatalf("expected 3 includes, got %d", len(incs))
	}
	if !incs[0].IsPinned {
		t.Errorf("SHA ref %q must be pinned", incs[0].RefInstalled)
	}
	if incs[1].IsPinned {
		t.Errorf("tag ref %q must not be pinned", incs[1].RefInstalled)
	}
	if incs[2].IsPinned {
		t.Errorf("branch ref %q must not be pinned", incs[2].RefInstalled)
	}
}

func TestCmdListCheckUpdatesFlagAccepted(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "harnesses"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Network probes are not yet wired in this PR — the flag must be accepted
	// but version_available / ref_available must remain omitted (the "unknown"
	// state of the three-state contract).
	var stdout bytes.Buffer
	if err := cmdListTo([]string{"--format", "json", "--check-updates"}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdListTo: %v", err)
	}
	for _, forbidden := range []string{`"version_available"`, `"ref_available"`} {
		if strings.Contains(stdout.String(), forbidden) {
			t.Errorf("output must omit %s until network probes land; got: %s", forbidden, stdout.String())
		}
	}
}

func TestCmdListCheckUpdatesRequiresJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "harnesses"), 0o755); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := cmdListTo([]string{"--check-updates"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error for --check-updates without --format json")
	}
	if !strings.Contains(stderr.String()+err.Error(), "--check-updates") {
		t.Errorf("expected error to mention --check-updates; stderr: %s; err: %v", stderr.String(), err)
	}
}
