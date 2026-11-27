//go:build unix

package main

import (
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/presence"
)

func TestInboxSessionOnlyWhereTheAllowRuleIsHonoured(t *testing.T) {
	for _, tc := range []struct {
		vendor                         string
		interactive, installed, manual bool
		want                           bool
	}{
		{"claude", true, true, false, true}, {"codex", true, true, false, true}, {"copilot", true, true, false, true},
		{"opencode", true, true, false, false}, {"cursor", true, true, false, false},
		{"claude", false, true, false, false}, {"claude", true, true, true, false},
		// A folder or inline run has no installed id, so no delegation can reach it.
		{"claude", true, false, false, false}, {"codex", true, false, false, false},
	} {
		if got := inboxSession(tc.vendor, tc.interactive, tc.installed, tc.manual); got != tc.want {
			t.Errorf("inboxSession(%s, interactive %v, installed %v, manual %v) = %v, want %v", tc.vendor, tc.interactive, tc.installed, tc.manual, got, tc.want)
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
