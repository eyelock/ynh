package harness

import (
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

func loadProfileIncludes(t *testing.T, dir, profile string) []plugin.IncludeMeta {
	t.Helper()
	hj, err := plugin.LoadPluginJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	return hj.Profiles[profile].Includes
}

// ---- AddProfileInclude ----------------------------------------------

func TestAddProfileInclude_New(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddProfile(dir, "ci"); err != nil {
		t.Fatal(err)
	}

	if err := AddProfileInclude(dir, "ci", "github.com/acme/x", AddOptions{Ref: "v1"}); err != nil {
		t.Fatalf("AddProfileInclude: %v", err)
	}
	incs := loadProfileIncludes(t, dir, "ci")
	if len(incs) != 1 || incs[0].Git != "github.com/acme/x" || incs[0].Ref != "v1" {
		t.Errorf("unexpected: %+v", incs)
	}
}

func TestAddProfileInclude_DuplicateErrors(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddProfile(dir, "ci"); err != nil {
		t.Fatal(err)
	}
	if err := AddProfileInclude(dir, "ci", "github.com/x", AddOptions{}); err != nil {
		t.Fatal(err)
	}
	err := AddProfileInclude(dir, "ci", "github.com/x", AddOptions{})
	if err == nil || !strings.Contains(err.Error(), "already present") {
		t.Errorf("want duplicate, got %v", err)
	}
}

func TestAddProfileInclude_Replace(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddProfile(dir, "ci"); err != nil {
		t.Fatal(err)
	}
	if err := AddProfileInclude(dir, "ci", "github.com/x", AddOptions{Ref: "v1"}); err != nil {
		t.Fatal(err)
	}
	if err := AddProfileInclude(dir, "ci", "github.com/x", AddOptions{Ref: "v2", Replace: true}); err != nil {
		t.Fatalf("replace: %v", err)
	}
	incs := loadProfileIncludes(t, dir, "ci")
	if len(incs) != 1 || incs[0].Ref != "v2" {
		t.Errorf("expected single v2, got %+v", incs)
	}
}

func TestAddProfileInclude_UnknownProfile(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	err := AddProfileInclude(dir, "ghost", "github.com/x", AddOptions{})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("want not-found, got %v", err)
	}
}

// ---- RemoveProfileInclude -------------------------------------------

func TestRemoveProfileInclude_Removes(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddProfile(dir, "ci"); err != nil {
		t.Fatal(err)
	}
	if err := AddProfileInclude(dir, "ci", "github.com/x", AddOptions{}); err != nil {
		t.Fatal(err)
	}

	if err := RemoveProfileInclude(dir, "ci", "github.com/x", RemoveOptions{}); err != nil {
		t.Fatalf("RemoveProfileInclude: %v", err)
	}
	if incs := loadProfileIncludes(t, dir, "ci"); len(incs) != 0 {
		t.Errorf("expected empty, got %+v", incs)
	}
}

func TestRemoveProfileInclude_NotFound(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddProfile(dir, "ci"); err != nil {
		t.Fatal(err)
	}
	err := RemoveProfileInclude(dir, "ci", "github.com/missing", RemoveOptions{})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("want not-found, got %v", err)
	}
}

func TestRemoveProfileInclude_AmbiguousRequiresPath(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddProfile(dir, "ci"); err != nil {
		t.Fatal(err)
	}
	if err := AddProfileInclude(dir, "ci", "github.com/x", AddOptions{Path: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := AddProfileInclude(dir, "ci", "github.com/x", AddOptions{Path: "b"}); err != nil {
		t.Fatal(err)
	}
	err := RemoveProfileInclude(dir, "ci", "github.com/x", RemoveOptions{})
	if err == nil || !strings.Contains(err.Error(), "matches multiple") {
		t.Errorf("want disambiguation error, got %v", err)
	}
}

func TestRemoveProfileInclude_WithPath(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddProfile(dir, "ci"); err != nil {
		t.Fatal(err)
	}
	if err := AddProfileInclude(dir, "ci", "github.com/x", AddOptions{Path: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := AddProfileInclude(dir, "ci", "github.com/x", AddOptions{Path: "b"}); err != nil {
		t.Fatal(err)
	}
	if err := RemoveProfileInclude(dir, "ci", "github.com/x", RemoveOptions{Path: "a"}); err != nil {
		t.Fatalf("RemoveProfileInclude: %v", err)
	}
	incs := loadProfileIncludes(t, dir, "ci")
	if len(incs) != 1 || incs[0].Path != "b" {
		t.Errorf("expected only path=b remaining, got %+v", incs)
	}
}

func TestRemoveProfileInclude_UnknownProfile(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	err := RemoveProfileInclude(dir, "ghost", "github.com/x", RemoveOptions{})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("want not-found, got %v", err)
	}
}

// ---- UpdateProfileInclude / FindProfileIncludeUpdateTarget ----------

func TestUpdateProfileInclude_Ref(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddProfile(dir, "ci"); err != nil {
		t.Fatal(err)
	}
	if err := AddProfileInclude(dir, "ci", "github.com/x", AddOptions{Ref: "v1"}); err != nil {
		t.Fatal(err)
	}
	newRef := "v2"
	if err := UpdateProfileInclude(dir, "ci", "github.com/x", UpdateOptions{Ref: &newRef}); err != nil {
		t.Fatalf("UpdateProfileInclude: %v", err)
	}
	incs := loadProfileIncludes(t, dir, "ci")
	if incs[0].Ref != "v2" {
		t.Errorf("ref = %q, want v2", incs[0].Ref)
	}
}

func TestUpdateProfileInclude_UnknownProfile(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	err := UpdateProfileInclude(dir, "ghost", "github.com/x", UpdateOptions{})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("want not-found, got %v", err)
	}
}

func TestFindProfileIncludeUpdateTarget_ComputesFinalState(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddProfile(dir, "ci"); err != nil {
		t.Fatal(err)
	}
	if err := AddProfileInclude(dir, "ci", "github.com/x", AddOptions{Path: "old", Ref: "v1"}); err != nil {
		t.Fatal(err)
	}
	newPath := "new"
	newRef := "v2"
	got, err := FindProfileIncludeUpdateTarget(dir, "ci", "github.com/x", UpdateOptions{NewPath: &newPath, Ref: &newRef})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got.Path != "new" || got.Ref != "v2" {
		t.Errorf("unexpected final state: %+v", got)
	}
}

func TestFindProfileIncludeUpdateTarget_UnknownProfile(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	_, err := FindProfileIncludeUpdateTarget(dir, "ghost", "github.com/x", UpdateOptions{})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("want not-found, got %v", err)
	}
}
