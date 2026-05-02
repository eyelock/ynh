//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type envelopeSourcesEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Description string `json:"description,omitempty"`
	Harnesses   int    `json:"harnesses"`
}

// TestSources_AddListRemove exercises the full sources lifecycle: add a
// directory containing a discoverable harness, list (JSON) to verify
// the entry appears with harness count, remove, list again to verify
// the entry is gone.
func TestSources_AddListRemove(t *testing.T) {
	s := newSandbox(t)

	// Build a directory with one discoverable harness in it.
	srcRoot := filepath.Join(t.TempDir(), "my-sources")
	harnessDir := filepath.Join(srcRoot, "demo")
	if err := os.MkdirAll(filepath.Join(harnessDir, ".ynh-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	plugin := `{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"demo","version":"0.1.0"}`
	if err := os.WriteFile(filepath.Join(harnessDir, ".ynh-plugin", "plugin.json"), []byte(plugin), 0o644); err != nil {
		t.Fatal(err)
	}

	// Add the source.
	out, _ := s.mustRunYnh(t, "sources", "add", srcRoot, "--name", "mine", "--description", "test source")
	if !strings.Contains(out, "Added source") {
		t.Errorf("expected 'Added source' in output, got: %s", out)
	}

	// List as JSON — must contain the entry with harness count = 1.
	out, _ = s.mustRunYnh(t, "sources", "list", "--format", "json")
	var entries []envelopeSourcesEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("parsing sources list JSON: %v\n%s", err, out)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 source entry, got %d: %+v", len(entries), entries)
	}
	e := entries[0]
	assertEqual(t, "name", e.Name, "mine")
	assertEqual(t, "description", e.Description, "test source")
	assertEqual(t, "harnesses", e.Harnesses, 1)
	if !strings.HasSuffix(e.Path, "my-sources") {
		t.Errorf("path = %q, expected to end with my-sources", e.Path)
	}

	// Remove the source.
	s.mustRunYnh(t, "sources", "remove", "mine")

	// Re-list — must be empty now.
	out, _ = s.mustRunYnh(t, "sources", "list", "--format", "json")
	var after []envelopeSourcesEntry
	if err := json.Unmarshal([]byte(out), &after); err != nil {
		t.Fatalf("parsing post-remove sources list JSON: %v\n%s", err, out)
	}
	if len(after) != 0 {
		t.Errorf("expected sources list to be empty after remove, got %d entries", len(after))
	}
}

// TestSources_AddRejectsDuplicate verifies that adding a source with a name
// that already exists fails with a clear error. The duplicate guard prevents
// silent overrides of registered sources.
func TestSources_AddRejectsDuplicate(t *testing.T) {
	s := newSandbox(t)

	srcA := filepath.Join(t.TempDir(), "a")
	srcB := filepath.Join(t.TempDir(), "b")
	for _, d := range []string{srcA, srcB} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	s.mustRunYnh(t, "sources", "add", srcA, "--name", "shared")

	_, errOut, err := s.runYnh(t, "sources", "add", srcB, "--name", "shared")
	if err == nil {
		t.Fatalf("expected duplicate-name add to fail, got success")
	}
	if !strings.Contains(errOut, "already exists") {
		t.Errorf("expected error to mention 'already exists', got: %s", errOut)
	}
}
