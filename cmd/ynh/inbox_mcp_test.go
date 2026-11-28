//go:build unix

package main

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/presence"
	"github.com/eyelock/ynh/internal/vendor"
)

func TestInboxSessionOnlyWhereTheAllowRuleIsHonoured(t *testing.T) {
	for _, tc := range []struct {
		vendor                                  string
		interactive, installed, manual, hooksOK bool
		want                                    bool
	}{
		{"claude", true, true, false, true, true}, {"codex", true, true, false, true, true}, {"copilot", true, true, false, true, true},
		{"opencode", true, true, false, true, false}, {"cursor", true, true, false, true, false},
		{"claude", false, true, false, true, false}, {"claude", true, true, true, true, false},
		// A folder or inline run has no installed id, so no delegation can reach it.
		{"claude", true, false, false, true, false}, {"codex", true, false, false, true, false},
		// Codex runs the inbox hooks only for an accepted harness.
		{"codex", true, true, false, false, false}, {"claude", true, true, false, false, true},
	} {
		if got := inboxSession(tc.vendor, tc.interactive, tc.installed, tc.manual, tc.hooksOK); got != tc.want {
			t.Errorf("inboxSession(%s, interactive %v, installed %v, manual %v, hooks %v) = %v, want %v", tc.vendor, tc.interactive, tc.installed, tc.manual, tc.hooksOK, got, tc.want)
		}
	}
}

// A vendor or harness that takes no task says why, so a task that goes
// headless instead can carry the reason.
func TestInboxRefusalSaysWhy(t *testing.T) {
	for _, tc := range []struct {
		vendor                     string
		installed, manual, hooksOK bool
		want                       string
	}{
		{"claude", true, false, true, ""}, {"codex", true, false, true, ""}, {"copilot", true, false, true, ""},
		{"opencode", true, false, true, "drops hook output"},
		{"cursor", true, false, true, "has not been run"},
		{"claude", true, true, true, "mcp_manual_approval"},
		{"claude", false, false, true, "not an installed harness"},
		{"codex", true, false, false, "ynh trust accept"},
	} {
		got := inboxRefusal(tc.vendor, tc.installed, tc.manual, tc.hooksOK)
		if (tc.want == "") != (got == "") || !strings.Contains(got, tc.want) {
			t.Errorf("inboxRefusal(%+v) = %q, want it to contain %q", tc, got, tc.want)
		}
	}
}

func TestInboxAllowArgsPerVendor(t *testing.T) {
	for vendor, want := range map[string][]string{
		"claude":  {"--allowedTools=mcp__plugin_eno_ynh-inbox"},
		"codex":   {"-c", `mcp_servers.ynh-inbox.default_tools_approval_mode="approve"`},
		"copilot": {"--allow-tool=ynh-inbox"},
		"cursor":  nil,
	} {
		if got := inboxAllowArgs(vendor, "eno"); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %v, want %v", vendor, got, want)
		}
	}
	// Claude Code writes any character outside letters, digits, _ and - as _.
	if got := inboxAllowArgs("claude", "my.seat"); got[0] != "--allowedTools=mcp__plugin_my_seat_ynh-inbox" {
		t.Errorf("claude rule for a dotted name = %v", got)
	}
}

func TestInboxMCPEntryIsTheSameForEveryRun(t *testing.T) {
	a, err := inboxMCPEntry("local--eno")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(a.Args, " ") != "inbox-mcp --harness local--eno" {
		t.Errorf("args = %v: the session id must not be in the entry, or no snapshot is reused", a.Args)
	}
}

const inboxClaudeHarness = `{"name":"ibx","version":"0.1.0","default_vendor":"claude"}`

// An interactive run is a taker: it gets the ynh-inbox server's allow rule,
// its session id in the environment and a presence record that accepts tasks.
// A one-shot run is none of these.
func TestRunInteractiveSessionAcceptsTasks(t *testing.T) {
	isolateRun(t)
	t.Chdir(t.TempDir())
	home := os.Getenv("YNH_HOME")
	id := installListTestHarness(t, home, "ibx", inboxClaudeHarness)

	var args []string
	var env string
	var live []presence.Entry
	stubLaunchArgs(t, func(a []string) error {
		args, env = a, os.Getenv("YNH_SESSION_ID")
		live = liveSessionsOf("local--ibx")
		return nil
	})
	if err := cmdRun([]string{id, "--interactive"}); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(args, "--allowedTools=mcp__plugin_ibx_ynh-inbox") {
		t.Errorf("no allow rule for ynh-inbox in %q", args)
	}
	if len(live) != 1 || !live[0].AcceptsTasks || live[0].SessionID != env || env == "" {
		t.Errorf("presence = %+v, YNH_SESSION_ID = %q: want one session that accepts tasks, under that id", live, env)
	}

	// A one-shot run needs the harness accepted, even one with no commands
	// (ynh #1058, N2).
	acceptForTest(t, id)
	args, live = nil, nil
	if err := cmdRun([]string{id, "--", "hi"}); err != nil {
		t.Fatal(err)
	}
	for _, a := range args {
		if strings.Contains(a, "ynh-inbox") {
			t.Errorf("a one-shot run has the inbox rule: %q", args)
		}
	}
	if len(live) != 1 || live[0].AcceptsTasks {
		t.Errorf("a one-shot run's presence = %+v, want one that takes no tasks", live)
	}
}

// An interactive session of a vendor that takes tasks gets the inbox's
// before_prompt and on_stop hooks in its assembled config, beside the
// harness's own; OpenCode, which cannot be told, gets none and its presence
// says why.
func TestRunInteractiveSessionGetsTheInboxHooks(t *testing.T) {
	isolateRun(t)
	t.Chdir(t.TempDir())
	home := os.Getenv("YNH_HOME")
	id := installListTestHarness(t, home, "ibx", inboxClaudeHarness)
	var hooks string
	old := launchSessionFn
	t.Cleanup(func() { launchSessionFn = old })
	launchSessionFn = func(_ vendor.Adapter, runDir, _ string, _ runArgs, _, _ string, _ []string, _ io.Writer) error {
		data, _ := os.ReadFile(filepath.Join(runDir, ".claude", "hooks", "hooks.json"))
		hooks = string(data)
		return nil
	}
	if err := cmdRun([]string{id, "--interactive"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"inbox-hook --harness local--ibx --vendor claude --event before_prompt", "inbox-hook --harness local--ibx --vendor claude --event on_stop"} {
		if !strings.Contains(hooks, want) {
			t.Errorf("%q not in the assembled hooks: %s", want, hooks)
		}
	}

	// OpenCode takes no task: no hooks, and the presence record says why.
	oc := installListTestHarness(t, home, "ibo", `{"name":"ibo","version":"0.1.0","default_vendor":"opencode"}`)
	var why string
	launchSessionFn = func(_ vendor.Adapter, runDir, _ string, _ runArgs, _, _ string, _ []string, _ io.Writer) error {
		for _, e := range liveSessionsOf("local--ibo") {
			why = e.NoTasksReason
		}
		data, _ := os.ReadFile(filepath.Join(runDir, ".opencode", "hooks", "hooks.json"))
		hooks = string(data)
		return nil
	}
	if err := cmdRun([]string{oc, "--interactive"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(hooks, "inbox-hook") || !strings.Contains(why, "drops hook output") {
		t.Errorf("opencode: hooks %q, reason %q", hooks, why)
	}
}
