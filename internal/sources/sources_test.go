package sources

import (
	"os"
	"path/filepath"
	"testing"
)

func writeHarness(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"name":"` + name + `","version":"0.1.0","description":"` + name + ` harness","default_vendor":"claude"}`
	if err := os.WriteFile(filepath.Join(dir, ".harness.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscover_RootHarness(t *testing.T) {
	root := t.TempDir()
	writeHarness(t, root, "root-harness")

	results, err := Discover(root, 2)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Name != "root-harness" {
		t.Errorf("Name = %q, want %q", results[0].Name, "root-harness")
	}
	if results[0].Path != root {
		t.Errorf("Path = %q, want %q", results[0].Path, root)
	}
}

func TestDiscover_ChildHarnesses(t *testing.T) {
	root := t.TempDir()
	writeHarness(t, filepath.Join(root, "alice"), "alice")
	writeHarness(t, filepath.Join(root, "bob"), "bob")

	results, err := Discover(root, 2)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	names := map[string]bool{}
	for _, r := range results {
		names[r.Name] = true
	}
	if !names["alice"] || !names["bob"] {
		t.Errorf("missing expected harnesses: %v", names)
	}
}

func TestDiscover_NestedDepth(t *testing.T) {
	root := t.TempDir()
	// Harness at depth 2: root/org/harness/.harness.json
	writeHarness(t, filepath.Join(root, "org", "deep"), "deep")

	results, err := Discover(root, 2)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Name != "deep" {
		t.Errorf("Name = %q, want %q", results[0].Name, "deep")
	}
}

func TestDiscover_MaxDepthRespected(t *testing.T) {
	root := t.TempDir()
	// Harness at depth 3 — beyond maxDepth=2
	writeHarness(t, filepath.Join(root, "a", "b", "too-deep"), "too-deep")

	results, err := Discover(root, 2)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("got %d results, want 0 (beyond max depth)", len(results))
	}
}

func TestDiscover_SkipsHiddenDirs(t *testing.T) {
	root := t.TempDir()
	writeHarness(t, filepath.Join(root, ".hidden"), "hidden")
	writeHarness(t, filepath.Join(root, "visible"), "visible")

	results, err := Discover(root, 2)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Name != "visible" {
		t.Errorf("Name = %q, want %q", results[0].Name, "visible")
	}
}

func TestDiscover_EmptyDir(t *testing.T) {
	root := t.TempDir()

	results, err := Discover(root, 2)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("got %d results, want 0", len(results))
	}
}

func TestDiscover_NonexistentDir(t *testing.T) {
	_, err := Discover("/nonexistent/path/12345", 2)
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
}

func TestDiscover_InvalidJSON(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "broken")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".harness.json"), []byte("{invalid"), 0o644); err != nil {
		t.Fatal(err)
	}

	results, err := Discover(root, 2)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("got %d results, want 0 (invalid JSON skipped)", len(results))
	}
}

func TestDiscover_MissingName(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "noname")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".harness.json"), []byte(`{"version":"0.1.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	results, err := Discover(root, 2)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("got %d results, want 0 (nameless harness skipped)", len(results))
	}
}

func TestDiscover_StopsAtHarnessDir(t *testing.T) {
	root := t.TempDir()
	// Parent is a harness, child is also a harness — only parent should be found
	writeHarness(t, filepath.Join(root, "parent"), "parent")
	writeHarness(t, filepath.Join(root, "parent", "child"), "child")

	results, err := Discover(root, 2)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1 (should stop at harness dir)", len(results))
	}
	if results[0].Name != "parent" {
		t.Errorf("Name = %q, want %q", results[0].Name, "parent")
	}
}

func TestDiscover_PopulatesAllFields(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "full")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"name":"full","version":"1.2.3","description":"A full harness","default_vendor":"codex","keywords":["go","dev"]}`
	if err := os.WriteFile(filepath.Join(dir, ".harness.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	results, err := Discover(root, 2)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}

	h := results[0]
	if h.Name != "full" {
		t.Errorf("Name = %q", h.Name)
	}
	if h.Version != "1.2.3" {
		t.Errorf("Version = %q", h.Version)
	}
	if h.Description != "A full harness" {
		t.Errorf("Description = %q", h.Description)
	}
	if h.DefaultVendor != "codex" {
		t.Errorf("DefaultVendor = %q", h.DefaultVendor)
	}
	if len(h.Keywords) != 2 || h.Keywords[0] != "go" || h.Keywords[1] != "dev" {
		t.Errorf("Keywords = %v", h.Keywords)
	}
}
