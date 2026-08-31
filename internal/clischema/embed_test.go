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

// Phase 3 bare-array commands that previously emitted structured output with
// no schema to check it against — `docs/cli-structured.md` claimed universal
// coverage while these two had none.
func TestBackendGolden(t *testing.T) { validateGolden(t, "backend", "backend.json") }
func TestSensorsGolden(t *testing.T) { validateGolden(t, "sensors", "sensors.json") }

// `ynh sensors show` resolves one sensor and is a different shape from the
// list. It advertises --format json in its own help, so it needs its own
// schema — without it the universal-coverage claim in docs/cli-structured.md
// is false, which an eval run caught after the first two schemas landed.
func TestSensorsShowGolden(t *testing.T) {
	validateGolden(t, "sensors-show", "sensors-show.json")
}

// migrate and quarantine also emit --format json. They were missed by the
// first sweep because neither appeared in `ynh help` until recently, which is
// the same drift TestEveryDispatchedCommandAppearsInUsage now guards.
func TestMigrateGolden(t *testing.T)    { validateGolden(t, "migrate", "migrate.json") }
func TestQuarantineGolden(t *testing.T) { validateGolden(t, "quarantine", "quarantine.json") }

// The list verbs added for #283. `ynh focus` and `ynh profile` were editor-only
// while `ynh sensors ls` had a list verb, so these were the odd ones out in
// their own command family.
func TestFocusGolden(t *testing.T)   { validateGolden(t, "focus", "focus.json") }
func TestProfileGolden(t *testing.T) { validateGolden(t, "profile", "profile.json") }

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

// TestAgentRunGolden validates the agent-run result envelope. The golden is a
// non-converged run on purpose: that is the case a pipeline actually has to
// read, and the one where under-reporting would go unnoticed.
func TestAgentRunGolden(t *testing.T) { validateGolden(t, "agent-run", "agent-run.json") }

// TestCheckCalibrateGolden validates the calibration envelope. The golden is a
// broken run on purpose: it carries one of each outcome, including the one
// that matters — a sensor that passed a fixture built to trip it.
func TestCheckCalibrateGolden(t *testing.T) {
	validateGolden(t, "check-calibrate", "check-calibrate.json")
}

// TestBaselineGolden validates the baseline read surface. The golden carries a
// sensor forgiving nothing, one with resolved findings, and a count-ratchet
// one — the three shapes a reader has to tell apart.
func TestBaselineGolden(t *testing.T) { validateGolden(t, "baseline", "baseline.json") }

// Every name `ynh schema --all` advertises must be fetchable by
// `ynh schema <name>`.
//
// The two disagreed. AllRaw walks the whole embedded tree while Raw hardcoded a
// "cli/" prefix, so the binary listed `plugin`, `marketplace` and `harness` —
// the author-facing schemas at the tree root — and then answered "unknown
// schema" for each. Adding agent/trajectory made it worse by one. A listing
// that names something unfetchable is worse than not listing it.
func TestEveryAdvertisedSchemaIsFetchable(t *testing.T) {
	all, err := AllRaw()
	if err != nil {
		t.Fatalf("AllRaw: %v", err)
	}
	if len(all) == 0 {
		t.Fatal("AllRaw returned nothing")
	}
	for name := range all {
		if _, err := Raw(name); err != nil {
			t.Errorf("`ynh schema --all` lists %q but `ynh schema %s` cannot fetch it: %v",
				name, name, err)
		}
	}
}

// Raw resolves a name containing "/" as a path into the embedded FS, so it is
// worth pinning that a traversal-shaped name cannot reach outside it.
//
// embed.FS already refuses unclean and rooted paths, and the blast radius is
// bounded anyway — the FS holds only compiled-in schema bytes, not the real
// filesystem. This exists so that neither property is quietly given up by a
// later change to Raw.
func TestRawRejectsTraversal(t *testing.T) {
	for _, name := range []string{
		"../../etc/passwd",
		"cli/../../go.mod",
		"../embed",
		"/etc/passwd",
		"cli/../cli/version",
		"./cli/version",
	} {
		if _, err := Raw(name); err == nil {
			t.Errorf("Raw(%q) should not resolve", name)
		}
	}

	// The legitimate forms still work, so the guard is not simply refusing
	// everything with a slash in it.
	for _, name := range []string{"version", "cli/version", "agent/trajectory", "plugin"} {
		if _, err := Raw(name); err != nil {
			t.Errorf("Raw(%q) should resolve: %v", name, err)
		}
	}
}

// Schema validation cannot catch a semantically inverted fixture: `repo` and
// `path` are both optional strings, so swapping their values validates
// cleanly. The search golden shipped for some time with the filesystem path in
// `path` and `repo` empty, the exact inverse of what the command emits and of
// the field descriptions in the same repository (#342).
//
// A golden is the most copyable artifact in the schema directory. Correct
// prose beside a wrong worked example loses to the example, which is how a
// consumer came to join onto the wrong field.
func TestSearchGoldenMatchesEmittedShape(t *testing.T) {
	path := findGolden(t, "search.json")
	if path == "" {
		t.Fatal("test/golden/search.json is missing; this test cannot pass by skipping")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var results []map[string]any
	if err := json.Unmarshal(data, &results); err != nil {
		t.Fatalf("parse golden: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("golden is empty; it asserts nothing")
	}

	var sawSource, sawRegistry bool
	for i, r := range results {
		from, _ := r["from"].(map[string]any)
		origin, _ := from["type"].(string)
		repo, _ := r["repo"].(string)
		_, hasPath := r["path"]

		switch origin {
		case "source":
			sawSource = true
			// cmd/ynh/search.go sets Repo to the harness directory and never
			// sets Path. A local result's repo is the whole answer.
			if repo == "" {
				t.Errorf("entry %d (source): repo is empty, but a local result carries "+
					"its harness directory there and has nothing else to install with", i)
			}
			if hasPath {
				t.Errorf("entry %d (source): path is present, but the command never sets it "+
					"for a local result; a consumer joining repo and path builds a "+
					"directory that has never existed", i)
			}
		case "registry":
			sawRegistry = true
			if repo == "" {
				t.Errorf("entry %d (registry): repo is empty", i)
			}
		default:
			t.Errorf("entry %d: unknown from.type %q", i, origin)
		}
	}

	// Both origins must appear, or the fixture stops covering the case where
	// they differ, which is the only case that matters here.
	if !sawSource {
		t.Error("golden has no local-source entry; the inversion it guards against is unrepresented")
	}
	if !sawRegistry {
		t.Error("golden has no registry entry; the two origins cannot be compared")
	}
}
