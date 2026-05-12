package migration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writePluginManifest writes a minimal .ynh-plugin/plugin.json with the
// given harness name into dir.
func writePluginManifest(t *testing.T, dir, name string) {
	t.Helper()
	pluginDir := filepath.Join(dir, ".ynh-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"name":"` + name + `","version":"0.1.0","default_vendor":"claude"}`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeInstalledRecord(t *testing.T, dir string, ins installedJSONShape) {
	t.Helper()
	pluginDir := filepath.Join(dir, ".ynh-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(ins)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "installed.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestMigrateToSchema3_CollapsesLocalInstall stages a pre-schema-3 local
// install (copy dir under HarnessesDir with installed.json pointing at a
// source tree) and verifies the migration writes a pointer file and
// removes the copy dir, leaving the source tree untouched.
func TestMigrateToSchema3_CollapsesLocalInstall(t *testing.T) {
	home := t.TempDir()

	// Source tree the user authored
	sourceDir := t.TempDir()
	writePluginManifest(t, sourceDir, "myharness")

	// Pre-schema-3 install copy at HarnessesDir/local--myharness
	copyDir := filepath.Join(home, "harnesses", "local--myharness")
	writePluginManifest(t, copyDir, "myharness")
	writeInstalledRecord(t, copyDir, installedJSONShape{
		SourceType:  "local",
		Source:      sourceDir,
		InstalledAt: "2026-05-11T00:00:00Z",
		Resolved: []resolvedSourceShape{
			{Git: "https://github.com/example/inc", Ref: "main", SHA: "abc123"},
		},
	})

	if _, err := MigrateToSchema3(home, MigrateOpts{}); err != nil {
		t.Fatalf("MigrateToSchema3: %v", err)
	}

	// Copy dir removed
	if _, err := os.Stat(copyDir); !os.IsNotExist(err) {
		t.Errorf("copy dir still present after migration: err=%v", err)
	}
	// Pointer written carrying the full record
	ptrPath := filepath.Join(home, "installed", "local--myharness.json")
	data, err := os.ReadFile(ptrPath)
	if err != nil {
		t.Fatalf("pointer not written: %v", err)
	}
	var ptr schema3Pointer
	if err := json.Unmarshal(data, &ptr); err != nil {
		t.Fatalf("invalid pointer: %v", err)
	}
	if ptr.ID != "local/myharness" {
		t.Errorf("pointer.id = %q, want local/myharness", ptr.ID)
	}
	if ptr.Name != "myharness" {
		t.Errorf("pointer.name = %q, want myharness", ptr.Name)
	}
	if ptr.Source != sourceDir {
		t.Errorf("pointer.source = %q, want %q", ptr.Source, sourceDir)
	}
	if len(ptr.Resolved) != 1 || ptr.Resolved[0].SHA != "abc123" {
		t.Errorf("resolved sources not absorbed: %+v", ptr.Resolved)
	}
	// Source tree was not touched
	if _, err := os.Stat(filepath.Join(sourceDir, ".ynh-plugin", "plugin.json")); err != nil {
		t.Errorf("source manifest missing after migration: %v", err)
	}
	// Schema stamp
	if v := ReadSchemaVersion(home); v != 3 {
		t.Errorf("schema version after migrate = %d, want 3", v)
	}
}

// TestMigrateToSchema3_AbsorbsForkProvenance stages a legacy fork
// (pointer file with minimal info + .ynh-plugin/installed.json in the
// source tree) and verifies the source-tree installed.json is folded
// into the pointer and removed.
func TestMigrateToSchema3_AbsorbsForkProvenance(t *testing.T) {
	home := t.TempDir()
	pointersDir := filepath.Join(home, "installed")
	if err := os.MkdirAll(pointersDir, 0o755); err != nil {
		t.Fatal(err)
	}

	sourceDir := t.TempDir()
	writePluginManifest(t, sourceDir, "myfork")
	writeInstalledRecord(t, sourceDir, installedJSONShape{
		SourceType:  "local",
		Source:      sourceDir,
		InstalledAt: "2026-05-11T00:00:00Z",
		ForkedFrom: &forkedFromShape{
			SourceType: "git",
			Source:     "github.com/upstream/repo",
		},
	})

	// Legacy schema-2 pointer carrying only source_type/source/installed_at.
	legacy := schema3Pointer{
		ID:   "local/myfork",
		Name: "myfork",
		installedJSONShape: installedJSONShape{
			SourceType:  "local",
			Source:      sourceDir,
			InstalledAt: "2026-05-11T00:00:00Z",
		},
	}
	data, _ := json.MarshalIndent(legacy, "", "  ")
	if err := os.WriteFile(filepath.Join(pointersDir, "local--myfork.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := MigrateToSchema3(home, MigrateOpts{}); err != nil {
		t.Fatalf("MigrateToSchema3: %v", err)
	}

	// Source-tree installed.json was removed
	if _, err := os.Stat(filepath.Join(sourceDir, ".ynh-plugin", "installed.json")); !os.IsNotExist(err) {
		t.Errorf("source-tree installed.json still present: err=%v", err)
	}
	// Pointer now carries the forked_from
	updated, _ := os.ReadFile(filepath.Join(pointersDir, "local--myfork.json"))
	var ptr schema3Pointer
	if err := json.Unmarshal(updated, &ptr); err != nil {
		t.Fatalf("invalid pointer after migrate: %v", err)
	}
	if ptr.ForkedFrom == nil {
		t.Fatal("forked_from not absorbed")
	}
	if ptr.ForkedFrom.Source != "github.com/upstream/repo" {
		t.Errorf("forked_from.source = %q", ptr.ForkedFrom.Source)
	}
}

// TestMigrateToSchema3_Idempotent verifies a second run finds nothing to do.
func TestMigrateToSchema3_Idempotent(t *testing.T) {
	home := t.TempDir()
	sourceDir := t.TempDir()
	writePluginManifest(t, sourceDir, "h")
	copyDir := filepath.Join(home, "harnesses", "local--h")
	writePluginManifest(t, copyDir, "h")
	writeInstalledRecord(t, copyDir, installedJSONShape{
		SourceType:  "local",
		Source:      sourceDir,
		InstalledAt: "now",
	})

	if _, err := MigrateToSchema3(home, MigrateOpts{}); err != nil {
		t.Fatal(err)
	}
	m, err := MigrateToSchema3(home, MigrateOpts{})
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if len(m.Entries) != 0 {
		t.Errorf("idempotent re-run produced entries: %+v", m.Entries)
	}
}

// TestMigrateToSchema3_TreeFormUntouched verifies a non-local copy dir
// (source_type=git or registry) is left alone — its content lives in
// HarnessesDir by design.
func TestMigrateToSchema3_TreeFormUntouched(t *testing.T) {
	home := t.TempDir()
	copyDir := filepath.Join(home, "harnesses", "github.com--org--repo--name")
	writePluginManifest(t, copyDir, "name")
	writeInstalledRecord(t, copyDir, installedJSONShape{
		SourceType:  "git",
		Source:      "https://github.com/org/repo",
		InstalledAt: "now",
	})

	if _, err := MigrateToSchema3(home, MigrateOpts{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(copyDir); err != nil {
		t.Errorf("tree-form install was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "installed", "github.com--org--repo--name.json")); !os.IsNotExist(err) {
		t.Errorf("pointer written for tree-form install: err=%v", err)
	}
}

// TestMigrateToSchema3_MissingSource aborts when the source tree is gone.
func TestMigrateToSchema3_MissingSource(t *testing.T) {
	home := t.TempDir()
	copyDir := filepath.Join(home, "harnesses", "local--ghost")
	writePluginManifest(t, copyDir, "ghost")
	writeInstalledRecord(t, copyDir, installedJSONShape{
		SourceType:  "local",
		Source:      filepath.Join(t.TempDir(), "gone"),
		InstalledAt: "now",
	})

	_, err := MigrateToSchema3(home, MigrateOpts{})
	if err == nil {
		t.Fatal("expected error for missing source, got nil")
	}
}
