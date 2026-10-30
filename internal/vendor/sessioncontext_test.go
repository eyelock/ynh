package vendor

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

// runWrapper writes the wrapper to a temp dir and runs it the way a hook is
// run: sh <script> <mode> '<command>', with stdin.
func runWrapper(t *testing.T, mode, command, stdin string) (stdout, stderr string, exit int) {
	t.Helper()
	for _, tool := range []string{"sh", "awk"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not installed", tool)
		}
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "wrap.sh")
	if err := os.WriteFile(script, []byte(sessionContextScript), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", script, mode, command)
	cmd.Env = append(os.Environ(), "TMPDIR="+dir)
	cmd.Stdin = strings.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	if ee, ok := err.(*exec.ExitError); ok {
		exit = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return out.String(), errb.String(), exit
}

func TestSessionContextWrapperOutput(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    string // the decoded field value; "" with wantNone means no output
		none    bool
	}{
		{"plain line", `echo hello`, "hello", false},
		{"quotes", `printf '%s\n' 'say "hi" and it'"'"'s fine'`, `say "hi" and it's fine`, false},
		{"backslashes", `printf '%s\n' 'C:\dir\n not a newline'`, `C:\dir\n not a newline`, false},
		{"multi-line", `printf 'one\ntwo\n\nfour\n'`, "one\ntwo\n\nfour", false},
		{"tabs and CR", `printf 'a\tb\r\nc\n'`, "a\tb\r\nc", false},
		{"control characters", `printf 'x\001y\033z\n'`, "x\x01y\x1bz", false},
		{"unicode", `printf '%s\n' 'caf\u00e9 \u2603 \xf0\x9f\x94\xa5'`, "", false},
		{"percent and braces in text", `printf '%s\n' '100% {done}'`, "100% {done}", false},
		{"empty", `true`, "", true},
		{"whitespace only", `printf '\n  \n'`, "", true},
	}
	for _, field := range []string{"additionalContext", "additional_context"} {
		for _, tc := range tests {
			t.Run(field+"/"+tc.name, func(t *testing.T) {
				out, _, exit := runWrapper(t, field, tc.command, "")
				if exit != 0 {
					t.Fatalf("exit %d", exit)
				}
				if tc.none {
					if out != "" {
						t.Fatalf("want no output, got %q", out)
					}
					return
				}
				var got map[string]string
				if err := json.Unmarshal([]byte(out), &got); err != nil {
					t.Fatalf("output is not valid JSON: %v\n%q", err, out)
				}
				want := tc.want
				if tc.name == "unicode" {
					want = "café ☃ 🔥"
					// printf in dash does not decode \u; build the expected text from the same bytes.
					raw, _, _ := runWrapper(t, field, `printf '%s\n' 'café ☃ 🔥'`, "")
					if err := json.Unmarshal([]byte(raw), &got); err != nil || got[field] != want {
						t.Fatalf("unicode: got %v err %v", got, err)
					}
					return
				}
				if v, ok := got[field]; !ok || v != want {
					t.Errorf("field %s = %q, want %q (all: %v)", field, v, want, got)
				}
			})
		}
	}
}

func TestSessionContextWrapperJSONPassThrough(t *testing.T) {
	in := `{"additionalContext": "already", "extra": [1, 2]}`
	out, _, exit := runWrapper(t, "additionalContext", "printf '%s\\n' '"+in+"'", "")
	if exit != 0 {
		t.Fatalf("exit %d", exit)
	}
	if strings.TrimSpace(out) != in {
		t.Errorf("JSON object must pass unchanged, got %q", out)
	}
}

func TestSessionContextWrapperBoth(t *testing.T) {
	out, _, _ := runWrapper(t, sessionContextBoth, `printf 'a "b"\nc\n'`, "")
	var got struct {
		AdditionalContext  string `json:"additionalContext"`
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%q", err, out)
	}
	want := "a \"b\"\nc"
	if got.AdditionalContext != want || got.HookSpecificOutput.AdditionalContext != want || got.HookSpecificOutput.HookEventName != "SessionStart" {
		t.Errorf("got %+v", got)
	}
}

func TestSessionContextWrapperStdinStatusStderr(t *testing.T) {
	// stdin reaches the command
	out, _, _ := runWrapper(t, "additionalContext", `cat`, "payload-from-vendor")
	var got map[string]string
	if err := json.Unmarshal([]byte(out), &got); err != nil || got["additionalContext"] != "payload-from-vendor" {
		t.Errorf("stdin not passed through: %q (%v)", out, err)
	}
	// exit status and stderr are kept
	out, errOut, exit := runWrapper(t, "additionalContext", `echo ctx; echo oops >&2; exit 7`, "")
	if exit != 7 {
		t.Errorf("exit = %d, want 7", exit)
	}
	if !strings.Contains(errOut, "oops") {
		t.Errorf("stderr lost: %q", errOut)
	}
	if !strings.Contains(out, "ctx") {
		t.Errorf("stdout lost on failure: %q", out)
	}
}

func TestSessionStartHookConfigPerVendor(t *testing.T) {
	hooks := map[string][]plugin.HookEntry{
		"on_session_start": {{Command: "./scripts/wake.sh it's"}},
		"on_stop":          {{Command: "./scripts/stop.sh"}},
	}
	tests := []struct {
		vendor, file string
		wantStart    string
		wantStop     string
		wrapperAt    string // "" when no wrapper is written
	}{
		{"claude", filepath.Join(".claude", "hooks", "hooks.json"), `"${CLAUDE_PLUGIN_ROOT}"/scripts/wake.sh it's`, `"${CLAUDE_PLUGIN_ROOT}"/scripts/stop.sh`, ""},
		{"codex", filepath.Join(".codex", "hooks.json"), `./scripts/wake.sh it's`, `./scripts/stop.sh`, ""},
		{"copilot", copilotSessionHookFile,
			`sh "${PLUGIN_ROOT}"/hooks/ynh-session-context.sh additionalContext '"${PLUGIN_ROOT}"/scripts/wake.sh it'\''s'`,
			`"${PLUGIN_ROOT}"/scripts/stop.sh`, filepath.Join(".copilot", SessionContextScript)},
		{"cursor", filepath.Join(".cursor", "hooks.json"),
			`sh ./hooks/ynh-session-context.sh additional_context './scripts/wake.sh it'\''s'`,
			`./scripts/stop.sh`, SessionContextScript},
	}
	for _, tc := range tests {
		t.Run(tc.vendor, func(t *testing.T) {
			a, err := Get(tc.vendor)
			if err != nil {
				t.Fatal(err)
			}
			files, err := a.GenerateHookConfig(hooks)
			if err != nil {
				t.Fatal(err)
			}
			var doc any
			if err := json.Unmarshal(files[tc.file], &doc); err != nil {
				t.Fatal(err)
			}
			cmds := commandsIn(doc)
			if !slices.Contains(cmds, tc.wantStart) || !slices.Contains(cmds, tc.wantStop) {
				t.Errorf("commands = %q\nwant %q and %q", cmds, tc.wantStart, tc.wantStop)
			}
			if tc.wrapperAt == "" {
				if len(files) != 1 {
					t.Errorf("no wrapper expected, files = %v", keysOf(files))
				}
				return
			}
			if string(files[tc.wrapperAt]) != sessionContextScript {
				t.Errorf("wrapper not written at %s: %v", tc.wrapperAt, keysOf(files))
			}
		})
	}
}

// commandsIn collects every "command" string in a decoded hook document.
func commandsIn(v any) []string {
	var out []string
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			if s, ok := val.(string); ok && k == "command" {
				out = append(out, s)
				continue
			}
			out = append(out, commandsIn(val)...)
		}
	case []any:
		for _, e := range x {
			out = append(out, commandsIn(e)...)
		}
	}
	return out
}

func TestSessionStartPluginAndMergedHookConfig(t *testing.T) {
	hooks := map[string][]plugin.HookEntry{"on_session_start": {{Command: "./wake.sh"}}}

	cp, _ := Get("copilot")
	files, err := cp.(*Copilot).GeneratePluginHookConfig(hooks)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files[SessionContextScript]; !ok || !strings.Contains(string(files[copilotPluginHookFile]), "additionalContext") {
		t.Errorf("copilot export: %v", keysOf(files))
	}

	cu, _ := Get("cursor")
	files, err = cu.(*Cursor).GeneratePluginHookConfig(hooks)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files[SessionContextScript]; !ok || !strings.Contains(string(files[pluginHookFile("cursor")]), "CURSOR_PLUGIN_ROOT") {
		t.Errorf("cursor export: %v", keysOf(files))
	}

	cl, _ := Get("claude")
	files, err = cl.(*Claude).GeneratePluginHookConfig(hooks)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || strings.Contains(string(files[pluginHookFile("claude")]), "ynh-session-context") {
		t.Errorf("claude alone must stay plain: %v", keysOf(files))
	}
	files, err = cl.(*Claude).GeneratePluginHookConfigForCopilot(hooks)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files[SessionContextScript]; !ok || !strings.Contains(string(files[pluginHookFile("claude")]), "claude+copilot") {
		t.Errorf("merged claude+copilot file must wrap in both-shapes mode: %v", keysOf(files))
	}
}

func TestSessionContextUnsupportedWarns(t *testing.T) {
	old := sessionStartContext["codex"]
	sessionStartContext["codex"] = "unsupported:no hook output channel"
	t.Cleanup(func() { sessionStartContext["codex"] = old })

	hooks := map[string][]plugin.HookEntry{"on_session_start": {{Command: "x"}}}
	var found bool
	for _, w := range HookWarnings("codex", hooks) {
		if strings.Contains(w, "on_session_start output does not reach the model on codex: no hook output channel") {
			found = true
		}
	}
	if !found {
		t.Errorf("want unsupported warning, got %v", HookWarnings("codex", hooks))
	}
	if got := HookWarnings("codex", map[string][]plugin.HookEntry{"on_stop": {{Command: "x"}}}); len(got) != 0 {
		t.Errorf("no session-start hook, no warning: %v", got)
	}
	for _, v := range []string{"claude", "copilot", "cursor"} {
		for _, w := range HookWarnings(v, hooks) {
			if strings.Contains(w, "does not reach the model") {
				t.Errorf("%s must not warn: %s", v, w)
			}
		}
	}
}
