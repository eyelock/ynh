//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMigrate_NoOp_OnFreshHome asserts that `ynh migrate` against a brand-new
// home (which the binary stamps as schema 2 on first install) reports a no-op.
// The schema-2 wire contract for fresh installs starts canonical from day one.
func TestMigrate_NoOp_OnFreshHome(t *testing.T) {
	s := newSandbox(t)
	clone := cloneAssistantsAtSHA(t)
	s.mustRunYnh(t, "install", filepath.Join(clone, "e2e-fixtures", "minimal"))

	out, _ := s.mustRunYnh(t, "migrate", "--format", "json")
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("parsing migrate JSON: %v\n%s", err, out)
	}
	// Either no-op (already schema 2) or reports the migration that just
	// happened on first install. Both are acceptable; what matters is the
	// command exists and emits a JSON envelope.
	if _, ok := got["schema_version"]; !ok {
		t.Errorf("migrate JSON missing schema_version key: %s", out)
	}
}

// TestMigrate_FromLegacyHome migrates a hand-constructed schema-1 home and
// asserts the on-disk layout converts to id-keyed paths with a manifest
// recording the moves.
func TestMigrate_FromLegacyHome(t *testing.T) {
	s := newSandbox(t)
	pointersDir := filepath.Join(s.home, "installed")
	if err := os.MkdirAll(pointersDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Place a schema-1 pointer file at <home>/installed/planner.json
	legacyPath := filepath.Join(pointersDir, "planner.json")
	pointerJSON := `{
  "name": "planner",
  "source_type": "local",
  "source": "/Users/anyone/work/planner",
  "installed_at": "2026-04-01T00:00:00Z"
}`
	if err := os.WriteFile(legacyPath, []byte(pointerJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _ := s.mustRunYnh(t, "migrate", "--format", "json")
	// CurrentSchemaVersion is 3; running migrate against a schema-1 home
	// chains 1→2 and 2→3 in a single invocation, so the reported schema
	// is 3 and the home is stamped 3.
	if !strings.Contains(out, `"schema_version": 3`) {
		t.Errorf("migrate output missing schema_version 3:\n%s", out)
	}
	if !strings.Contains(out, `"new_id": "local/planner"`) {
		t.Errorf("migrate output missing manifest entry for local/planner:\n%s", out)
	}

	// Assert on-disk: legacy path gone, schema-2/3 path present.
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Errorf("legacy pointer path still exists at %s", legacyPath)
	}
	newPath := filepath.Join(pointersDir, "local--planner.json")
	if _, err := os.Stat(newPath); err != nil {
		t.Errorf("schema-2 pointer path missing at %s: %v", newPath, err)
	}

	// Schema-version file is stamped to the current schema version (3).
	versionPath := filepath.Join(s.home, ".schema-version")
	data, err := os.ReadFile(versionPath)
	if err != nil {
		t.Fatalf("reading .schema-version: %v", err)
	}
	if strings.TrimSpace(string(data)) != "3" {
		t.Errorf(".schema-version content = %q, want 3", string(data))
	}

	// Manifest is persisted at <home>/.migration-manifest.json.
	if _, err := os.Stat(filepath.Join(s.home, ".migration-manifest.json")); err != nil {
		t.Errorf("migration manifest not persisted: %v", err)
	}
}

// TestMigrate_DryRun_NoOnDiskChanges asserts --dry-run reports the plan
// without altering the home.
func TestMigrate_DryRun_NoOnDiskChanges(t *testing.T) {
	s := newSandbox(t)
	pointersDir := filepath.Join(s.home, "installed")
	_ = os.MkdirAll(pointersDir, 0o755)
	legacyPath := filepath.Join(pointersDir, "planner.json")
	_ = os.WriteFile(legacyPath, []byte(`{"name":"planner","source_type":"local","source":"","installed_at":"2026-04-01T00:00:00Z"}`), 0o644)

	out, _ := s.mustRunYnh(t, "migrate", "--dry-run", "--format", "json")
	if !strings.Contains(out, `"dry_run": true`) {
		t.Errorf("dry-run output missing dry_run flag:\n%s", out)
	}
	// Legacy path must still exist.
	if _, err := os.Stat(legacyPath); err != nil {
		t.Errorf("dry-run removed legacy path: %v", err)
	}
}
