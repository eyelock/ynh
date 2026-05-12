package harness

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

// writeForkTree creates a minimal harness tree at dir with given name.
func writeForkTree(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, plugin.PluginDir), 0o755); err != nil {
		t.Fatal(err)
	}
	hj := `{"name":"` + name + `","version":"0.1.0","default_vendor":"claude"}`
	if err := os.WriteFile(filepath.Join(dir, plugin.PluginDir, plugin.PluginFile), []byte(hj), 0o644); err != nil {
		t.Fatal(err)
	}
	ins := &plugin.InstalledJSON{
		SourceType:  "local",
		Source:      dir,
		InstalledAt: "2026-05-01T00:00:00Z",
		ForkedFrom: &plugin.ForkedFromJSON{
			SourceType: "git",
			Source:     "github.com/example/" + name,
		},
	}
	if err := plugin.SaveInstalledJSON(dir, ins); err != nil {
		t.Fatal(err)
	}
}

func TestPointer_SaveLoadRoundTrip(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	ptr := &Pointer{
		Name: "researcher",
		InstalledJSON: plugin.InstalledJSON{
			SourceType:  "local",
			Source:      "/users/x/work/researcher",
			InstalledAt: "2026-05-01T00:00:00Z",
		},
	}
	if err := SavePointer(ptr); err != nil {
		t.Fatalf("SavePointer: %v", err)
	}
	got, err := LoadPointer("researcher")
	if err != nil {
		t.Fatalf("LoadPointer: %v", err)
	}
	if got == nil {
		t.Fatal("LoadPointer returned nil")
	}
	if got.Name != ptr.Name || got.Source != ptr.Source {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestPointer_LoadMissing(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	got, err := LoadPointer("nope")
	if err != nil {
		t.Fatalf("LoadPointer error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil pointer, got %+v", got)
	}
}

func TestLoadByID_PointerBeatsTreePrecedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	// Create a tree-shaped install at the schema-2 path
	treeDir := filepath.Join(home, "harnesses", "local--researcher")
	writeForkTree(t, treeDir, "researcher")

	// Create a fork tree elsewhere and register it via a schema-1 pointer
	// (simulating a fork written by an older binary, exercising the fallback
	// in LoadByID that reads name-keyed pointer files).
	forkDir := filepath.Join(t.TempDir(), "researcher")
	writeForkTree(t, forkDir, "researcher")
	if err := SavePointer(&Pointer{
		Name: "researcher",
		InstalledJSON: plugin.InstalledJSON{
			SourceType:  "local",
			Source:      forkDir,
			InstalledAt: "2026-05-01T00:00:00Z",
		},
	}); err != nil {
		t.Fatal(err)
	}

	p, err := LoadByID("local/researcher")
	if err != nil {
		t.Fatalf("LoadByID: %v", err)
	}
	// Pointer must win — Dir resolves to forkDir, not the tree.
	absFork, _ := filepath.Abs(forkDir)
	if p.Dir != absFork {
		t.Errorf("LoadByID resolved to %q, want %q (pointer should beat tree)", p.Dir, absFork)
	}
}

func TestLoadByID_PointerWithMissingSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	// Schema-1 pointer (name-keyed) pointing at a path that no longer exists,
	// exercising the LoadByID schema-1 fallback for "local/<name>" ids.
	if err := SavePointer(&Pointer{
		Name: "ghost",
		InstalledJSON: plugin.InstalledJSON{
			SourceType:  "local",
			Source:      filepath.Join(t.TempDir(), "deleted"),
			InstalledAt: "2026-05-01T00:00:00Z",
		},
	}); err != nil {
		t.Fatal(err)
	}

	_, err := LoadByID("local/ghost")
	if err == nil {
		t.Fatal("expected error for missing pointer source")
	}
	msg := err.Error()
	for _, want := range []string{"ghost", "no longer exists", "ynh uninstall"} {
		if !contains(msg, want) {
			t.Errorf("error missing %q substring: %v", want, msg)
		}
	}
}

func TestListAll_UnionsPointersAndTrees(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	// Tree-shaped install
	treeDir := filepath.Join(home, "harnesses", "tree-one")
	writeForkTree(t, treeDir, "tree-one")

	// Pointer-shaped install
	forkDir := filepath.Join(t.TempDir(), "fork-one")
	writeForkTree(t, forkDir, "fork-one")
	if err := SavePointer(&Pointer{
		Name: "fork-one",
		InstalledJSON: plugin.InstalledJSON{
			SourceType:  "local",
			Source:      forkDir,
			InstalledAt: "2026-05-01T00:00:00Z",
		},
	}); err != nil {
		t.Fatal(err)
	}

	entries, err := ListAll()
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	names := map[string]string{}
	for _, e := range entries {
		names[e.Name] = e.Dir
	}
	if _, ok := names["tree-one"]; !ok {
		t.Errorf("ListAll missing tree entry; got %+v", names)
	}
	if got, ok := names["fork-one"]; !ok {
		t.Errorf("ListAll missing pointer entry; got %+v", names)
	} else if abs, _ := filepath.Abs(forkDir); got != abs {
		t.Errorf("pointer entry Dir = %q, want %q", got, abs)
	}
}

func TestListAll_PointerWinsOverTreeOnDuplicate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	// Both a flat tree and a pointer for the same name (legacy state).
	treeDir := filepath.Join(home, "harnesses", "dup")
	writeForkTree(t, treeDir, "dup")

	forkDir := filepath.Join(t.TempDir(), "dup")
	writeForkTree(t, forkDir, "dup")
	if err := SavePointer(&Pointer{
		Name: "dup",
		InstalledJSON: plugin.InstalledJSON{
			SourceType:  "local",
			Source:      forkDir,
			InstalledAt: "2026-05-01T00:00:00Z",
		},
	}); err != nil {
		t.Fatal(err)
	}

	entries, err := ListAll()
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	count := 0
	var dupDir string
	for _, e := range entries {
		if e.Name == "dup" {
			count++
			dupDir = e.Dir
		}
	}
	if count != 1 {
		t.Errorf("expected exactly one entry for 'dup', got %d", count)
	}
	absFork, _ := filepath.Abs(forkDir)
	if dupDir != absFork {
		t.Errorf("dup entry Dir = %q, want %q (pointer should win)", dupDir, absFork)
	}
}

func TestRemovePointer_Idempotent(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	if err := RemovePointer("nonexistent"); err != nil {
		t.Errorf("RemovePointer on missing: %v", err)
	}
	if err := SavePointer(&Pointer{
		Name: "x",
		InstalledJSON: plugin.InstalledJSON{
			SourceType:  "local",
			Source:      "/tmp/x",
			InstalledAt: "now",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := RemovePointer("x"); err != nil {
		t.Errorf("RemovePointer: %v", err)
	}
	if got, _ := LoadPointer("x"); got != nil {
		t.Errorf("pointer still present after remove: %+v", got)
	}
}

// TestLoadByID_Schema1PointerFallback verifies that LoadByID("local/<name>")
// resolves a schema-1 name-keyed pointer file (<name>.json) when no schema-2
// id-keyed file (local--<name>.json) exists. This is the regression test for
// the bug where ynh fork wrote schema-1 pointers into an already-schema-2 home
// and ynh info / include update / delegate add all returned "not found".
func TestLoadByID_Schema1PointerFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	forkDir := t.TempDir()
	writeForkTree(t, forkDir, "ynh-dev")

	// Write a schema-1 pointer: <name>.json (no id field, no local-- prefix).
	if err := SavePointer(&Pointer{
		Name: "ynh-dev",
		InstalledJSON: plugin.InstalledJSON{
			SourceType:  "local",
			Source:      forkDir,
			InstalledAt: "2026-05-08T19:26:52Z",
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Schema-2 id-keyed file must NOT exist (confirms we're testing the fallback).
	schema2Path := PointerPathByID("local/ynh-dev")
	if _, err := os.Stat(schema2Path); err == nil {
		t.Fatalf("unexpected schema-2 pointer at %s", schema2Path)
	}

	p, err := LoadByID("local/ynh-dev")
	if err != nil {
		t.Fatalf("LoadByID: %v", err)
	}
	if p.Name != "ynh-dev" {
		t.Errorf("Name = %q, want ynh-dev", p.Name)
	}
}

// A fork (local/<name>) and a remote registry install sharing the same leaf
// name but with distinct canonical ids must both appear in ListAll. The
// canonical-id work (#127) made this possible; deduplicating by bare name
// was the regression.
func TestListAll_ForkAndRegistryInstallSameLeafName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	// Pointer-shaped fork: local/termq-dev
	forkDir := filepath.Join(t.TempDir(), "termq-dev")
	writeForkTree(t, forkDir, "termq-dev")
	if err := SavePointer(&Pointer{
		Name: "termq-dev",
		InstalledJSON: plugin.InstalledJSON{
			SourceType:  "local",
			Source:      forkDir,
			InstalledAt: "2026-05-01T00:00:00Z",
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Schema-2 registry install: github.com/eyelock/assistants/termq-dev
	// Dir name is the id-fsname: github.com--eyelock--assistants--termq-dev
	registryDir := filepath.Join(home, "harnesses", "github.com--eyelock--assistants--termq-dev")
	writeForkTree(t, registryDir, "termq-dev")

	entries, err := ListAll()
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}

	ids := map[string]string{} // id → dir
	for _, e := range entries {
		var id string
		if e.Namespace == "" {
			id = "local/" + e.Name
		} else {
			id = e.Namespace + "/" + e.Name
		}
		ids[id] = e.Dir
	}

	if _, ok := ids["local/termq-dev"]; !ok {
		t.Errorf("ListAll missing local/termq-dev; got %+v", ids)
	}
	if _, ok := ids["github.com/eyelock/assistants/termq-dev"]; !ok {
		t.Errorf("ListAll missing github.com/eyelock/assistants/termq-dev; got %+v", ids)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d: %+v", len(entries), entries)
	}
}

// contains is a no-stdlib substring check to avoid pulling in strings.Contains
// for a single helper. Keeps the test file small and obvious.
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
