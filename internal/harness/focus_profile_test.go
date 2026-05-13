package harness

import (
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

// ---- AddFocus -------------------------------------------------------

func TestAddFocus_New(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")

	if err := AddFocus(dir, "review", "review changes", FocusAddOptions{}); err != nil {
		t.Fatalf("AddFocus: %v", err)
	}

	hj, err := plugin.LoadPluginJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	f, ok := hj.Focuses["review"]
	if !ok {
		t.Fatal("focus not persisted")
	}
	if f.Prompt != "review changes" {
		t.Errorf("prompt = %q, want %q", f.Prompt, "review changes")
	}
	if f.Profile != "" {
		t.Errorf("expected no profile binding, got %q", f.Profile)
	}
}

func TestAddFocus_WithProfile(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddProfile(dir, "ci"); err != nil {
		t.Fatal(err)
	}

	if err := AddFocus(dir, "audit", "audit", FocusAddOptions{Profile: "ci"}); err != nil {
		t.Fatalf("AddFocus: %v", err)
	}

	hj, _ := plugin.LoadPluginJSON(dir)
	if hj.Focuses["audit"].Profile != "ci" {
		t.Errorf("profile = %q, want ci", hj.Focuses["audit"].Profile)
	}
}

func TestAddFocus_EmptyName(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	err := AddFocus(dir, "", "p", FocusAddOptions{})
	if err == nil || !strings.Contains(err.Error(), "name") {
		t.Errorf("want name error, got %v", err)
	}
}

func TestAddFocus_EmptyPrompt(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	err := AddFocus(dir, "x", "", FocusAddOptions{})
	if err == nil || !strings.Contains(err.Error(), "prompt") {
		t.Errorf("want prompt error, got %v", err)
	}
}

func TestAddFocus_DuplicateErrors(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddFocus(dir, "x", "p", FocusAddOptions{}); err != nil {
		t.Fatal(err)
	}
	err := AddFocus(dir, "x", "p2", FocusAddOptions{})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("want duplicate error, got %v", err)
	}
}

func TestAddFocus_UnknownProfile(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	err := AddFocus(dir, "x", "p", FocusAddOptions{Profile: "nope"})
	if err == nil || !strings.Contains(err.Error(), "unknown profile") {
		t.Errorf("want unknown profile error, got %v", err)
	}
}

// ---- RemoveFocus ----------------------------------------------------

func TestRemoveFocus_Removes(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddFocus(dir, "x", "p", FocusAddOptions{}); err != nil {
		t.Fatal(err)
	}

	if err := RemoveFocus(dir, "x"); err != nil {
		t.Fatalf("RemoveFocus: %v", err)
	}
	hj, _ := plugin.LoadPluginJSON(dir)
	if len(hj.Focuses) != 0 {
		t.Errorf("expected focuses cleared, got %v", hj.Focuses)
	}
}

func TestRemoveFocus_NotFound(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	err := RemoveFocus(dir, "ghost")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("want not-found error, got %v", err)
	}
}

// ---- UpdateFocus ----------------------------------------------------

func TestUpdateFocus_Prompt(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddFocus(dir, "x", "old", FocusAddOptions{}); err != nil {
		t.Fatal(err)
	}
	newp := "new prompt"
	if err := UpdateFocus(dir, "x", FocusUpdateOptions{Prompt: &newp}); err != nil {
		t.Fatalf("UpdateFocus: %v", err)
	}
	hj, _ := plugin.LoadPluginJSON(dir)
	if hj.Focuses["x"].Prompt != "new prompt" {
		t.Errorf("prompt = %q, want %q", hj.Focuses["x"].Prompt, "new prompt")
	}
}

func TestUpdateFocus_EmptyPromptRejected(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddFocus(dir, "x", "old", FocusAddOptions{}); err != nil {
		t.Fatal(err)
	}
	empty := ""
	err := UpdateFocus(dir, "x", FocusUpdateOptions{Prompt: &empty})
	if err == nil || !strings.Contains(err.Error(), "prompt") {
		t.Errorf("want empty-prompt error, got %v", err)
	}
}

func TestUpdateFocus_ClearProfile(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddProfile(dir, "ci"); err != nil {
		t.Fatal(err)
	}
	if err := AddFocus(dir, "x", "p", FocusAddOptions{Profile: "ci"}); err != nil {
		t.Fatal(err)
	}
	empty := ""
	if err := UpdateFocus(dir, "x", FocusUpdateOptions{Profile: &empty}); err != nil {
		t.Fatalf("UpdateFocus: %v", err)
	}
	hj, _ := plugin.LoadPluginJSON(dir)
	if hj.Focuses["x"].Profile != "" {
		t.Errorf("profile should be cleared, got %q", hj.Focuses["x"].Profile)
	}
}

func TestUpdateFocus_NotFound(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	prompt := "x"
	err := UpdateFocus(dir, "ghost", FocusUpdateOptions{Prompt: &prompt})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("want not-found, got %v", err)
	}
}

func TestUpdateFocus_UnknownProfile(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddFocus(dir, "x", "p", FocusAddOptions{}); err != nil {
		t.Fatal(err)
	}
	bad := "nope"
	err := UpdateFocus(dir, "x", FocusUpdateOptions{Profile: &bad})
	if err == nil || !strings.Contains(err.Error(), "unknown profile") {
		t.Errorf("want unknown profile, got %v", err)
	}
}

func TestUpdateFocus_OmittedFieldsUnchanged(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddProfile(dir, "ci"); err != nil {
		t.Fatal(err)
	}
	if err := AddFocus(dir, "x", "p", FocusAddOptions{Profile: "ci"}); err != nil {
		t.Fatal(err)
	}
	if err := UpdateFocus(dir, "x", FocusUpdateOptions{}); err != nil {
		t.Fatalf("noop update: %v", err)
	}
	hj, _ := plugin.LoadPluginJSON(dir)
	if hj.Focuses["x"].Prompt != "p" || hj.Focuses["x"].Profile != "ci" {
		t.Errorf("unexpected state: %+v", hj.Focuses["x"])
	}
}

// ---- AddProfile / RemoveProfile -------------------------------------

func TestAddProfile_New(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")

	if err := AddProfile(dir, "ci"); err != nil {
		t.Fatalf("AddProfile: %v", err)
	}
	hj, _ := plugin.LoadPluginJSON(dir)
	if _, ok := hj.Profiles["ci"]; !ok {
		t.Fatal("profile not persisted")
	}
}

func TestAddProfile_EmptyName(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	err := AddProfile(dir, "")
	if err == nil || !strings.Contains(err.Error(), "name") {
		t.Errorf("want name error, got %v", err)
	}
}

func TestAddProfile_DuplicateErrors(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddProfile(dir, "ci"); err != nil {
		t.Fatal(err)
	}
	err := AddProfile(dir, "ci")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("want duplicate, got %v", err)
	}
}

func TestRemoveProfile_Removes(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddProfile(dir, "ci"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveProfile(dir, "ci"); err != nil {
		t.Fatalf("RemoveProfile: %v", err)
	}
	hj, _ := plugin.LoadPluginJSON(dir)
	if len(hj.Profiles) != 0 {
		t.Errorf("expected profiles cleared, got %v", hj.Profiles)
	}
}

func TestRemoveProfile_NotFound(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	err := RemoveProfile(dir, "ghost")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("want not-found, got %v", err)
	}
}

func TestRemoveProfile_BlockedByFocus(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddProfile(dir, "ci"); err != nil {
		t.Fatal(err)
	}
	if err := AddFocus(dir, "blocker", "p", FocusAddOptions{Profile: "ci"}); err != nil {
		t.Fatal(err)
	}

	err := RemoveProfile(dir, "ci")
	if err == nil {
		t.Fatal("expected error when focus references profile")
	}
	if !strings.Contains(err.Error(), "blocker") || !strings.Contains(err.Error(), "referenced") {
		t.Errorf("error should mention referencing focus, got: %v", err)
	}
}

func TestRemoveProfile_LastsBlockerListSorted(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddProfile(dir, "ci"); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"zeta", "alpha", "mu"} {
		if err := AddFocus(dir, n, "p", FocusAddOptions{Profile: "ci"}); err != nil {
			t.Fatal(err)
		}
	}
	err := RemoveProfile(dir, "ci")
	if err == nil {
		t.Fatal("expected error")
	}
	// alpha should appear before mu before zeta in the error
	msg := err.Error()
	ai := strings.Index(msg, "alpha")
	mi := strings.Index(msg, "mu")
	zi := strings.Index(msg, "zeta")
	if ai < 0 || mi <= ai || zi <= mi {
		t.Errorf("blockers not sorted alphabetically: %v", err)
	}
}
