package clischema

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestSchemasCompile verifies every embedded schema parses, registers, and
// (for CLI schemas) compiles. Catches: malformed JSON, missing $id,
// unsupported keywords, broken $refs.
func TestSchemasCompile(t *testing.T) {
	names := Names()
	if len(names) == 0 {
		t.Fatal("no CLI schemas loaded")
	}
	for _, n := range names {
		if _, err := Get(n); err != nil {
			t.Errorf("Get(%q): %v", n, err)
		}
	}
}

// TestSingleSchemaTree guards the invariant that replaced the old
// embed-vs-docs parity test: there is exactly one copy of every schema, at
// docs/schema, embedded from there by docs/schema/embed.go.
//
// The repo previously held three trees — docs/schema, cmd/ynd/schema, and
// internal/clischema/schema — of which only two were pinned to each other.
// The unpinned pair drifted: five path-traversal guards lived in the
// published plugin and marketplace schemas and in neither copy any validator
// loaded, so a documented guarantee went unenforced. A parity test can only
// detect that after it happens; a single directory makes it unrepresentable.
// This test is what keeps a second directory from quietly reappearing.
func TestSingleSchemaTree(t *testing.T) {
	root := repoRoot(t)
	if root == "" {
		t.Skip("repo root not reachable from package CWD")
	}
	canonical := filepath.Join(root, "docs", "schema")

	var strays []string
	walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // unreadable trees are not this test's business
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "bin", "dist", "node_modules", ".worktrees", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".schema.json") {
			return nil
		}
		if strings.HasPrefix(path, canonical+string(filepath.Separator)) {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		strays = append(strays, rel)
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk: %v", walkErr)
	}
	sort.Strings(strays)
	for _, rel := range strays {
		t.Errorf("schema outside docs/schema: %s\n"+
			"Every schema lives once, at docs/schema, embedded by docs/schema/embed.go. "+
			"A second copy is how the published and enforced schemas silently diverged.", rel)
	}
}

// repoRoot walks up from the package CWD to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	for dir := wd; dir != "/"; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
	}
	return ""
}

// TestVersionGolden round-trips the captured version envelope golden through
// the version schema. This is the load-bearing piece: any drift between Go
// emission and the schema fails this test.
func TestVersionGolden(t *testing.T) {
	goldenPath := findGolden(t, "version.json")
	if goldenPath == "" {
		t.Skip("test/golden/version.json not found")
	}
	data, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("parse golden: %v", err)
	}
	s, err := Get("version")
	if err != nil {
		t.Fatalf("Get version: %v", err)
	}
	if err := s.Validate(v); err != nil {
		t.Errorf("version golden does not validate against schema: %v", err)
	}
}

// TestListGolden validates the representative list envelope.
func TestListGolden(t *testing.T) { validateGolden(t, "list", "list.json") }

// TestInfoGolden validates the representative info envelope.
func TestInfoGolden(t *testing.T) { validateGolden(t, "info", "info.json") }

// TestForkGolden validates the representative fork envelope.
func TestForkGolden(t *testing.T) { validateGolden(t, "fork", "fork.json") }

// TestSearchGolden / TestSourcesGolden / TestRegistryGolden / TestPathsGolden /
// TestVendorsGolden / TestStatusGolden — Phase 3 commands. Schemas describe
// today's bare-array/bare-object shapes; envelope retrofit is a future
// capabilities-bumping change tracked outside this plan.
func TestSearchGolden(t *testing.T)   { validateGolden(t, "search", "search.json") }
func TestSourcesGolden(t *testing.T)  { validateGolden(t, "sources", "sources.json") }
func TestRegistryGolden(t *testing.T) { validateGolden(t, "registry", "registry.json") }
func TestPathsGolden(t *testing.T)    { validateGolden(t, "paths", "paths.json") }
func TestVendorsGolden(t *testing.T)  { validateGolden(t, "vendors", "vendors.json") }
func TestStatusGolden(t *testing.T)   { validateGolden(t, "status", "status.json") }

// TestInstalledGolden validates the install-provenance envelope.
func TestInstalledGolden(t *testing.T) { validateGolden(t, "installed", "installed.json") }

func TestCheckGolden(t *testing.T) { validateGolden(t, "check", "check.json") }

// TestRaw verifies the byte-getter used by `ynh schema <name>`.
func TestRaw(t *testing.T) {
	data, err := Raw("version")
	if err != nil {
		t.Fatalf("Raw(version): %v", err)
	}
	if len(data) == 0 {
		t.Fatal("Raw returned empty bytes")
	}
	// Quick sanity check that the bytes parse and carry our $id.
	if !bytesContains(data, []byte("eyelock.github.io/ynh/schema/cli/version")) {
		t.Errorf("Raw output missing expected $id; got: %s", string(data))
	}
}

func TestRaw_Unknown(t *testing.T) {
	if _, err := Raw("does-not-exist"); err == nil {
		t.Error("expected error for unknown schema")
	}
}

func bytesContains(haystack, needle []byte) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func validateGolden(t *testing.T, schemaName, goldenName string) {
	t.Helper()
	path := findGolden(t, goldenName)
	if path == "" {
		t.Skipf("test/golden/%s not found", goldenName)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("parse golden: %v", err)
	}
	s, err := Get(schemaName)
	if err != nil {
		t.Fatalf("Get %s: %v", schemaName, err)
	}
	if err := s.Validate(v); err != nil {
		t.Errorf("%s golden does not validate: %v", goldenName, err)
	}
}

// TestErrorEnvelopeGolden validates a representative error envelope against
// the error schema, exercising the cross-cutting failure shape every command
// can return.
func TestErrorEnvelopeGolden(t *testing.T) {
	goldenPath := findGolden(t, "error.json")
	if goldenPath == "" {
		t.Skip("test/golden/error.json not found")
	}
	data, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("parse golden: %v", err)
	}
	s, err := Get("error")
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if err := s.Validate(v); err != nil {
		t.Errorf("error golden does not validate: %v", err)
	}
}

func findGolden(t *testing.T, name string) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	for dir := wd; dir != "/"; dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, "test", "golden", name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}
