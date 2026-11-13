package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/config"
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
	e, err := delegateMCPEntry(o.eps, "local/parent", "/work")
	if err != nil || e == nil {
		t.Fatalf("entry = %v, %v", e, err)
	}
	if e.Transport() != plugin.MCPTypeStdio || len(e.Args) < 5 || e.Args[0] != "delegate-mcp" || e.Args[2] != "local/parent" || e.Args[4] != "/work" {
		t.Errorf("entry = %+v", e)
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
	e, err = delegateMCPEntry(o.eps, "local/parent", "/work")
	if err != nil || e.Args[len(e.Args)-2] != "--max-wait" || e.Args[len(e.Args)-1] != "20" {
		t.Errorf("with %s=20: %v %v", delegateMaxWaitEnv, e, err)
	}
	t.Setenv(delegateMaxWaitEnv, "soon")
	if _, err := delegateMCPEntry(o.eps, "local/parent", "/work"); err == nil {
		t.Error("a wait that is not a number of seconds should be refused")
	}
}

func TestNoA2ADelegatesNoDelegateMCPEntry(t *testing.T) {
	if e, err := delegateMCPEntry(nil, "local/parent", "/work"); e != nil || err != nil {
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
