//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestInstall_MigratesLegacyHarnessJson covers the migration path: a source
// directory containing only the legacy `.harness.json` (pre-1.0 layout) must
// be transparently migrated to `.ynh-plugin/plugin.json` during install.
//
// Lifts coverage of internal/migration which is otherwise only exercised
// by unit tests.
func TestInstall_MigratesLegacyHarnessJson(t *testing.T) {
	s := newSandbox(t)

	srcDir := filepath.Join(t.TempDir(), "legacy")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"$schema":"https://eyelock.github.io/ynh/schema/harness.schema.json","name":"legacy","version":"0.1.0"}`
	if err := os.WriteFile(filepath.Join(srcDir, ".harness.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	s.mustRunYnh(t, "install", srcDir)

	// In the install dir, the legacy file must be gone and the new layout present.
	installDir := filepath.Join(s.home, "harnesses", "legacy")
	if _, err := os.Stat(filepath.Join(installDir, ".harness.json")); !os.IsNotExist(err) {
		t.Errorf("legacy .harness.json should have been removed in install dir, err=%v", err)
	}
	assertFileExists(t, filepath.Join(installDir, ".ynh-plugin", "plugin.json"))

	// And ynh ls should see it under its declared name.
	out, _ := s.mustRunYnh(t, "ls", "--format", "json")
	var got envelopeLs
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("parsing ls JSON: %v\n%s", err, out)
	}
	if len(got.Harnesses) != 1 || got.Harnesses[0].Name != "legacy" {
		t.Fatalf("expected one harness named 'legacy', got %+v", got.Harnesses)
	}
}

// TestInstall_ReinstallReplaces verifies that re-installing a local harness
// of the same name overwrites in place — no duplicates appear in `ynh ls`,
// and the install dir reflects the second source's content.
//
// Behavioural backstop for the alreadyInstalled / RemoveAll branch in the
// install flow.
func TestInstall_ReinstallReplaces(t *testing.T) {
	s := newSandbox(t)

	// First install — minimal harness.
	srcA := filepath.Join(t.TempDir(), "first")
	if err := os.MkdirAll(filepath.Join(srcA, ".ynh-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	pluginA := `{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"twin","version":"0.1.0","description":"first"}`
	if err := os.WriteFile(filepath.Join(srcA, ".ynh-plugin", "plugin.json"), []byte(pluginA), 0o644); err != nil {
		t.Fatal(err)
	}
	s.mustRunYnh(t, "install", srcA)

	// Second install — same name, different description, different source dir.
	srcB := filepath.Join(t.TempDir(), "second")
	if err := os.MkdirAll(filepath.Join(srcB, ".ynh-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	pluginB := `{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"twin","version":"0.2.0","description":"second"}`
	if err := os.WriteFile(filepath.Join(srcB, ".ynh-plugin", "plugin.json"), []byte(pluginB), 0o644); err != nil {
		t.Fatal(err)
	}
	s.mustRunYnh(t, "install", srcB)

	// ls must show exactly one entry — the second one.
	out, _ := s.mustRunYnh(t, "ls", "--format", "json")
	var got envelopeLs
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("parsing ls JSON: %v\n%s", err, out)
	}
	if len(got.Harnesses) != 1 {
		t.Fatalf("expected 1 harness after reinstall, got %d: %+v", len(got.Harnesses), got.Harnesses)
	}
	h := got.Harnesses[0]
	assertEqual(t, "name", h.Name, "twin")
	assertEqual(t, "version_installed", h.VersionInstalled, "0.2.0")
	assertEqual(t, "description", h.Description, "second")
}
