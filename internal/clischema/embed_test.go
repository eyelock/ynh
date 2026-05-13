package clischema

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
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

// TestSchemaParityWithDocs guarantees the embedded source-of-truth schemas
// and the publish-facing copy under docs/schema/ stay byte-identical. The
// repo holds two copies because go:embed cannot traverse "..", so docs/
// hosts the discoverable copy and internal/clischema/schema/ is the embed
// authoritative. Any drift is a CI failure.
func TestSchemaParityWithDocs(t *testing.T) {
	docsRoot := docsSchemaRoot(t)
	if docsRoot == "" {
		t.Skip("docs/schema not reachable from package CWD")
	}

	embedPaths := map[string][]byte{}
	allRaw, err := AllRaw()
	if err != nil {
		t.Fatalf("AllRaw: %v", err)
	}
	for name, data := range allRaw {
		embedPaths[name+".schema.json"] = data
	}

	// Parity scope: only docs/schema/cli/ and docs/schema/shared/. The
	// existing author-controlled schemas at docs/schema/{plugin,marketplace,harness}.schema.json
	// describe author-authored files (plugin.json, marketplace.json) and are
	// not embedded by this package.
	var docsList []string
	for _, sub := range []string{"cli", "shared"} {
		root := filepath.Join(docsRoot, sub)
		if _, err := os.Stat(root); err != nil {
			continue
		}
		walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(docsRoot, path)
			docsList = append(docsList, rel)
			return nil
		})
		if walkErr != nil {
			t.Fatalf("walk %s: %v", root, walkErr)
		}
	}
	sort.Strings(docsList)

	for _, rel := range docsList {
		data, err := os.ReadFile(filepath.Join(docsRoot, rel))
		if err != nil {
			t.Errorf("read %s: %v", rel, err)
			continue
		}
		emb, ok := embedPaths[rel]
		if !ok {
			t.Errorf("docs has %s but embed does not", rel)
			continue
		}
		if string(emb) != string(data) {
			t.Errorf("drift: %s differs between embed and docs/schema/", rel)
		}
		delete(embedPaths, rel)
	}
	for rel := range embedPaths {
		t.Errorf("embed has %s but docs/schema/ does not", rel)
	}
}

// docsSchemaRoot resolves the repo's docs/schema directory by walking up
// from the package CWD. Returns "" if not found (test will skip).
func docsSchemaRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	for dir := wd; dir != "/"; dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, "docs", "schema")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
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
