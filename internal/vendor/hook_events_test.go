package vendor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

// allCanonicalHooks declares one hook for each of the six canonical events.
func allCanonicalHooks() map[string][]plugin.HookEntry {
	return map[string][]plugin.HookEntry{
		"before_tool":      {{Command: "cmd-before-tool"}},
		"after_tool":       {{Command: "cmd-after-tool"}},
		"before_prompt":    {{Command: "cmd-before-prompt"}},
		"on_stop":          {{Command: "cmd-on-stop"}},
		"on_session_start": {{Command: "cmd-session-start"}},
		"on_session_end":   {{Command: "cmd-session-end"}},
	}
}

// hookFile generates a vendor's session hook file and returns its bytes.
func hookFile(t *testing.T, name, wantPath string) []byte {
	t.Helper()
	a, err := Get(name)
	if err != nil {
		t.Fatal(err)
	}
	files, err := a.GenerateHookConfig(allCanonicalHooks())
	if err != nil {
		t.Fatal(err)
	}
	data, ok := files[wantPath]
	if !ok {
		t.Fatalf("%s: no %s in %v", name, wantPath, keysOf(files))
	}
	return data
}

// TestHookEventsAllSixPerVendor pins each vendor's native name for every
// canonical event, as the documented JSON shape of each vendor's hook file.
// Sources: code.claude.com/docs/en/hooks, learn.chatgpt.com/docs/hooks,
// cursor.com/docs/hooks, docs.github.com/en/copilot/reference/hooks-configuration.
func TestHookEventsAllSixPerVendor(t *testing.T) {
	canonical := []string{"before_tool", "after_tool", "before_prompt", "on_stop", "on_session_start", "on_session_end"}
	cases := []struct {
		vendor string
		path   string
		native map[string]string
		nested bool // Claude and Codex nest matcher groups; Cursor and Copilot are flat
	}{
		{"claude", filepath.Join(".claude", "hooks", "hooks.json"), map[string]string{
			"before_tool": "PreToolUse", "after_tool": "PostToolUse", "before_prompt": "UserPromptSubmit",
			"on_stop": "Stop", "on_session_start": "SessionStart", "on_session_end": "SessionEnd"}, true},
		{"codex", filepath.Join(".codex", "hooks.json"), map[string]string{
			"before_tool": "PreToolUse", "after_tool": "PostToolUse", "before_prompt": "UserPromptSubmit",
			"on_stop": "Stop", "on_session_start": "SessionStart", "on_session_end": "SessionEnd"}, true},
		{"cursor", filepath.Join(".cursor", "hooks.json"), map[string]string{
			"before_tool": "preToolUse", "after_tool": "postToolUse", "before_prompt": "beforeSubmitPrompt",
			"on_stop": "stop", "on_session_start": "sessionStart", "on_session_end": "sessionEnd"}, false},
		{"copilot", filepath.Join(".copilot", "hooks", "hooks.json"), map[string]string{
			"before_tool": "PreToolUse", "after_tool": "PostToolUse", "before_prompt": "UserPromptSubmit",
			"on_stop": "Stop", "on_session_start": "SessionStart", "on_session_end": "SessionEnd"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.vendor, func(t *testing.T) {
			var doc struct {
				Version *int                         `json:"version"`
				Hooks   map[string][]json.RawMessage `json:"hooks"`
			}
			if err := json.Unmarshal(hookFile(t, tc.vendor, tc.path), &doc); err != nil {
				t.Fatal(err)
			}
			if len(doc.Hooks) != len(canonical) {
				t.Errorf("events = %d, want %d: %v", len(doc.Hooks), len(canonical), doc.Hooks)
			}
			wantVersion := tc.vendor == "cursor" || tc.vendor == "copilot"
			if wantVersion != (doc.Version != nil && *doc.Version == 1) {
				t.Errorf("version = %v, want version 1 present: %v", doc.Version, wantVersion)
			}
			for _, ev := range canonical {
				native := tc.native[ev]
				entries, ok := doc.Hooks[native]
				if !ok || len(entries) != 1 {
					t.Errorf("%s -> %s: entries = %d, want 1", ev, native, len(entries))
					continue
				}
				var cmd string
				if tc.nested {
					var g struct {
						Hooks []struct {
							Type    string `json:"type"`
							Command string `json:"command"`
						} `json:"hooks"`
					}
					if err := json.Unmarshal(entries[0], &g); err != nil || len(g.Hooks) != 1 || g.Hooks[0].Type != "command" {
						t.Errorf("%s: bad nested group %s", native, entries[0])
						continue
					}
					cmd = g.Hooks[0].Command
				} else {
					var e struct {
						Type    string `json:"type"`
						Command string `json:"command"`
					}
					if err := json.Unmarshal(entries[0], &e); err != nil {
						t.Fatal(err)
					}
					if tc.vendor == "copilot" && e.Type != "command" {
						t.Errorf("%s: type = %q, want command", native, e.Type)
					}
					cmd = e.Command
				}
				if want := allCanonicalHooks()[ev][0].Command; cmd != want {
					t.Errorf("%s: command = %q, want %q", native, cmd, want)
				}
			}
		})
	}
}

// TestCopilotGenerateHookConfig: Copilot now writes hooks, in its documented
// format, to the --plugin-dir plugin's hooks/hooks.json for a session and to
// hooks.json at the plugin root for an export.
func TestCopilotGenerateHookConfig(t *testing.T) {
	c := &Copilot{}
	hooks := map[string][]plugin.HookEntry{
		"before_tool":    {{Matcher: "Bash", Command: "./scripts/guard.sh --strict"}},
		"on_stop":        {{Matcher: "ignored", Command: "/usr/local/bin/done.sh"}},
		"on_session_end": {{Command: "./scripts/bye.sh"}},
	}

	session, err := c.GenerateHookConfig(hooks)
	if err != nil {
		t.Fatal(err)
	}
	plug, err := c.GeneratePluginHookConfig(hooks)
	if err != nil {
		t.Fatal(err)
	}
	if got := keysOf(session); !slices.Equal(got, []string{filepath.Join(".copilot", "hooks", "hooks.json")}) {
		t.Errorf("session files = %v", got)
	}
	if got := keysOf(plug); !slices.Equal(got, []string{"hooks.json"}) {
		t.Errorf("plugin files = %v", got)
	}

	want := `{
  "hooks": {
    "PreToolUse": [
      {
        "type": "command",
        "command": "\"${PLUGIN_ROOT}\"/scripts/guard.sh --strict",
        "matcher": "Bash"
      }
    ],
    "SessionEnd": [
      {
        "type": "command",
        "command": "\"${PLUGIN_ROOT}\"/scripts/bye.sh"
      }
    ],
    "Stop": [
      {
        "type": "command",
        "command": "/usr/local/bin/done.sh"
      }
    ]
  },
  "version": 1
}
`
	if got := string(session[filepath.Join(".copilot", "hooks", "hooks.json")]); got != want {
		t.Errorf("session hooks.json:\n%s\nwant:\n%s", got, want)
	}
	if string(plug["hooks.json"]) != want {
		t.Errorf("plugin hooks.json differs from the session file")
	}
	if got := c.SessionHookScriptDir(); got != ".copilot" {
		t.Errorf("SessionHookScriptDir = %q, want .copilot", got)
	}
}

// TestCursorToolEventsMapToPreAndPostToolUse: before_tool and after_tool mean
// any tool on Cursor, as they do on Claude and Codex, not shell-only and
// file-edit-only.
func TestCursorToolEventsMapToPreAndPostToolUse(t *testing.T) {
	var doc struct {
		Hooks map[string][]struct {
			Command string `json:"command"`
			Matcher string `json:"matcher"`
		} `json:"hooks"`
	}
	data := hookFile(t, "cursor", filepath.Join(".cursor", "hooks.json"))
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	for _, old := range []string{"beforeShellExecution", "afterFileEdit"} {
		if _, ok := doc.Hooks[old]; ok {
			t.Errorf("%s must no longer be written", old)
		}
	}
	if got := doc.Hooks["preToolUse"]; len(got) != 1 || got[0].Command != "cmd-before-tool" {
		t.Errorf("preToolUse = %v", got)
	}
	if got := doc.Hooks["postToolUse"]; len(got) != 1 || got[0].Command != "cmd-after-tool" {
		t.Errorf("postToolUse = %v", got)
	}
}

// TestCursorMatcherOnToolEventsOnly: Cursor documents a matcher for
// preToolUse and postToolUse; elsewhere it is not written.
func TestCursorMatcherOnToolEventsOnly(t *testing.T) {
	files, err := (&Cursor{}).GenerateHookConfig(map[string][]plugin.HookEntry{
		"before_tool": {{Matcher: "Shell", Command: "a"}},
		"on_stop":     {{Matcher: "x", Command: "b"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(files[filepath.Join(".cursor", "hooks.json")])
	if !strings.Contains(s, `"matcher": "Shell"`) || strings.Contains(s, `"matcher": "x"`) {
		t.Errorf("matcher handling wrong:\n%s", s)
	}
}

// TestHookWarnings: a hook a vendor cannot carry as declared is reported by
// name, never dropped silently.
func TestHookWarnings(t *testing.T) {
	t.Run("all six events map everywhere", func(t *testing.T) {
		for _, v := range Available() {
			if w := HookWarnings(v, allCanonicalHooks()); len(w) != 0 {
				t.Errorf("%s: unexpected warnings %q", v, w)
			}
		}
	})

	t.Run("unmapped event names event and vendor", func(t *testing.T) {
		delete(cursorHookEventMap, "on_session_end")
		t.Cleanup(func() { cursorHookEventMap["on_session_end"] = "sessionEnd" })
		w := HookWarnings("cursor", allCanonicalHooks())
		if len(w) != 1 || !strings.Contains(w[0], "on_session_end") || !strings.Contains(w[0], "cursor") {
			t.Fatalf("warnings = %q", w)
		}
		// The hook is left out of the file, and the others are kept.
		files, err := (&Cursor{}).GenerateHookConfig(allCanonicalHooks())
		if err != nil {
			t.Fatal(err)
		}
		if s := string(files[filepath.Join(".cursor", "hooks.json")]); strings.Contains(s, "cmd-session-end") || !strings.Contains(s, "cmd-on-stop") {
			t.Errorf("unexpected file:\n%s", s)
		}
	})

	t.Run("declared reason is quoted", func(t *testing.T) {
		copilotUnsupportedHookEvents["on_session_end"] = "no such event"
		delete(copilotHookEventMap, "on_session_end")
		t.Cleanup(func() {
			delete(copilotUnsupportedHookEvents, "on_session_end")
			copilotHookEventMap["on_session_end"] = "SessionEnd"
		})
		w := HookWarnings("copilot", map[string][]plugin.HookEntry{"on_session_end": {{Command: "x"}}})
		if len(w) != 1 || !strings.HasSuffix(w[0], ": no such event") {
			t.Fatalf("warnings = %q", w)
		}
	})

	t.Run("matcher where the vendor ignores it", func(t *testing.T) {
		w := HookWarnings("cursor", map[string][]plugin.HookEntry{"on_session_start": {{Matcher: "startup", Command: "x"}}})
		if len(w) != 1 || !strings.Contains(w[0], "ignored") {
			t.Fatalf("warnings = %q", w)
		}
		if w := HookWarnings("claude", map[string][]plugin.HookEntry{"on_session_start": {{Matcher: "startup", Command: "x"}}}); len(w) != 0 {
			t.Errorf("claude warnings = %q, want none", w)
		}
	})

	t.Run("claude tool name on cursor", func(t *testing.T) {
		w := HookWarnings("cursor", map[string][]plugin.HookEntry{"before_tool": {{Matcher: "Bash", Command: "x"}}})
		if len(w) != 1 || !strings.Contains(w[0], "Shell") {
			t.Fatalf("warnings = %q", w)
		}
		if w := HookWarnings("cursor", map[string][]plugin.HookEntry{"before_tool": {{Matcher: "Shell", Command: "x"}}}); len(w) != 0 {
			t.Errorf("warnings = %q, want none", w)
		}
	})
}

// TestEveryCanonicalEventMappedOrDeclared is the Go side of
// scripts/hook-event-parity.sh: every canonical event is mapped, or declared
// unsupported with a reason, for every vendor.
func TestEveryCanonicalEventMappedOrDeclared(t *testing.T) {
	for _, v := range Available() {
		native, unsupported, ok := hookEventSupport(v)
		if !ok {
			t.Errorf("%s has no hook event declarations", v)
			continue
		}
		for ev := range plugin.ValidHookEvents {
			_, mapped := native[ev]
			reason, declared := unsupported[ev]
			if mapped == declared {
				t.Errorf("%s: %s must be mapped or declared unsupported, exactly one (mapped=%v declared=%v)", v, ev, mapped, declared)
			}
			if declared && reason == "" {
				t.Errorf("%s: %s is declared unsupported without a reason", v, ev)
			}
		}
	}
}

// TestHookTimeoutNativeField: a per-hook timeout is written in each vendor's
// own field and unit (seconds), and left out when unset.
func TestHookTimeoutNativeField(t *testing.T) {
	hooks := map[string][]plugin.HookEntry{
		"before_tool": {{Command: "slow", Timeout: 12}, {Command: "plain"}},
	}
	cases := []struct {
		vendor, path, want string
	}{
		{"claude", filepath.Join(".claude", "hooks", "hooks.json"), `"timeout": 12`},
		{"codex", filepath.Join(".codex", "hooks.json"), `"timeout": 12`},
		{"cursor", filepath.Join(".cursor", "hooks.json"), `"timeout": 12`},
		{"copilot", filepath.Join(".copilot", "hooks", "hooks.json"), `"timeoutSec": 12`},
	}
	for _, tc := range cases {
		t.Run(tc.vendor, func(t *testing.T) {
			a, err := Get(tc.vendor)
			if err != nil {
				t.Fatal(err)
			}
			files, err := a.GenerateHookConfig(hooks)
			if err != nil {
				t.Fatal(err)
			}
			got := string(files[tc.path])
			if !strings.Contains(got, tc.want) {
				t.Errorf("missing %s:\n%s", tc.want, got)
			}
			if strings.Count(got, "imeout") != 1 {
				t.Errorf("the hook without a timeout must carry none:\n%s", got)
			}
			if w := HookWarnings(tc.vendor, hooks); len(w) != 0 {
				t.Errorf("warnings = %q, want none", w)
			}
		})
	}
}

// TestHookTimeoutClamped: Claude lets SessionEnd hooks raise the shared budget
// to 60 s and Codex allows 3 s; a larger request is clamped and warned about,
// naming the event, the vendor, the request and the cap. Other events are not.
func TestHookTimeoutClamped(t *testing.T) {
	hooks := map[string][]plugin.HookEntry{
		"on_session_end": {{Command: "bye", Timeout: 120}},
		"on_stop":        {{Command: "stop", Timeout: 120}},
	}
	for _, tc := range []struct {
		vendor, path string
		cap          string
	}{
		{"claude", filepath.Join(".claude", "hooks", "hooks.json"), "60"},
		{"codex", filepath.Join(".codex", "hooks.json"), "3"},
	} {
		t.Run(tc.vendor, func(t *testing.T) {
			a, _ := Get(tc.vendor)
			files, err := a.GenerateHookConfig(hooks)
			if err != nil {
				t.Fatal(err)
			}
			var doc struct {
				Hooks map[string][]struct {
					Hooks []struct {
						Timeout int `json:"timeout"`
					} `json:"hooks"`
				} `json:"hooks"`
			}
			if err := json.Unmarshal(files[tc.path], &doc); err != nil {
				t.Fatal(err)
			}
			if got := doc.Hooks["SessionEnd"][0].Hooks[0].Timeout; strconvItoa(got) != tc.cap {
				t.Errorf("SessionEnd timeout = %d, want %s", got, tc.cap)
			}
			if got := doc.Hooks["Stop"][0].Hooks[0].Timeout; got != 120 {
				t.Errorf("Stop timeout = %d, want 120 (no cap)", got)
			}
			w := HookWarnings(tc.vendor, hooks)
			if len(w) != 1 || !strings.Contains(w[0], "on_session_end") || !strings.Contains(w[0], tc.vendor) ||
				!strings.Contains(w[0], "120s") || !strings.Contains(w[0], tc.cap+"s") {
				t.Errorf("warnings = %q", w)
			}
		})
	}
}

func strconvItoa(n int) string { return fmt.Sprint(n) }

// TestHookTimeoutUnsupportedVendor: a vendor with no timeout field writes
// none and says that its default applies instead, never silently.
func TestHookTimeoutUnsupportedVendor(t *testing.T) {
	oldField, oldReason := cursorHookTimeoutField, cursorHookTimeoutUnsupported
	cursorHookTimeoutField, cursorHookTimeoutUnsupported = "", "no such field"
	t.Cleanup(func() { cursorHookTimeoutField, cursorHookTimeoutUnsupported = oldField, oldReason })

	hooks := map[string][]plugin.HookEntry{"on_stop": {{Command: "x", Timeout: 9}}}
	files, err := (&Cursor{}).GenerateHookConfig(hooks)
	if err != nil {
		t.Fatal(err)
	}
	if s := string(files[filepath.Join(".cursor", "hooks.json")]); strings.Contains(s, "imeout") {
		t.Errorf("no timeout field must be written:\n%s", s)
	}
	w := HookWarnings("cursor", hooks)
	if len(w) != 1 || !strings.Contains(w[0], "on_stop") || !strings.Contains(w[0], "cursor") ||
		!strings.Contains(w[0], "9s") || !strings.Contains(w[0], "default applies") || !strings.HasSuffix(w[0], ": no such field") {
		t.Errorf("warnings = %q", w)
	}
}

// writeSessionEndHooks writes an assembled Claude session hooks file with the
// given SessionEnd timeouts and returns the config path.
func writeSessionEndHooks(t *testing.T, timeouts ...int) string {
	t.Helper()
	entries := make([]plugin.HookEntry, len(timeouts))
	for i, s := range timeouts {
		entries[i] = plugin.HookEntry{Command: "bye", Timeout: s}
	}
	files, err := (&Claude{}).GenerateHookConfig(map[string][]plugin.HookEntry{"on_session_end": entries})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for rel, data := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, rel), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func lookupFrom(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

// A plugin hook's timeout does not raise Claude's SessionEnd budget, so a
// session ynh launches sets CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS itself:
// the largest SessionEnd timeout in ms, capped at 60 s, never over the user's.
func TestClaudeSessionEndEnv(t *testing.T) {
	cases := []struct {
		name     string
		timeouts []int
		user     map[string]string
		wantEnv  string
		wantWarn bool
	}{
		{"largest timeout wins", []int{3, 10, 5}, nil, ClaudeSessionEndEnvVar + "=10000", false},
		{"capped at 60 s", []int{120}, nil, ClaudeSessionEndEnvVar + "=60000", false},
		{"no timeout, no env", []int{0}, nil, "", false},
		{"user value kept when enough", []int{10}, map[string]string{ClaudeSessionEndEnvVar: "20000"}, "", false},
		{"user value lower: kept and warned", []int{10}, map[string]string{ClaudeSessionEndEnvVar: "2000"}, "", true},
		{"user value unparsable: warned", []int{10}, map[string]string{ClaudeSessionEndEnvVar: "soon"}, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env, warns := ClaudeSessionEndEnv(writeSessionEndHooks(t, tc.timeouts...), lookupFrom(tc.user))
			got := ""
			if len(env) > 0 {
				got = env[0]
			}
			if got != tc.wantEnv || len(env) > 1 {
				t.Errorf("env = %q, want %q", env, tc.wantEnv)
			}
			if (len(warns) > 0) != tc.wantWarn {
				t.Errorf("warnings = %q, want warn=%v", warns, tc.wantWarn)
			}
		})
	}
	if env, _ := ClaudeSessionEndEnv(t.TempDir(), lookupFrom(nil)); env != nil {
		t.Errorf("no hooks file: env = %q, want none", env)
	}
}

// An export cannot set the variable, so it says what to set.
func TestExportHookWarnings_ClaudeSessionEnd(t *testing.T) {
	long := map[string][]plugin.HookEntry{"on_session_end": {{Command: "x", Timeout: 10}}}
	w := ExportHookWarnings("claude", long)
	if len(w) != 1 || !strings.Contains(w[0], ClaudeSessionEndEnvVar+"=10000") || !strings.Contains(w[0], "ignores") {
		t.Fatalf("warnings = %q", w)
	}
	for name, hooks := range map[string]map[string][]plugin.HookEntry{
		"within the default budget": {"on_session_end": {{Command: "x", Timeout: 1}}},
		"no timeout":                {"on_session_end": {{Command: "x"}}},
	} {
		if w := ExportHookWarnings("claude", hooks); len(w) != 0 {
			t.Errorf("%s: warnings = %q", name, w)
		}
	}
	if w := ExportHookWarnings("copilot", long); len(w) != 0 {
		t.Errorf("copilot honours the timeout, got %q", w)
	}
}

// Cursor's CLI does not run beforeSubmitPrompt or stop hooks in print mode
// (confirmed live), so a non-interactive launch warns, naming the events.
func TestCursorPrintModeWarnings(t *testing.T) {
	write := func(hooks map[string][]plugin.HookEntry) string {
		files, err := (&Cursor{}).GenerateHookConfig(hooks)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		for rel, data := range files {
			if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, rel)), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, rel), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}

	w := CursorPrintModeWarnings(write(allCanonicalHooks()))
	if len(w) != 2 || !strings.Contains(w[0], "before_prompt") || !strings.Contains(w[0], "beforeSubmitPrompt") ||
		!strings.Contains(w[1], "on_stop") || !strings.Contains(w[1], "stop") || !strings.Contains(w[1], "will not fire") {
		t.Errorf("warnings = %q", w)
	}
	if w := CursorPrintModeWarnings(write(map[string][]plugin.HookEntry{"before_tool": {{Command: "x"}}, "on_session_end": {{Command: "y"}}})); len(w) != 0 {
		t.Errorf("tool and session hooks fire in print mode, got %q", w)
	}
	if w := CursorPrintModeWarnings(t.TempDir()); len(w) != 0 {
		t.Errorf("no hooks file: %q", w)
	}
}
