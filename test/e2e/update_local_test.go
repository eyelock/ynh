//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestUpdate_LocalIncludeSkipped verifies the documented constraint that
// `ynh update` only refreshes git-source includes. A local-path include
// has nothing to fetch, so update must succeed without error and leave
// installed.json unchanged.
func TestUpdate_LocalIncludeSkipped(t *testing.T) {
	s := newSandbox(t)

	upstream := filepath.Join(t.TempDir(), "local-upstream", "skills", "thing")
	if err := os.MkdirAll(upstream, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(upstream, "SKILL.md"),
		[]byte("---\nname: thing\ndescription: local skill.\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	harness := filepath.Join(t.TempDir(), "local-only")
	if err := os.MkdirAll(filepath.Join(harness, ".ynh-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`{
  "$schema": "https://eyelock.github.io/ynh/schema/plugin.schema.json",
  "name": "local-only",
  "version": "0.1.0",
  "includes": [{"local": %q}]
}
`, filepath.Dir(filepath.Dir(upstream)))
	if err := os.WriteFile(filepath.Join(harness, ".ynh-plugin", "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	s.mustRunYnh(t, "install", harness)

	before := readInstalledJSON(t, filepath.Join(s.home, "harnesses", "local-only"))

	// Update must succeed even though there's nothing remote to refresh.
	s.mustRunYnh(t, "update", "local-only")

	after := readInstalledJSON(t, filepath.Join(s.home, "harnesses", "local-only"))

	// Local includes don't get a resolved entry (no SHA to record),
	// so the count should match before/after.
	assertEqual(t, "resolved entries unchanged", len(after.Resolved), len(before.Resolved))
}
