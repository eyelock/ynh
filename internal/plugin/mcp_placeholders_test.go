package plugin

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandPluginPlaceholders(t *testing.T) {
	root, data := filepath.Join("/", "plugins", "p"), filepath.Join("/", "data", "p")
	servers := map[string]MCPServer{
		"local": {Command: "./bin/srv", Args: []string{"--cfg", "${PLUGIN_ROOT}/c.json", "--cache", "${PLUGIN_DATA}/x"},
			Env: map[string]string{"A": "${PLUGIN_ROOT}", "B": "${PLUGIN_DATA}/${PLUGIN_ROOT}"}, Cwd: "${PLUGIN_ROOT}/work"},
		"dot":    {Command: "npx", Cwd: "./data"},
		"remote": {URL: "https://x/${PLUGIN_ROOT}", Headers: map[string]string{"H": "${PLUGIN_DATA}"}},
	}
	out := ExpandPluginPlaceholders(servers, root, data, true)

	l := out["local"]
	if l.Command != filepath.Join(root, "bin", "srv") {
		t.Errorf("command = %q", l.Command)
	}
	if l.Args[1] != root+"/c.json" || l.Args[3] != data+"/x" {
		t.Errorf("args = %v", l.Args)
	}
	if l.Env["A"] != root || l.Env["B"] != data+"/"+root || l.Env[MCPEnvRoot] != root || l.Env[MCPEnvData] != data {
		t.Errorf("env = %v", l.Env)
	}
	if l.Cwd != root+"/work" {
		t.Errorf("cwd = %q", l.Cwd)
	}
	if out["dot"].Cwd != filepath.Join(root, "data") || out["dot"].Command != "npx" {
		t.Errorf("dot = %+v", out["dot"])
	}
	if r := out["remote"]; r.URL != "https://x/${PLUGIN_ROOT}" || r.Headers["H"] != "${PLUGIN_DATA}" || r.Env != nil {
		t.Errorf("remote must be untouched: %+v", r)
	}
	if servers["local"].Args[1] != "${PLUGIN_ROOT}/c.json" {
		t.Error("input mutated")
	}
}

func TestExpandPluginPlaceholders_NoEnvUnlessAsked(t *testing.T) {
	out := ExpandPluginPlaceholders(map[string]MCPServer{"s": {Command: "x"}}, "/r", "/d", false)
	if out["s"].Env != nil {
		t.Errorf("env = %v, want none", out["s"].Env)
	}
	out = ExpandPluginPlaceholders(map[string]MCPServer{"s": {Command: "./x", Cwd: "./w"}}, "", "", false)
	if out["s"].Command != "./x" || out["s"].Cwd != "./w" {
		t.Errorf("without a root, ./ paths must stay as written: %+v", out["s"])
	}
	if ExpandPluginPlaceholders(nil, "/r", "/d", true) != nil {
		t.Error("nil in, nil out")
	}
}

// The reserved names are placeholders, not credentials: the allowlist
// neither admits nor refuses them.
func TestReservedNamesAreNotEnvReferences(t *testing.T) {
	servers := map[string]MCPServer{"s": {Command: "x", Env: map[string]string{"A": "${PLUGIN_ROOT}/x", "B": "${TOKEN}"}}}
	issues := UndeclaredMCPEnvRefs(servers, []string{"OTHER"})
	if len(issues) != 1 || !strings.Contains(issues[0], "${TOKEN}") {
		t.Errorf("issues = %v, want only TOKEN flagged", issues)
	}
	out, err := ExpandMCPEnv(servers, []string{"TOKEN"}, func(string) (string, bool) { return "t", true })
	if err != nil {
		t.Fatal(err)
	}
	if out["s"].Env["A"] != "${PLUGIN_ROOT}/x" || out["s"].Env["B"] != "t" {
		t.Errorf("env = %v", out["s"].Env)
	}
}
