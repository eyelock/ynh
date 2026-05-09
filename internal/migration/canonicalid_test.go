package migration

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
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

// ---- install tree tests ----

// writeInstallTree creates a minimal schema-1 plugin dir under harnessesDir/name
// with the given source URL (empty = local).
func writeInstallTree(t *testing.T, harnessesDir, name, source string) string {
	t.Helper()
	dir := filepath.Join(harnessesDir, name)
	writeFile(t, filepath.Join(dir, plugin.PluginDir, plugin.PluginFile),
		`{"name":"`+name+`","version":"1.0.0"}`)
	writeFile(t, filepath.Join(dir, plugin.PluginDir, plugin.InstalledFile),
		`{"source_type":"git","source":"`+source+`","installed_at":"2026-04-01T00:00:00Z"}`)
	return dir
}

func TestMigrate_FlatInstallTree_Local(t *testing.T) {
	home := t.TempDir()
	harnessesDir := filepath.Join(home, "harnesses")
	if err := os.MkdirAll(harnessesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeInstallTree(t, harnessesDir, "planner", "")

	m, err := MigrateToSchema2(home, MigrateOpts{})
	if err != nil {
		t.Fatalf("MigrateToSchema2: %v", err)
	}

	// Old flat dir is gone; schema-2 path exists.
	oldDir := filepath.Join(harnessesDir, "planner")
	newDir := filepath.Join(harnessesDir, "local--planner")
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Errorf("old flat dir still exists")
	}
	if _, err := os.Stat(newDir); err != nil {
		t.Fatalf("schema-2 dir missing: %v", err)
	}
	// installed.json namespace stamped.
	ins, err := plugin.LoadInstalledJSON(newDir)
	if err != nil {
		t.Fatalf("LoadInstalledJSON: %v", err)
	}
	if ins.Namespace != "local" {
		t.Errorf("namespace = %q, want %q", ins.Namespace, "local")
	}
	// Manifest entry.
	if len(m.Entries) == 0 {
		t.Fatal("expected at least one manifest entry")
	}
	var found bool
	for _, e := range m.Entries {
		if e.Kind == "install_tree_flat" && e.OldID == "planner" && e.NewID == "local/planner" {
			found = true
		}
	}
	if !found {
		t.Errorf("install_tree_flat entry missing: %+v", m.Entries)
	}
}

func TestMigrate_FlatInstallTree_Remote(t *testing.T) {
	home := t.TempDir()
	harnessesDir := filepath.Join(home, "harnesses")
	_ = os.MkdirAll(harnessesDir, 0o755)
	writeInstallTree(t, harnessesDir, "david", "https://github.com/eyelock/assistants")

	m, err := MigrateToSchema2(home, MigrateOpts{})
	if err != nil {
		t.Fatalf("MigrateToSchema2: %v", err)
	}

	newDir := filepath.Join(harnessesDir, "github.com--eyelock--assistants--david")
	if _, err := os.Stat(newDir); err != nil {
		t.Fatalf("expected schema-2 dir at %s: %v", newDir, err)
	}
	ins, err := plugin.LoadInstalledJSON(newDir)
	if err != nil {
		t.Fatalf("LoadInstalledJSON: %v", err)
	}
	wantNS := "github.com/eyelock/assistants"
	if ins.Namespace != wantNS {
		t.Errorf("namespace = %q, want %q", ins.Namespace, wantNS)
	}
	if len(m.Entries) == 0 || m.Entries[0].NewID != "github.com/eyelock/assistants/david" {
		t.Errorf("manifest NewID = %+v", m.Entries)
	}
}

func TestMigrate_FlatInstallTree_DryRun(t *testing.T) {
	home := t.TempDir()
	harnessesDir := filepath.Join(home, "harnesses")
	_ = os.MkdirAll(harnessesDir, 0o755)
	writeInstallTree(t, harnessesDir, "planner", "")

	m, err := MigrateToSchema2(home, MigrateOpts{DryRun: true})
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}

	// No on-disk changes.
	oldDir := filepath.Join(harnessesDir, "planner")
	if _, err := os.Stat(oldDir); err != nil {
		t.Errorf("dry-run removed flat dir: %v", err)
	}
	newDir := filepath.Join(harnessesDir, "local--planner")
	if _, err := os.Stat(newDir); !os.IsNotExist(err) {
		t.Errorf("dry-run created schema-2 dir")
	}
	// Manifest still populated.
	if len(m.Entries) == 0 {
		t.Error("dry-run manifest should have entries")
	}
}

func TestMigrate_FlatInstallTree_Idempotent(t *testing.T) {
	home := t.TempDir()
	harnessesDir := filepath.Join(home, "harnesses")
	_ = os.MkdirAll(harnessesDir, 0o755)
	writeInstallTree(t, harnessesDir, "planner", "")

	if _, err := MigrateToSchema2(home, MigrateOpts{}); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	m2, err := MigrateToSchema2(home, MigrateOpts{})
	if err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	// Second run must not produce install_tree_flat entries.
	for _, e := range m2.Entries {
		if e.Kind == "install_tree_flat" {
			t.Errorf("idempotent re-run produced install_tree_flat entry: %+v", e)
		}
	}
}

func TestMigrate_FlatInstallTree_MissingInstalledJSON_Aborts(t *testing.T) {
	home := t.TempDir()
	harnessesDir := filepath.Join(home, "harnesses")
	_ = os.MkdirAll(harnessesDir, 0o755)

	// Plugin dir exists but no installed.json.
	dir := filepath.Join(harnessesDir, "badharness")
	writeFile(t, filepath.Join(dir, plugin.PluginDir, plugin.PluginFile),
		`{"name":"badharness","version":"1.0.0"}`)

	_, err := MigrateToSchema2(home, MigrateOpts{})
	if err == nil {
		t.Fatal("expected abort on missing installed.json")
	}
	if !errors.Is(err, ErrMigrationAborted) {
		t.Errorf("expected ErrMigrationAborted, got: %v", err)
	}
}

func TestMigrate_FlatInstallTree_MissingInstalledJSON_SkipBroken(t *testing.T) {
	home := t.TempDir()
	harnessesDir := filepath.Join(home, "harnesses")
	_ = os.MkdirAll(harnessesDir, 0o755)

	dir := filepath.Join(harnessesDir, "badharness")
	writeFile(t, filepath.Join(dir, plugin.PluginDir, plugin.PluginFile),
		`{"name":"badharness","version":"1.0.0"}`)

	m, err := MigrateToSchema2(home, MigrateOpts{SkipBroken: true})
	if err != nil {
		t.Fatalf("SkipBroken: %v", err)
	}
	if len(m.Quarantined) != 1 {
		t.Fatalf("expected 1 quarantined, got %d", len(m.Quarantined))
	}
	// Dir moved out.
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("broken harness dir still present")
	}
}

func TestMigrate_NonPluginDir_Skipped(t *testing.T) {
	home := t.TempDir()
	harnessesDir := filepath.Join(home, "harnesses")
	_ = os.MkdirAll(harnessesDir, 0o755)

	// A random dir with no plugin manifests — should be ignored.
	empty := filepath.Join(harnessesDir, "somedir")
	_ = os.MkdirAll(empty, 0o755)

	m, err := MigrateToSchema2(home, MigrateOpts{})
	if err != nil {
		t.Fatalf("MigrateToSchema2: %v", err)
	}
	for _, e := range m.Entries {
		if e.OldID == "somedir" {
			t.Errorf("non-plugin dir produced manifest entry: %+v", e)
		}
	}
}

// ---- namespaced parent tests ----

func TestMigrate_NamespacedParent(t *testing.T) {
	home := t.TempDir()
	harnessesDir := filepath.Join(home, "harnesses")
	_ = os.MkdirAll(harnessesDir, 0o755)

	// Legacy two-level layout: harnesses/eyelock--assistants/david/
	parent := filepath.Join(harnessesDir, "eyelock--assistants")
	child := filepath.Join(parent, "david")
	writeFile(t, filepath.Join(child, plugin.PluginDir, plugin.PluginFile),
		`{"name":"david","version":"1.0.0"}`)
	writeFile(t, filepath.Join(child, plugin.PluginDir, plugin.InstalledFile),
		`{"source_type":"git","source":"https://github.com/eyelock/assistants","installed_at":"2026-04-01T00:00:00Z"}`)

	m, err := MigrateToSchema2(home, MigrateOpts{})
	if err != nil {
		t.Fatalf("MigrateToSchema2: %v", err)
	}

	// Parent dir removed (was empty after child migrated).
	if _, err := os.Stat(parent); !os.IsNotExist(err) {
		t.Errorf("legacy namespaced parent dir still exists")
	}
	// Schema-2 flat path.
	newDir := filepath.Join(harnessesDir, "github.com--eyelock--assistants--david")
	if _, err := os.Stat(newDir); err != nil {
		t.Fatalf("schema-2 dir missing: %v", err)
	}
	// Manifest entry kind.
	var found bool
	for _, e := range m.Entries {
		if e.Kind == "install_tree_ns" && e.OldID == "david" && e.NewID == "github.com/eyelock/assistants/david" {
			found = true
		}
	}
	if !found {
		t.Errorf("install_tree_ns entry missing: %+v", m.Entries)
	}
}

func TestMigrate_NamespacedParent_DryRun(t *testing.T) {
	home := t.TempDir()
	harnessesDir := filepath.Join(home, "harnesses")
	_ = os.MkdirAll(harnessesDir, 0o755)

	parent := filepath.Join(harnessesDir, "eyelock--assistants")
	child := filepath.Join(parent, "david")
	writeFile(t, filepath.Join(child, plugin.PluginDir, plugin.PluginFile),
		`{"name":"david","version":"1.0.0"}`)
	writeFile(t, filepath.Join(child, plugin.PluginDir, plugin.InstalledFile),
		`{"source_type":"git","source":"https://github.com/eyelock/assistants","installed_at":"2026-04-01T00:00:00Z"}`)

	m, err := MigrateToSchema2(home, MigrateOpts{DryRun: true})
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	// Parent still exists.
	if _, err := os.Stat(parent); err != nil {
		t.Errorf("dry-run removed parent dir")
	}
	// Manifest populated.
	if len(m.Entries) == 0 {
		t.Error("dry-run manifest should have entries")
	}
}

func TestMigrate_NamespacedParent_SkipBroken(t *testing.T) {
	home := t.TempDir()
	harnessesDir := filepath.Join(home, "harnesses")
	_ = os.MkdirAll(harnessesDir, 0o755)

	// Child harness with no installed.json → quarantine.
	parent := filepath.Join(harnessesDir, "eyelock--assistants")
	child := filepath.Join(parent, "david")
	writeFile(t, filepath.Join(child, plugin.PluginDir, plugin.PluginFile),
		`{"name":"david","version":"1.0.0"}`)

	m, err := MigrateToSchema2(home, MigrateOpts{SkipBroken: true})
	if err != nil {
		t.Fatalf("SkipBroken: %v", err)
	}
	if len(m.Quarantined) != 1 {
		t.Fatalf("expected 1 quarantined, got %d", len(m.Quarantined))
	}
}

// ---- loadHarnessName legacy fallback ----

func TestMigrate_InstallTree_LegacyHarnessNameFallback(t *testing.T) {
	home := t.TempDir()
	harnessesDir := filepath.Join(home, "harnesses")
	_ = os.MkdirAll(harnessesDir, 0o755)

	// A dir that qualifies as IsLegacyPluginDir (.claude-plugin/plugin.json)
	// but has NO .ynh-plugin/plugin.json, so loadHarnessName falls through
	// to LoadHarnessJSON (.harness.json).
	dir := filepath.Join(harnessesDir, "legacy")
	writeFile(t, filepath.Join(dir, ".claude-plugin", "plugin.json"), `{"name":"legacy","version":"1.0.0"}`)
	writeFile(t, filepath.Join(dir, plugin.HarnessFile), `{"name":"legacy","version":"1.0.0"}`)
	writeFile(t, filepath.Join(dir, plugin.PluginDir, plugin.InstalledFile),
		`{"source_type":"local","source":"","installed_at":"2026-04-01T00:00:00Z"}`)

	m, err := MigrateToSchema2(home, MigrateOpts{})
	if err != nil {
		t.Fatalf("MigrateToSchema2: %v", err)
	}

	newDir := filepath.Join(harnessesDir, "local--legacy")
	if _, err := os.Stat(newDir); err != nil {
		t.Fatalf("schema-2 dir missing: %v", err)
	}
	var found bool
	for _, e := range m.Entries {
		if e.NewID == "local/legacy" {
			found = true
		}
	}
	if !found {
		t.Errorf("manifest missing local/legacy entry: %+v", m.Entries)
	}
}

// ---- installExistsForLauncher ----

func TestMigrate_UnknownLauncher_WithBackingPointer(t *testing.T) {
	home := t.TempDir()
	binDir := filepath.Join(home, "bin")
	installedDir := filepath.Join(home, "installed")
	_ = os.MkdirAll(binDir, 0o755)
	_ = os.MkdirAll(installedDir, 0o755)

	// Schema-2 pointer already exists (no migration needed for the pointer).
	writeFile(t, filepath.Join(installedDir, "local--custom.json"),
		`{"id":"local/custom","name":"custom","source_type":"local","source":"","installed_at":"2026-04-01T00:00:00Z"}`)

	// Launcher for "custom" with legacy bare-name content.
	launcherPath := filepath.Join(binDir, "custom")
	_ = os.WriteFile(launcherPath, []byte("#!/bin/bash\n# Generated by ynh - do not edit\nexec ynh run \"custom\" \"$@\"\n"), 0o755)

	m, err := MigrateToSchema2(home, MigrateOpts{})
	if err != nil {
		t.Fatalf("MigrateToSchema2: %v", err)
	}

	// Launcher should be rewritten with canonical id.
	got, err := os.ReadFile(launcherPath)
	if err != nil {
		t.Fatalf("read launcher: %v", err)
	}
	want := "#!/bin/bash\n# Generated by ynh - do not edit\nexec ynh run \"local/custom\" \"$@\"\n"
	if string(got) != want {
		t.Errorf("launcher not rewritten:\n got:  %q\n want: %q", got, want)
	}
	var found bool
	for _, e := range m.Entries {
		if e.Kind == "launcher" && e.OldID == "custom" {
			found = true
		}
	}
	if !found {
		t.Errorf("launcher entry missing: %+v", m.Entries)
	}
}

func TestMigrate_UnknownLauncher_WithBackingInstallTree(t *testing.T) {
	home := t.TempDir()
	binDir := filepath.Join(home, "bin")
	harnessesDir := filepath.Join(home, "harnesses")
	_ = os.MkdirAll(binDir, 0o755)
	_ = os.MkdirAll(harnessesDir, 0o755)

	// Schema-2 install tree exists at local--custom (no pointer file).
	treeDir := filepath.Join(harnessesDir, "local--custom")
	writeFile(t, filepath.Join(treeDir, plugin.PluginDir, plugin.PluginFile),
		`{"name":"custom","version":"1.0.0"}`)
	writeFile(t, filepath.Join(treeDir, plugin.PluginDir, plugin.InstalledFile),
		`{"id":"local/custom","namespace":"local","source_type":"local","source":"","installed_at":"2026-04-01T00:00:00Z"}`)

	// Launcher with legacy content.
	launcherPath := filepath.Join(binDir, "custom")
	_ = os.WriteFile(launcherPath, []byte("#!/bin/bash\n# Generated by ynh - do not edit\nexec ynh run \"custom\" \"$@\"\n"), 0o755)

	_, err := MigrateToSchema2(home, MigrateOpts{})
	if err != nil {
		t.Fatalf("MigrateToSchema2: %v", err)
	}

	// Launcher rewritten.
	got, err := os.ReadFile(launcherPath)
	if err != nil {
		t.Fatalf("read launcher: %v", err)
	}
	want := "#!/bin/bash\n# Generated by ynh - do not edit\nexec ynh run \"local/custom\" \"$@\"\n"
	if string(got) != want {
		t.Errorf("launcher not rewritten:\n got:  %q\n want: %q", got, want)
	}
}

func TestMigrate_UnknownLauncher_OrphanSkipped(t *testing.T) {
	home := t.TempDir()
	binDir := filepath.Join(home, "bin")
	_ = os.MkdirAll(binDir, 0o755)

	// No pointer file, no install tree → orphan.
	launcherPath := filepath.Join(binDir, "orphan")
	originalContent := "#!/bin/bash\n# Generated by ynh - do not edit\nexec ynh run \"orphan\" \"$@\"\n"
	_ = os.WriteFile(launcherPath, []byte(originalContent), 0o755)

	_, err := MigrateToSchema2(home, MigrateOpts{})
	if err != nil {
		t.Fatalf("MigrateToSchema2: %v", err)
	}

	// Launcher left unchanged.
	got, err := os.ReadFile(launcherPath)
	if err != nil {
		t.Fatalf("read launcher: %v", err)
	}
	if string(got) != originalContent {
		t.Errorf("orphan launcher was modified: %q", got)
	}
}

func TestMigrate_AlreadyMigratedLauncher_Skipped(t *testing.T) {
	home := t.TempDir()
	binDir := filepath.Join(home, "bin")
	installedDir := filepath.Join(home, "installed")
	_ = os.MkdirAll(binDir, 0o755)
	_ = os.MkdirAll(installedDir, 0o755)

	// Schema-2 pointer.
	writeFile(t, filepath.Join(installedDir, "local--mytool.json"),
		`{"id":"local/mytool","name":"mytool","source_type":"local","source":"","installed_at":"2026-04-01T00:00:00Z"}`)

	// Launcher already has the canonical id content.
	launcherPath := filepath.Join(binDir, "mytool")
	alreadyMigrated := "#!/bin/bash\n# Generated by ynh - do not edit\nexec ynh run \"local/mytool\" \"$@\"\n"
	_ = os.WriteFile(launcherPath, []byte(alreadyMigrated), 0o755)

	m, err := MigrateToSchema2(home, MigrateOpts{})
	if err != nil {
		t.Fatalf("MigrateToSchema2: %v", err)
	}

	// Already-migrated launcher must not produce a manifest entry.
	for _, e := range m.Entries {
		if e.Kind == "launcher" && e.OldID == "mytool" {
			t.Errorf("already-migrated launcher produced manifest entry: %+v", e)
		}
	}
}

// ---- ReadManifest edge cases ----

func TestReadManifest_NoFile_ReturnsNil(t *testing.T) {
	home := t.TempDir()
	m, err := ReadManifest(home)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if m != nil {
		t.Errorf("expected nil manifest on missing file, got %+v", m)
	}
}

func TestReadManifest_CorruptFile_ReturnsError(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, MigrationManifestPath), `{not json}`)
	_, err := ReadManifest(home)
	if err == nil {
		t.Error("expected error on corrupt manifest")
	}
}

// ---- WriteSchemaVersion creates parent dir ----

func TestWriteSchemaVersion_CreatesDir(t *testing.T) {
	home := t.TempDir()
	nested := filepath.Join(home, "newdir")
	if err := WriteSchemaVersion(nested, 2); err != nil {
		t.Fatalf("WriteSchemaVersion: %v", err)
	}
	if v := ReadSchemaVersion(nested); v != 2 {
		t.Errorf("schema version = %d, want 2", v)
	}
}

// ---- Description methods ----

func TestDescriptions(t *testing.T) {
	var hfm HarnessFormatMigrator
	if hfm.Description() == "" {
		t.Error("HarnessFormatMigrator.Description() is empty")
	}
	var hsm HarnessStorageMigrator
	if hsm.Description() == "" {
		t.Error("HarnessStorageMigrator.Description() is empty")
	}
	var rfm RegistryFormatMigrator
	if rfm.Description() == "" {
		t.Error("RegistryFormatMigrator.Description() is empty")
	}
}

// ---- splitHostFromID ----

func TestSplitHostFromID(t *testing.T) {
	cases := []struct {
		id       string
		wantHost string
		wantRest string
	}{
		{"github.com/eyelock/assistants/david", "github.com", "eyelock/assistants/david"},
		{"local/planner", "local", "planner"},
		{"noslash", "", "noslash"},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			host, rest := splitHostFromID(c.id)
			if host != c.wantHost || rest != c.wantRest {
				t.Errorf("splitHostFromID(%q) = (%q, %q), want (%q, %q)",
					c.id, host, rest, c.wantHost, c.wantRest)
			}
		})
	}
}

// ---- manifest not written on no-op migration ----

func TestMigrate_EmptyHome_NoManifest(t *testing.T) {
	home := t.TempDir()
	if _, err := MigrateToSchema2(home, MigrateOpts{}); err != nil {
		t.Fatalf("MigrateToSchema2: %v", err)
	}
	m, err := ReadManifest(home)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if m != nil {
		t.Errorf("expected no manifest on empty-home migration, got %+v", m)
	}
}

// ---- quarantine dry-run path ----

func TestMigrate_BrokenPointer_DryRunQuarantine(t *testing.T) {
	home := t.TempDir()
	pointersDir := filepath.Join(home, "installed")
	_ = os.MkdirAll(pointersDir, 0o755)
	brokenPath := filepath.Join(pointersDir, "broken.json")
	_ = os.WriteFile(brokenPath, []byte(`{not json`), 0o644)

	m, err := MigrateToSchema2(home, MigrateOpts{DryRun: true, SkipBroken: true})
	if err != nil {
		t.Fatalf("dry-run with SkipBroken: %v", err)
	}
	if len(m.Quarantined) != 1 {
		t.Fatalf("expected 1 quarantined in dry-run, got %d", len(m.Quarantined))
	}
	if m.Quarantined[0].Quarantined != "(dry-run)" {
		t.Errorf("dry-run quarantine marker = %q, want (dry-run)", m.Quarantined[0].Quarantined)
	}
	// File must not have moved.
	if _, err := os.Stat(brokenPath); err != nil {
		t.Errorf("dry-run moved the file: %v", err)
	}
}

// ---- pointer with no name ----

func TestMigrate_PointerWithNoName_Aborts(t *testing.T) {
	home := t.TempDir()
	pointersDir := filepath.Join(home, "installed")
	_ = os.MkdirAll(pointersDir, 0o755)
	_ = os.WriteFile(filepath.Join(pointersDir, "noname.json"),
		[]byte(`{"source_type":"local","source":"","installed_at":"2026-04-01T00:00:00Z"}`), 0o644)

	_, err := MigrateToSchema2(home, MigrateOpts{})
	if err == nil {
		t.Fatal("expected error on pointer with no name")
	}
	if !errors.Is(err, ErrMigrationAborted) {
		t.Errorf("expected ErrMigrationAborted, got: %v", err)
	}
}

// ---- quarantine homeFromPath coverage ----

func TestMigrate_HandEditedLauncher_DryRunQuarantine(t *testing.T) {
	home := t.TempDir()
	binDir := filepath.Join(home, "bin")
	pointersDir := filepath.Join(home, "installed")
	_ = os.MkdirAll(binDir, 0o755)
	_ = os.MkdirAll(pointersDir, 0o755)

	_ = os.WriteFile(filepath.Join(pointersDir, "planner.json"),
		[]byte(`{"name":"planner","source_type":"local","source":"","installed_at":"2026-04-01T00:00:00Z"}`), 0o644)

	launcherPath := filepath.Join(binDir, "planner")
	_ = os.WriteFile(launcherPath, []byte("#!/bin/bash\n# hand edited\n"), 0o755)

	m, err := MigrateToSchema2(home, MigrateOpts{DryRun: true, SkipBroken: true})
	if err != nil {
		t.Fatalf("dry-run SkipBroken: %v", err)
	}
	if len(m.Quarantined) == 0 {
		t.Error("expected quarantine entry for hand-edited launcher in dry-run")
	}
	// File must still be there (dry-run).
	if _, err := os.Stat(launcherPath); err != nil {
		t.Errorf("dry-run removed launcher: %v", err)
	}
}

// ---- MigrateToSchema2 with both pointer and install tree ----

func TestMigrate_PointerAndInstallTree(t *testing.T) {
	home := t.TempDir()
	pointersDir := filepath.Join(home, "installed")
	harnessesDir := filepath.Join(home, "harnesses")
	_ = os.MkdirAll(pointersDir, 0o755)
	_ = os.MkdirAll(harnessesDir, 0o755)

	_ = os.WriteFile(filepath.Join(pointersDir, "planner.json"),
		[]byte(`{"name":"planner","source_type":"local","source":"","installed_at":"2026-04-01T00:00:00Z"}`), 0o644)
	writeInstallTree(t, harnessesDir, "planner", "")

	m, err := MigrateToSchema2(home, MigrateOpts{})
	if err != nil {
		t.Fatalf("MigrateToSchema2: %v", err)
	}

	// Two entries: pointer + install_tree_flat.
	kinds := map[string]bool{}
	for _, e := range m.Entries {
		kinds[e.Kind] = true
	}
	if !kinds["pointer"] {
		t.Error("expected pointer entry")
	}
	if !kinds["install_tree_flat"] {
		t.Error("expected install_tree_flat entry")
	}

	// Manifest persisted (has entries).
	persisted, err := ReadManifest(home)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if persisted == nil {
		t.Fatal("expected manifest on disk")
	}
	data, _ := json.Marshal(persisted)
	if len(data) == 0 {
		t.Error("persisted manifest is empty")
	}
}
