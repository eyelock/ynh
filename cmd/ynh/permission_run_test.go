package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/delegation"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/permission"
)

func noEnv(string) string { return "" }

func envOf(kv map[string]string) func(string) string { return func(k string) string { return kv[k] } }

// A golden per vendor and level: what a one-shot run of a harness that
// declares that level is passed. Provisional cells follow ynh #1059's final
// results; the table is permission.For.
func TestResolveRunPermission_GoldenPerVendor(t *testing.T) {
	for _, v := range []string{"claude", "codex", "cursor", "opencode", "copilot"} {
		for _, l := range []permission.Level{permission.None, permission.Edits, permission.All} {
			t.Run(v+"/"+string(l), func(t *testing.T) {
				p := &harness.Harness{Name: "h", AutoApprove: string(l)}
				rp, err := resolveRunPermission(p, v, "", t.TempDir(), false, noEnv, 1000)
				want, werr := permission.For(v, l)
				if werr != nil {
					if err == nil || !strings.Contains(err.Error(), "cannot hold") {
						t.Fatalf("want a refusal, got %v", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if !slices.Equal(rp.Args, want.Args) || !slices.Equal(rp.Env, want.Env) || rp.Level != l {
					t.Errorf("got %+v, want %+v", rp, want)
				}
			})
		}
	}
}

func TestResolveRunPermission_UndeclaredOpensAtAll(t *testing.T) {
	rp, err := resolveRunPermission(&harness.Harness{Name: "h"}, "claude", "", t.TempDir(), false, noEnv, 1000)
	if err != nil || rp.Level != permission.All || rp.From != permission.SourceDefault {
		t.Fatalf("%+v %v", rp, err)
	}
	if !slices.Equal(rp.Args, []string{"--permission-mode", "bypassPermissions"}) {
		t.Errorf("args = %q", rp.Args)
	}
}

func TestResolveRunPermission_Narrowing(t *testing.T) {
	p := &harness.Harness{Name: "h", AutoApprove: "edits"}
	// The flag narrows, with its own source.
	rp, err := resolveRunPermission(p, "claude", "none", t.TempDir(), false, noEnv, 1000)
	if err != nil || rp.Level != permission.None || rp.From != permission.SourceFlag {
		t.Fatalf("%+v %v", rp, err)
	}
	// A wider flag is refused.
	if _, err := resolveRunPermission(p, "claude", "all", t.TempDir(), false, noEnv, 1000); err == nil || !strings.Contains(err.Error(), "wider than the ceiling") {
		t.Errorf("wider flag must be refused: %v", err)
	}
	// An unknown word is refused.
	if _, err := resolveRunPermission(p, "claude", "most", t.TempDir(), false, noEnv, 1000); err == nil {
		t.Error("unknown level must be refused")
	}
	// The caller's cap narrows (D6), and says so.
	rp, err = resolveRunPermission(&harness.Harness{Name: "h"}, "codex", "", t.TempDir(), true, envOf(map[string]string{permission.CapEnv: "none"}), 1000)
	if err != nil || rp.Level != permission.None || rp.From != permission.SourceCallerCap {
		t.Fatalf("%+v %v", rp, err)
	}
	// A delegate's own declaration is delegate_default; a request source comes from the entry or call.
	rp, _ = resolveRunPermission(p, "claude", "", t.TempDir(), true, noEnv, 1000)
	if rp.From != permission.SourceDelegateDefault {
		t.Errorf("from = %s", rp.From)
	}
	rp, _ = resolveRunPermission(p, "claude", "none", t.TempDir(), true, envOf(map[string]string{permission.SourceEnv: "entry"}), 1000)
	if rp.From != "entry" {
		t.Errorf("from = %s", rp.From)
	}
}

func TestResolveRunPermission_ProjectModeAndRoot(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".claude", "settings.json"), []byte(`{"permissions":{"defaultMode":"plan"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	rp, err := resolveRunPermission(&harness.Harness{Name: "h"}, "claude", "", dir, false, noEnv, 1000)
	if err != nil || rp.Level != permission.None || rp.From != permission.SourceProject {
		t.Fatalf("%+v %v", rp, err)
	}
	// The project's mode is passed explicitly, so a user-level setting cannot
	// widen the run, and it is what Claude's opened-mode check expects.
	if !slices.Equal(rp.Args, []string{"--permission-mode", "plan"}) || rp.OpenedMode != "plan" {
		t.Errorf("args %q opened %q", rp.Args, rp.OpenedMode)
	}
	if !strings.Contains(strings.Join(rp.Lines, "\n"), "settings.json") {
		t.Errorf("no project line: %q", rp.Lines)
	}
	if _, err := resolveRunPermission(&harness.Harness{Name: "h"}, "claude", "", t.TempDir(), false, noEnv, 0); err == nil || !strings.Contains(err.Error(), "as root") {
		t.Errorf("root at all must be refused: %v", err)
	}
	if _, err := resolveRunPermission(&harness.Harness{Name: "h"}, "claude", "edits", t.TempDir(), false, noEnv, 0); err != nil {
		t.Errorf("root at edits is allowed: %v", err)
	}
}

// OpenCode's permission block is merged into the caller's own
// OPENCODE_CONFIG_CONTENT, nested keys included: a narrower rule of the
// caller's own stays.
func TestApplyEnv_KeepsTheCallersNestedPermissionKeys(t *testing.T) {
	t.Setenv("OPENCODE_CONFIG_CONTENT", `{"theme":"dark","permission":{"webfetch":"deny","bash":{"rm *":"deny"}}}`)
	t.Setenv(permission.CapEnv, "")
	rp, err := resolveRunPermission(&harness.Harness{Name: "h", AutoApprove: "edits"}, "opencode", "", t.TempDir(), false, os.Getenv, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if err := rp.applyEnv(); err != nil {
		t.Fatal(err)
	}
	got := os.Getenv("OPENCODE_CONFIG_CONTENT")
	for _, want := range []string{`"theme":"dark"`, `"webfetch":"deny"`, `"edit":"allow"`} {
		if !strings.Contains(got, want) {
			t.Errorf("%s lost from %s", want, got)
		}
	}
	if os.Getenv(permission.CapEnv) != "edits" {
		t.Errorf("cap = %q", os.Getenv(permission.CapEnv))
	}
}

// An interactive run with --auto-approve passes the vendor nothing and caps
// the delegates it starts (S4).
func TestRun_InteractiveFlagCapsDelegates(t *testing.T) {
	isolateRun(t)
	t.Chdir(t.TempDir())
	t.Setenv(permission.CapEnv, "")
	dir := filepath.Join(t.TempDir(), "cop")
	writeSkillHarness(t, dir, "cop")
	var got []string
	var capSeen string
	stubLaunchArgs(t, func(a []string) error { got = a; capSeen = os.Getenv(permission.CapEnv); return nil })
	if err := cmdRun([]string{dir, "-v", "claude", "--auto-approve", "edits"}); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(got, "--permission-mode") {
		t.Errorf("an interactive run is unchanged: %q", got)
	}
	if capSeen != "edits" {
		t.Errorf("cap = %q, want edits", capSeen)
	}
}

// ynh run reads --auto-approve itself and never passes it on to the vendor.
func TestParseRunArgs_AutoApproveIsNotForwarded(t *testing.T) {
	for _, args := range [][]string{
		{"h", "--auto-approve", "edits", "--", "task"},
		{"h", "--auto-approve=edits", "--", "task"},
		{"h", "--auto-approve", "edits", "task"},
	} {
		ra := parseRunArgs(args)
		if ra.AutoApprove != "edits" || ra.HarnessName != "h" || ra.Prompt != "task" || len(ra.VendorArgs) != 0 {
			t.Errorf("%q: %+v", args, ra)
		}
	}
}

func TestDelegateRunArgs_CarryTheLevel(t *testing.T) {
	rec := delegation.Record{Delegate: "local/rev", Task: "go", AutoApprove: "none", AutoApproveFrom: "call"}
	got := delegateRunArgs(&rec)
	want := []string{"run", "local/rev", "--auto-approve", "none", "--", "go"}
	if !slices.Equal(got, want) {
		t.Errorf("args = %q, want %q", got, want)
	}
	if ra := parseRunArgs(got[1:]); ra.AutoApprove != "none" || len(ra.VendorArgs) != 0 {
		t.Errorf("ynh run reads %+v", ra)
	}
	cmd := delegateLauncher(&rec)
	if !slices.Contains(cmd.Env, permission.SourceEnv+"=call") {
		t.Errorf("the launcher does not carry the level's source")
	}
}

// The ladder of a delegate's level (D2, D6): its own, then the entry, then the
// calling session's cap; a wider entry is refused.
func TestDelegateStep(t *testing.T) {
	declared := &harness.Harness{Name: "d", AutoApprove: "edits"}
	undeclared := &harness.Harness{Name: "d"}
	step, err := delegateStep(undeclared, harness.Delegate{}, noEnv)
	if err != nil || step.Level != permission.All || step.From != permission.SourceDefault {
		t.Errorf("%+v %v", step, err)
	}
	step, _ = delegateStep(declared, harness.Delegate{}, noEnv)
	if step.Level != permission.Edits || step.From != permission.SourceDelegateDefault {
		t.Errorf("%+v", step)
	}
	step, _ = delegateStep(declared, harness.Delegate{AutoApprove: "none"}, noEnv)
	if step.Level != permission.None || step.From != permission.SourceEntry {
		t.Errorf("%+v", step)
	}
	step, _ = delegateStep(undeclared, harness.Delegate{AutoApprove: "edits"}, envOf(map[string]string{permission.CapEnv: "none"}))
	if step.Level != permission.None || step.From != permission.SourceCallerCap {
		t.Errorf("%+v", step)
	}
	if _, err := delegateStep(declared, harness.Delegate{AutoApprove: "all"}, noEnv); err == nil || !strings.Contains(err.Error(), "wider than the ceiling") {
		t.Errorf("a wider entry must be refused: %v", err)
	}
}

// Codex: a project that narrows the run is passed --sandbox explicitly.
func TestResolveRunPermission_CodexProjectModeIsExplicit(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".codex", "config.toml"), []byte("approval_policy = \"on-request\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rp, err := resolveRunPermission(&harness.Harness{Name: "h"}, "codex", "", dir, false, noEnv, 1000)
	if err != nil || !slices.Contains(rp.Args, "--sandbox") || slices.Contains(rp.Args, "--dangerously-bypass-approvals-and-sandbox") {
		t.Fatalf("%+v %v", rp, err)
	}
}

// The opened-mode check (ynw-c5hme): a one-shot Claude Code session carries a
// SessionStart hook, on ynh run and on a delegate session alike.
func TestOpenedModeSettings(t *testing.T) {
	if got, err := openedModeSettings("codex", "", []string{"x"}); err != nil || !slices.Equal(got, []string{"x"}) {
		t.Errorf("only Claude reports a mode: %q %v", got, err)
	}
	got, err := openedModeSettings("claude", "acceptEdits", []string{"--model", "m"})
	if err != nil || len(got) != 4 || got[0] != "--settings" || !strings.Contains(got[1], "permission-check --expect") || !strings.Contains(got[1], "acceptEdits") || !strings.Contains(got[1], "UserPromptSubmit") || !strings.Contains(got[1], "PreToolUse") || strings.Contains(got[1], "SessionStart") || got[2] != "--model" {
		t.Fatalf("%q %v", got, err)
	}
}

// A run's own --settings is merged into, never skipped: its keys and its own
// SessionStart hooks stay, as a JSON value, a --settings= value and a file.
func TestOpenedModeSettings_MergesTheCallersSettings(t *testing.T) {
	own := `{"model":"opus","hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"echo own"}]}],"UserPromptSubmit":[{"hooks":[{"type":"command","command":"echo prompt"}]}],"Stop":[]}}`
	file := filepath.Join(t.TempDir(), "s.json")
	if err := os.WriteFile(file, []byte(own), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string][]string{
		"json value": {"--settings", own, "--model", "m"},
		"equals":     {"--settings=" + own},
		"file":       {"--settings", file},
	} {
		got, err := openedModeSettings("claude", "plan", args)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		joined := strings.Join(got, " ")
		for _, want := range []string{`"model":"opus"`, "echo own", "permission-check --expect", `"Stop"`, "echo prompt", `"UserPromptSubmit"`, `"PreToolUse"`} {
			if !strings.Contains(joined, want) {
				t.Errorf("%s: %q lost from %s", name, want, joined)
			}
		}
		if strings.Count(joined, "--settings") != 1 {
			t.Errorf("%s: want one --settings in %s", name, joined)
		}
	}
	// What cannot be merged exactly is refused, never run unchecked.
	for name, args := range map[string][]string{
		"two":              {"--settings", "{}", "--settings", "{}"},
		"no value":         {"--settings"},
		"bad json":         {"--settings", "{nope"},
		"missing":          {"--settings", filepath.Join(t.TempDir(), "absent.json")},
		"hooks array":      {"--settings", `{"hooks":[]}`},
		"event not a list": {"--settings", `{"hooks":{"PreToolUse":{}}}`},
	} {
		if got, err := openedModeSettings("claude", "plan", args); err == nil || !strings.Contains(err.Error(), "refusing to run") {
			t.Errorf("%s: want a refusal, got %q %v", name, got, err)
		}
	}
}

func TestRun_OneShotClaudeCarriesTheOpenedModeCheck(t *testing.T) {
	for _, delegate := range []bool{false, true} {
		isolateRun(t)
		t.Chdir(t.TempDir())
		t.Setenv(permission.CapEnv, "")
		if delegate {
			t.Setenv(delegationHandleEnv, "dlg-test")
		}
		dir := filepath.Join(t.TempDir(), "h")
		writeSkillHarness(t, dir, "h")
		var got []string
		stubLaunchArgs(t, func(a []string) error { got = a; return nil })
		if err := cmdRun([]string{dir, "-v", "claude", "--trust", "--auto-approve", "edits", "--", "hi"}); err != nil {
			t.Fatalf("delegate=%v: %v", delegate, err)
		}
		joined := strings.Join(got, " ")
		if !strings.Contains(joined, "--permission-mode acceptEdits --disallowedTools Bash") || !strings.Contains(joined, "permission-check --expect") {
			t.Errorf("delegate=%v: args = %q", delegate, got)
		}
	}
}

func TestCmdPermissionCheck(t *testing.T) {
	run := func(expect, event, input string) (stdout, stderr string, err error) {
		var out, errb strings.Builder
		err = cmdPermissionCheck([]string{"--expect", expect, "--event", event}, strings.NewReader(input), &out, &errb)
		return out.String(), errb.String(), err
	}
	for _, event := range []string{"user_prompt", "pre_tool"} {
		if out, msg, err := run("acceptEdits", event, `{"permission_mode":"acceptEdits"}`); out != "" || msg != "" || err != nil {
			t.Errorf("%s: a matching mode must not stop the session: %q %q %v", event, out, msg, err)
		}
		if out, msg, err := run("acceptEdits", event, `{"nothing":1}`); out != "" || msg != "" || err != nil {
			t.Errorf("%s: a hook input without a mode passes: %q %q %v", event, out, msg, err)
		}
	}
	// On the prompt, a mismatch exits 2 with the message on stderr: that blocks
	// the prompt before any tool runs.
	out, msg, err := run("acceptEdits", "user_prompt", `{"permission_mode":"bypassPermissions"}`)
	var coded *exitCodeError
	if !errors.As(err, &coded) || coded.code != 2 || !strings.Contains(msg, `started in "bypassPermissions"`) || out != "" {
		t.Errorf("prompt mismatch: out %q msg %q err %v", out, msg, err)
	}
	// On a tool use, the backstop denies the tool.
	out, _, err = run("acceptEdits", "pre_tool", `{"permission_mode":"bypassPermissions"}`)
	if err != nil || !strings.Contains(out, `"permissionDecision":"deny"`) || !strings.Contains(out, `"hookEventName":"PreToolUse"`) || !strings.Contains(out, "bypassPermissions") {
		t.Errorf("pre_tool mismatch: %q %v", out, err)
	}
	if err := cmdPermissionCheck([]string{"--expect", "plan"}, strings.NewReader("{}"), io.Discard, io.Discard); err == nil {
		t.Error("an event is required")
	}
}

// The delegate's own run writes the level it opened at back to its record, so
// delegate_status reports it after the project's narrowing (ynw-id091).
func TestRun_DelegateWritesTheOpenedLevelBackToItsRecord(t *testing.T) {
	isolateRun(t)
	work := t.TempDir()
	t.Chdir(work)
	t.Setenv(permission.CapEnv, "")
	t.Setenv(delegationHandleEnv, "dlg-test")
	if err := os.MkdirAll(filepath.Join(work, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, ".claude", "settings.json"), []byte(`{"permissions":{"defaultMode":"plan"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	recPath := filepath.Join(t.TempDir(), "rec.json")
	rec := delegation.Record{Handle: "dlg-test", Parent: "local/p", Delegate: "local/h", Folder: work, Status: delegation.StatusRunning, AutoApprove: "all", AutoApproveFrom: "default"}
	data, _ := json.Marshal(rec)
	if err := os.WriteFile(recPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(delegationRecordEnv, recPath)
	dir := filepath.Join(t.TempDir(), "h")
	writeSkillHarness(t, dir, "h")
	stubLaunchArgs(t, func([]string) error { return nil })
	if err := cmdRun([]string{dir, "-v", "claude", "--trust", "--", "hi"}); err != nil {
		t.Fatal(err)
	}
	got, err := delegation.Load(recPath)
	if err != nil {
		t.Fatal(err)
	}
	if got.AutoApprove != "none" || got.AutoApproveFrom != "project" {
		t.Errorf("record shows %s (%s), want none (project)", got.AutoApprove, got.AutoApproveFrom)
	}
}

// A session the opened-mode check stops ends the vendor with exit 0; ynh run
// then exits nonzero and says why (ynw-bty92).
func TestStoppedByOpenedModeCheck(t *testing.T) {
	if err := stoppedByOpenedModeCheck("", nil); err != nil {
		t.Errorf("no check: %v", err)
	}
	stop := filepath.Join(t.TempDir(), "stop")
	if err := stoppedByOpenedModeCheck(stop, nil); err != nil {
		t.Errorf("a run the check did not stop: %v", err)
	}
	t.Setenv(permission.StopEnv, stop)
	for _, event := range []string{"user_prompt", "pre_tool"} {
		_ = os.Remove(stop)
		_ = cmdPermissionCheck([]string{"--expect", "plan", "--event", event}, strings.NewReader(`{"permission_mode":"bypassPermissions"}`), io.Discard, io.Discard)
		err := stoppedByOpenedModeCheck(stop, nil)
		if err == nil || !strings.Contains(err.Error(), "stopped by the opened-mode check") || !strings.Contains(err.Error(), `started in "bypassPermissions"`) {
			t.Errorf("%s: err = %v", event, err)
		}
	}
	// A launch that already failed keeps its own error.
	boom := errors.New("vendor failed")
	if err := stoppedByOpenedModeCheck(stop, boom); !errors.Is(err, boom) {
		t.Errorf("err = %v", err)
	}
}

// End to end: a run whose vendor exits 0 after the hook stopped it is an error.
func TestRun_OneShotStoppedByTheCheckExitsNonzero(t *testing.T) {
	isolateRun(t)
	t.Chdir(t.TempDir())
	t.Setenv(permission.CapEnv, "")
	dir := filepath.Join(t.TempDir(), "h")
	writeSkillHarness(t, dir, "h")
	stubLaunchArgs(t, func([]string) error {
		// What the hook does when Claude opens in another mode, then Claude exits 0.
		_ = cmdPermissionCheck([]string{"--expect", "acceptEdits", "--event", "user_prompt"}, strings.NewReader(`{"permission_mode":"bypassPermissions"}`), io.Discard, io.Discard)
		return nil
	})
	err := cmdRun([]string{dir, "-v", "claude", "--trust", "--auto-approve", "edits", "--", "hi"})
	if err == nil || !strings.Contains(err.Error(), "stopped by the opened-mode check") {
		t.Fatalf("err = %v, want a nonzero exit saying why", err)
	}
}
