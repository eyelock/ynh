package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

// setupInstalledHarness writes a harness into the ynh harnesses directory and
// returns the install dir. Requires YNH_HOME to be set via t.Setenv.
func setupInstalledHarness(t *testing.T, name string) string {
	t.Helper()
	dir := InstalledDir(name)
	writeTestHarness(t, dir, name)
	return dir
}

// overrideHarnessesDir points YNH_HOME at a temp dir so installed-harness
// lookups are isolated per test.
func overrideHarnessesDir(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
}

// loadIncludes is a test helper that reads the includes from a harness dir.
func loadIncludes(t *testing.T, dir string) []plugin.IncludeMeta {
	t.Helper()
	hj, err := plugin.LoadPluginJSON(dir)
	if err != nil {
		t.Fatalf("LoadPluginJSON: %v", err)
	}
	return hj.Includes
}

// ---- ResolveEditTarget -------------------------------------------------------

func TestResolveEditTarget_InstalledName(t *testing.T) {
	overrideHarnessesDir(t)
	dir := setupInstalledHarness(t, "my-harness")

	got, installed, err := ResolveEditTarget("my-harness")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != dir {
		t.Errorf("dir = %q, want %q", got, dir)
	}
	if !installed {
		t.Error("expected installed=true for name-based ref")
	}
}

func TestResolveEditTarget_Path(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "local")

	got, installed, err := ResolveEditTarget(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != dir {
		t.Errorf("dir = %q, want %q", got, dir)
	}
	if installed {
		t.Error("expected installed=false for path-based ref")
	}
}

func TestResolveEditTarget_NotFound(t *testing.T) {
	overrideHarnessesDir(t)

	_, _, err := ResolveEditTarget("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown harness name")
	}
}

func TestResolveEditTarget_PathNoHarness(t *testing.T) {
	dir := t.TempDir()

	_, _, err := ResolveEditTarget(dir)
	if err == nil {
		t.Fatal("expected error for path without .harness.json")
	}
	if !strings.Contains(err.Error(), "no harness manifest") {
		t.Errorf("error should mention missing manifest, got: %v", err)
	}
}

// ---- AddInclude -------------------------------------------------------

func TestAddInclude_New(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")

	err := AddInclude(dir, "github.com/acme/tools", AddOptions{Path: "plugins/search", Pick: []string{"web"}})
	if err != nil {
		t.Fatalf("AddInclude: %v", err)
	}

	incs := loadIncludes(t, dir)
	if len(incs) != 1 {
		t.Fatalf("expected 1 include, got %d", len(incs))
	}
	if incs[0].Git != "github.com/acme/tools" || incs[0].Path != "plugins/search" {
		t.Errorf("unexpected include: %+v", incs[0])
	}
}

func TestAddInclude_DuplicateErrors(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")

	opts := AddOptions{Path: "plugins/search"}
	if err := AddInclude(dir, "github.com/acme/tools", opts); err != nil {
		t.Fatalf("first add: %v", err)
	}
	err := AddInclude(dir, "github.com/acme/tools", opts)
	if err == nil {
		t.Fatal("expected error on duplicate add")
	}
	if !strings.Contains(err.Error(), "already present") {
		t.Errorf("error should mention already present, got: %v", err)
	}
}

func TestAddInclude_Replace(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")

	if err := AddInclude(dir, "github.com/acme/tools", AddOptions{Ref: "v1"}); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if err := AddInclude(dir, "github.com/acme/tools", AddOptions{Ref: "v2", Replace: true}); err != nil {
		t.Fatalf("replace: %v", err)
	}

	incs := loadIncludes(t, dir)
	if len(incs) != 1 {
		t.Fatalf("expected 1 include after replace, got %d", len(incs))
	}
	if incs[0].Ref != "v2" {
		t.Errorf("expected ref v2 after replace, got %q", incs[0].Ref)
	}
}

func TestAddInclude_SameURLDifferentPath(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")

	if err := AddInclude(dir, "github.com/acme/tools", AddOptions{Path: "a"}); err != nil {
		t.Fatalf("add a: %v", err)
	}
	if err := AddInclude(dir, "github.com/acme/tools", AddOptions{Path: "b"}); err != nil {
		t.Fatalf("add b: %v", err)
	}

	incs := loadIncludes(t, dir)
	if len(incs) != 2 {
		t.Fatalf("expected 2 includes, got %d", len(incs))
	}
}

// ---- RemoveInclude -------------------------------------------------------

func TestRemoveInclude_Removes(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddInclude(dir, "github.com/acme/tools", AddOptions{}); err != nil {
		t.Fatal(err)
	}

	if err := RemoveInclude(dir, "github.com/acme/tools", RemoveOptions{}); err != nil {
		t.Fatalf("RemoveInclude: %v", err)
	}

	incs := loadIncludes(t, dir)
	if len(incs) != 0 {
		t.Fatalf("expected 0 includes, got %d", len(incs))
	}
}

func TestRemoveInclude_NotFound(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")

	err := RemoveInclude(dir, "github.com/acme/tools", RemoveOptions{})
	if err == nil {
		t.Fatal("expected error for missing include")
	}
}

func TestRemoveInclude_AmbiguousRequiresPath(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddInclude(dir, "github.com/acme/tools", AddOptions{Path: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := AddInclude(dir, "github.com/acme/tools", AddOptions{Path: "b"}); err != nil {
		t.Fatal(err)
	}

	err := RemoveInclude(dir, "github.com/acme/tools", RemoveOptions{})
	if err == nil {
		t.Fatal("expected error for ambiguous URL without path")
	}
	if !strings.Contains(err.Error(), "disambiguate") {
		t.Errorf("error should mention disambiguate, got: %v", err)
	}
}

func TestRemoveInclude_WithPath(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddInclude(dir, "github.com/acme/tools", AddOptions{Path: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := AddInclude(dir, "github.com/acme/tools", AddOptions{Path: "b"}); err != nil {
		t.Fatal(err)
	}

	if err := RemoveInclude(dir, "github.com/acme/tools", RemoveOptions{Path: "a"}); err != nil {
		t.Fatalf("RemoveInclude with path: %v", err)
	}

	incs := loadIncludes(t, dir)
	if len(incs) != 1 || incs[0].Path != "b" {
		t.Errorf("expected 1 include with path=b, got %+v", incs)
	}
}

// ---- UpdateInclude -------------------------------------------------------

func TestUpdateInclude_Ref(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddInclude(dir, "github.com/acme/tools", AddOptions{Ref: "v1"}); err != nil {
		t.Fatal(err)
	}

	newRef := "v2"
	if err := UpdateInclude(dir, "github.com/acme/tools", UpdateOptions{Ref: &newRef}); err != nil {
		t.Fatalf("UpdateInclude: %v", err)
	}

	incs := loadIncludes(t, dir)
	if incs[0].Ref != "v2" {
		t.Errorf("expected ref v2, got %q", incs[0].Ref)
	}
}

func TestUpdateInclude_Path(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddInclude(dir, "github.com/acme/tools", AddOptions{Path: "old"}); err != nil {
		t.Fatal(err)
	}

	newPath := "new"
	if err := UpdateInclude(dir, "github.com/acme/tools", UpdateOptions{NewPath: &newPath}); err != nil {
		t.Fatalf("UpdateInclude: %v", err)
	}

	incs := loadIncludes(t, dir)
	if incs[0].Path != "new" {
		t.Errorf("expected path new, got %q", incs[0].Path)
	}
}

func TestUpdateInclude_Pick(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddInclude(dir, "github.com/acme/tools", AddOptions{Pick: []string{"a"}}); err != nil {
		t.Fatal(err)
	}

	if err := UpdateInclude(dir, "github.com/acme/tools", UpdateOptions{Pick: []string{"b", "c"}, SetPick: true}); err != nil {
		t.Fatalf("UpdateInclude: %v", err)
	}

	incs := loadIncludes(t, dir)
	if len(incs[0].Pick) != 2 || incs[0].Pick[0] != "b" {
		t.Errorf("expected pick=[b,c], got %v", incs[0].Pick)
	}
}

func TestUpdateInclude_OmittedFieldsUnchanged(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddInclude(dir, "github.com/acme/tools", AddOptions{Ref: "v1", Path: "p", Pick: []string{"x"}}); err != nil {
		t.Fatal(err)
	}

	newRef := "v2"
	if err := UpdateInclude(dir, "github.com/acme/tools", UpdateOptions{Ref: &newRef}); err != nil {
		t.Fatalf("UpdateInclude: %v", err)
	}

	incs := loadIncludes(t, dir)
	if incs[0].Ref != "v2" {
		t.Errorf("ref should be v2, got %q", incs[0].Ref)
	}
	if incs[0].Path != "p" {
		t.Errorf("path should be unchanged (p), got %q", incs[0].Path)
	}
	if len(incs[0].Pick) != 1 || incs[0].Pick[0] != "x" {
		t.Errorf("pick should be unchanged ([x]), got %v", incs[0].Pick)
	}
}

func TestUpdateInclude_AmbiguousRequiresFromPath(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddInclude(dir, "github.com/acme/tools", AddOptions{Path: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := AddInclude(dir, "github.com/acme/tools", AddOptions{Path: "b"}); err != nil {
		t.Fatal(err)
	}

	newRef := "v2"
	err := UpdateInclude(dir, "github.com/acme/tools", UpdateOptions{Ref: &newRef})
	if err == nil {
		t.Fatal("expected error for ambiguous URL without --from-path")
	}
	if !strings.Contains(err.Error(), "disambiguate") {
		t.Errorf("error should mention disambiguate, got: %v", err)
	}
}

func TestUpdateInclude_FromPath(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddInclude(dir, "github.com/acme/tools", AddOptions{Path: "a", Ref: "v1"}); err != nil {
		t.Fatal(err)
	}
	if err := AddInclude(dir, "github.com/acme/tools", AddOptions{Path: "b", Ref: "v1"}); err != nil {
		t.Fatal(err)
	}

	newRef := "v2"
	if err := UpdateInclude(dir, "github.com/acme/tools", UpdateOptions{FromPath: "a", Ref: &newRef}); err != nil {
		t.Fatalf("UpdateInclude with from-path: %v", err)
	}

	incs := loadIncludes(t, dir)
	refByPath := map[string]string{}
	for _, inc := range incs {
		refByPath[inc.Path] = inc.Ref
	}
	if refByPath["a"] != "v2" {
		t.Errorf("expected a→v2, got %q", refByPath["a"])
	}
	if refByPath["b"] != "v1" {
		t.Errorf("expected b→v1 (unchanged), got %q", refByPath["b"])
	}
}

// ---- ValidatePicks -------------------------------------------------------

func TestValidatePicks_Valid(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "web-search")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# web-search"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ValidatePicks(dir, []string{"skills/web-search"}); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidatePicks_RejectsBareName(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "web-search")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# web-search"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Bare basename is no longer valid — canonical form is type/name.
	err := ValidatePicks(dir, []string{"web-search"})
	if err == nil {
		t.Fatal("expected error when pick is a bare name without type/ prefix")
	}
	if !strings.Contains(err.Error(), "skills/web-search") {
		t.Errorf("error should suggest the canonical form, got: %v", err)
	}
}

func TestValidatePicks_Unknown(t *testing.T) {
	dir := t.TempDir()

	err := ValidatePicks(dir, []string{"skills/nonexistent"})
	if err == nil {
		t.Fatal("expected error for unknown pick")
	}
	if !strings.Contains(err.Error(), "skills/nonexistent") {
		t.Errorf("error should name the unknown pick, got: %v", err)
	}
}

func TestValidatePicks_Empty(t *testing.T) {
	dir := t.TempDir()
	if err := ValidatePicks(dir, nil); err != nil {
		t.Fatalf("nil picks should always pass: %v", err)
	}
}

// TestValidatePicks_BasenameClash asserts the canonical type/name form
// disambiguates a skill and an agent that share a basename.
//
// With the old bare-name validation they collided on a single key ("foo")
// and the user could not pick one without also picking the other.
func TestValidatePicks_BasenameClash(t *testing.T) {
	dir := t.TempDir()

	// skill "foo"
	skillDir := filepath.Join(dir, "skills", "foo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# foo skill"), 0o644); err != nil {
		t.Fatal(err)
	}

	// agent "foo.md"
	agentsDir := filepath.Join(dir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsDir, "foo.md"), []byte("agent foo"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Either alone is valid.
	if err := ValidatePicks(dir, []string{"skills/foo"}); err != nil {
		t.Errorf("pick skills/foo alone should validate: %v", err)
	}
	if err := ValidatePicks(dir, []string{"agents/foo.md"}); err != nil {
		t.Errorf("pick agents/foo.md alone should validate: %v", err)
	}
	// Both together.
	if err := ValidatePicks(dir, []string{"skills/foo", "agents/foo.md"}); err != nil {
		t.Errorf("picking both skills/foo and agents/foo.md should validate: %v", err)
	}
	// Bare "foo" is ambiguous and no longer accepted.
	err := ValidatePicks(dir, []string{"foo"})
	if err == nil {
		t.Error("bare basename should not validate (canonical type/name required)")
	}
}

// ---- FindUpdateTarget -------------------------------------------------------

func TestFindUpdateTarget_ComputesFinalState(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddInclude(dir, "github.com/acme/tools", AddOptions{Ref: "v1", Path: "old"}); err != nil {
		t.Fatal(err)
	}

	newPath := "new"
	newRef := "v2"
	inc, err := FindUpdateTarget(dir, "github.com/acme/tools", UpdateOptions{NewPath: &newPath, Ref: &newRef})
	if err != nil {
		t.Fatalf("FindUpdateTarget: %v", err)
	}
	if inc.Path != "new" || inc.Ref != "v2" {
		t.Errorf("expected path=new ref=v2, got path=%q ref=%q", inc.Path, inc.Ref)
	}
}
