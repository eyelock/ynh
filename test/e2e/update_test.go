//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestUpdate_NoChange asserts that running `ynh update` against an
// upstream whose HEAD has not moved leaves installed.json.resolved[].sha
// unchanged and reports no changes.
func TestUpdate_NoChange(t *testing.T) {
	s := newSandbox(t)
	upstream := newLocalUpstream(t, "include-target", "first content")
	harness := newLocalFloatingHarness(t, "no-change-harness", upstream)

	s.mustRunYnh(t, "install", harness)
	before := readInstalledJSON(t, filepath.Join(s.home, "harnesses", "no-change-harness"))
	if len(before.Resolved) != 1 {
		t.Fatalf("setup: expected 1 resolved entry, got %d", len(before.Resolved))
	}

	out, _ := s.mustRunYnh(t, "update", "no-change-harness")

	after := readInstalledJSON(t, filepath.Join(s.home, "harnesses", "no-change-harness"))
	if len(after.Resolved) != 1 {
		t.Fatalf("expected 1 resolved entry after update, got %d", len(after.Resolved))
	}
	assertEqual(t, "resolved[0].sha unchanged", after.Resolved[0].SHA, before.Resolved[0].SHA)

	// Update output should not announce a change. We don't assert exact wording
	// but flag if it shouts about updates.
	if containsAny(out, "Updated", "moved") && !containsAny(out, "no change", "unchanged") {
		t.Logf("update output (informational):\n%s", out)
	}
}

// TestUpdate_HeadMoves asserts that when an upstream HEAD moves, the
// recorded SHA in installed.json.resolved[].sha follows.
//
// This is the regression case the suite was built for: prior to #115
// the resolved SHA was not recorded at all; after it, the wrong SHA was
// recorded for floating refs. This test fails on both regressions.
func TestUpdate_HeadMoves(t *testing.T) {
	s := newSandbox(t)
	upstream := newLocalUpstream(t, "include-target", "first content")
	harness := newLocalFloatingHarness(t, "moving-harness", upstream)

	s.mustRunYnh(t, "install", harness)
	before := readInstalledJSON(t, filepath.Join(s.home, "harnesses", "moving-harness"))
	if len(before.Resolved) != 1 {
		t.Fatalf("setup: expected 1 resolved entry, got %d", len(before.Resolved))
	}
	beforeSHA := before.Resolved[0].SHA

	commitToUpstream(t, upstream, "include-target/SKILL.md", "second content")

	s.mustRunYnh(t, "update", "moving-harness")

	after := readInstalledJSON(t, filepath.Join(s.home, "harnesses", "moving-harness"))
	if len(after.Resolved) != 1 {
		t.Fatalf("expected 1 resolved entry after update, got %d", len(after.Resolved))
	}
	if after.Resolved[0].SHA == beforeSHA {
		t.Errorf("expected SHA to move after upstream commit, but it stayed at %s", beforeSHA)
	}
	if !sha40.MatchString(after.Resolved[0].SHA) {
		t.Errorf("post-update SHA %q is not 40-char hex", after.Resolved[0].SHA)
	}
}

// newLocalUpstream creates a bare-but-cloneable git repo in a tempdir
// and seeds it with includePath/SKILL.md containing initial content.
// Returns a file:// URL suitable for use as an `includes[].git` value.
func newLocalUpstream(t *testing.T, includePath, initialContent string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "upstream")
	if err := os.MkdirAll(filepath.Join(dir, includePath), 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "init", "--quiet", "--initial-branch=main")
	mustGit(t, dir, "config", "user.email", "e2e@example.invalid")
	mustGit(t, dir, "config", "user.name", "e2e")
	mustGit(t, dir, "config", "uploadpack.allowReachableSHA1InWant", "true")
	skillBody := fmt.Sprintf("---\nname: include-target\ndescription: %s\n---\n", initialContent)
	if err := os.WriteFile(filepath.Join(dir, includePath, "SKILL.md"), []byte(skillBody), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "--quiet", "-m", "initial")
	return "file://" + dir
}

// commitToUpstream writes a new content blob at relPath inside the bare
// upstream URL (file:// path) and creates a new commit.
func commitToUpstream(t *testing.T, fileURL, relPath, content string) {
	t.Helper()
	dir := filepath.Clean(strings.TrimPrefix(fileURL, "file://"))
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, relPath)), 0o755); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf("---\nname: include-target\ndescription: %s\n---\n", content)
	if err := os.WriteFile(filepath.Join(dir, relPath), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "--quiet", "-m", "update")
}

// newLocalFloatingHarness writes a harness directory containing a
// .ynh-plugin/plugin.json with one floating include pointing at the
// given upstream URL. Returns the harness path suitable for `ynh install`.
func newLocalFloatingHarness(t *testing.T, name, upstreamURL string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	pluginDir := filepath.Join(dir, ".ynh-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": %q,
  "version": "0.1.0",
  "default_vendor": "claude",
  "includes": [{"git": %q, "path": "include-target"}]
}
`, name, upstreamURL)
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func containsAny(haystack string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}
