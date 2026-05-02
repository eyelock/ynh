//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// installedJSONShape mirrors internal/plugin.InstalledJSON for the
// fields the suite asserts on. Kept as a local type so the test does
// not import internal/* (treats the binary as a black box).
type installedJSONShape struct {
	SourceType   string                `json:"source_type"`
	Source       string                `json:"source"`
	Ref          string                `json:"ref,omitempty"`
	SHA          string                `json:"sha,omitempty"`
	Path         string                `json:"path,omitempty"`
	Namespace    string                `json:"namespace,omitempty"`
	RegistryName string                `json:"registry_name,omitempty"`
	InstalledAt  string                `json:"installed_at"`
	ForkedFrom   *forkedFromShape      `json:"forked_from,omitempty"`
	Resolved     []resolvedSourceShape `json:"resolved,omitempty"`
}

type resolvedSourceShape struct {
	Git  string `json:"git"`
	Ref  string `json:"ref,omitempty"`
	Path string `json:"path,omitempty"`
	SHA  string `json:"sha"`
}

type forkedFromShape struct {
	SourceType   string `json:"source_type"`
	Source       string `json:"source"`
	Ref          string `json:"ref,omitempty"`
	SHA          string `json:"sha,omitempty"`
	Path         string `json:"path,omitempty"`
	Namespace    string `json:"namespace,omitempty"`
	RegistryName string `json:"registry_name,omitempty"`
	Version      string `json:"version,omitempty"`
}

var sha40 = regexp.MustCompile(`^[0-9a-f]{40}$`)

// TestInstall_Local_Minimal proves the end-to-end loop: a minimal
// harness on the local filesystem installs cleanly, lays down the
// expected directory structure, and records a well-formed installed.json
// with source_type=local.
func TestInstall_Local_Minimal(t *testing.T) {
	s := newSandbox(t)
	clone := cloneAssistantsAtSHA(t)
	fixturePath := filepath.Join(clone, "e2e-fixtures", "minimal")

	out, _ := s.mustRunYnh(t, "install", fixturePath)
	if !regexp.MustCompile(`Installed harness "minimal"`).MatchString(out) {
		t.Errorf("install stdout missing success line:\n%s", out)
	}

	harnessDir := filepath.Join(s.home, "harnesses", "minimal")
	assertDirExists(t, harnessDir)
	assertDirExists(t, filepath.Join(harnessDir, ".ynh-plugin"))
	assertFileExists(t, filepath.Join(harnessDir, ".ynh-plugin", "plugin.json"))
	assertFileExists(t, filepath.Join(harnessDir, ".ynh-plugin", "installed.json"))
	assertFileExists(t, filepath.Join(s.home, "bin", "minimal"))

	got := readInstalledJSON(t, harnessDir)

	assertEqual(t, "source_type", got.SourceType, "local")
	assertEqual(t, "source", got.Source, fixturePath)
	if _, err := time.Parse(time.RFC3339, got.InstalledAt); err != nil {
		t.Errorf("installed_at %q is not RFC3339: %v", got.InstalledAt, err)
	}
	assertEqual(t, "ref", got.Ref, "")
	assertEqual(t, "sha", got.SHA, "")
	assertEqual(t, "path", got.Path, "")
	assertEqual(t, "namespace", got.Namespace, "")
	assertEqual(t, "registry_name", got.RegistryName, "")
	if len(got.Resolved) != 0 {
		t.Errorf("expected no resolved entries, got %d", len(got.Resolved))
	}
}

// TestInstall_Git_Minimal exercises the git install path with --ref
// pinning to AssistantsFixturesSHA. Asserts source_type=git, the
// recorded SHA matches the pin, and --path is preserved.
func TestInstall_Git_Minimal(t *testing.T) {
	s := newSandbox(t)
	out, _ := s.mustRunYnh(t,
		"install", "https://github.com/eyelock/assistants",
		"--path", "e2e-fixtures/minimal",
		"--ref", AssistantsFixturesSHA,
	)
	if !strings.Contains(out, `Installed harness "minimal"`) {
		t.Errorf("install stdout missing success line:\n%s", out)
	}

	harnessDir := filepath.Join(s.home, "harnesses", "minimal")
	got := readInstalledJSON(t, harnessDir)

	assertEqual(t, "source_type", got.SourceType, "git")
	assertEqual(t, "source", got.Source, "https://github.com/eyelock/assistants")
	assertEqual(t, "path", got.Path, "e2e-fixtures/minimal")
	// Ref records the symbolic ref (branch/tag); for SHA-pinned installs
	// the SHA already lives in SHA so Ref stays empty.
	assertEqual(t, "sha", got.SHA, AssistantsFixturesSHA)
	if !sha40.MatchString(got.SHA) {
		t.Errorf("sha %q is not 40-char hex", got.SHA)
	}
}

// TestInstall_FloatingInclude verifies that an include with no ref pin
// resolves to a concrete SHA at install time and gets recorded in
// installed.json.resolved[].
func TestInstall_FloatingInclude(t *testing.T) {
	s := newSandbox(t)
	clone := cloneAssistantsAtSHA(t)
	fixturePath := filepath.Join(clone, "e2e-fixtures", "with-floating-include")

	s.mustRunYnh(t, "install", fixturePath)
	got := readInstalledJSON(t, filepath.Join(s.home, "harnesses", "with-floating-include"))

	if len(got.Resolved) != 1 {
		t.Fatalf("expected 1 resolved entry, got %d: %+v", len(got.Resolved), got.Resolved)
	}
	r := got.Resolved[0]
	assertEqual(t, "resolved[0].git", r.Git, "github.com/eyelock/assistants")
	// Floating include: develop now records the resolved branch name (e.g.
	// "develop") in Ref alongside the concrete SHA. Just confirm Ref is
	// populated — the exact branch name varies with upstream HEAD.
	if r.Ref == "" {
		t.Errorf("expected resolved[0].ref to carry the resolved branch name, got empty")
	}
	assertEqual(t, "resolved[0].path", r.Path, "e2e-fixtures/included-skill")
	if !sha40.MatchString(r.SHA) {
		t.Errorf("resolved[0].sha %q is not 40-char hex", r.SHA)
	}
}

// TestInstall_PinnedInclude verifies that a SHA-pinned include records
// exactly the pinned commit in installed.json.resolved[].
func TestInstall_PinnedInclude(t *testing.T) {
	s := newSandbox(t)
	clone := cloneAssistantsAtSHA(t)
	fixturePath := filepath.Join(clone, "e2e-fixtures", "with-pinned-include")

	s.mustRunYnh(t, "install", fixturePath)
	got := readInstalledJSON(t, filepath.Join(s.home, "harnesses", "with-pinned-include"))

	if len(got.Resolved) != 1 {
		t.Fatalf("expected 1 resolved entry, got %d", len(got.Resolved))
	}
	const pinnedSHA = "8713efacdee8a2b05bdb70fee83be73b66222cc4"
	r := got.Resolved[0]
	// Ref holds the symbolic ref (branch/tag); SHA-only includes leave it
	// empty since the SHA fully identifies the commit.
	assertEqual(t, "resolved[0].sha", r.SHA, pinnedSHA)
}

// TestInstall_TagInclude verifies that a tag-pinned include resolves
// the tag to a concrete commit SHA at install time.
func TestInstall_TagInclude(t *testing.T) {
	s := newSandbox(t)
	clone := cloneAssistantsAtSHA(t)
	fixturePath := filepath.Join(clone, "e2e-fixtures", "with-tag-include")

	s.mustRunYnh(t, "install", fixturePath)
	got := readInstalledJSON(t, filepath.Join(s.home, "harnesses", "with-tag-include"))

	if len(got.Resolved) != 1 {
		t.Fatalf("expected 1 resolved entry, got %d", len(got.Resolved))
	}
	r := got.Resolved[0]
	assertEqual(t, "resolved[0].ref", r.Ref, AssistantsFixturesV1Tag)
	if !sha40.MatchString(r.SHA) {
		t.Errorf("resolved[0].sha %q is not 40-char hex (tag did not resolve)", r.SHA)
	}
	// The tag points at the initial-fixture commit (8713efa…). Verify exact match
	// to catch regressions in tag resolution.
	const tagCommitSHA = "8713efacdee8a2b05bdb70fee83be73b66222cc4"
	assertEqual(t, "resolved[0].sha", r.SHA, tagCommitSHA)
}

// TestInstall_InvalidSchema_Rejected verifies that ynh refuses a harness
// with an unknown top-level field (DisallowUnknownFields enforcement).
//
// Synthesised in-test rather than checked into eyelock/assistants so the
// assistants repo's own `ynd validate` CI doesn't trip on a deliberately
// invalid file.
func TestInstall_InvalidSchema_Rejected(t *testing.T) {
	s := newSandbox(t)

	dir := filepath.Join(t.TempDir(), "invalid-schema")
	if err := os.MkdirAll(filepath.Join(dir, ".ynh-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"invalid","version":"0.1.0","this_field_does_not_exist":true}`
	if err := os.WriteFile(filepath.Join(dir, ".ynh-plugin", "plugin.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	_, stderr, err := s.runYnh(t, "install", dir)
	if err == nil {
		t.Fatal("expected install to fail for invalid-schema fixture")
	}
	if !strings.Contains(stderr, "this_field_does_not_exist") && !strings.Contains(stderr, "unknown field") {
		t.Errorf("expected stderr to mention the rejected field; got:\n%s", stderr)
	}
}

func readInstalledJSON(t *testing.T, harnessDir string) installedJSONShape {
	t.Helper()
	path := filepath.Join(harnessDir, ".ynh-plugin", "installed.json")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading installed.json: %v", err)
	}
	var got installedJSONShape
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("parsing installed.json: %v\n%s", err, body)
	}
	return got
}
