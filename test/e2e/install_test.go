//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

// installedJSONShape mirrors internal/plugin.InstalledJSON for the
// fields the suite asserts on. Kept as a local type so the test does
// not import internal/* (treats the binary as a black box).
type installedJSONShape struct {
	SourceType   string `json:"source_type"`
	Source       string `json:"source"`
	Ref          string `json:"ref,omitempty"`
	SHA          string `json:"sha,omitempty"`
	Path         string `json:"path,omitempty"`
	Namespace    string `json:"namespace,omitempty"`
	RegistryName string `json:"registry_name,omitempty"`
	InstalledAt  string `json:"installed_at"`
}

// TestInstall_Local_Minimal proves the end-to-end loop: a minimal
// harness on the local filesystem installs cleanly, lays down the
// expected directory structure, and records a well-formed installed.json
// with source_type=local.
//
// Reproducibility: the test clones eyelock/assistants into a tempdir,
// checks out AssistantsFixturesSHA, and installs from the resulting
// local path. The fixture content is therefore frozen to the pinned SHA
// regardless of what HEAD of develop looks like at test time.
//
// Coverage of git/registry source types lands in Phase 2. Git-source
// reproducibility requires either a `--ref` flag on `ynh install` or
// structural-only assertions against live github.com — see Phase 2 plan.
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
