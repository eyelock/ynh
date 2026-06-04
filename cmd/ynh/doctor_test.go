package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSettings(t *testing.T, name, body string) {
	t.Helper()
	if err := os.MkdirAll(".claude", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(".claude", name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCmdDoctor_NoSettingsNudges(t *testing.T) {
	t.Chdir(t.TempDir())
	var buf bytes.Buffer
	if err := cmdDoctorTo(nil, &buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "no .claude/settings.json") || !strings.Contains(out, "ynh hook export") {
		t.Errorf("expected nudge for missing settings, got: %s", out)
	}
}

func TestCmdDoctor_CleanSettings(t *testing.T) {
	t.Chdir(t.TempDir())
	writeSettings(t, "settings.json", `{
		"hooks": {"PostToolUse": [{"matcher":"Edit","hooks":[{"type":"command","command":"$CLAUDE_PROJECT_DIR/x.sh"}]}]}
	}`)
	var buf bytes.Buffer
	if err := cmdDoctorTo(nil, &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "ok") {
		t.Errorf("expected clean result, got: %s", buf.String())
	}
}

func TestCmdDoctor_CanonicalNameLeak(t *testing.T) {
	t.Chdir(t.TempDir())
	writeSettings(t, "settings.json", `{
		"hooks": {"after_tool": [{"matcher":"Edit","hooks":[{"type":"command","command":"$CLAUDE_PROJECT_DIR/x.sh"}]}]}
	}`)
	var buf bytes.Buffer
	if err := cmdDoctorTo(nil, &buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "canonical name") || !strings.Contains(out, "PostToolUse") {
		t.Errorf("expected canonical-name warning naming PostToolUse, got: %s", out)
	}
}

func TestCmdDoctor_RelativeCommand(t *testing.T) {
	t.Chdir(t.TempDir())
	writeSettings(t, "settings.json", `{
		"hooks": {"PreToolUse": [{"matcher":"Bash","hooks":[{"type":"command","command":"./tools/guard.sh"}]}]}
	}`)
	var buf bytes.Buffer
	if err := cmdDoctorTo(nil, &buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "cwd-relative") || !strings.Contains(out, "$CLAUDE_PROJECT_DIR") {
		t.Errorf("expected cwd-relative warning, got: %s", out)
	}
}

func TestCmdDoctor_InspectsLocalFile(t *testing.T) {
	t.Chdir(t.TempDir())
	writeSettings(t, "settings.local.json", `{
		"hooks": {"on_stop": [{"hooks":[{"type":"command","command":"sweep.sh"}]}]}
	}`)
	var buf bytes.Buffer
	if err := cmdDoctorTo(nil, &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "settings.local.json") {
		t.Errorf("expected the local settings file to be inspected, got: %s", buf.String())
	}
}

func TestCmdDoctor_InvalidJSON(t *testing.T) {
	t.Chdir(t.TempDir())
	writeSettings(t, "settings.json", `{ not json`)
	var buf bytes.Buffer
	if err := cmdDoctorTo(nil, &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "not valid JSON") {
		t.Errorf("expected invalid-JSON warning, got: %s", buf.String())
	}
}

func TestCmdDoctor_RejectsArgs(t *testing.T) {
	if err := cmdDoctor([]string{"extra"}); err == nil {
		t.Error("expected error for unexpected argument")
	}
}

func TestExtractHookCommands_BothShapes(t *testing.T) {
	nested := []any{map[string]any{"hooks": []any{map[string]any{"command": "a.sh"}, map[string]any{"command": "b.sh"}}}}
	if got := extractHookCommands(nested); len(got) != 2 || got[0] != "a.sh" {
		t.Errorf("nested shape: got %v", got)
	}
	flat := []any{map[string]any{"command": "c.sh"}}
	if got := extractHookCommands(flat); len(got) != 1 || got[0] != "c.sh" {
		t.Errorf("flat shape: got %v", got)
	}
	if got := extractHookCommands("not-an-array"); got != nil {
		t.Errorf("non-array should yield nil, got %v", got)
	}
}
