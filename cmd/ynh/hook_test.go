package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
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

func writeExportHarness(t *testing.T, dir string) {
	t.Helper()
	hj := &plugin.HarnessJSON{
		Name:    "h",
		Version: "0.1.0",
		Hooks: map[string][]plugin.HookEntry{
			"after_tool": {{Matcher: "Edit|Write", Command: "./tools/x.sh"}},
			"on_stop":    {{Command: "make check"}},
		},
	}
	if err := plugin.SavePluginJSON(dir, hj); err != nil {
		t.Fatal(err)
	}
}

func readSettingsHooks(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var s map[string]any
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	hooks, _ := s["hooks"].(map[string]any)
	return hooks
}

func TestCmdHookExport_CreatesSettingsAnchored(t *testing.T) {
	proj := t.TempDir()
	t.Chdir(proj)
	hdir := filepath.Join(proj, "h")
	writeExportHarness(t, hdir)

	var buf bytes.Buffer
	if err := cmdHookTo([]string{"export", hdir, "--target", "settings"}, &buf); err != nil {
		t.Fatalf("export: %v", err)
	}
	hooks := readSettingsHooks(t, filepath.Join(".claude", "settings.json"))

	post, ok := hooks["PostToolUse"].([]any)
	if !ok || len(post) != 1 {
		t.Fatalf("PostToolUse missing/wrong: %v", hooks["PostToolUse"])
	}
	inner := post[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)
	if inner["command"] != "$CLAUDE_PROJECT_DIR/tools/x.sh" {
		t.Errorf("command not anchored: %v", inner["command"])
	}
	if _, ok := hooks["Stop"]; !ok {
		t.Error("Stop event missing from export")
	}
}

func TestCmdHookExport_LocalTarget(t *testing.T) {
	proj := t.TempDir()
	t.Chdir(proj)
	hdir := filepath.Join(proj, "h")
	writeExportHarness(t, hdir)

	var buf bytes.Buffer
	if err := cmdHookTo([]string{"export", hdir, "--target", "local"}, &buf); err != nil {
		t.Fatalf("export: %v", err)
	}
	if _, err := os.Stat(filepath.Join(".claude", "settings.local.json")); err != nil {
		t.Errorf("expected settings.local.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(".claude", "settings.json")); !os.IsNotExist(err) {
		t.Error("settings.json should not be written for --target local")
	}
}

func TestCmdHookExport_PreservesAndIdempotent(t *testing.T) {
	proj := t.TempDir()
	t.Chdir(proj)
	hdir := filepath.Join(proj, "h")
	writeExportHarness(t, hdir)

	// Pre-existing settings with a non-hook key and a user hook.
	if err := os.MkdirAll(".claude", 0o755); err != nil {
		t.Fatal(err)
	}
	seed := `{"model":"opus","hooks":{"PostToolUse":[{"matcher":"Edit|Write","hooks":[{"type":"command","command":"user.sh"}]}]}}`
	if err := os.WriteFile(filepath.Join(".claude", "settings.json"), []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := cmdHookTo([]string{"export", hdir, "--target", "settings"}, &buf); err != nil {
		t.Fatalf("export: %v", err)
	}
	// Non-hook key preserved.
	data, _ := os.ReadFile(filepath.Join(".claude", "settings.json"))
	var s map[string]any
	_ = json.Unmarshal(data, &s)
	if s["model"] != "opus" {
		t.Error("non-hook key 'model' not preserved")
	}
	// User hook + ynh hook both present under PostToolUse.
	post := s["hooks"].(map[string]any)["PostToolUse"].([]any)
	if len(post) != 2 {
		t.Errorf("PostToolUse groups = %d, want 2 (user + ynh)", len(post))
	}

	// Re-run is idempotent: no new groups added.
	var buf2 bytes.Buffer
	if err := cmdHookTo([]string{"export", hdir, "--target", "settings"}, &buf2); err != nil {
		t.Fatalf("re-export: %v", err)
	}
	if !strings.Contains(buf2.String(), "0 new") {
		t.Errorf("re-run should add 0 groups, got: %s", buf2.String())
	}
}

func TestCmdHookExport_DryRunWritesNothing(t *testing.T) {
	proj := t.TempDir()
	t.Chdir(proj)
	hdir := filepath.Join(proj, "h")
	writeExportHarness(t, hdir)

	var buf bytes.Buffer
	if err := cmdHookTo([]string{"export", hdir, "--target", "settings", "--dry-run"}, &buf); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(".claude", "settings.json")); !os.IsNotExist(err) {
		t.Error("--dry-run must not write a file")
	}
	if !strings.Contains(buf.String(), "dry run") {
		t.Errorf("expected dry-run notice, got: %s", buf.String())
	}
}

func TestCmdHookExport_Errors(t *testing.T) {
	proj := t.TempDir()
	t.Chdir(proj)
	hdir := filepath.Join(proj, "h")
	writeExportHarness(t, hdir)

	cases := []struct {
		name string
		args []string
		want string
	}{
		{"missing target", []string{"export", hdir}, "--target"},
		{"bad target", []string{"export", hdir, "--target", "bogus"}, "must be 'settings'"},
		{"non-claude vendor", []string{"export", hdir, "-v", "cursor", "--target", "settings"}, "Claude-specific"},
		{"no positional", []string{"export", "--target", "settings"}, "usage"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := cmdHookTo(tc.args, &buf)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("args %v: got err %v, want contains %q", tc.args, err, tc.want)
			}
		})
	}
}

func TestCmdHookExport_NoHooks(t *testing.T) {
	proj := t.TempDir()
	t.Chdir(proj)
	hdir := filepath.Join(proj, "h")
	writeHookTestHarness(t, hdir, "h") // no hooks declared

	var buf bytes.Buffer
	err := cmdHookTo([]string{"export", hdir, "--target", "settings"}, &buf)
	if err == nil || !strings.Contains(err.Error(), "no hooks") {
		t.Errorf("expected no-hooks error, got: %v", err)
	}
}

func TestMergeHookEntries_UnionDedupPreserve(t *testing.T) {
	mustJSON := func(s string) map[string]any {
		var m map[string]any
		if err := json.Unmarshal([]byte(s), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	settings := mustJSON(`{
		"model": "opus",
		"hooks": {"PreToolUse": [{"matcher":"Bash","hooks":[{"type":"command","command":"user.sh"}]}]}
	}`)
	generated := mustJSON(`{
		"PreToolUse": [{"matcher":"Bash","hooks":[{"type":"command","command":"ynh.sh"}]}],
		"Stop": [{"hooks":[{"type":"command","command":"sweep.sh"}]}]
	}`)

	added := mergeHookEntries(settings, generated)
	if added != 2 {
		t.Errorf("added = %d, want 2", added)
	}
	if settings["model"] != "opus" {
		t.Error("non-hook key not preserved")
	}
	pre := settings["hooks"].(map[string]any)["PreToolUse"].([]any)
	if len(pre) != 2 {
		t.Errorf("PreToolUse groups = %d, want 2", len(pre))
	}
	// Idempotent: same generated input adds nothing.
	if added2 := mergeHookEntries(settings, generated); added2 != 0 {
		t.Errorf("re-merge added = %d, want 0", added2)
	}
}
