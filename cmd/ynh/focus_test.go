package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

func writeFocusTestHarness(t *testing.T, dir, name string) {
	t.Helper()
	hj := &plugin.HarnessJSON{Name: name, Version: "0.1.0"}
	if err := plugin.SavePluginJSON(dir, hj); err != nil {
		t.Fatal(err)
	}
}

func loadTestFocuses(t *testing.T, dir string) map[string]plugin.Focus {
	t.Helper()
	hj, err := plugin.LoadPluginJSON(dir)
	if err != nil {
		t.Fatalf("LoadPluginJSON: %v", err)
	}
	return hj.Focuses
}

func TestCmdFocus_NoArgs(t *testing.T) {
	err := cmdFocus([]string{})
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Errorf("expected usage error, got: %v", err)
	}
}

func TestCmdFocus_UnknownSubcommand(t *testing.T) {
	err := cmdFocus([]string{"bogus"})
	if err == nil || !strings.Contains(err.Error(), "unknown focus subcommand") {
		t.Errorf("expected unknown subcommand error, got: %v", err)
	}
}

func TestCmdFocusAdd_Basic(t *testing.T) {
	dir := t.TempDir()
	writeFocusTestHarness(t, dir, "h")

	var buf bytes.Buffer
	if err := cmdFocusTo([]string{"add", dir, "review", "review this code"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := loadTestFocuses(t, dir)
	f, ok := got["review"]
	if !ok || f.Prompt != "review this code" {
		t.Errorf("expected focus review with prompt, got %+v", got)
	}
	if !strings.Contains(buf.String(), "Added focus") {
		t.Errorf("expected confirmation, got: %q", buf.String())
	}
}

func TestCmdFocusAdd_WithProfile(t *testing.T) {
	dir := t.TempDir()
	hj := &plugin.HarnessJSON{
		Name: "h", Version: "0.1.0",
		Profiles: map[string]plugin.Profile{"thorough": {}},
	}
	if err := plugin.SavePluginJSON(dir, hj); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := cmdFocusTo([]string{"add", dir, "deep", "audit this", "--profile", "thorough"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := loadTestFocuses(t, dir)
	if f := got["deep"]; f.Profile != "thorough" {
		t.Errorf("expected profile thorough, got %+v", f)
	}
}

func TestCmdFocusAdd_UnknownProfile(t *testing.T) {
	dir := t.TempDir()
	writeFocusTestHarness(t, dir, "h")

	var buf bytes.Buffer
	err := cmdFocusTo([]string{"add", dir, "deep", "audit", "--profile", "nope"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "unknown profile") {
		t.Errorf("expected unknown profile error, got: %v", err)
	}
}

func TestCmdFocusAdd_Duplicate(t *testing.T) {
	dir := t.TempDir()
	writeFocusTestHarness(t, dir, "h")

	var buf bytes.Buffer
	if err := cmdFocusTo([]string{"add", dir, "f", "p"}, &buf); err != nil {
		t.Fatal(err)
	}
	err := cmdFocusTo([]string{"add", dir, "f", "p2"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected already-exists error, got: %v", err)
	}
}

func TestCmdFocusAdd_EmptyPrompt(t *testing.T) {
	dir := t.TempDir()
	writeFocusTestHarness(t, dir, "h")

	var buf bytes.Buffer
	err := cmdFocusTo([]string{"add", dir, "f", ""}, &buf)
	if err == nil || !strings.Contains(err.Error(), "prompt must not be empty") {
		t.Errorf("expected empty-prompt error, got: %v", err)
	}
}

func TestCmdFocusRemove_Basic(t *testing.T) {
	dir := t.TempDir()
	writeFocusTestHarness(t, dir, "h")

	var buf bytes.Buffer
	if err := cmdFocusTo([]string{"add", dir, "f", "p"}, &buf); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	if err := cmdFocusTo([]string{"remove", dir, "f"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(loadTestFocuses(t, dir)) != 0 {
		t.Errorf("expected no focuses after remove")
	}
}

func TestCmdFocusRemove_NotFound(t *testing.T) {
	dir := t.TempDir()
	writeFocusTestHarness(t, dir, "h")

	var buf bytes.Buffer
	err := cmdFocusTo([]string{"remove", dir, "f"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected not-found error, got: %v", err)
	}
}

func TestCmdFocusUpdate_Prompt(t *testing.T) {
	dir := t.TempDir()
	writeFocusTestHarness(t, dir, "h")

	var buf bytes.Buffer
	_ = cmdFocusTo([]string{"add", dir, "f", "old"}, &buf)
	buf.Reset()

	if err := cmdFocusTo([]string{"update", dir, "f", "--prompt", "new"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := loadTestFocuses(t, dir)
	if got["f"].Prompt != "new" {
		t.Errorf("expected new prompt, got %+v", got["f"])
	}
}

func TestCmdFocusUpdate_ClearProfile(t *testing.T) {
	dir := t.TempDir()
	hj := &plugin.HarnessJSON{
		Name: "h", Version: "0.1.0",
		Profiles: map[string]plugin.Profile{"x": {}},
		Focuses:  map[string]plugin.Focus{"f": {Profile: "x", Prompt: "p"}},
	}
	if err := plugin.SavePluginJSON(dir, hj); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := cmdFocusTo([]string{"update", dir, "f", "--clear", "profile"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := loadTestFocuses(t, dir)
	if got["f"].Profile != "" {
		t.Errorf("expected profile cleared, got %q", got["f"].Profile)
	}
}

func TestCmdFocusUpdate_ProfileAndClearMutex(t *testing.T) {
	dir := t.TempDir()
	writeFocusTestHarness(t, dir, "h")

	var buf bytes.Buffer
	_ = cmdFocusTo([]string{"add", dir, "f", "p"}, &buf)

	err := cmdFocusTo([]string{"update", dir, "f", "--profile", "x", "--clear", "profile"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("expected mutually-exclusive error, got: %v", err)
	}
}

func TestCmdFocusUpdate_NoFlags(t *testing.T) {
	dir := t.TempDir()
	writeFocusTestHarness(t, dir, "h")

	var buf bytes.Buffer
	_ = cmdFocusTo([]string{"add", dir, "f", "p"}, &buf)

	err := cmdFocusTo([]string{"update", dir, "f"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "at least one of") {
		t.Errorf("expected required-flag error, got: %v", err)
	}
}
