package harness

import (
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

// loadDelegates is a test helper that reads the delegates_to from a harness dir.
func loadDelegates(t *testing.T, dir string) []plugin.DelegateMeta {
	t.Helper()
	hj, err := plugin.LoadPluginJSON(dir)
	if err != nil {
		t.Fatalf("LoadPluginJSON: %v", err)
	}
	return hj.DelegatesTo
}

// ---- AddDelegate -------------------------------------------------------

func TestAddDelegate_New(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")

	err := AddDelegate(dir, "github.com/acme/agent", DelegateAddOptions{Ref: "main"})
	if err != nil {
		t.Fatalf("AddDelegate: %v", err)
	}

	dels := loadDelegates(t, dir)
	if len(dels) != 1 {
		t.Fatalf("expected 1 delegate, got %d", len(dels))
	}
	if dels[0].Git != "github.com/acme/agent" || dels[0].Ref != "main" {
		t.Errorf("unexpected delegate: %+v", dels[0])
	}
}

func TestAddDelegate_DuplicateErrors(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")

	opts := DelegateAddOptions{}
	if err := AddDelegate(dir, "github.com/acme/agent", opts); err != nil {
		t.Fatalf("first add: %v", err)
	}
	err := AddDelegate(dir, "github.com/acme/agent", opts)
	if err == nil {
		t.Fatal("expected error on duplicate add")
	}
	if !strings.Contains(err.Error(), "already present") {
		t.Errorf("error should mention already present, got: %v", err)
	}
}

func TestAddDelegate_SameURLDifferentPath(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")

	if err := AddDelegate(dir, "github.com/acme/mono", DelegateAddOptions{Path: "agents/a"}); err != nil {
		t.Fatalf("add a: %v", err)
	}
	if err := AddDelegate(dir, "github.com/acme/mono", DelegateAddOptions{Path: "agents/b"}); err != nil {
		t.Fatalf("add b: %v", err)
	}

	dels := loadDelegates(t, dir)
	if len(dels) != 2 {
		t.Fatalf("expected 2 delegates, got %d", len(dels))
	}
}

func TestAddDelegate_WithPath(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")

	if err := AddDelegate(dir, "github.com/acme/mono", DelegateAddOptions{Path: "sub", Ref: "v1"}); err != nil {
		t.Fatalf("AddDelegate: %v", err)
	}

	dels := loadDelegates(t, dir)
	if dels[0].Path != "sub" || dels[0].Ref != "v1" {
		t.Errorf("unexpected delegate: %+v", dels[0])
	}
}

// ---- RemoveDelegate -------------------------------------------------------

func TestRemoveDelegate_Removes(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddDelegate(dir, "github.com/acme/agent", DelegateAddOptions{}); err != nil {
		t.Fatal(err)
	}

	if err := RemoveDelegate(dir, "github.com/acme/agent", DelegateRemoveOptions{}); err != nil {
		t.Fatalf("RemoveDelegate: %v", err)
	}

	dels := loadDelegates(t, dir)
	if len(dels) != 0 {
		t.Fatalf("expected 0 delegates, got %d", len(dels))
	}
}

func TestRemoveDelegate_NotFound(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")

	err := RemoveDelegate(dir, "github.com/acme/agent", DelegateRemoveOptions{})
	if err == nil {
		t.Fatal("expected error for missing delegate")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention not found, got: %v", err)
	}
}

func TestRemoveDelegate_AmbiguousRequiresPath(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddDelegate(dir, "github.com/acme/mono", DelegateAddOptions{Path: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := AddDelegate(dir, "github.com/acme/mono", DelegateAddOptions{Path: "b"}); err != nil {
		t.Fatal(err)
	}

	err := RemoveDelegate(dir, "github.com/acme/mono", DelegateRemoveOptions{})
	if err == nil {
		t.Fatal("expected error for ambiguous URL without path")
	}
	if !strings.Contains(err.Error(), "disambiguate") {
		t.Errorf("error should mention disambiguate, got: %v", err)
	}
}

func TestRemoveDelegate_WithPath(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddDelegate(dir, "github.com/acme/mono", DelegateAddOptions{Path: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := AddDelegate(dir, "github.com/acme/mono", DelegateAddOptions{Path: "b"}); err != nil {
		t.Fatal(err)
	}

	if err := RemoveDelegate(dir, "github.com/acme/mono", DelegateRemoveOptions{Path: "a"}); err != nil {
		t.Fatalf("RemoveDelegate with path: %v", err)
	}

	dels := loadDelegates(t, dir)
	if len(dels) != 1 || dels[0].Path != "b" {
		t.Errorf("expected 1 delegate with path=b, got %+v", dels)
	}
}

// ---- UpdateDelegate -------------------------------------------------------

func TestUpdateDelegate_Ref(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddDelegate(dir, "github.com/acme/agent", DelegateAddOptions{Ref: "v1"}); err != nil {
		t.Fatal(err)
	}

	newRef := "v2"
	if err := UpdateDelegate(dir, "github.com/acme/agent", DelegateUpdateOptions{Ref: &newRef}); err != nil {
		t.Fatalf("UpdateDelegate: %v", err)
	}

	dels := loadDelegates(t, dir)
	if dels[0].Ref != "v2" {
		t.Errorf("expected ref v2, got %q", dels[0].Ref)
	}
}

func TestUpdateDelegate_Path(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddDelegate(dir, "github.com/acme/agent", DelegateAddOptions{Path: "old"}); err != nil {
		t.Fatal(err)
	}

	newPath := "new"
	if err := UpdateDelegate(dir, "github.com/acme/agent", DelegateUpdateOptions{NewPath: &newPath}); err != nil {
		t.Fatalf("UpdateDelegate: %v", err)
	}

	dels := loadDelegates(t, dir)
	if dels[0].Path != "new" {
		t.Errorf("expected path new, got %q", dels[0].Path)
	}
}

func TestUpdateDelegate_OmittedFieldsUnchanged(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddDelegate(dir, "github.com/acme/agent", DelegateAddOptions{Ref: "v1", Path: "p"}); err != nil {
		t.Fatal(err)
	}

	newRef := "v2"
	if err := UpdateDelegate(dir, "github.com/acme/agent", DelegateUpdateOptions{Ref: &newRef}); err != nil {
		t.Fatalf("UpdateDelegate: %v", err)
	}

	dels := loadDelegates(t, dir)
	if dels[0].Ref != "v2" {
		t.Errorf("ref should be v2, got %q", dels[0].Ref)
	}
	if dels[0].Path != "p" {
		t.Errorf("path should be unchanged (p), got %q", dels[0].Path)
	}
}

func TestUpdateDelegate_NotFound(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")

	newRef := "v2"
	err := UpdateDelegate(dir, "github.com/acme/agent", DelegateUpdateOptions{Ref: &newRef})
	if err == nil {
		t.Fatal("expected error for missing delegate")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention not found, got: %v", err)
	}
}

func TestUpdateDelegate_AmbiguousRequiresFromPath(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddDelegate(dir, "github.com/acme/mono", DelegateAddOptions{Path: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := AddDelegate(dir, "github.com/acme/mono", DelegateAddOptions{Path: "b"}); err != nil {
		t.Fatal(err)
	}

	newRef := "v2"
	err := UpdateDelegate(dir, "github.com/acme/mono", DelegateUpdateOptions{Ref: &newRef})
	if err == nil {
		t.Fatal("expected error for ambiguous URL without --from-path")
	}
	if !strings.Contains(err.Error(), "disambiguate") {
		t.Errorf("error should mention disambiguate, got: %v", err)
	}
}

func TestUpdateDelegate_FromPath(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddDelegate(dir, "github.com/acme/mono", DelegateAddOptions{Path: "a", Ref: "v1"}); err != nil {
		t.Fatal(err)
	}
	if err := AddDelegate(dir, "github.com/acme/mono", DelegateAddOptions{Path: "b", Ref: "v1"}); err != nil {
		t.Fatal(err)
	}

	newRef := "v2"
	if err := UpdateDelegate(dir, "github.com/acme/mono", DelegateUpdateOptions{FromPath: "a", Ref: &newRef}); err != nil {
		t.Fatalf("UpdateDelegate with from-path: %v", err)
	}

	dels := loadDelegates(t, dir)
	refByPath := map[string]string{}
	for _, del := range dels {
		refByPath[del.Path] = del.Ref
	}
	if refByPath["a"] != "v2" {
		t.Errorf("expected a→v2, got %q", refByPath["a"])
	}
	if refByPath["b"] != "v1" {
		t.Errorf("expected b→v1 (unchanged), got %q", refByPath["b"])
	}
}

// ---- FindDelegateUpdateTarget -------------------------------------------------------

func TestFindDelegateUpdateTarget_ComputesFinalState(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddDelegate(dir, "github.com/acme/agent", DelegateAddOptions{Ref: "v1", Path: "old"}); err != nil {
		t.Fatal(err)
	}

	newPath := "new"
	newRef := "v2"
	del, err := FindDelegateUpdateTarget(dir, "github.com/acme/agent", DelegateUpdateOptions{NewPath: &newPath, Ref: &newRef})
	if err != nil {
		t.Fatalf("FindDelegateUpdateTarget: %v", err)
	}
	if del.Path != "new" || del.Ref != "v2" {
		t.Errorf("expected path=new ref=v2, got path=%q ref=%q", del.Path, del.Ref)
	}
}
