package symlink

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eyelock/ynh/internal/vendor"
)

func TestLoadLog_FileNotExists(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("YNH_HOME", "")

	log, err := LoadLog()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(log.Installations) != 0 {
		t.Errorf("expected empty log, got %d installations", len(log.Installations))
	}
}

func TestSaveAndLoad_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("YNH_HOME", "")

	log := &Log{}
	log.Record("david", "cursor", "/home/user/project", []vendor.SymlinkEntry{
		{Target: "/staging/skills/hello", Link: "/project/.cursor/skills/hello"},
	})

	if err := log.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := LoadLog()
	if err != nil {
		t.Fatalf("LoadLog failed: %v", err)
	}

	if len(loaded.Installations) != 1 {
		t.Fatalf("expected 1 installation, got %d", len(loaded.Installations))
	}

	inst := loaded.Installations[0]
	if inst.Persona != "david" {
		t.Errorf("Persona = %q, want %q", inst.Persona, "david")
	}
	if inst.Vendor != "cursor" {
		t.Errorf("Vendor = %q, want %q", inst.Vendor, "cursor")
	}
	if inst.Project != "/home/user/project" {
		t.Errorf("Project = %q", inst.Project)
	}
	if len(inst.Symlinks) != 1 {
		t.Fatalf("Symlinks = %d, want 1", len(inst.Symlinks))
	}
}

func TestRecord_AppendsEntry(t *testing.T) {
	log := &Log{}
	log.Record("alice", "cursor", "/proj1", nil)
	log.Record("bob", "codex", "/proj2", nil)

	if len(log.Installations) != 2 {
		t.Fatalf("expected 2 installations, got %d", len(log.Installations))
	}
}

func TestFindInstallation_ExactMatch(t *testing.T) {
	log := &Log{}
	log.Record("alice", "cursor", "/proj1", []vendor.SymlinkEntry{{Target: "a", Link: "b"}})
	log.Record("bob", "codex", "/proj2", []vendor.SymlinkEntry{{Target: "c", Link: "d"}})

	inst := log.FindInstallation("bob", "codex", "/proj2")
	if inst == nil {
		t.Fatal("expected to find installation")
	}
	if inst.Persona != "bob" {
		t.Errorf("Persona = %q, want %q", inst.Persona, "bob")
	}
}

func TestFindInstallation_NotFound(t *testing.T) {
	log := &Log{}
	log.Record("alice", "cursor", "/proj1", nil)

	inst := log.FindInstallation("bob", "cursor", "/proj1")
	if inst != nil {
		t.Error("expected nil for non-matching persona")
	}
}

func TestRemoveInstallation(t *testing.T) {
	log := &Log{}
	log.Record("alice", "cursor", "/proj1", nil)
	log.Record("bob", "codex", "/proj2", nil)

	log.RemoveInstallation("alice", "cursor", "/proj1")
	if len(log.Installations) != 1 {
		t.Fatalf("expected 1 installation after removal, got %d", len(log.Installations))
	}
	if log.Installations[0].Persona != "bob" {
		t.Errorf("remaining installation should be bob, got %q", log.Installations[0].Persona)
	}
}

func TestRemoveInstallation_NotFound(t *testing.T) {
	log := &Log{}
	log.Record("alice", "cursor", "/proj1", nil)

	log.RemoveInstallation("nonexistent", "cursor", "/proj1")
	if len(log.Installations) != 1 {
		t.Error("removal of non-existent should not change log")
	}
}

func TestPrune_FindsOrphans(t *testing.T) {
	dir := t.TempDir()

	log := &Log{}
	// All symlinks broken (nonexistent paths).
	log.Record("alice", "cursor", "/gone", []vendor.SymlinkEntry{
		{Target: "/no/target", Link: "/no/link"},
	})

	orphans := log.Prune()
	if len(orphans) != 1 {
		t.Fatalf("expected 1 orphan, got %d", len(orphans))
	}

	// Now create a valid symlink for a different installation.
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	log.Record("bob", "cursor", dir, []vendor.SymlinkEntry{
		{Target: target, Link: link},
	})

	orphans = log.Prune()
	if len(orphans) != 1 {
		t.Fatalf("expected 1 orphan (alice), got %d", len(orphans))
	}
	if orphans[0].Persona != "alice" {
		t.Errorf("orphan should be alice, got %q", orphans[0].Persona)
	}
}

func TestRecord_Upsert(t *testing.T) {
	log := &Log{}
	log.Record("alice", "cursor", "/proj1", []vendor.SymlinkEntry{
		{Target: "a", Link: "b"},
	})
	log.Record("bob", "codex", "/proj2", nil)

	// Record same persona+vendor+project again with different symlinks
	log.Record("alice", "cursor", "/proj1", []vendor.SymlinkEntry{
		{Target: "c", Link: "d"},
		{Target: "e", Link: "f"},
	})

	// Should update in-place, not append
	if len(log.Installations) != 2 {
		t.Fatalf("expected 2 installations (upsert), got %d", len(log.Installations))
	}

	inst := log.FindInstallation("alice", "cursor", "/proj1")
	if inst == nil {
		t.Fatal("expected to find alice's installation")
	}
	if len(inst.Symlinks) != 2 {
		t.Errorf("expected 2 symlinks after upsert, got %d", len(inst.Symlinks))
	}
	if inst.Symlinks[0].Target != "c" {
		t.Errorf("expected updated symlink target 'c', got %q", inst.Symlinks[0].Target)
	}
}

func TestRecord_DifferentKeysAppend(t *testing.T) {
	log := &Log{}
	log.Record("alice", "cursor", "/proj1", nil)
	log.Record("alice", "codex", "/proj1", nil)  // different vendor
	log.Record("alice", "cursor", "/proj2", nil) // different project

	if len(log.Installations) != 3 {
		t.Fatalf("expected 3 installations (different keys), got %d", len(log.Installations))
	}
}

func TestPrune_EmptySymlinks(t *testing.T) {
	log := &Log{}
	log.Record("alice", "cursor", "/proj1", []vendor.SymlinkEntry{})

	orphans := log.Prune()
	if len(orphans) != 1 {
		t.Fatalf("expected 1 orphan for empty symlinks, got %d", len(orphans))
	}
	if orphans[0].Persona != "alice" {
		t.Errorf("orphan should be alice, got %q", orphans[0].Persona)
	}
}

func TestPrune_NilSymlinks(t *testing.T) {
	log := &Log{}
	log.Record("alice", "cursor", "/proj1", nil)

	orphans := log.Prune()
	if len(orphans) != 1 {
		t.Fatalf("expected 1 orphan for nil symlinks, got %d", len(orphans))
	}
}

func TestRemoveOrphans(t *testing.T) {
	log := &Log{}
	log.Record("alice", "cursor", "/proj1", nil)
	log.Record("bob", "codex", "/proj2", nil)

	orphans := []Installation{log.Installations[0]}
	log.RemoveOrphans(orphans)

	if len(log.Installations) != 1 {
		t.Fatalf("expected 1 after RemoveOrphans, got %d", len(log.Installations))
	}
	if log.Installations[0].Persona != "bob" {
		t.Errorf("remaining should be bob, got %q", log.Installations[0].Persona)
	}
}
