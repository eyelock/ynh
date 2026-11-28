package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/permission"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/vendor"
)

func TestDelegateMCPEntryCarriesTheEndpointsAndNoOtherVariable(t *testing.T) {
	parent, store := endpointSetup(t, "a2a")
	t.Setenv(delegateMaxWaitEnv, "")
	o, err := openEndpointsWith(parent, "local/parent", t.TempDir(), &store, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	defer o.close()
	e, err := delegateMCPEntry(o.eps, "local/parent", "/work", "")
	if err != nil || e == nil {
		t.Fatalf("entry = %v, %v", e, err)
	}
	if e.Transport() != plugin.MCPTypeStdio || len(e.Args) < 5 || e.Args[0] != "delegate-mcp" || e.Args[2] != "local/parent" || e.Args[4] != "/work" {
		t.Errorf("entry = %+v", e)
	}
	if strings.Contains(strings.Join(e.Args, " "), "--questions") {
		t.Errorf("an unset delegate_questions put %v on the command line", e.Args)
	}
	if c, err := delegateMCPEntry(o.eps, "local/parent", "/work", "client"); err != nil || !strings.HasSuffix(strings.Join(c.Args, " "), "--questions client") {
		t.Errorf("delegate_questions client: args = %v, err %v", c, err)
	}
	if _, err := delegateMCPEntry(o.eps, "local/parent", "/work", "elsewhere"); err == nil {
		t.Error("an unknown delegate_questions value was accepted")
	}
	if e.Env["YNH_DELEGATES"] != "reviewer" || e.Env["YNH_DELEGATE_REVIEWER_CARD"] != o.eps[0].CardURL || e.Env["YNH_DELEGATE_REVIEWER_AUTH"] != "Bearer "+o.eps[0].Token {
		t.Errorf("env = %v", e.Env)
	}
	if e.Env["YNH_HOME"] != config.HomeDir() {
		t.Errorf("YNH_HOME = %q, want %q: a vendor may hand its MCP servers a trimmed environment", e.Env["YNH_HOME"], config.HomeDir())
	}
	for k := range e.Env {
		if strings.HasPrefix(k, "A2ACLI_") {
			t.Errorf("%s belongs to the vendor session, not to the delegates' server", k)
		}
	}
	// The same entry on every vendor's config.
	for _, name := range []string{"claude", "codex", "copilot", "cursor", "opencode"} {
		a, err := vendor.Get(name)
		if err != nil {
			t.Fatal(err)
		}
		files, err := a.GenerateMCPConfig(map[string]plugin.MCPServer{delegateMCPServerName: *e})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var all strings.Builder
		for _, c := range files {
			all.Write(c)
		}
		for _, want := range []string{delegateMCPServerName, "delegate-mcp", o.eps[0].Token, o.eps[0].CardURL} {
			if !strings.Contains(all.String(), want) {
				t.Errorf("%s: the MCP config lacks %q:\n%s", name, want, all.String())
			}
		}
	}
	// The wait setting travels as an argument.
	t.Setenv(delegateMaxWaitEnv, "20")
	e, err = delegateMCPEntry(o.eps, "local/parent", "/work", "")
	if err != nil || e.Args[len(e.Args)-2] != "--max-wait" || e.Args[len(e.Args)-1] != "20" {
		t.Errorf("with %s=20: %v %v", delegateMaxWaitEnv, e, err)
	}
	t.Setenv(delegateMaxWaitEnv, "soon")
	if _, err := delegateMCPEntry(o.eps, "local/parent", "/work", ""); err == nil {
		t.Error("a wait that is not a number of seconds should be refused")
	}
}

func TestNoA2ADelegatesNoDelegateMCPEntry(t *testing.T) {
	if e, err := delegateMCPEntry(nil, "local/parent", "/work", ""); e != nil || err != nil {
		t.Errorf("entry = %v, %v", e, err)
	}
}

func TestDelegatesFromEnv(t *testing.T) {
	env := map[string]string{"YNH_DELEGATES": "reviewer,code-fixer", "YNH_DELEGATE_REVIEWER_CARD": "http://a", "YNH_DELEGATE_REVIEWER_AUTH": "Bearer x",
		"YNH_DELEGATE_CODE_FIXER_CARD": "http://b", "YNH_DELEGATE_CODE_FIXER_AUTH": "Bearer y"}
	ds, err := delegatesFromEnv(func(k string) string { return env[k] })
	if err != nil || len(ds) != 2 || ds[1].Name != "code-fixer" || ds[1].Auth != "Bearer y" {
		t.Fatalf("delegates = %+v, %v", ds, err)
	}
	delete(env, "YNH_DELEGATE_CODE_FIXER_AUTH")
	if _, err := delegatesFromEnv(func(k string) string { return env[k] }); err == nil || !strings.Contains(err.Error(), "CODE_FIXER_AUTH") {
		t.Errorf("a missing token: %v", err)
	}
	if _, err := delegatesFromEnv(func(string) string { return "" }); err == nil {
		t.Error("no delegates should be refused")
	}
}

// delegate_questions unset is auto by session: an interactive parent's
// client gets the question, a headless parent's model does; a harness that
// sets one keeps it in either session.
func TestDelegateQuestionsFor_AutoBySession(t *testing.T) {
	for _, tt := range []struct {
		name     string
		declared string
		oneShot  bool
		want     string
	}{
		{"interactive, unset: client", "", false, "client"},
		{"headless, unset: model", "", true, "model"},
		{"interactive, forced model", "model", false, "model"},
		{"headless, forced client", "client", true, "client"},
		{"interactive, forced client", "client", false, "client"},
		{"headless, forced model", "model", true, "model"},
	} {
		if got := delegateQuestionsFor(tt.declared, tt.oneShot); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}

// The entry `ynh run` writes carries the resolved route, so the server gets
// client in an interactive session and model in a headless one.
func TestDelegateMCPEntry_ResolvedBySession(t *testing.T) {
	parent, store := endpointSetup(t, "a2a")
	t.Setenv(delegateMaxWaitEnv, "")
	o, err := openEndpointsWith(parent, "local/parent", t.TempDir(), &store, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	defer o.close()
	for oneShot, want := range map[bool]string{false: "--questions client", true: "--questions model"} {
		e, err := delegateMCPEntry(o.eps, "local/parent", "/work", delegateQuestionsFor("", oneShot))
		if err != nil || !strings.HasSuffix(strings.Join(e.Args, " "), want) {
			t.Errorf("oneShot=%v: args = %v, err %v, want ...%s", oneShot, e, err, want)
		}
	}
}

// The wait setting is the user's, set before `ynh run`; the run's scrub of an
// enclosing session's endpoints must not clear it before the entry reads it.
func TestDelegateMaxWaitSurvivesTheRunScrub(t *testing.T) {
	parent, store := endpointSetup(t, "a2a")
	t.Setenv("YNH_DELEGATE_STALE_CARD", "http://stale") // from an enclosing session
	t.Setenv(delegateMaxWaitEnv, "90")
	o, err := openEndpointsWith(parent, "local/parent", t.TempDir(), &store, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	defer o.close()
	if _, ok := os.LookupEnv("YNH_DELEGATE_STALE_CARD"); ok {
		t.Error("the scrub left an enclosing session's endpoint variable")
	}
	e, err := delegateMCPEntry(o.eps, "local/parent", "/work", "")
	if err != nil || e.Args[len(e.Args)-2] != "--max-wait" || e.Args[len(e.Args)-1] != "90" {
		t.Errorf("with %s=90 set before the run: %v %v", delegateMaxWaitEnv, e, err)
	}
}

func TestBadDelegateMaxWaitRefusesTheRunPath(t *testing.T) {
	for _, bad := range []string{"0", "abc", "-5", "1.5"} {
		parent, store := endpointSetup(t, "a2a")
		t.Setenv(delegateMaxWaitEnv, bad)
		o, err := openEndpointsWith(parent, "local/parent", t.TempDir(), &store, &bytes.Buffer{})
		if err != nil {
			t.Fatal(err)
		}
		_, err = delegateMCPEntry(o.eps, "local/parent", "/work", "")
		o.close()
		if err == nil || !strings.Contains(err.Error(), "want a whole number of seconds above 0") {
			t.Errorf("%s=%q: err = %v", delegateMaxWaitEnv, bad, err)
		}
	}
}

// The launch line names each delegate with the level it opens at when run
// unattended (ynh #1058, S2): all when it declares none.
func TestLaunchLineShowsEachDelegatesLevel(t *testing.T) {
	parent, store := endpointSetup(t, "a2a")
	t.Setenv(delegateMaxWaitEnv, "")
	t.Setenv("YNH_AUTO_APPROVE_CAP", "")
	var out bytes.Buffer
	o, err := openEndpointsWith(parent, "local/parent", t.TempDir(), &store, &out)
	if err != nil {
		t.Fatal(err)
	}
	defer o.close()
	if !strings.Contains(out.String(), "A2A delegate endpoint(s): ") || !strings.Contains(out.String(), " (opens at all)") {
		t.Errorf("launch line = %q, want each delegate with the level it opens at", out.String())
	}
}

func TestDelegateLaunchList(t *testing.T) {
	levels := map[string]permission.Level{"reviewer": permission.All, "librarian": permission.Edits}
	if got := delegateLaunchList("librarian,reviewer", levels); got != "librarian (opens at edits), reviewer (opens at all)" {
		t.Errorf("got %q", got)
	}
	if got := delegateLaunchList("reviewer,other", levels); got != "reviewer (opens at all), other" {
		t.Errorf("a delegate with no level is shown bare: %q", got)
	}
}
