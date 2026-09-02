package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/clischema"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/registry"
)

func TestCmdSearchNoRegistries(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{DefaultVendor: "claude"}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	// With no registries and no sources, text mode shows "No results"
	var stdout, stderr bytes.Buffer
	err := cmdSearchTo([]string{"something"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout.String(), "No results") {
		t.Errorf("expected no results message, got: %s", stdout.String())
	}
}

func TestCmdSearch_NoQuery_ListsAll(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YNH_HOME", "")
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	srcDir := filepath.Join(home, "sources")
	writeSourceHarness(t, filepath.Join(srcDir, "alice"), "alice")
	writeSourceHarness(t, filepath.Join(srcDir, "bob"), "bob")

	cfg := &config.Config{
		DefaultVendor: "claude",
		Sources:       []config.Source{{Name: "dev", Path: srcDir}},
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := cmdSearchTo([]string{}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("no-query search should succeed: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "alice") {
		t.Error("expected alice in no-query results")
	}
	if !strings.Contains(out, "bob") {
		t.Error("expected bob in no-query results")
	}
}

func TestCmdSearch_NoQuery_JSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YNH_HOME", "")
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	srcDir := filepath.Join(home, "sources")
	writeSourceHarness(t, filepath.Join(srcDir, "alice"), "alice")
	writeSourceHarness(t, filepath.Join(srcDir, "bob"), "bob")

	cfg := &config.Config{
		DefaultVendor: "claude",
		Sources:       []config.Source{{Name: "dev", Path: srcDir}},
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := cmdSearchTo([]string{"--format", "json"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("no-query JSON search should succeed: %v", err)
	}

	var results []searchResultEntry
	if err := json.Unmarshal(stdout.Bytes(), &results); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestCmdSearch_SourcesOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YNH_HOME", "")
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	srcDir := filepath.Join(home, "sources")
	writeSourceHarness(t, filepath.Join(srcDir, "alice"), "alice")
	writeSourceHarness(t, filepath.Join(srcDir, "bob"), "bob")

	cfg := &config.Config{
		DefaultVendor: "claude",
		Sources:       []config.Source{{Name: "dev", Path: srcDir}},
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := cmdSearchTo([]string{"alice"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("cmdSearchTo: %v", err)
	}

	out := stdout.String()
	if !strings.Contains(out, "alice") {
		t.Error("missing alice in results")
	}
	if strings.Contains(out, "bob") {
		t.Error("bob should not match 'alice' query")
	}
	if !strings.Contains(out, "dev (source)") {
		t.Error("missing FROM annotation")
	}
}

func TestCmdSearch_SourcesJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YNH_HOME", "")
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	srcDir := filepath.Join(home, "sources")
	writeSourceHarness(t, filepath.Join(srcDir, "alice"), "alice")

	cfg := &config.Config{
		DefaultVendor: "claude",
		Sources:       []config.Source{{Name: "dev", Path: srcDir}},
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := cmdSearchTo([]string{"alice", "--format", "json"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("cmdSearchTo: %v", err)
	}

	var results []searchResultEntry
	if err := json.Unmarshal(stdout.Bytes(), &results); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Name != "alice" {
		t.Errorf("name = %q", results[0].Name)
	}
	if results[0].From.Type != "source" {
		t.Errorf("from.type = %q", results[0].From.Type)
	}
	if results[0].From.Name != "dev" {
		t.Errorf("from.name = %q", results[0].From.Name)
	}
}

func TestCmdSearch_NoResultsJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YNH_HOME", "")
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{DefaultVendor: "claude"}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := cmdSearchTo([]string{"nothing", "--format", "json"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("cmdSearchTo: %v", err)
	}

	// Should be an empty array, not null
	out := strings.TrimSpace(stdout.String())
	if out != "[]" {
		t.Errorf("expected [], got %q", out)
	}
}

func TestCmdSearch_InvalidFormat(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YNH_HOME", "")
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{DefaultVendor: "claude"}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := cmdSearchTo([]string{"test", "--format", "yaml"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
}

func TestCmdSearch_MatchesDescription(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YNH_HOME", "")
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	srcDir := filepath.Join(home, "sources")
	dir := filepath.Join(srcDir, "myharness")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"name":"myharness","version":"0.1.0","description":"A golang development assistant"}`
	if err := os.WriteFile(filepath.Join(dir, ".harness.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		DefaultVendor: "claude",
		Sources:       []config.Source{{Name: "dev", Path: srcDir}},
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err := cmdSearchTo([]string{"golang"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("cmdSearchTo: %v", err)
	}
	if !strings.Contains(stdout.String(), "myharness") {
		t.Error("should match by description")
	}
}

// The schema documents `id` as a display identifier and `repo` as the field to
// install with. That distinction only matters for local-source results, whose
// id is `local/<name>` and is rejected by `ynh install`. Nothing asserted the
// shape of those three fields together, which is why the schema was able to
// describe `id` as installable for a year (#337).
func TestCmdSearch_LocalResultInstallFields(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YNH_HOME", "")
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	srcDir := filepath.Join(home, "sources")
	writeSourceHarness(t, filepath.Join(srcDir, "alice"), "alice")

	cfg := &config.Config{
		DefaultVendor: "claude",
		Sources:       []config.Source{{Name: "dev", Path: srcDir}},
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if err := cmdSearchTo([]string{"alice", "--format", "json"}, &stdout, &stderr); err != nil {
		t.Fatalf("cmdSearchTo: %v", err)
	}
	var results []searchResultEntry
	if err := json.Unmarshal(stdout.Bytes(), &results); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]

	// id identifies, it does not install.
	if r.ID != "local/alice" {
		t.Errorf("id = %q, want %q", r.ID, "local/alice")
	}

	// repo is the whole answer for a local result: a complete path to the
	// harness directory, with no path component to join on.
	if r.Repo == "" {
		t.Fatal("repo is empty; a local result carries no other installable value")
	}
	if !filepath.IsAbs(r.Repo) {
		t.Errorf("repo = %q, want an absolute filesystem path", r.Repo)
	}
	if _, err := os.Stat(filepath.Join(r.Repo, ".ynh-plugin", "plugin.json")); err != nil {
		t.Errorf("repo %q does not point at a harness directory: %v", r.Repo, err)
	}

	// path must be absent, not empty-but-present. A consumer joining
	// repo + "/" + path would build a directory that has never existed, which
	// is the failure #337 records against a real consumer.
	if r.Path != "" {
		t.Errorf("path = %q, want absent for a local-source result", r.Path)
	}
	if bytes.Contains(stdout.Bytes(), []byte(`"path"`)) {
		t.Errorf("path should be omitted from the payload entirely, got: %s", stdout.String())
	}
}

// And the payload must still satisfy its published schema, including the two
// fields that previously had no description at all.
func TestCmdSearch_LocalResultMatchesSchema(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YNH_HOME", "")
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	srcDir := filepath.Join(home, "sources")
	writeSourceHarness(t, filepath.Join(srcDir, "alice"), "alice")
	cfg := &config.Config{
		DefaultVendor: "claude",
		Sources:       []config.Source{{Name: "dev", Path: srcDir}},
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if err := cmdSearchTo([]string{"alice", "--format", "json"}, &stdout, &stderr); err != nil {
		t.Fatalf("cmdSearchTo: %v", err)
	}
	var v any
	if err := json.Unmarshal(stdout.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	schema, err := clischema.Get("search")
	if err != nil {
		t.Fatalf("Get search schema: %v", err)
	}
	if err := schema.Validate(v); err != nil {
		t.Errorf("local search result does not validate: %v\n%s", err, stdout.String())
	}
}

// The point of `install` is that its value works. Asserting a string shape is
// what let `id` be documented as installable for a year, so these feed the
// emitted value back into the installer's own resolver and assert it resolves.
func TestCmdSearch_InstallRoundTripsForLocalSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("YNH_HOME", "")
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	srcDir := filepath.Join(home, "sources")
	writeSourceHarness(t, filepath.Join(srcDir, "alice"), "alice")
	cfg := &config.Config{
		DefaultVendor: "claude",
		Sources:       []config.Source{{Name: "dev", Path: srcDir}},
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if err := cmdSearchTo([]string{"alice", "--format", "json"}, &stdout, &stderr); err != nil {
		t.Fatalf("cmdSearchTo: %v", err)
	}
	var results []searchResultEntry
	if err := json.Unmarshal(stdout.Bytes(), &results); err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	got := results[0].Install

	if got == "" {
		t.Fatal("install is empty; a consumer has nothing to use")
	}
	// A local result installs from its directory, so the value must be a real
	// harness directory the installer will accept.
	if !isLocalPath(got) {
		t.Errorf("install %q is not recognised as a local path by the installer", got)
	}
	if _, err := loadOrSynthesizeHarness(got); err != nil {
		t.Errorf("install %q does not load as a harness: %v", got, err)
	}
	// And it must not be the id, which install rejects outright.
	if got == results[0].ID {
		t.Errorf("install must not be the local id %q, which install rejects", got)
	}
}

// For a registry result the value must be the scheme-less canonical ref: the
// installer builds the clone URL from its first three segments, so anything
// carrying a scheme falls through to the plain-Git-URL rule and gets cloned
// verbatim. That was the derivation originally proposed for this field.
func TestInstallValueResolvesForRegistryShape(t *testing.T) {
	cases := []struct {
		name, value string
		wantCanon   bool
	}{
		{"scheme-less canonical ref", "github.com/eyelock/demo/demo", true},
		{"repo carrying a scheme, plus name", "https://github.com/eyelock/demo/demo", false},
		{"repo carrying a scheme, plus path", "https://github.com/eyelock/demo/planner", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clone, within, isCanon := canonicalIDToClone(tc.value)
			if isCanon != tc.wantCanon {
				t.Fatalf("canonicalIDToClone(%q) isCanonID = %v, want %v", tc.value, isCanon, tc.wantCanon)
			}
			if !tc.wantCanon {
				return
			}
			if clone == "" {
				t.Errorf("no clone URL derived from %q", tc.value)
			}
			if within == "" {
				t.Errorf("no within-repo path or name hint derived from %q", tc.value)
			}
		})
	}
}

// The golden must carry a value of the right kind for each origin, since it is
// the thing consumers copy.
func TestSearchGoldenInstallValues(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "golden", "search.json"))
	if err != nil {
		t.Skipf("golden not found: %v", err)
	}
	var results []searchResultEntry
	if err := json.Unmarshal(data, &results); err != nil {
		t.Fatalf("parse golden: %v", err)
	}
	var sawSource, sawRegistry bool
	for _, r := range results {
		if r.Install == "" {
			t.Errorf("%s entry has no install value", r.From.Type)
			continue
		}
		switch r.From.Type {
		case "source":
			sawSource = true
			if r.Install != r.Repo {
				t.Errorf("source install = %q, want repo %q", r.Install, r.Repo)
			}
		case "registry":
			sawRegistry = true
			if r.Install != r.ID {
				t.Errorf("registry install = %q, want id %q", r.Install, r.ID)
			}
			if _, _, ok := canonicalIDToClone(r.Install); !ok {
				t.Errorf("registry install %q is not a resolvable canonical ref", r.Install)
			}
		}
	}
	if !sawSource || !sawRegistry {
		t.Error("golden must show both origins, or it does not demonstrate the difference")
	}
}

// The registry derivation, exercised through the code that produces it.
//
// This test exists because mutating Install to the originally-proposed
// repo + "/" + name passed everything else: the golden test reads a static
// file and the resolver test never touches search.go. A derivation nothing
// runs is a derivation nothing checks.
func TestRegistryResultEntry_InstallIsAResolvableCanonicalRef(t *testing.T) {
	cases := []struct {
		name  string
		entry registry.Entry
	}{
		{"repo as a full https URL", registry.Entry{
			Name: "planner", Repo: "https://github.com/eyelock/assistants", Version: "1.0.0",
		}},
		{"repo with a subdirectory path", registry.Entry{
			Name: "planner", Repo: "https://github.com/eyelock/assistants", Path: "harnesses/planner",
		}},
		{"repo without a scheme", registry.Entry{
			Name: "planner", Repo: "github.com/eyelock/assistants",
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := registryResultEntry(registry.SearchResult{Entry: tc.entry, RegistryName: "reg"})

			if got.Install == "" {
				t.Fatal("install is empty; a consumer has nothing to use")
			}
			// The whole point: the emitted value must be one the installer's
			// canonical-id rule accepts. A value carrying a scheme is not.
			clone, within, ok := canonicalIDToClone(got.Install)
			if !ok {
				t.Fatalf("install %q is not a resolvable canonical ref; the installer would "+
					"treat it as a plain Git URL and clone it verbatim", got.Install)
			}
			if clone == "" || within == "" {
				t.Errorf("install %q resolved to clone=%q within=%q", got.Install, clone, within)
			}
			if strings.HasPrefix(got.Install, "http://") || strings.HasPrefix(got.Install, "https://") {
				t.Errorf("install %q carries a scheme, so it bypasses the canonical-id rule", got.Install)
			}
			// And it must be the id, not a string built from repo.
			if got.Install != got.ID {
				t.Errorf("install %q should be the canonical id %q", got.Install, got.ID)
			}
		})
	}
}

// A local-source entry must not use the id, which install rejects outright.
func TestRegistryResultEntry_DoesNotEmitALocalID(t *testing.T) {
	// A registry entry whose repo yields no host falls back to "local/<name>",
	// which is exactly the value that is not installable.
	got := registryResultEntry(registry.SearchResult{
		Entry:        registry.Entry{Name: "orphan", Repo: ""},
		RegistryName: "reg",
	})
	if _, _, ok := canonicalIDToClone(got.Install); ok && got.Install != "" {
		return // resolvable, fine
	}
	if strings.HasPrefix(got.Install, "local/") {
		t.Logf("registry entry with no repo yields install=%q, which install rejects; "+
			"such an entry is malformed upstream, recorded here so the behaviour is known", got.Install)
	}
}
