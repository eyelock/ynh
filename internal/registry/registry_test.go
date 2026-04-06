package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func createTestRegistry(t *testing.T, name string, entries []Entry) string {
	t.Helper()
	dir := t.TempDir()

	reg := Registry{
		Name:        name,
		Description: "Test registry: " + name,
		Entries:     entries,
	}

	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "registry.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestLoadFromDir(t *testing.T) {
	dir := createTestRegistry(t, "test-reg", []Entry{
		{
			Name:        "my-harness",
			Description: "A test harness",
			Keywords:    []string{"go", "testing"},
			Repo:        "github.com/test/repo",
			Path:        "personas/mine",
			Vendors:     []string{"claude", "cursor"},
			Version:     "1.0.0",
		},
	})

	reg, err := LoadFromDir(dir)
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}

	if reg.Name != "test-reg" {
		t.Errorf("name = %q, want test-reg", reg.Name)
	}
	if len(reg.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(reg.Entries))
	}

	e := reg.Entries[0]
	if e.Name != "my-harness" {
		t.Errorf("entry name = %q", e.Name)
	}
	if e.Repo != "github.com/test/repo" {
		t.Errorf("entry repo = %q", e.Repo)
	}
	if e.Path != "personas/mine" {
		t.Errorf("entry path = %q", e.Path)
	}
}

func TestLoadFromDirMissing(t *testing.T) {
	_, err := LoadFromDir(t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing registry.json")
	}
}

func TestSearchByName(t *testing.T) {
	regs := []Registry{
		{
			Name: "reg1",
			Entries: []Entry{
				{Name: "david", Description: "Go developer harness"},
				{Name: "alice", Description: "Python data science"},
			},
		},
	}

	results := Search(regs, "david")
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if results[0].Entry.Name != "david" {
		t.Errorf("name = %q", results[0].Entry.Name)
	}
}

func TestSearchByDescription(t *testing.T) {
	regs := []Registry{
		{
			Name: "reg1",
			Entries: []Entry{
				{Name: "david", Description: "Go developer harness"},
				{Name: "alice", Description: "Python data science"},
			},
		},
	}

	results := Search(regs, "python")
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if results[0].Entry.Name != "alice" {
		t.Errorf("name = %q", results[0].Entry.Name)
	}
}

func TestSearchByKeyword(t *testing.T) {
	regs := []Registry{
		{
			Name: "reg1",
			Entries: []Entry{
				{Name: "david", Keywords: []string{"go", "backend"}},
				{Name: "alice", Keywords: []string{"python", "ml"}},
			},
		},
	}

	results := Search(regs, "backend")
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if results[0].Entry.Name != "david" {
		t.Errorf("name = %q", results[0].Entry.Name)
	}
}

func TestSearchCaseInsensitive(t *testing.T) {
	regs := []Registry{
		{
			Name: "reg1",
			Entries: []Entry{
				{Name: "David", Description: "GO Developer"},
			},
		},
	}

	results := Search(regs, "david")
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}

	results = Search(regs, "go developer")
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
}

func TestSearchNoMatch(t *testing.T) {
	regs := []Registry{
		{
			Name: "reg1",
			Entries: []Entry{
				{Name: "david", Description: "Go developer"},
			},
		},
	}

	results := Search(regs, "nonexistent")
	if len(results) != 0 {
		t.Errorf("results = %d, want 0", len(results))
	}
}

func TestSearchMultipleRegistries(t *testing.T) {
	regs := []Registry{
		{
			Name: "reg1",
			Entries: []Entry{
				{Name: "david", Description: "Go developer"},
			},
		},
		{
			Name: "reg2",
			Entries: []Entry{
				{Name: "alice", Description: "Go specialist"},
			},
		},
	}

	results := Search(regs, "go")
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if results[0].RegistryName != "reg1" || results[1].RegistryName != "reg2" {
		t.Error("registry names not preserved")
	}
}

func TestLookupExact(t *testing.T) {
	regs := []Registry{
		{
			Name: "reg1",
			Entries: []Entry{
				{Name: "david"},
				{Name: "alice"},
			},
		},
		{
			Name: "reg2",
			Entries: []Entry{
				{Name: "david"},
			},
		},
	}

	// All registries
	results := LookupExact(regs, "david", "")
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}

	// Scoped to reg2
	results = LookupExact(regs, "david", "reg2")
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if results[0].RegistryName != "reg2" {
		t.Errorf("registry = %q, want reg2", results[0].RegistryName)
	}

	// No match
	results = LookupExact(regs, "bob", "")
	if len(results) != 0 {
		t.Errorf("results = %d, want 0", len(results))
	}
}

func TestLookupExactCaseInsensitive(t *testing.T) {
	regs := []Registry{
		{
			Name:    "reg1",
			Entries: []Entry{{Name: "David"}},
		},
	}

	results := LookupExact(regs, "david", "")
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
}
