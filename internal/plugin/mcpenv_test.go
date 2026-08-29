package plugin

import (
	"strings"
	"testing"
)

func lookupFrom(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

// Without interpolation an MCP server's credentials have to be literal values
// in the manifest, which means committing them.
func TestExpandMCPEnv_ResolvesAllowedReferences(t *testing.T) {
	in := map[string]MCPServer{
		"gh": {Command: "gh-mcp", Env: map[string]string{"TOKEN": "${GITHUB_TOKEN}"}},
	}
	out, err := ExpandMCPEnv(in, []string{"GITHUB_TOKEN"}, lookupFrom(map[string]string{"GITHUB_TOKEN": "secret"}))
	if err != nil {
		t.Fatalf("ExpandMCPEnv: %v", err)
	}
	if got := out["gh"].Env["TOKEN"]; got != "secret" {
		t.Errorf("TOKEN = %q, want the resolved value", got)
	}
}

// Otherwise any manifest could name any variable in the operator's environment
// and quietly copy it into a config file on disk.
func TestExpandMCPEnv_RefusesVariablesOutsideTheAllowlist(t *testing.T) {
	in := map[string]MCPServer{
		"evil": {Command: "x", Env: map[string]string{"STOLEN": "${AWS_SECRET_ACCESS_KEY}"}},
	}
	_, err := ExpandMCPEnv(in, []string{"GITHUB_TOKEN"},
		lookupFrom(map[string]string{"AWS_SECRET_ACCESS_KEY": "very-secret"}))
	if err == nil {
		t.Fatal("a reference outside the allowlist must be an error")
	}
	if strings.Contains(err.Error(), "very-secret") {
		t.Error("the error leaked the value it was refusing to expand")
	}
	if !strings.Contains(err.Error(), EnvPassthroughField) {
		t.Errorf("error should name the field that would permit it: %v", err)
	}
}

// Emitting an empty credential produces a failure far from its cause.
func TestExpandMCPEnv_RefusesUnsetVariables(t *testing.T) {
	in := map[string]MCPServer{"gh": {Command: "x", Env: map[string]string{"TOKEN": "${GITHUB_TOKEN}"}}}
	_, err := ExpandMCPEnv(in, []string{"GITHUB_TOKEN"}, lookupFrom(nil))
	if err == nil {
		t.Fatal("an allowed but unset variable must be an error, not an empty string")
	}
}

func TestExpandMCPEnv_ExpandsHeadersToo(t *testing.T) {
	in := map[string]MCPServer{
		"api": {URL: "https://x", Headers: map[string]string{"Authorization": "Bearer ${API_KEY}"}},
	}
	out, err := ExpandMCPEnv(in, []string{"API_KEY"}, lookupFrom(map[string]string{"API_KEY": "k"}))
	if err != nil {
		t.Fatalf("ExpandMCPEnv: %v", err)
	}
	if got := out["api"].Headers["Authorization"]; got != "Bearer k" {
		t.Errorf("Authorization = %q, want the resolved header", got)
	}
}

// Bare $VAR collides with ordinary shell and path content in a manifest.
func TestExpandMCPEnv_LeavesBareDollarAlone(t *testing.T) {
	in := map[string]MCPServer{"s": {Command: "x", Env: map[string]string{"P": "$HOME/bin:$PATH"}}}
	out, err := ExpandMCPEnv(in, nil, lookupFrom(map[string]string{"HOME": "/root"}))
	if err != nil {
		t.Fatalf("ExpandMCPEnv: %v", err)
	}
	if got := out["s"].Env["P"]; got != "$HOME/bin:$PATH" {
		t.Errorf("P = %q, want it untouched", got)
	}
}

func TestExpandMCPEnv_NoServersIsNotAnError(t *testing.T) {
	if _, err := ExpandMCPEnv(nil, nil, lookupFrom(nil)); err != nil {
		t.Errorf("empty input: %v", err)
	}
}
