package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

func writeHookTestHarness(t *testing.T, dir, name string) {
	t.Helper()
	hj := &plugin.HarnessJSON{Name: name, Version: "0.1.0"}
	if err := plugin.SavePluginJSON(dir, hj); err != nil {
		t.Fatal(err)
	}
}

func loadTestHarness(t *testing.T, dir string) *plugin.HarnessJSON {
	t.Helper()
	hj, err := plugin.LoadPluginJSON(dir)
	if err != nil {
		t.Fatalf("LoadPluginJSON: %v", err)
	}
	return hj
}

func TestCmdHook_NoArgs(t *testing.T) {
	err := cmdHook([]string{})
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Errorf("expected usage error, got: %v", err)
	}
}

func TestCmdHook_UnknownSubcommand(t *testing.T) {
	err := cmdHook([]string{"bogus"})
	if err == nil || !strings.Contains(err.Error(), "unknown hook subcommand") {
		t.Errorf("expected unknown subcommand error, got: %v", err)
	}
}

func TestCmdHookAdd_Basic(t *testing.T) {
	dir := t.TempDir()
	writeHookTestHarness(t, dir, "h")

	var buf bytes.Buffer
	if err := cmdHookTo([]string{"add", dir, "before_tool", "echo go"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hj := loadTestHarness(t, dir)
	entries := hj.Hooks["before_tool"]
	if len(entries) != 1 || entries[0].Command != "echo go" {
		t.Errorf("expected one hook, got %+v", entries)
	}
}

func TestCmdHookAdd_WithMatcher(t *testing.T) {
	dir := t.TempDir()
	writeHookTestHarness(t, dir, "h")

	var buf bytes.Buffer
	if err := cmdHookTo([]string{"add", dir, "before_tool", "echo x", "--matcher", "Write"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hj := loadTestHarness(t, dir)
	if hj.Hooks["before_tool"][0].Matcher != "Write" {
		t.Errorf("expected matcher=Write, got %+v", hj.Hooks["before_tool"][0])
	}
}

func TestCmdHookAdd_UnknownEvent(t *testing.T) {
	dir := t.TempDir()
	writeHookTestHarness(t, dir, "h")

	var buf bytes.Buffer
	err := cmdHookTo([]string{"add", dir, "bogus", "cmd"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "unknown hook event") {
		t.Errorf("expected unknown-event error, got: %v", err)
	}
}

func TestCmdHookRemove_Basic(t *testing.T) {
	dir := t.TempDir()
	writeHookTestHarness(t, dir, "h")

	var buf bytes.Buffer
	_ = cmdHookTo([]string{"add", dir, "before_tool", "a"}, &buf)
	_ = cmdHookTo([]string{"add", dir, "before_tool", "b"}, &buf)

	if err := cmdHookTo([]string{"remove", dir, "before_tool", "0"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hj := loadTestHarness(t, dir)
	entries := hj.Hooks["before_tool"]
	if len(entries) != 1 || entries[0].Command != "b" {
		t.Errorf("expected single remaining entry 'b', got %+v", entries)
	}
}

func TestCmdHookRemove_LastEntryDropsKey(t *testing.T) {
	dir := t.TempDir()
	writeHookTestHarness(t, dir, "h")

	var buf bytes.Buffer
	_ = cmdHookTo([]string{"add", dir, "before_tool", "a"}, &buf)

	if err := cmdHookTo([]string{"remove", dir, "before_tool", "0"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hj := loadTestHarness(t, dir)
	if _, ok := hj.Hooks["before_tool"]; ok {
		t.Errorf("expected event key removed when empty")
	}
}

func TestCmdHookRemove_OutOfRange(t *testing.T) {
	dir := t.TempDir()
	writeHookTestHarness(t, dir, "h")

	var buf bytes.Buffer
	_ = cmdHookTo([]string{"add", dir, "before_tool", "a"}, &buf)

	err := cmdHookTo([]string{"remove", dir, "before_tool", "5"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Errorf("expected out-of-range error, got: %v", err)
	}
}

func TestCmdHookRemove_NonInteger(t *testing.T) {
	dir := t.TempDir()
	writeHookTestHarness(t, dir, "h")

	var buf bytes.Buffer
	err := cmdHookTo([]string{"remove", dir, "before_tool", "not-int"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "must be an integer") {
		t.Errorf("expected integer error, got: %v", err)
	}
}
