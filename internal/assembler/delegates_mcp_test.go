package assembler

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/mcpexec"
	"github.com/eyelock/ynh/internal/vendor"
)

// delegateRepo writes a harness manifest and skills into dir and makes it a
// local git repo, the form a delegate takes.
func delegateRepo(t *testing.T, dir string, manifest map[string]any, skills ...string) string {
	t.Helper()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := writePluginJSONFile(dir, data); err != nil {
		t.Fatal(err)
	}
	for _, s := range skills {
		if err := os.MkdirAll(filepath.Join(dir, "skills", s), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "skills", s, "SKILL.md"), []byte("skill "+s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "init")
	return dir
}

func assembleFor(t *testing.T, vendorName, repo string, lookup func(string) (string, bool)) (workDir, agent string, declared []DelegateMCP, warnings string, err error) {
	t.Helper()
	t.Setenv("YNH_HOME", t.TempDir())
	adapter, aerr := vendor.Get(vendorName)
	if aerr != nil {
		t.Fatal(aerr)
	}
	workDir = t.TempDir()
	var warn strings.Builder
	declared, err = AssembleDelegates(workDir, adapter, []harness.Delegate{{GitSource: harness.GitSource{Git: repo}}}, "", DelegateOptions{Lookup: lookup, Warn: &warn, Launch: true})
	warnings = warn.String()
	if err != nil {
		return
	}
	dir := adapter.ArtifactDirs()["agents"]
	data, rerr := os.ReadFile(filepath.Join(workDir, adapter.ConfigDir(), dir, "probe.md"))
	if rerr != nil {
		t.Fatalf("agent file: %v", rerr)
	}
	return workDir, string(data), declared, warnings, nil
}

func frontmatterMCP(t *testing.T, agent string) []map[string]map[string]any {
	t.Helper()
	for _, line := range strings.Split(agent, "\n") {
		if rest, ok := strings.CutPrefix(line, "mcpServers: "); ok {
			var out []map[string]map[string]any
			if err := json.Unmarshal([]byte(rest), &out); err != nil {
				t.Fatalf("mcpServers is not a JSON list of one-key maps: %v\n%s", err, rest)
			}
			return out
		}
	}
	return nil
}

// A delegate's servers go in a Claude agent's frontmatter, in .mcp.json's
// per-server shape, with ${VAR} left literal: the file carries no secret.
func TestAssembleDelegates_ClaudeCarriesServers(t *testing.T) {
	repo := delegateRepo(t, t.TempDir(), map[string]any{
		"name": "probe", "version": "0.1.0", "description": "Probe agent",
		"env_passthrough": []string{"PROBE_TOKEN"},
		"mcp_servers": map[string]any{
			"local": map[string]any{"command": "python3", "args": []string{"/opt/probe.py"}, "env": map[string]string{"TOKEN": "${PROBE_TOKEN}"}},
			"plain": map[string]any{"command": "node", "args": []string{"/opt/plain.js"}},
			"docs":  map[string]any{"url": "https://docs.example.com/mcp", "headers": map[string]string{"Authorization": "Bearer ${PROBE_TOKEN}", "X-Org": "acme"}},
			"open":  map[string]any{"url": "https://open.example.com/mcp"},
			"old":   map[string]any{"url": "https://old.example.com/sse", "type": "sse"},
		},
	})
	lookup := func(k string) (string, bool) { return secretValue, k == "PROBE_TOKEN" }

	workDir, agent, declared, warnings, err := assembleFor(t, "claude", repo, lookup)
	if err != nil {
		t.Fatal(err)
	}
	if warnings != "" {
		t.Errorf("Claude carries them, got warnings %q", warnings)
	}
	if strings.Contains(agent, secretValue) {
		t.Errorf("the agent file must not hold the secret:\n%s", agent)
	}

	list := frontmatterMCP(t, agent)
	if len(list) != 5 {
		t.Fatalf("want 5 entries, got %v", list)
	}
	got := map[string]map[string]any{}
	for _, e := range list {
		if len(e) != 1 {
			t.Errorf("each entry is a one-key map, got %v", e)
		}
		for k, v := range e {
			got[k] = v
		}
	}
	if got["local"]["command"] != "python3" || got["local"]["type"] != nil {
		t.Errorf("stdio entry wrong: %v", got["local"])
	}
	if env, _ := got["local"]["env"].(map[string]any); env["TOKEN"] != "${PROBE_TOKEN}" {
		t.Errorf("env keeps the reference: %v", got["local"])
	}
	if got["docs"]["type"] != "http" || got["docs"]["url"] != "https://docs.example.com/mcp" {
		t.Errorf("remote entry needs type http: %v", got["docs"])
	}
	if got["old"]["type"] != "sse" {
		t.Errorf("sse entry wrong: %v", got["old"])
	}

	// The CLI is handed the servers directly, since Claude ignores the field
	// in a plugin's agents.
	launch, err := os.ReadFile(filepath.Join(workDir, vendor.ClaudeDelegateAgentsFile))
	if err != nil {
		t.Fatalf("launch file: %v", err)
	}
	if strings.Contains(string(launch), secretValue) {
		t.Errorf("the --agents JSON must not hold the secret:\n%s", launch)
	}
	var defs map[string]struct {
		Description string                      `json:"description"`
		Prompt      string                      `json:"prompt"`
		MCPServers  []map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(launch, &defs); err != nil {
		t.Fatal(err)
	}
	d := defs["probe"]
	if d.Description != "Probe agent" || len(d.MCPServers) != 5 || !strings.Contains(d.Prompt, "invoked as a delegate") {
		t.Errorf("launch definition wrong: %+v", d)
	}
	launched := map[string]map[string]any{}
	for _, e := range d.MCPServers {
		for k, v := range e {
			launched[k] = v
		}
	}

	envFile := filepath.Join(workDir, "delegates", "probe", mcpexec.EnvFileName)
	wantArgs := []any{"mcp-exec", "--env-file", envFile, "--", "python3", "/opt/probe.py"}
	if launched["local"]["command"] != "ynh" || !reflect.DeepEqual(launched["local"]["args"], wantArgs) {
		t.Errorf("a stdio server that references a variable goes through the launcher: %v", launched["local"])
	}
	if env, _ := launched["local"]["env"].(map[string]any); env["TOKEN"] != "${PROBE_TOKEN}" {
		t.Errorf("the launcher expands env, so the reference stays: %v", launched["local"])
	}
	if !reflect.DeepEqual(launched["plain"], got["plain"]) || launched["plain"]["command"] != "node" {
		t.Errorf("a server with no reference is unchanged: %v", launched["plain"])
	}
	if !reflect.DeepEqual(launched["open"], got["open"]) || !reflect.DeepEqual(launched["old"], got["old"]) {
		t.Errorf("a remote server with no reference is unchanged: %v %v", launched["open"], launched["old"])
	}
	docs := launched["docs"]
	wantHelper := "ynh mcp-headers --env-file '" + envFile + "' -- 'Authorization: Bearer ${PROBE_TOKEN}'"
	if docs["headersHelper"] != wantHelper {
		t.Errorf("headersHelper = %v, want %s", docs["headersHelper"], wantHelper)
	}
	if h, _ := docs["headers"].(map[string]any); len(h) != 1 || h["X-Org"] != "acme" {
		t.Errorf("only the headers with no reference stay in headers: %v", docs["headers"])
	}

	// The argument handed to Claude is the JSON, so this is the whole of what
	// any local process can read.
	args := vendor.ClaudeDelegateAgentArgs(workDir)
	if len(args) != 2 || args[0] != "--agents" || strings.Contains(strings.Join(args, " "), secretValue) {
		t.Errorf("launch args = %v", args)
	}

	// The values are in the env file, in the run directory, for the owner.
	info, err := os.Stat(envFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("env file mode = %v", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(filepath.Dir(envFile))
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Errorf("env file directory mode = %v", dirInfo.Mode().Perm())
	}
	data, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatal(err)
	}
	vars, err := mcpexec.Parse(data)
	if err != nil || !reflect.DeepEqual(vars, map[string]string{"PROBE_TOKEN": secretValue}) {
		t.Errorf("env file holds exactly the referenced variable: %v, %v", vars, err)
	}
	if info, err := os.Stat(filepath.Join(workDir, vendor.ClaudeDelegateAgentsFile)); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("launch file mode: %v %v", info, err)
	}

	if len(declared) != 5 || declared[0].Delegate != "probe" || declared[0].Source != harness.MCPSourceRoot {
		t.Errorf("declared = %+v", declared)
	}
}

const secretValue = "s3cret-value-xyz"

// Without Launch (preview, image) nothing is written that a run needs: no
// launch file, no env file, and so no secret.
func TestAssembleDelegates_NoLaunchWritesNoSecrets(t *testing.T) {
	repo := delegateRepo(t, t.TempDir(), map[string]any{
		"name": "probe", "version": "0.1.0", "env_passthrough": []string{"PROBE_TOKEN"},
		"mcp_servers": map[string]any{"s": map[string]any{"command": "/bin/s", "env": map[string]string{"T": "${PROBE_TOKEN}"}}},
	})
	t.Setenv("YNH_HOME", t.TempDir())
	adapter, err := vendor.Get("claude")
	if err != nil {
		t.Fatal(err)
	}
	workDir := t.TempDir()
	lookup := func(string) (string, bool) { return secretValue, true }
	declared, err := AssembleDelegates(workDir, adapter, []harness.Delegate{{GitSource: harness.GitSource{Git: repo}}}, "", DelegateOptions{Lookup: lookup})
	if err != nil || len(declared) != 1 {
		t.Fatalf("declared %v err %v", declared, err)
	}
	err = filepath.WalkDir(workDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		if strings.Contains(string(data), secretValue) || strings.HasSuffix(path, mcpexec.EnvFileName) || strings.HasSuffix(path, vendor.ClaudeDelegateAgentsFile) {
			t.Errorf("%s should not exist or hold the secret", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// A secret is a problem for the server that has to be started through the
// launcher; the other cases are errors or exemptions, not silent.
func TestAssembleDelegates_SecretHandling(t *testing.T) {
	cases := []struct {
		name     string
		manifest map[string]any
		lookup   func(string) (string, bool)
		wantErr  string
	}{
		{
			name: "a reference in the url is refused",
			manifest: map[string]any{"env_passthrough": []string{"T"},
				"mcp_servers": map[string]any{"r": map[string]any{"url": "https://x.example.com/${T}/mcp"}}},
			wantErr: "move the secret to a header",
		},
		{
			name: "a reference in args must be declared",
			manifest: map[string]any{"env_passthrough": []string{"OTHER"},
				"mcp_servers": map[string]any{"s": map[string]any{"command": "/bin/s", "args": []string{"--t=${T}"}}}},
			wantErr: "${T}, which is not in env_passthrough",
		},
		{
			name: "a reference in args must be set",
			manifest: map[string]any{"env_passthrough": []string{"T"},
				"mcp_servers": map[string]any{"s": map[string]any{"command": "/bin/s", "args": []string{"--t=${T}"}}}},
			lookup:  func(string) (string, bool) { return "", false },
			wantErr: "${T}, which is not set",
		},
		{
			name: "a reference in env must be declared",
			manifest: map[string]any{"env_passthrough": []string{"OTHER"},
				"mcp_servers": map[string]any{"s": map[string]any{"command": "/bin/s", "env": map[string]string{"K": "${T}"}}}},
			wantErr: "${T}, which is not in env_passthrough",
		},
		{
			name: "a reference in env must be set",
			manifest: map[string]any{"env_passthrough": []string{"T"},
				"mcp_servers": map[string]any{"s": map[string]any{"command": "/bin/s", "env": map[string]string{"K": "${T}"}}}},
			lookup:  func(string) (string, bool) { return "", false },
			wantErr: "${T}, which is not set",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := map[string]any{"name": "probe", "version": "0.1.0"}
			maps.Copy(m, tc.manifest)
			repo := delegateRepo(t, t.TempDir(), m)
			lookup := tc.lookup
			if lookup == nil {
				lookup = func(string) (string, bool) { return "v", true }
			}
			_, _, _, _, err := assembleFor(t, "claude", repo, lookup)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) || !strings.Contains(err.Error(), "probe") {
				t.Errorf("want an error naming the delegate and containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

// A reference in the args is a secret too: the launcher expands it, and the
// env file holds its value, never the --agents JSON.
func TestAssembleDelegates_ArgsReferenceGoesThroughLauncher(t *testing.T) {
	repo := delegateRepo(t, t.TempDir(), map[string]any{
		"name": "probe", "version": "0.1.0", "env_passthrough": []string{"T"},
		"mcp_servers": map[string]any{"s": map[string]any{"command": "srv", "args": []string{"--token=${T}"}}},
	})
	workDir, _, _, _, err := assembleFor(t, "claude", repo, func(string) (string, bool) { return secretValue, true })
	if err != nil {
		t.Fatal(err)
	}
	launch, err := os.ReadFile(filepath.Join(workDir, vendor.ClaudeDelegateAgentsFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(launch), secretValue) || !strings.Contains(string(launch), `"--token=${T}"`) {
		t.Errorf("launch JSON should keep the reference only:\n%s", launch)
	}
	data, err := os.ReadFile(filepath.Join(workDir, "delegates", "probe", mcpexec.EnvFileName))
	if err != nil || !strings.Contains(string(data), secretValue) {
		t.Errorf("env file: %q, %v", data, err)
	}
}

// Each delegate gets its own env file with only the variables its servers
// reference, regenerated on every run.
func TestAssembleDelegates_EnvFilePerDelegateRegenerated(t *testing.T) {
	repo := delegateRepo(t, t.TempDir(), map[string]any{
		"name": "probe", "version": "0.1.0", "env_passthrough": []string{"A", "B"},
		"mcp_servers": map[string]any{"s": map[string]any{"command": "/bin/s", "env": map[string]string{"K": "${A}"}}},
	})
	t.Setenv("YNH_HOME", t.TempDir())
	adapter, err := vendor.Get("claude")
	if err != nil {
		t.Fatal(err)
	}
	workDir := t.TempDir()
	envFile := filepath.Join(workDir, "delegates", "probe", mcpexec.EnvFileName)
	// A previous run left a wider file with loose permissions.
	if err := os.MkdirAll(filepath.Dir(envFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(envFile, []byte("OLD=\"stale\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lookup := func(k string) (string, bool) { return "val-" + k, true }
	if _, err := AssembleDelegates(workDir, adapter, []harness.Delegate{{GitSource: harness.GitSource{Git: repo}}}, "", DelegateOptions{Lookup: lookup, Launch: true}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "A=\"val-A\"\n" {
		t.Errorf("env file = %q, want only the referenced variable", data)
	}
	if info, _ := os.Stat(envFile); info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, an existing file must be tightened", info.Mode().Perm())
	}
}

func TestWriteEnvFileRefusesUnsafeDelegateName(t *testing.T) {
	for _, name := range []string{"", ".", "..", "a/b", `a\b`, "../x"} {
		if _, err := writeEnvFile(t.TempDir(), name, map[string]string{"A": "1"}); err == nil {
			t.Errorf("delegate name %q should be refused", name)
		}
	}
}

// A delegate's included harness's servers and skills are the delegate's.
func TestAssembleDelegates_IncludesCarried(t *testing.T) {
	inc := t.TempDir()
	if err := writePluginJSONFile(inc, []byte(`{"name":"extra","version":"1.0.0","mcp_servers":{"inc-srv":{"command":"/bin/inc"}}}`)); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(inc, "skills", "inc-skill"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inc, "skills", "inc-skill", "SKILL.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := delegateRepo(t, t.TempDir(), map[string]any{
		"name": "probe", "version": "0.1.0",
		"includes":    []map[string]any{{"local": inc}},
		"mcp_servers": map[string]any{"own": map[string]any{"command": "/bin/own"}},
	}, "own-skill")

	_, agent, declared, _, err := assembleFor(t, "claude", repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, e := range frontmatterMCP(t, agent) {
		for k := range e {
			names[k] = true
		}
	}
	if !names["inc-srv"] || !names["own"] {
		t.Errorf("want the include's and the delegate's own servers, got %v", names)
	}
	for _, skill := range []string{"- inc-skill", "- own-skill"} {
		if !strings.Contains(agent, skill) {
			t.Errorf("skills should list %q:\n%s", skill, agent)
		}
	}
	sources := map[string]string{}
	for _, d := range declared {
		sources[d.Server] = d.Source
	}
	if sources["own"] != harness.MCPSourceRoot || sources["inc-srv"] == harness.MCPSourceRoot || sources["inc-srv"] == "" {
		t.Errorf("sources = %v", sources)
	}
}

// A vendor whose subagents cannot carry servers is told so, once per
// delegate, and the agent file carries nothing.
func TestAssembleDelegates_OtherVendorsWarn(t *testing.T) {
	repo := delegateRepo(t, t.TempDir(), map[string]any{
		"name": "probe", "version": "0.1.0",
		"mcp_servers": map[string]any{"b": map[string]any{"command": "/bin/b"}, "a": map[string]any{"url": "https://a.example.com"}},
	})
	for _, name := range []string{"cursor", "copilot"} {
		t.Run(name, func(t *testing.T) {
			adapter, err := vendor.Get(name)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := adapter.ArtifactDirs()["agents"]; !ok {
				t.Skip("no agents directory")
			}
			workDir, agent, _, warnings, err := assembleFor(t, name, repo, nil)
			if err != nil {
				t.Fatal(err)
			}
			want := "warning: delegate probe declares MCP servers (a, b) that " + adapter.DisplayName() + " subagents cannot carry; they are not available to it\n"
			if !strings.HasSuffix(warnings, want) || strings.Count(warnings, "\n") != 1 {
				t.Errorf("warnings = %q, want it to end with %q", warnings, want)
			}
			if strings.Contains(agent, "mcpServers") {
				t.Errorf("no field for a vendor that cannot carry them:\n%s", agent)
			}
			if _, err := os.Stat(filepath.Join(workDir, vendor.ClaudeDelegateAgentsFile)); err == nil {
				t.Error("the launch file is Claude's alone")
			}
		})
	}
}

// A delegate with no servers is what it was before: no field, no warning, no
// launch file.
func TestAssembleDelegates_NoServersUnchanged(t *testing.T) {
	repo := delegateRepo(t, t.TempDir(), map[string]any{"name": "probe", "version": "0.1.0", "description": "Plain."})
	for _, name := range []string{"claude", "cursor"} {
		t.Run(name, func(t *testing.T) {
			workDir, agent, declared, warnings, err := assembleFor(t, name, repo, nil)
			if err != nil {
				t.Fatal(err)
			}
			want := "---\nname: probe\ndescription: Plain.\n---\n\nYou are the **probe** harness, invoked as a delegate.\n\n"
			if agent != want {
				t.Errorf("agent =\n%q\nwant\n%q", agent, want)
			}
			if warnings != "" || len(declared) != 0 {
				t.Errorf("warnings %q declared %v", warnings, declared)
			}
			if _, err := os.Stat(filepath.Join(workDir, vendor.ClaudeDelegateAgentsFile)); err == nil {
				t.Error("no servers, no launch file")
			}
		})
	}
}

// A server the delegate's own env_passthrough does not allow fails the
// assembly rather than reaching the agent unexpanded.
func TestAssembleDelegates_UnsetVariableIsAnError(t *testing.T) {
	repo := delegateRepo(t, t.TempDir(), map[string]any{
		"name": "probe", "version": "0.1.0",
		"env_passthrough": []string{"NEEDED"},
		"mcp_servers":     map[string]any{"s": map[string]any{"command": "/bin/s", "env": map[string]string{"K": "${NEEDED}"}}},
	})
	_, _, _, _, err := assembleFor(t, "claude", repo, func(string) (string, bool) { return "", false })
	if err == nil || !strings.Contains(err.Error(), "probe") {
		t.Errorf("want an error naming the delegate, got %v", err)
	}
}
