package harness

import (
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

// ---- AddHook / RemoveHook (harness-level) ---------------------------

func TestAddHook_New(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")

	if err := AddHook(dir, "before_tool", "./guard.sh", HookAddOptions{Matcher: "Write"}); err != nil {
		t.Fatalf("AddHook: %v", err)
	}
	hj, _ := plugin.LoadPluginJSON(dir)
	if len(hj.Hooks["before_tool"]) != 1 {
		t.Fatalf("expected 1 hook, got %d", len(hj.Hooks["before_tool"]))
	}
	got := hj.Hooks["before_tool"][0]
	if got.Command != "./guard.sh" || got.Matcher != "Write" {
		t.Errorf("unexpected entry: %+v", got)
	}
}

func TestAddHook_Appends(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddHook(dir, "before_tool", "a", HookAddOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := AddHook(dir, "before_tool", "b", HookAddOptions{}); err != nil {
		t.Fatal(err)
	}
	hj, _ := plugin.LoadPluginJSON(dir)
	if len(hj.Hooks["before_tool"]) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(hj.Hooks["before_tool"]))
	}
}

func TestAddHook_UnknownEvent(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	err := AddHook(dir, "bogus_event", "cmd", HookAddOptions{})
	if err == nil || !strings.Contains(err.Error(), "unknown hook event") {
		t.Errorf("want unknown event, got %v", err)
	}
}

func TestAddHook_EmptyCommand(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	err := AddHook(dir, "before_tool", "", HookAddOptions{})
	if err == nil || !strings.Contains(err.Error(), "command") {
		t.Errorf("want empty-command error, got %v", err)
	}
}

func TestRemoveHook_RemovesEntry(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddHook(dir, "before_tool", "a", HookAddOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := AddHook(dir, "before_tool", "b", HookAddOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := RemoveHook(dir, "before_tool", 0); err != nil {
		t.Fatalf("RemoveHook: %v", err)
	}
	hj, _ := plugin.LoadPluginJSON(dir)
	if len(hj.Hooks["before_tool"]) != 1 || hj.Hooks["before_tool"][0].Command != "b" {
		t.Errorf("expected only 'b' remaining, got %+v", hj.Hooks["before_tool"])
	}
}

func TestRemoveHook_DropsEmptyEventKey(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddHook(dir, "before_tool", "only", HookAddOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := RemoveHook(dir, "before_tool", 0); err != nil {
		t.Fatal(err)
	}
	hj, _ := plugin.LoadPluginJSON(dir)
	if _, ok := hj.Hooks["before_tool"]; ok {
		t.Errorf("event key should be deleted when no entries remain")
	}
	if hj.Hooks != nil {
		t.Errorf("Hooks should be nil after last entry removed, got %v", hj.Hooks)
	}
}

func TestRemoveHook_EventNotFound(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	err := RemoveHook(dir, "before_tool", 0)
	if err == nil || !strings.Contains(err.Error(), "no hooks") {
		t.Errorf("want no-hooks error, got %v", err)
	}
}

func TestRemoveHook_IndexOutOfRange(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddHook(dir, "before_tool", "a", HookAddOptions{}); err != nil {
		t.Fatal(err)
	}
	err := RemoveHook(dir, "before_tool", 5)
	if err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Errorf("want range error, got %v", err)
	}
}

// ---- AddProfileHook / RemoveProfileHook -----------------------------

func TestAddProfileHook_New(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddProfile(dir, "ci"); err != nil {
		t.Fatal(err)
	}
	if err := AddProfileHook(dir, "ci", "before_tool", "lint", HookAddOptions{Matcher: "*"}); err != nil {
		t.Fatalf("AddProfileHook: %v", err)
	}
	hj, _ := plugin.LoadPluginJSON(dir)
	p := hj.Profiles["ci"]
	if len(p.Hooks["before_tool"]) != 1 {
		t.Errorf("expected 1 entry, got %d", len(p.Hooks["before_tool"]))
	}
}

func TestAddProfileHook_UnknownEvent(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddProfile(dir, "ci"); err != nil {
		t.Fatal(err)
	}
	err := AddProfileHook(dir, "ci", "bogus", "x", HookAddOptions{})
	if err == nil || !strings.Contains(err.Error(), "unknown hook event") {
		t.Errorf("want unknown event, got %v", err)
	}
}

func TestAddProfileHook_EmptyCommand(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddProfile(dir, "ci"); err != nil {
		t.Fatal(err)
	}
	err := AddProfileHook(dir, "ci", "before_tool", "", HookAddOptions{})
	if err == nil || !strings.Contains(err.Error(), "command") {
		t.Errorf("want empty-command, got %v", err)
	}
}

func TestAddProfileHook_UnknownProfile(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	err := AddProfileHook(dir, "ghost", "before_tool", "x", HookAddOptions{})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("want profile not-found, got %v", err)
	}
}

func TestRemoveProfileHook_DropsEmptyKey(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddProfile(dir, "ci"); err != nil {
		t.Fatal(err)
	}
	if err := AddProfileHook(dir, "ci", "before_tool", "x", HookAddOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := RemoveProfileHook(dir, "ci", "before_tool", 0); err != nil {
		t.Fatalf("RemoveProfileHook: %v", err)
	}
	hj, _ := plugin.LoadPluginJSON(dir)
	p := hj.Profiles["ci"]
	if _, ok := p.Hooks["before_tool"]; ok {
		t.Errorf("event key should be cleared")
	}
}

func TestRemoveProfileHook_UnknownProfile(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	err := RemoveProfileHook(dir, "ghost", "before_tool", 0)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("want not-found, got %v", err)
	}
}

func TestRemoveProfileHook_NoHooksForEvent(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddProfile(dir, "ci"); err != nil {
		t.Fatal(err)
	}
	err := RemoveProfileHook(dir, "ci", "before_tool", 0)
	if err == nil || !strings.Contains(err.Error(), "no hooks") {
		t.Errorf("want no-hooks, got %v", err)
	}
}

func TestRemoveProfileHook_IndexOutOfRange(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddProfile(dir, "ci"); err != nil {
		t.Fatal(err)
	}
	if err := AddProfileHook(dir, "ci", "before_tool", "x", HookAddOptions{}); err != nil {
		t.Fatal(err)
	}
	err := RemoveProfileHook(dir, "ci", "before_tool", 9)
	if err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Errorf("want range error, got %v", err)
	}
}
