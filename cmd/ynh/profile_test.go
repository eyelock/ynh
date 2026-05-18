// Tests for `ynh profile add|remove` and the profile-scoped surface that
// `ynh hook|mcp|include` now provides via `--profile <name>`. The original
// `ynh profile hook|mcp|include` sub-trees were folded into the top-level
// commands; tests here exercise the new shape.
package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

func writeProfileTestHarness(t *testing.T, dir, name string) {
	t.Helper()
	hj := &plugin.HarnessJSON{Name: name, Version: "0.1.0"}
	if err := plugin.SavePluginJSON(dir, hj); err != nil {
		t.Fatal(err)
	}
}

func loadTestProfiles(t *testing.T, dir string) map[string]plugin.Profile {
	t.Helper()
	hj, err := plugin.LoadPluginJSON(dir)
	if err != nil {
		t.Fatalf("LoadPluginJSON: %v", err)
	}
	return hj.Profiles
}

// ---- routing -----------------------------------------------------

func TestCmdProfile_NoArgs(t *testing.T) {
	err := cmdProfile([]string{})
	if err == nil || !strings.Contains(err.Error(), "usage") {
		t.Errorf("expected usage error, got: %v", err)
	}
}

func TestCmdProfile_UnknownSubcommand(t *testing.T) {
	err := cmdProfile([]string{"bogus"})
	if err == nil || !strings.Contains(err.Error(), "unknown profile subcommand") {
		t.Errorf("expected unknown subcommand error, got: %v", err)
	}
}

// ---- add / remove ------------------------------------------------

func TestCmdProfileAdd_Basic(t *testing.T) {
	dir := t.TempDir()
	writeProfileTestHarness(t, dir, "h")

	var buf bytes.Buffer
	if err := cmdProfileTo([]string{"add", dir, "thorough"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := loadTestProfiles(t, dir)
	if _, ok := got["thorough"]; !ok {
		t.Errorf("expected profile thorough, got %+v", got)
	}
}

func TestCmdProfileAdd_Duplicate(t *testing.T) {
	dir := t.TempDir()
	writeProfileTestHarness(t, dir, "h")

	var buf bytes.Buffer
	_ = cmdProfileTo([]string{"add", dir, "p"}, &buf)
	err := cmdProfileTo([]string{"add", dir, "p"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected duplicate error, got: %v", err)
	}
}

func TestCmdProfileRemove_Basic(t *testing.T) {
	dir := t.TempDir()
	writeProfileTestHarness(t, dir, "h")

	var buf bytes.Buffer
	_ = cmdProfileTo([]string{"add", dir, "p"}, &buf)
	buf.Reset()

	if err := cmdProfileTo([]string{"remove", dir, "p"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(loadTestProfiles(t, dir)) != 0 {
		t.Errorf("expected no profiles after remove")
	}
}

func TestCmdProfileRemove_BlockedByFocus(t *testing.T) {
	dir := t.TempDir()
	hj := &plugin.HarnessJSON{
		Name: "h", Version: "0.1.0",
		Profiles: map[string]plugin.Profile{"p": {}},
		Focuses:  map[string]plugin.Focus{"f": {Profile: "p", Prompt: "x"}},
	}
	if err := plugin.SavePluginJSON(dir, hj); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	err := cmdProfileTo([]string{"remove", dir, "p"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "referenced by focus") {
		t.Errorf("expected referenced-by error, got: %v", err)
	}
}

// ---- profile-scoped hook (now top-level with --profile) ---------

func setupProfile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeProfileTestHarness(t, dir, "h")
	var buf bytes.Buffer
	if err := cmdProfileTo([]string{"add", dir, "p"}, &buf); err != nil {
		t.Fatalf("setup profile: %v", err)
	}
	return dir
}

func TestCmdProfileHookAdd_Basic(t *testing.T) {
	dir := setupProfile(t)
	var buf bytes.Buffer
	if err := cmdHookTo([]string{"add", dir, "before_tool", "echo before", "--profile", "p"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := loadTestProfiles(t, dir)
	entries := got["p"].Hooks["before_tool"]
	if len(entries) != 1 || entries[0].Command != "echo before" {
		t.Errorf("expected one hook entry, got %+v", entries)
	}
}

func TestCmdProfileHookAdd_UnknownEvent(t *testing.T) {
	dir := setupProfile(t)
	var buf bytes.Buffer
	err := cmdHookTo([]string{"add", dir, "garbage", "cmd", "--profile", "p"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "unknown hook event") {
		t.Errorf("expected unknown-event error, got: %v", err)
	}
}

func TestCmdProfileHookRemove_Basic(t *testing.T) {
	dir := setupProfile(t)
	var buf bytes.Buffer
	_ = cmdHookTo([]string{"add", dir, "before_tool", "a", "--profile", "p"}, &buf)
	_ = cmdHookTo([]string{"add", dir, "before_tool", "b", "--profile", "p"}, &buf)
	buf.Reset()

	if err := cmdHookTo([]string{"remove", dir, "before_tool", "0", "--profile", "p"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := loadTestProfiles(t, dir)
	entries := got["p"].Hooks["before_tool"]
	if len(entries) != 1 || entries[0].Command != "b" {
		t.Errorf("expected single remaining entry 'b', got %+v", entries)
	}
}

func TestCmdProfileHookRemove_LastEntryDropsKey(t *testing.T) {
	dir := setupProfile(t)
	var buf bytes.Buffer
	_ = cmdHookTo([]string{"add", dir, "before_tool", "a", "--profile", "p"}, &buf)

	if err := cmdHookTo([]string{"remove", dir, "before_tool", "0", "--profile", "p"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := loadTestProfiles(t, dir)
	if _, ok := got["p"].Hooks["before_tool"]; ok {
		t.Errorf("expected before_tool key to be removed when empty")
	}
}

func TestCmdProfileHookRemove_OutOfRange(t *testing.T) {
	dir := setupProfile(t)
	var buf bytes.Buffer
	_ = cmdHookTo([]string{"add", dir, "before_tool", "a", "--profile", "p"}, &buf)

	err := cmdHookTo([]string{"remove", dir, "before_tool", "5", "--profile", "p"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Errorf("expected out-of-range error, got: %v", err)
	}
}

// ---- profile-scoped mcp (now top-level with --profile) ----------

func TestCmdProfileMCPAdd_Command(t *testing.T) {
	dir := setupProfile(t)
	var buf bytes.Buffer
	if err := cmdMCPTo([]string{
		"add", dir, "github", "--profile", "p",
		"--command", "gh", "--arg", "mcp", "--env", "TOK=abc",
	}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := loadTestProfiles(t, dir)
	srv := got["p"].MCPServers["github"]
	if srv == nil || srv.Command != "gh" || len(srv.Args) != 1 || srv.Args[0] != "mcp" || srv.Env["TOK"] != "abc" {
		t.Errorf("expected gh mcp server, got %+v", srv)
	}
}

func TestCmdProfileMCPAdd_Null(t *testing.T) {
	dir := setupProfile(t)
	var buf bytes.Buffer
	if err := cmdMCPTo([]string{"add", dir, "ditched", "--null", "--profile", "p"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := loadTestProfiles(t, dir)
	srv, ok := got["p"].MCPServers["ditched"]
	if !ok || srv != nil {
		t.Errorf("expected null entry, got %v ok=%v", srv, ok)
	}
}

func TestCmdProfileMCPAdd_BothCommandAndURL(t *testing.T) {
	dir := setupProfile(t)
	var buf bytes.Buffer
	err := cmdMCPTo([]string{
		"add", dir, "x", "--profile", "p", "--command", "c", "--url", "u",
	}, &buf)
	if err == nil || (!strings.Contains(err.Error(), "not both") && !strings.Contains(err.Error(), "cannot have both")) {
		t.Errorf("expected both-error, got: %v", err)
	}
}

func TestCmdProfileMCPAdd_Neither(t *testing.T) {
	dir := setupProfile(t)
	var buf bytes.Buffer
	err := cmdMCPTo([]string{"add", dir, "x", "--profile", "p"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "requires --command") {
		t.Errorf("expected requires-flag error, got: %v", err)
	}
}

func TestCmdProfileMCPAdd_BadEnv(t *testing.T) {
	dir := setupProfile(t)
	var buf bytes.Buffer
	err := cmdMCPTo([]string{
		"add", dir, "x", "--profile", "p", "--command", "c", "--env", "MISSING_EQ",
	}, &buf)
	if err == nil || !strings.Contains(err.Error(), "K=V") {
		t.Errorf("expected K=V error, got: %v", err)
	}
}

func TestCmdProfileMCPRemove_Basic(t *testing.T) {
	dir := setupProfile(t)
	var buf bytes.Buffer
	_ = cmdMCPTo([]string{"add", dir, "x", "--profile", "p", "--command", "c"}, &buf)

	if err := cmdMCPTo([]string{"remove", dir, "x", "--profile", "p"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := loadTestProfiles(t, dir)
	if _, ok := got["p"].MCPServers["x"]; ok {
		t.Errorf("expected server removed")
	}
}

func TestCmdProfileMCPUpdate_Basic(t *testing.T) {
	dir := setupProfile(t)
	var buf bytes.Buffer
	_ = cmdMCPTo([]string{"add", dir, "x", "--profile", "p", "--command", "old"}, &buf)

	if err := cmdMCPTo([]string{"update", dir, "x", "--profile", "p", "--command", "new"}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := loadTestProfiles(t, dir)
	if got["p"].MCPServers["x"].Command != "new" {
		t.Errorf("expected command=new, got %+v", got["p"].MCPServers["x"])
	}
}

func TestCmdProfileMCPUpdate_NullEntry(t *testing.T) {
	dir := setupProfile(t)
	var buf bytes.Buffer
	_ = cmdMCPTo([]string{"add", dir, "x", "--null", "--profile", "p"}, &buf)

	err := cmdMCPTo([]string{"update", dir, "x", "--profile", "p", "--command", "c"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "null entry") {
		t.Errorf("expected null-entry error, got: %v", err)
	}
}

func TestCmdProfileMCPUpdate_NoFlags(t *testing.T) {
	dir := setupProfile(t)
	var buf bytes.Buffer
	_ = cmdMCPTo([]string{"add", dir, "x", "--profile", "p", "--command", "c"}, &buf)

	err := cmdMCPTo([]string{"update", dir, "x", "--profile", "p"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "at least one") {
		t.Errorf("expected at-least-one error, got: %v", err)
	}
}

// ---- profile-scoped include (now top-level with --profile) ------

func TestCmdProfileIncludeAdd_Basic(t *testing.T) {
	dir := setupProfile(t)
	var buf bytes.Buffer
	if err := cmdIncludeTo([]string{
		"add", dir, "github.com/acme/tools", "--profile", "p",
	}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := loadTestProfiles(t, dir)
	incs := got["p"].Includes
	if len(incs) != 1 || incs[0].Git != "github.com/acme/tools" {
		t.Errorf("expected one include, got %+v", incs)
	}
}

func TestCmdProfileIncludeRemove_Basic(t *testing.T) {
	dir := setupProfile(t)
	var buf bytes.Buffer
	_ = cmdIncludeTo([]string{"add", dir, "github.com/acme/tools", "--profile", "p"}, &buf)

	if err := cmdIncludeTo([]string{
		"remove", dir, "github.com/acme/tools", "--profile", "p",
	}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := loadTestProfiles(t, dir)
	if len(got["p"].Includes) != 0 {
		t.Errorf("expected no includes after remove")
	}
}

func TestCmdProfileIncludeUpdate_Ref(t *testing.T) {
	dir := setupProfile(t)
	var buf bytes.Buffer
	_ = cmdIncludeTo([]string{"add", dir, "github.com/acme/tools", "--profile", "p", "--ref", "v1"}, &buf)

	if err := cmdIncludeTo([]string{
		"update", dir, "github.com/acme/tools", "--profile", "p", "--ref", "v2",
	}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := loadTestProfiles(t, dir)
	if got["p"].Includes[0].Ref != "v2" {
		t.Errorf("expected ref=v2, got %+v", got["p"].Includes[0])
	}
}

func TestCmdProfileIncludeUpdate_NoFlags(t *testing.T) {
	dir := setupProfile(t)
	var buf bytes.Buffer
	_ = cmdIncludeTo([]string{"add", dir, "github.com/acme/tools", "--profile", "p"}, &buf)

	err := cmdIncludeTo([]string{"update", dir, "github.com/acme/tools", "--profile", "p"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "at least one") {
		t.Errorf("expected at-least-one error, got: %v", err)
	}
}
