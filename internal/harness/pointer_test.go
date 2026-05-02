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
		Name:        "researcher",
		SourceType:  "local",
		Source:      "/users/x/work/researcher",
		InstalledAt: "2026-05-01T00:00:00Z",
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

func TestLoad_PointerBeatsTreePrecedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	// Create a tree-shaped install at ~/.ynh/harnesses/researcher
	treeDir := filepath.Join(home, "harnesses", "researcher")
	writeForkTree(t, treeDir, "researcher")

	// Create a fork tree elsewhere and a pointer registering it
	forkDir := filepath.Join(t.TempDir(), "researcher")
	writeForkTree(t, forkDir, "researcher")
	if err := SavePointer(&Pointer{
		Name:        "researcher",
		SourceType:  "local",
		Source:      forkDir,
		InstalledAt: "2026-05-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}

	p, err := Load("researcher")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Pointer must win — Dir resolves to forkDir, not the flat tree.
	absFork, _ := filepath.Abs(forkDir)
	if p.Dir != absFork {
		t.Errorf("Load resolved to %q, want %q (pointer should beat flat tree)", p.Dir, absFork)
	}
}

func TestLoad_PointerWithMissingSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	// Pointer references a path that doesn't exist
	if err := SavePointer(&Pointer{
		Name:        "ghost",
		SourceType:  "local",
		Source:      filepath.Join(t.TempDir(), "deleted"),
		InstalledAt: "2026-05-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}

	_, err := Load("ghost")
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
		Name: "fork-one", SourceType: "local", Source: forkDir,
		InstalledAt: "2026-05-01T00:00:00Z",
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
		Name: "dup", SourceType: "local", Source: forkDir,
		InstalledAt: "2026-05-01T00:00:00Z",
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
		Name: "x", SourceType: "local", Source: "/tmp/x", InstalledAt: "now",
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
