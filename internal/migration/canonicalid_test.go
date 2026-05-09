package migration

import (
	"encoding/json"
	"errors"
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

func TestMigrate_RewritesLegacyLauncher(t *testing.T) {
	home := t.TempDir()
	binDir := filepath.Join(home, "bin")
	pointersDir := filepath.Join(home, "installed")
	_ = os.MkdirAll(binDir, 0o755)
	_ = os.MkdirAll(pointersDir, 0o755)

	// Schema-1 pointer file → migration will produce manifest entry
	// {OldID: "planner", NewID: "local/planner"}.
	_ = os.WriteFile(filepath.Join(pointersDir, "planner.json"),
		[]byte(`{"name":"planner","source_type":"local","source":"","installed_at":"2026-04-01T00:00:00Z"}`), 0o644)

	// Schema-1 launcher referencing the bare name.
	launcherPath := filepath.Join(binDir, "planner")
	legacyContent := "#!/bin/bash\n# Generated by ynh - do not edit\nexec ynh run \"planner\" \"$@\"\n"
	_ = os.WriteFile(launcherPath, []byte(legacyContent), 0o755)

	m, err := MigrateToSchema2(home, MigrateOpts{})
	if err != nil {
		t.Fatalf("MigrateToSchema2: %v", err)
	}

	got, err := os.ReadFile(launcherPath)
	if err != nil {
		t.Fatalf("read launcher post-migrate: %v", err)
	}
	want := "#!/bin/bash\n# Generated by ynh - do not edit\nexec ynh run \"local/planner\" \"$@\"\n"
	if string(got) != want {
		t.Errorf("launcher not rewritten:\n got:  %q\n want: %q", got, want)
	}

	// Manifest records the launcher rewrite.
	var seenLauncher bool
	for _, e := range m.Entries {
		if e.Kind == "launcher" && e.OldID == "planner" && e.NewID == "local/planner" {
			seenLauncher = true
		}
	}
	if !seenLauncher {
		t.Errorf("manifest missing launcher entry: %+v", m.Entries)
	}
}

func TestMigrate_HandEditedLauncher_AbortsByDefault(t *testing.T) {
	home := t.TempDir()
	binDir := filepath.Join(home, "bin")
	pointersDir := filepath.Join(home, "installed")
	_ = os.MkdirAll(binDir, 0o755)
	_ = os.MkdirAll(pointersDir, 0o755)

	_ = os.WriteFile(filepath.Join(pointersDir, "planner.json"),
		[]byte(`{"name":"planner","source_type":"local","source":"","installed_at":"2026-04-01T00:00:00Z"}`), 0o644)

	launcherPath := filepath.Join(binDir, "planner")
	// Hand-edited content — diverges from the generated template.
	_ = os.WriteFile(launcherPath, []byte("#!/bin/bash\n# my custom launcher\nexec ynh run \"planner\" --my-flag \"$@\"\n"), 0o755)

	_, err := MigrateToSchema2(home, MigrateOpts{})
	if err == nil {
		t.Fatal("expected migration to abort on hand-edited launcher")
	}
	if !errors.Is(err, ErrMigrationAborted) {
		t.Errorf("expected ErrMigrationAborted wrap, got: %v", err)
	}
}

func TestMigrate_HandEditedLauncher_QuarantinedWithSkipBroken(t *testing.T) {
	home := t.TempDir()
	binDir := filepath.Join(home, "bin")
	pointersDir := filepath.Join(home, "installed")
	_ = os.MkdirAll(binDir, 0o755)
	_ = os.MkdirAll(pointersDir, 0o755)

	_ = os.WriteFile(filepath.Join(pointersDir, "planner.json"),
		[]byte(`{"name":"planner","source_type":"local","source":"","installed_at":"2026-04-01T00:00:00Z"}`), 0o644)

	launcherPath := filepath.Join(binDir, "planner")
	_ = os.WriteFile(launcherPath, []byte("#!/bin/bash\n# my custom launcher\n"), 0o755)

	m, err := MigrateToSchema2(home, MigrateOpts{SkipBroken: true})
	if err != nil {
		t.Fatalf("with --skip-broken: %v", err)
	}
	if len(m.Quarantined) != 1 {
		t.Fatalf("expected 1 quarantined launcher, got %d", len(m.Quarantined))
	}
	if _, err := os.Stat(launcherPath); !os.IsNotExist(err) {
		t.Errorf("hand-edited launcher still in bin/")
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

// makeHarnessFixture writes a minimal harness under dir/name with the given
// source and source_type in installed.json. Used by migrateOneInstall tests.
func makeHarnessFixture(t *testing.T, dir, name, sourceType, source string) string {
	t.Helper()
	hDir := filepath.Join(dir, name)
	pluginDir := filepath.Join(hDir, ".ynh-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", pluginDir, err)
	}
	pluginJSON := `{"name":"` + name + `","version":"0.1.0","description":"fixture"}`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(pluginJSON), 0o644); err != nil {
		t.Fatalf("write plugin.json: %v", err)
	}
	installedJSON := `{"source_type":"` + sourceType + `","source":"` + source + `","installed_at":"2026-01-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(pluginDir, "installed.json"), []byte(installedJSON), 0o644); err != nil {
		t.Fatalf("write installed.json: %v", err)
	}
	return hDir
}

func TestMigrateOneInstall_FlatLocalInstall(t *testing.T) {
	home := t.TempDir()
	harnessesDir := filepath.Join(home, "harnesses")
	_ = os.MkdirAll(harnessesDir, 0o755)

	makeHarnessFixture(t, harnessesDir, "planner", "local", "/Users/david/planner")

	m, err := MigrateToSchema2(home, MigrateOpts{})
	if err != nil {
		t.Fatalf("MigrateToSchema2: %v", err)
	}

	newPath := filepath.Join(harnessesDir, "local--planner")
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("expected harness at %s: %v", newPath, err)
	}
	if _, err := os.Stat(filepath.Join(harnessesDir, "planner")); !os.IsNotExist(err) {
		t.Errorf("old harness dir still exists")
	}

	data, err := os.ReadFile(filepath.Join(newPath, ".ynh-plugin", "installed.json"))
	if err != nil {
		t.Fatalf("reading installed.json: %v", err)
	}
	var ins map[string]any
	if err := json.Unmarshal(data, &ins); err != nil {
		t.Fatalf("parsing installed.json: %v", err)
	}
	if ins["namespace"] != "local" {
		t.Errorf("installed.json namespace = %v, want local", ins["namespace"])
	}

	if len(m.Entries) != 1 {
		t.Fatalf("expected 1 manifest entry, got %d", len(m.Entries))
	}
	e := m.Entries[0]
	if e.NewID != "local/planner" || e.Kind != "install_tree_flat" {
		t.Errorf("manifest entry = %+v", e)
	}
}

func TestMigrateOneInstall_FlatRemoteInstall(t *testing.T) {
	home := t.TempDir()
	harnessesDir := filepath.Join(home, "harnesses")
	_ = os.MkdirAll(harnessesDir, 0o755)

	makeHarnessFixture(t, harnessesDir, "assistants", "git", "https://github.com/eyelock/assistants")

	m, err := MigrateToSchema2(home, MigrateOpts{})
	if err != nil {
		t.Fatalf("MigrateToSchema2: %v", err)
	}

	newPath := filepath.Join(harnessesDir, "github.com--eyelock--assistants--assistants")
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("expected harness at %s: %v", newPath, err)
	}
	if _, err := os.Stat(filepath.Join(harnessesDir, "assistants")); !os.IsNotExist(err) {
		t.Errorf("old harness dir still exists")
	}

	data, err := os.ReadFile(filepath.Join(newPath, ".ynh-plugin", "installed.json"))
	if err != nil {
		t.Fatalf("reading installed.json: %v", err)
	}
	var ins map[string]any
	if err := json.Unmarshal(data, &ins); err != nil {
		t.Fatalf("parsing installed.json: %v", err)
	}
	if ins["namespace"] != "github.com/eyelock/assistants" {
		t.Errorf("installed.json namespace = %v, want github.com/eyelock/assistants", ins["namespace"])
	}

	if len(m.Entries) != 1 {
		t.Fatalf("expected 1 manifest entry, got %d", len(m.Entries))
	}
	if m.Entries[0].NewID != "github.com/eyelock/assistants/assistants" {
		t.Errorf("manifest NewID = %q", m.Entries[0].NewID)
	}
}

func TestMigrateOneInstall_NamespacedParent(t *testing.T) {
	home := t.TempDir()
	harnessesDir := filepath.Join(home, "harnesses")
	// Two-level legacy layout: harnesses/eyelock--assistants/my-harness/
	nsParent := filepath.Join(harnessesDir, "eyelock--assistants")
	_ = os.MkdirAll(nsParent, 0o755)
	makeHarnessFixture(t, nsParent, "my-harness", "git", "https://github.com/eyelock/assistants")

	m, err := MigrateToSchema2(home, MigrateOpts{})
	if err != nil {
		t.Fatalf("MigrateToSchema2: %v", err)
	}

	newPath := filepath.Join(harnessesDir, "github.com--eyelock--assistants--my-harness")
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("expected harness at %s: %v", newPath, err)
	}
	// Namespaced parent dir is removed once all children are migrated.
	if _, err := os.Stat(nsParent); !os.IsNotExist(err) {
		t.Errorf("namespaced parent dir still exists after migration")
	}

	if len(m.Entries) != 1 {
		t.Fatalf("expected 1 manifest entry, got %d", len(m.Entries))
	}
	if m.Entries[0].Kind != "install_tree_ns" {
		t.Errorf("manifest kind = %q, want install_tree_ns", m.Entries[0].Kind)
	}
}

func TestMigrateOneInstall_DryRun(t *testing.T) {
	home := t.TempDir()
	harnessesDir := filepath.Join(home, "harnesses")
	_ = os.MkdirAll(harnessesDir, 0o755)
	makeHarnessFixture(t, harnessesDir, "planner", "local", "/path/to/planner")

	m, err := MigrateToSchema2(home, MigrateOpts{DryRun: true})
	if err != nil {
		t.Fatalf("MigrateToSchema2 dry-run: %v", err)
	}

	// On-disk layout unchanged.
	if _, err := os.Stat(filepath.Join(harnessesDir, "planner")); err != nil {
		t.Errorf("dry-run removed harness dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(harnessesDir, "local--planner")); !os.IsNotExist(err) {
		t.Errorf("dry-run created renamed dir")
	}

	if len(m.Entries) != 1 {
		t.Fatalf("expected 1 dry-run manifest entry, got %d", len(m.Entries))
	}
	if m.Entries[0].Kind != "install_tree_flat" {
		t.Errorf("dry-run manifest kind = %q, want install_tree_flat", m.Entries[0].Kind)
	}
}

func TestMigrateOneInstall_MissingInstalledJSON_Aborts(t *testing.T) {
	home := t.TempDir()
	harnessesDir := filepath.Join(home, "harnesses")
	pluginDir := filepath.Join(harnessesDir, "planner", ".ynh-plugin")
	_ = os.MkdirAll(pluginDir, 0o755)
	// plugin.json present, but no installed.json
	_ = os.WriteFile(filepath.Join(pluginDir, "plugin.json"),
		[]byte(`{"name":"planner","version":"0.1.0"}`), 0o644)

	_, err := MigrateToSchema2(home, MigrateOpts{})
	if err == nil {
		t.Fatal("expected migration to abort on missing installed.json")
	}
	if !errors.Is(err, ErrMigrationAborted) {
		t.Errorf("expected ErrMigrationAborted, got: %v", err)
	}
}

func TestMigrateOneInstall_MissingInstalledJSON_QuarantinedWithSkipBroken(t *testing.T) {
	home := t.TempDir()
	harnessesDir := filepath.Join(home, "harnesses")
	pluginDir := filepath.Join(harnessesDir, "planner", ".ynh-plugin")
	_ = os.MkdirAll(pluginDir, 0o755)
	_ = os.WriteFile(filepath.Join(pluginDir, "plugin.json"),
		[]byte(`{"name":"planner","version":"0.1.0"}`), 0o644)

	m, err := MigrateToSchema2(home, MigrateOpts{SkipBroken: true})
	if err != nil {
		t.Fatalf("with --skip-broken: %v", err)
	}
	if len(m.Quarantined) != 1 {
		t.Fatalf("expected 1 quarantined entry, got %d", len(m.Quarantined))
	}
	if _, err := os.Stat(filepath.Join(harnessesDir, "planner")); !os.IsNotExist(err) {
		t.Errorf("broken harness still in harnesses dir")
	}
}

func TestMigrateOneInstall_AlreadyMigrated_Idempotent(t *testing.T) {
	home := t.TempDir()
	harnessesDir := filepath.Join(home, "harnesses")
	_ = os.MkdirAll(harnessesDir, 0o755)
	makeHarnessFixture(t, harnessesDir, "planner", "local", "/path/to/planner")

	// First pass — migrates to local--planner.
	if _, err := MigrateToSchema2(home, MigrateOpts{}); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	// Second pass — must be a no-op.
	m2, err := MigrateToSchema2(home, MigrateOpts{})
	if err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if len(m2.Entries) != 0 {
		t.Errorf("idempotent re-run produced entries: %+v", m2.Entries)
	}
	if len(m2.Quarantined) != 0 {
		t.Errorf("idempotent re-run produced quarantined entries: %+v", m2.Quarantined)
	}
}
