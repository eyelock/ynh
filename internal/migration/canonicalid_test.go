package migration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReadSchemaVersion_DefaultsToOne(t *testing.T) {
	home := t.TempDir()
	if v := ReadSchemaVersion(home); v != 1 {
		t.Errorf("ReadSchemaVersion on empty home = %d, want 1", v)
	}
}

func TestReadSchemaVersion_ReadsTwo(t *testing.T) {
	home := t.TempDir()
	if err := WriteSchemaVersion(home, 2); err != nil {
		t.Fatalf("WriteSchemaVersion: %v", err)
	}
	if v := ReadSchemaVersion(home); v != 2 {
		t.Errorf("ReadSchemaVersion after stamp = %d, want 2", v)
	}
}

func TestMigrate_EmptyHome_StampsSchemaVersion(t *testing.T) {
	home := t.TempDir()
	m, err := MigrateToSchema2(home, MigrateOpts{})
	if err != nil {
		t.Fatalf("MigrateToSchema2: %v", err)
	}
	if v := ReadSchemaVersion(home); v != 2 {
		t.Errorf("schema version after migrate = %d, want 2", v)
	}
	if len(m.Entries) != 0 {
		t.Errorf("expected empty entries on empty home, got %d", len(m.Entries))
	}
}

func TestMigrate_LocalPointer(t *testing.T) {
	home := t.TempDir()
	pointersDir := filepath.Join(home, "installed")
	if err := os.MkdirAll(pointersDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Schema-1 pointer file at <home>/installed/planner.json
	oldPath := filepath.Join(pointersDir, "planner.json")
	if err := os.WriteFile(oldPath, []byte(`{
  "name": "planner",
  "source_type": "local",
  "source": "/Users/david/work/planner",
  "installed_at": "2026-04-01T00:00:00Z"
}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	m, err := MigrateToSchema2(home, MigrateOpts{})
	if err != nil {
		t.Fatalf("MigrateToSchema2: %v", err)
	}

	// Old path is gone
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("old pointer path still exists: %v", err)
	}
	// New path is at <home>/installed/local--planner.json
	newPath := filepath.Join(pointersDir, "local--planner.json")
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("new pointer path missing: %v", err)
	}
	// Content carries id field
	data, _ := os.ReadFile(newPath)
	var p map[string]any
	_ = json.Unmarshal(data, &p)
	if p["id"] != "local/planner" {
		t.Errorf("pointer id = %v, want local/planner", p["id"])
	}
	// Manifest reflects the move
	if len(m.Entries) != 1 {
		t.Fatalf("expected 1 manifest entry, got %d", len(m.Entries))
	}
	if m.Entries[0].OldID != "planner" || m.Entries[0].NewID != "local/planner" {
		t.Errorf("manifest entry = %+v", m.Entries[0])
	}
	if m.Entries[0].Kind != "pointer" {
		t.Errorf("manifest kind = %q, want pointer", m.Entries[0].Kind)
	}
}

func TestMigrate_RemotePointer(t *testing.T) {
	home := t.TempDir()
	pointersDir := filepath.Join(home, "installed")
	_ = os.MkdirAll(pointersDir, 0o755)

	oldPath := filepath.Join(pointersDir, "david.json")
	_ = os.WriteFile(oldPath, []byte(`{
  "name": "david",
  "source_type": "git",
  "source": "https://github.com/eyelock/assistants",
  "installed_at": "2026-04-01T00:00:00Z"
}`), 0o644)

	m, err := MigrateToSchema2(home, MigrateOpts{})
	if err != nil {
		t.Fatalf("MigrateToSchema2: %v", err)
	}

	wantNew := filepath.Join(pointersDir, "github.com--eyelock--assistants--david.json")
	if _, err := os.Stat(wantNew); err != nil {
		t.Fatalf("expected pointer at %s: %v", wantNew, err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Errorf("old path still exists")
	}
	if m.Entries[0].NewID != "github.com/eyelock/assistants/david" {
		t.Errorf("NewID = %q, want github.com/eyelock/assistants/david", m.Entries[0].NewID)
	}
}

func TestMigrate_DryRun_NoOnDiskChanges(t *testing.T) {
	home := t.TempDir()
	pointersDir := filepath.Join(home, "installed")
	_ = os.MkdirAll(pointersDir, 0o755)
	oldPath := filepath.Join(pointersDir, "planner.json")
	_ = os.WriteFile(oldPath, []byte(`{"name":"planner","source_type":"local","source":"","installed_at":"2026-04-01T00:00:00Z"}`), 0o644)

	m, err := MigrateToSchema2(home, MigrateOpts{DryRun: true})
	if err != nil {
		t.Fatalf("MigrateToSchema2 dry-run: %v", err)
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Errorf("dry-run removed old path: %v", err)
	}
	newPath := filepath.Join(pointersDir, "local--planner.json")
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Errorf("dry-run created new path: %v", err)
	}
	if v := ReadSchemaVersion(home); v != 1 {
		t.Errorf("dry-run stamped schema version = %d, want 1", v)
	}
	if len(m.Entries) != 1 {
		t.Errorf("dry-run manifest entries = %d, want 1", len(m.Entries))
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	home := t.TempDir()
	pointersDir := filepath.Join(home, "installed")
	_ = os.MkdirAll(pointersDir, 0o755)
	_ = os.WriteFile(filepath.Join(pointersDir, "planner.json"), []byte(`{"name":"planner","source_type":"local","source":"","installed_at":"2026-04-01T00:00:00Z"}`), 0o644)

	if _, err := MigrateToSchema2(home, MigrateOpts{}); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	m2, err := MigrateToSchema2(home, MigrateOpts{})
	if err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if len(m2.Entries) != 0 {
		t.Errorf("idempotent re-run produced entries: %+v", m2.Entries)
	}
}

func TestMigrate_Manifest_Persisted(t *testing.T) {
	home := t.TempDir()
	pointersDir := filepath.Join(home, "installed")
	_ = os.MkdirAll(pointersDir, 0o755)
	_ = os.WriteFile(filepath.Join(pointersDir, "planner.json"), []byte(`{"name":"planner","source_type":"local","source":"","installed_at":"2026-04-01T00:00:00Z"}`), 0o644)

	if _, err := MigrateToSchema2(home, MigrateOpts{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	m, err := ReadManifest(home)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if m == nil {
		t.Fatal("expected manifest, got nil")
	}
	if m.SchemaVersion != 2 {
		t.Errorf("manifest.schema_version = %d, want 2", m.SchemaVersion)
	}
	if len(m.Entries) != 1 {
		t.Errorf("manifest entries = %d, want 1", len(m.Entries))
	}
}

func TestMigrate_BrokenPointer_AbortsByDefault(t *testing.T) {
	home := t.TempDir()
	pointersDir := filepath.Join(home, "installed")
	_ = os.MkdirAll(pointersDir, 0o755)
	// Malformed JSON
	_ = os.WriteFile(filepath.Join(pointersDir, "broken.json"), []byte(`{not json`), 0o644)

	_, err := MigrateToSchema2(home, MigrateOpts{})
	if err == nil {
		t.Fatal("expected migration to abort on broken entry")
	}
}

func TestMigrate_BrokenPointer_QuarantinedWithSkipBroken(t *testing.T) {
	home := t.TempDir()
	pointersDir := filepath.Join(home, "installed")
	_ = os.MkdirAll(pointersDir, 0o755)
	brokenPath := filepath.Join(pointersDir, "broken.json")
	_ = os.WriteFile(brokenPath, []byte(`{not json`), 0o644)

	m, err := MigrateToSchema2(home, MigrateOpts{SkipBroken: true})
	if err != nil {
		t.Fatalf("with --skip-broken: %v", err)
	}
	if len(m.Quarantined) != 1 {
		t.Fatalf("expected 1 quarantined entry, got %d", len(m.Quarantined))
	}
	// The broken file must have moved out of pointers/
	if _, err := os.Stat(brokenPath); !os.IsNotExist(err) {
		t.Errorf("broken pointer still in pointers dir")
	}
	// Schema is stamped to 2 even with broken entries
	if v := ReadSchemaVersion(home); v != 2 {
		t.Errorf("schema version = %d, want 2", v)
	}
}
