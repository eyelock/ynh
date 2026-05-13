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

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
)

// installListTestHarness creates a harness with custom JSON in the harnesses dir.
// installListTestHarness writes a schema-2 install at home/harnesses/local--<name>/
// and returns the canonical id ("local/<name>") that callers should pass to
// the cmdInfoTo / cmdListTo etc functions under test.
func installListTestHarness(t *testing.T, home, name, harnessJSON string) string {
	t.Helper()
	id := "local/" + name
	dir := harness.InstalledDirByID(id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".harness.json"), []byte(harnessJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	return id
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
	if got.SchemaVersion < 1 {
		t.Errorf("envelope schema_version = %d, want >= 1; output: %s", got.SchemaVersion, stdout.String())
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
	skillDir := filepath.Join(home, "harnesses", "local--test-harness", "skills", "greet")
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
	if e.Path != filepath.Join(home, "harnesses", "local--test-harness") {
		t.Errorf("path = %q, want %s", e.Path, filepath.Join(home, "harnesses", "local--test-harness"))
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

// installListTestHarnessNS creates a schema-2 namespaced install at
// home/harnesses/<host--ns--name>/ and returns the canonical id callers
// pass to commands. For namespaces without an explicit host (legacy
// "<org>/<repo>"), the id is "<ns>/<name>" — matches the previous
// schema-1 layout's host-stripped namespace key.
func installListTestHarnessNS(t *testing.T, home, ns, name, harnessJSON string) string {
	t.Helper()
	id := ns + "/" + name
	fsName := strings.ReplaceAll(id, "/", "--")
	dir := filepath.Join(home, "harnesses", fsName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".harness.json"), []byte(harnessJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestCmdListJSONNamespacedPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	installListTestHarnessNS(t, home, "eyelock/assistants", "planner",
		`{"name":"planner","version":"1.0.0","default_vendor":"claude",
		  "installed_from":{"source_type":"git","source":"github.com/eyelock/assistants"}}`)

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

	wantPath := filepath.Join(home, "harnesses", "eyelock--assistants--planner")
	if env.Harnesses[0].Path != wantPath {
		t.Errorf("path = %q, want %q", env.Harnesses[0].Path, wantPath)
	}
	if got, want := env.Harnesses[0].Namespace, "github.com/eyelock/assistants"; got != want {
		t.Errorf("namespace = %q, want %q", got, want)
	}
}

// TestCmdListJSONFlatInstallNoNamespace asserts that a flat (non-namespaced)
// install emits no namespace key — `omitempty` keeps the JSON envelope clean
// for local harnesses and matches the schema's "nil for flat installs" promise.
func TestCmdListJSONFlatInstallNoNamespace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	installListTestHarness(t, home, "alpha",
		`{"name":"alpha","version":"1.0.0","default_vendor":"claude"}`)

	var stdout bytes.Buffer
	if err := cmdListTo([]string{"--format", "json"}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdListTo: %v", err)
	}

	if strings.Contains(stdout.String(), `"namespace"`) {
		t.Errorf("flat install must not emit namespace key; got: %s", stdout.String())
	}

	var env listEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Harnesses[0].Namespace != "" {
		t.Errorf("namespace = %q, want empty", env.Harnesses[0].Namespace)
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

// Broken-pointer entries (source missing or plugin.json absent) must be emitted
// as kind "local-fork-broken" with a non-empty broken_reason so JSON consumers
// (e.g. TermQ) can route them to a QUARANTINED group instead of displaying
// them as valid but empty-field harnesses.
func TestCmdListJSON_BrokenPointerKind(t *testing.T) {
	t.Run("source_path_missing", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("YNH_HOME", home)

		if err := harness.SavePointer(&harness.Pointer{
			Name: "ghost",
			InstalledJSON: plugin.InstalledJSON{
				SourceType:  "local",
				Source:      filepath.Join(t.TempDir(), "gone"),
				InstalledAt: "2026-05-01T00:00:00Z",
			},
		}); err != nil {
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
			t.Fatalf("expected 1 harness, got %d; output: %s", len(got.Harnesses), stdout.String())
		}
		h := got.Harnesses[0]
		if h.Kind != "local-fork-broken" {
			t.Errorf("kind = %q, want local-fork-broken", h.Kind)
		}
		if h.BrokenReason == "" {
			t.Errorf("broken_reason is empty; expected non-empty reason")
		}
	})

	t.Run("plugin_json_missing", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("YNH_HOME", home)

		// Source dir exists but has no .ynh-plugin/plugin.json.
		srcDir := t.TempDir()
		if err := harness.SavePointer(&harness.Pointer{
			Name: "hollow",
			InstalledJSON: plugin.InstalledJSON{
				SourceType:  "local",
				Source:      srcDir,
				InstalledAt: "2026-05-01T00:00:00Z",
			},
		}); err != nil {
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
			t.Fatalf("expected 1 harness, got %d; output: %s", len(got.Harnesses), stdout.String())
		}
		h := got.Harnesses[0]
		if h.Kind != "local-fork-broken" {
			t.Errorf("kind = %q, want local-fork-broken", h.Kind)
		}
		if h.BrokenReason == "" {
			t.Errorf("broken_reason is empty; expected non-empty reason")
		}
	})
}

// Broken-pointer placeholder rows must emit [] not null for empty
// includes/delegates_to. JSON consumers expect the canonical empty-array shape
// regardless of whether the source tree could be loaded.
func TestCmdListJSONOrphanPointerEmptyArrays(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	if err := harness.SavePointer(&harness.Pointer{
		Name: "stranded",
		InstalledJSON: plugin.InstalledJSON{
			SourceType:  "local",
			Source:      filepath.Join(t.TempDir(), "gone"),
			InstalledAt: "2026-05-01T00:00:00Z",
		},
	}); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	if err := cmdListTo([]string{"--format", "json"}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdListTo: %v", err)
	}
	out := stdout.String()
	if strings.Contains(out, `"includes": null`) {
		t.Errorf("includes serialised as null; want []. output: %s", out)
	}
	if strings.Contains(out, `"delegates_to": null`) {
		t.Errorf("delegates_to serialised as null; want []. output: %s", out)
	}

	var got listEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, out)
	}
	if len(got.Harnesses) != 1 {
		t.Fatalf("expected 1 harness, got %d; output: %s", len(got.Harnesses), out)
	}
	if got.Harnesses[0].Includes == nil {
		t.Error("Includes is nil; want empty slice")
	}
	if got.Harnesses[0].DelegatesTo == nil {
		t.Error("DelegatesTo is nil; want empty slice")
	}
}

// writePluginTree creates a minimal schema-3 harness tree at dir with the
// given name and installed.json provenance. Used by the fork+registry
// regression tests below.
func writePluginTree(t *testing.T, dir, name string, ins *plugin.InstalledJSON) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, plugin.PluginDir), 0o755); err != nil {
		t.Fatal(err)
	}
	hj := `{"name":"` + name + `","version":"0.1.0","default_vendor":"claude"}`
	if err := os.WriteFile(filepath.Join(dir, plugin.PluginDir, plugin.PluginFile), []byte(hj), 0o644); err != nil {
		t.Fatal(err)
	}
	if ins != nil {
		if err := plugin.SaveInstalledJSON(dir, ins); err != nil {
			t.Fatal(err)
		}
	}
}

// TestCmdList_ForkAndRegistryWithSameLeafNameAreDistinct verifies that
// `ynh ls --format json` emits two entries with distinct canonical ids,
// distinct kinds, and distinct sources when a leaf name exists as both a
// local-fork pointer and a registry/tree-form install. Regression test for
// the list.go load shadowing bug introduced by the schema-3 read-symmetry
// change in PR #158, where the per-row load hard-coded "local/<name>" and
// shadowed the tree-form entry with the pointer-form harness.
func TestCmdList_ForkAndRegistryWithSameLeafNameAreDistinct(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	// Pointer-shaped local fork (local/termq-dev). Provenance carries
	// forked_from so classifyKind returns "local-fork".
	forkDir := filepath.Join(t.TempDir(), "termq-dev-fork")
	writePluginTree(t, forkDir, "termq-dev", &plugin.InstalledJSON{
		SourceType:  "local",
		Source:      forkDir,
		InstalledAt: "2026-05-01T00:00:00Z",
		ForkedFrom: &plugin.ForkedFromJSON{
			SourceType: "registry",
			Source:     "github.com/eyelock/assistants",
		},
	})
	if err := harness.SavePointer(&harness.Pointer{
		Name: "termq-dev",
		InstalledJSON: plugin.InstalledJSON{
			SourceType:  "local",
			Source:      forkDir,
			InstalledAt: "2026-05-01T00:00:00Z",
			ForkedFrom: &plugin.ForkedFromJSON{
				SourceType: "registry",
				Source:     "github.com/eyelock/assistants",
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Tree-form registry install at the schema-2 path. The dir name is
	// the id-fsname so ListAll classifies it as namespaced.
	registryDir := filepath.Join(home, "harnesses", "github.com--eyelock--assistants--termq-dev")
	writePluginTree(t, registryDir, "termq-dev", &plugin.InstalledJSON{
		SourceType:  "registry",
		Source:      "github.com/eyelock/assistants",
		InstalledAt: "2026-05-02T00:00:00Z",
	})

	var stdout bytes.Buffer
	if err := cmdListTo([]string{"--format", "json"}, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdListTo: %v", err)
	}
	var env listEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, stdout.String())
	}

	var rows []listEntry
	for _, e := range env.Harnesses {
		if e.Name == "termq-dev" {
			rows = append(rows, e)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 termq-dev rows, got %d; output: %s", len(rows), stdout.String())
	}

	idsByKind := map[string]string{}
	sourcesByKind := map[string]string{}
	for _, r := range rows {
		idsByKind[r.Kind] = r.ID
		if r.InstalledFrom != nil {
			sourcesByKind[r.Kind] = r.InstalledFrom.Source
		}
	}

	if idsByKind["local-fork"] != "local/termq-dev" {
		t.Errorf("local-fork id = %q, want local/termq-dev", idsByKind["local-fork"])
	}
	if idsByKind["registry"] != "github.com/eyelock/assistants/termq-dev" {
		t.Errorf("registry id = %q, want github.com/eyelock/assistants/termq-dev", idsByKind["registry"])
	}
	if sourcesByKind["local-fork"] == sourcesByKind["registry"] {
		t.Errorf("fork and registry rows share source %q; expected distinct sources", sourcesByKind["local-fork"])
	}
}

// TestCmdList_TextFormat_ForkAndRegistry mirrors the JSON regression test
// against the text printer. Two rows must appear with distinct KIND and
// SOURCE columns.
func TestCmdList_TextFormat_ForkAndRegistry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	forkDir := filepath.Join(t.TempDir(), "termq-dev-fork")
	writePluginTree(t, forkDir, "termq-dev", &plugin.InstalledJSON{
		SourceType:  "local",
		Source:      forkDir,
		InstalledAt: "2026-05-01T00:00:00Z",
		ForkedFrom: &plugin.ForkedFromJSON{
			SourceType: "registry",
			Source:     "github.com/eyelock/assistants",
		},
	})
	if err := harness.SavePointer(&harness.Pointer{
		Name: "termq-dev",
		InstalledJSON: plugin.InstalledJSON{
			SourceType:  "local",
			Source:      forkDir,
			InstalledAt: "2026-05-01T00:00:00Z",
			ForkedFrom: &plugin.ForkedFromJSON{
				SourceType: "registry",
				Source:     "github.com/eyelock/assistants",
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	registryDir := filepath.Join(home, "harnesses", "github.com--eyelock--assistants--termq-dev")
	writePluginTree(t, registryDir, "termq-dev", &plugin.InstalledJSON{
		SourceType:  "registry",
		Source:      "github.com/eyelock/assistants",
		InstalledAt: "2026-05-02T00:00:00Z",
	})

	var stdout bytes.Buffer
	if err := cmdListTo(nil, &stdout, io.Discard); err != nil {
		t.Fatalf("cmdListTo: %v", err)
	}
	out := stdout.String()

	var termqLines []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "termq-dev") {
			termqLines = append(termqLines, line)
		}
	}
	if len(termqLines) != 2 {
		t.Fatalf("expected 2 termq-dev rows, got %d; output:\n%s", len(termqLines), out)
	}
	if termqLines[0] == termqLines[1] {
		t.Errorf("fork and registry rows render identically:\n  %s\n  %s", termqLines[0], termqLines[1])
	}
	sawLocalFork := strings.Contains(termqLines[0], "local-fork") || strings.Contains(termqLines[1], "local-fork")
	sawRegistry := strings.Contains(termqLines[0], "registry") || strings.Contains(termqLines[1], "registry")
	if !sawLocalFork {
		t.Errorf("expected one row with KIND=local-fork; got:\n%s", out)
	}
	if !sawRegistry {
		t.Errorf("expected one row with KIND=registry; got:\n%s", out)
	}
}
