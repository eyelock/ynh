package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/delegation"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
)

func waiting(handles ...string) []delegation.InboxEntry {
	var es []delegation.InboxEntry
	for _, h := range handles {
		es = append(es, delegation.InboxEntry{Record: &delegation.Record{Handle: h, Parent: "local/ynw"}})
	}
	return es
}

// What each hook prints, per vendor and event, as the vendors pass it to the
// model (spike ynh #907).
func TestInboxHookOutput_PerVendor(t *testing.T) {
	line := "ynh inbox: 1 task waiting in your inbox from local/ynw. Finish what the person asked first, tell them a task is waiting and from whom, then call inbox_take."
	tests := []struct {
		vendor, event string
		check         func(t *testing.T, out string)
	}{
		{"claude", "before_prompt", func(t *testing.T, out string) {
			if out != line {
				t.Errorf("plain stdout expected, got %q", out)
			}
		}},
		{"codex", "before_prompt", func(t *testing.T, out string) {
			if out != line {
				t.Errorf("plain stdout expected, got %q", out)
			}
		}},
		{"copilot", "before_prompt", func(t *testing.T, out string) {
			var v map[string]string
			if json.Unmarshal([]byte(out), &v) != nil || v["additionalContext"] != line {
				t.Errorf("additionalContext JSON expected, got %q", out)
			}
		}},
		{"claude", "on_stop", blockCheck(line)}, {"codex", "on_stop", blockCheck(line)}, {"copilot", "on_stop", blockCheck(line)},
	}
	for _, tt := range tests {
		t.Run(tt.vendor+"/"+tt.event, func(t *testing.T) {
			out, _ := inboxHookOutput(tt.vendor, tt.event, waiting("dlg-1"), map[string]bool{}, false)
			tt.check(t, out)
		})
	}
}

func blockCheck(line string) func(t *testing.T, out string) {
	return func(t *testing.T, out string) {
		var v map[string]string
		if json.Unmarshal([]byte(out), &v) != nil || v["decision"] != "block" || v["reason"] != line {
			t.Errorf("block JSON expected, got %q", out)
		}
	}
}

func TestInboxHookOutput_SilentWhenThereIsNothingToSay(t *testing.T) {
	for _, event := range []string{"before_prompt", "on_stop"} {
		if out, _ := inboxHookOutput("claude", event, nil, nil, false); out != "" {
			t.Errorf("%s with an empty inbox printed %q", event, out)
		}
	}
	// A stop that is the continuation of a block, and a task already announced,
	// never wake the model again.
	if out, _ := inboxHookOutput("claude", "on_stop", waiting("dlg-1"), map[string]bool{}, true); out != "" {
		t.Errorf("stop_hook_active printed %q", out)
	}
	if out, fresh := inboxHookOutput("codex", "on_stop", waiting("dlg-1"), map[string]bool{"dlg-1": true}, false); out != "" || fresh != nil {
		t.Errorf("an announced task woke the model again: %q", out)
	}
	// A new task is announced even after an earlier one was.
	if out, fresh := inboxHookOutput("codex", "on_stop", waiting("dlg-1", "dlg-2"), map[string]bool{"dlg-1": true}, false); out == "" || len(fresh) != 1 || fresh[0] != "dlg-2" {
		t.Errorf("out %q fresh %v", out, fresh)
	}
	// before_prompt may repeat: the person's next prompt is the cue.
	if out, _ := inboxHookOutput("claude", "before_prompt", waiting("dlg-1"), map[string]bool{"dlg-1": true}, false); out == "" {
		t.Error("before_prompt must tell the model while the task waits")
	}
}

func TestCmdInboxHook(t *testing.T) {
	isolateRun(t)
	var out strings.Builder
	// An empty or unreadable mailbox prints nothing and never fails.
	if err := cmdInboxHook([]string{"--harness", "local--none", "--vendor", "claude", "--event", "before_prompt"}, strings.NewReader(""), &out); err != nil || out.String() != "" {
		t.Errorf("empty mailbox: %q %v", out.String(), err)
	}
	for _, bad := range [][]string{{}, {"--harness", "x"}, {"--harness", "x", "--vendor", "claude", "--event", "after_tool"}, {"--bogus", "1"}} {
		if err := cmdInboxHook(bad, strings.NewReader(""), &out); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestWithInboxHooks_AddsAfterTheHarnessOwnAndLeavesItAlone(t *testing.T) {
	p := &harness.Harness{Name: "h", Hooks: map[string][]plugin.HookEntry{"on_stop": {{Command: "./mine.sh"}}, "before_tool": {{Command: "./x.sh"}}}}
	got := withInboxHooks(p, "/usr/bin/ynh", "local--h", "codex")
	if len(got.Hooks["on_stop"]) != 2 || got.Hooks["on_stop"][0].Command != "./mine.sh" || !strings.Contains(got.Hooks["on_stop"][1].Command, "inbox-hook --harness local--h --vendor codex --event on_stop") {
		t.Errorf("on_stop = %+v", got.Hooks["on_stop"])
	}
	if len(got.Hooks["before_prompt"]) != 1 || len(got.Hooks["before_tool"]) != 1 {
		t.Errorf("hooks = %+v", got.Hooks)
	}
	if len(p.Hooks["on_stop"]) != 1 || len(p.Hooks) != 2 {
		t.Errorf("the harness was changed: %+v", p.Hooks)
	}
}
