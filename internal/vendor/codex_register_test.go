package vendor

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// writeCodexAgents writes agent files into runDir/.codex/agents/ the way a
// run does: a delegate through DelegateAgentFile and a harness agent through
// TransformArtifact.
func writeCodexAgents(t *testing.T, runDir string) string {
	t.Helper()
	dir := filepath.Join(runDir, ".codex", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	c := &Codex{}
	delName, delData, err := c.DelegateAgentFile("helper", `Says "hi"`, "Reply hi.")
	if err != nil {
		t.Fatal(err)
	}
	agentName, agentData, err := c.TransformArtifact("agents", "file-name.md",
		[]byte("---\nname: front-matter\ndescription: A test agent.\n---\n\nReply ok.\n"))
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{delName: delData, agentName: agentData} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestCodexAgentArgs_RegistersEachAgentByConfigFile(t *testing.T) {
	runDir := t.TempDir()
	dir := writeCodexAgents(t, runDir)

	args, warnings := CodexAgentArgs(runDir)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %q, want none", warnings)
	}
	// Sorted by file: file-name.toml (registered by its frontmatter name),
	// then helper.toml. The description is the file's TOML string as written.
	want := []string{
		"-c", `agents.front-matter.config_file="` + filepath.Join(dir, "file-name.toml") + `"`,
		"-c", `agents.front-matter.description="A test agent."`,
		"-c", `agents.helper.config_file="` + filepath.Join(dir, "helper.toml") + `"`,
		"-c", `agents.helper.description="Says \"hi\""`,
	}
	if !equalArgs(args, want) {
		t.Errorf("args =\n%q\nwant\n%q", args, want)
	}
}

func TestCodexAgentArgs_NoTrustOverride(t *testing.T) {
	runDir := t.TempDir()
	writeCodexAgents(t, runDir)
	args, _ := CodexAgentArgs(runDir)
	for _, a := range args {
		if strings.HasPrefix(a, "projects.") {
			t.Errorf("args %q carry a trust override %q; registration needs none", args, a)
		}
	}
}

func TestCodexAgentArgs_DottedNameIsWarnedNotRegistered(t *testing.T) {
	runDir := t.TempDir()
	dir := filepath.Join(runDir, ".codex", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_, data, err := (&Codex{}).DelegateAgentFile("team.dev", "A delegate.", "Do it.")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "team.dev.toml"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	args, warnings := CodexAgentArgs(runDir)
	if len(args) != 0 {
		t.Errorf("args = %q, want none for a dotted name", args)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], `"team.dev"`) || !strings.Contains(warnings[0], "'.'") {
		t.Errorf("warnings = %q, want one naming \"team.dev\" and the '.'", warnings)
	}
}

func TestCodexAgentArgs_NoAgents(t *testing.T) {
	if args, warnings := CodexAgentArgs(""); args != nil || warnings != nil {
		t.Errorf("empty run dir: args %q, warnings %q", args, warnings)
	}
	if args, warnings := CodexAgentArgs(t.TempDir()); args != nil || warnings != nil {
		t.Errorf("no .codex/agents: args %q, warnings %q", args, warnings)
	}
}

func TestCodexLaunchArgs_RegisterAgents(t *testing.T) {
	runDir := t.TempDir()
	writeCodexAgents(t, runDir)
	agents, _ := CodexAgentArgs(runDir)
	extra := []string{"--model", "m"}
	var w bytes.Buffer

	tests := []struct {
		name string
		got  []string
		want []string
	}{
		{
			name: "interactive",
			got:  codexInteractiveArgs(runDir, extra, &w),
			want: append(append([]string{}, agents...), "--model", "m"),
		},
		{
			name: "exec",
			got:  codexExecArgs(runDir, "go", extra, &w),
			want: append(append([]string{"exec", "--skip-git-repo-check"}, agents...), "--model", "m", "go"),
		},
		{
			name: "interactive resume",
			got:  withCodexRunArgs([]string{"resume", "abc"}, agents),
			want: append(append([]string{"resume"}, agents...), "abc"),
		},
		{
			name: "one-shot resume",
			got:  withCodexRunArgs([]string{"exec", "resume", "--last", "go"}, agents),
			want: append(append([]string{"exec", "resume", "--skip-git-repo-check"}, agents...), "--last", "go"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !equalArgs(tt.got, tt.want) {
				t.Errorf("args =\n%q\nwant\n%q", tt.got, tt.want)
			}
		})
	}
	if w.Len() != 0 {
		t.Errorf("unexpected warnings: %s", w.String())
	}
}

func TestCodexLaunchArgs_PrintWarnings(t *testing.T) {
	runDir := t.TempDir()
	dir := filepath.Join(runDir, ".codex", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_, data, _ := (&Codex{}).DelegateAgentFile("a.b", "d", "p")
	if err := os.WriteFile(filepath.Join(dir, "a.b.toml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	var w bytes.Buffer
	codexExecArgs(runDir, "go", nil, &w)
	if !strings.HasPrefix(w.String(), "warning: agent \"a.b\"") {
		t.Errorf("stderr = %q, want a warning naming the agent", w.String())
	}
}

func TestCodexMCP_LoadsEachServerWithoutSecretsOnTheCommandLine(t *testing.T) {
	runDir := t.TempDir()
	doc := `{"mcpServers":{
"local":{"command":"py","args":["a b","-x"],"env":{"K":"s3cret\"q"},"cwd":"/w"},
"plain":{"command":"py2","args":["y"]},
"remote":{"type":"http","url":"https://x/mcp","headers":{"Authorization":"Bearer t0ken","X-Static":"v"}},
"legacy":{"type":"sse","url":"https://x/sse"},
"bad.name":{"command":"x"}}}`
	if err := os.WriteFile(filepath.Join(runDir, ".mcp.json"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	got := CodexMCP(runDir)
	line := strings.Join(got.Args, "\n")
	for _, want := range []string{
		`mcp_servers.local.command="ynh"`,
		`"mcp-exec", "--env-file", "` + runDir,
		`"--", "py", "a b", "-x"]`,
		`mcp_servers.local.env={"K" = "${YNH_MCP_`,
		`mcp_servers.local.cwd="/w"`,
		`mcp_servers.plain.command="py2"`,
		`mcp_servers.remote.url="https://x/mcp"`,
		`mcp_servers.remote.env_http_headers={"Authorization" = "YNH_MCP_HEADER_SECRET_`,
	} {
		if !strings.Contains(line, want) {
			t.Errorf("missing %s in:\n%s", want, line)
		}
	}
	for _, secret := range []string{"s3cret", "t0ken", "Bearer"} {
		if strings.Contains(line, secret) {
			t.Errorf("%q is on the command line:\n%s", secret, line)
		}
	}
	for _, name := range []string{"bad.name", "mcp_servers.legacy"} {
		if strings.Contains(line, name) {
			t.Errorf("%s was passed:\n%s", name, line)
		}
	}
	if len(got.Warnings) != 2 || !strings.Contains(got.Warnings[0], "bad.name") && !strings.Contains(got.Warnings[1], "bad.name") {
		t.Errorf("warnings = %v", got.Warnings)
	}
	if !strings.Contains(strings.Join(got.Warnings, "\n"), "legacy") {
		t.Errorf("no warning names the sse server: %v", got.Warnings)
	}
	env := strings.Join(got.Env, "\n")
	if !strings.Contains(env, "=Bearer t0ken") || !strings.Contains(env, "=v") {
		t.Errorf("env = %v, want the header values", got.Env)
	}
	file := filepath.Join(runDir, ".env.ynh")
	data, err := os.ReadFile(file)
	if err != nil || !strings.Contains(string(data), `s3cret\"q`) {
		t.Errorf("env file = %q, %v", data, err)
	}
	if fi, _ := os.Stat(file); fi == nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("env file mode = %v, want 0600", fi)
	}
}

func TestCodexMCP_NoFile(t *testing.T) {
	if got := CodexMCP(t.TempDir()); got.Args != nil || got.Env != nil || got.Warnings != nil {
		t.Errorf("got %+v", got)
	}
}

func TestCodexMCP_NumberingIsDeterministic(t *testing.T) {
	runDir := t.TempDir()
	doc := `{"mcpServers":{
"a":{"command":"py","env":{"A1":"va1","A2":"va2","A3":"va3","A4":"va4"}},
"b":{"command":"py","env":{"B1":"vb1","B2":"vb2","B3":"vb3","B4":"vb4"}},
"r":{"type":"http","url":"https://x/mcp","headers":{"H1":"vh1","H2":"vh2","H3":"vh3","H4":"vh4"}}}}`
	if err := os.WriteFile(filepath.Join(runDir, ".mcp.json"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	first := CodexMCP(runDir)
	firstFile, _ := os.ReadFile(filepath.Join(runDir, ".env.ynh"))
	wantEnv := []string{"YNH_MCP_HEADER_SECRET_8=vh1", "YNH_MCP_HEADER_SECRET_9=vh2", "YNH_MCP_HEADER_SECRET_10=vh3", "YNH_MCP_HEADER_SECRET_11=vh4"}
	sorted := append([]string(nil), first.Env...)
	sort.Strings(sorted)
	sort.Strings(wantEnv)
	if strings.Join(sorted, ",") != strings.Join(wantEnv, ",") {
		t.Errorf("header env = %v, want %v", first.Env, wantEnv)
	}
	for _, want := range []string{`YNH_MCP_0="va1"`, `YNH_MCP_3="va4"`, `YNH_MCP_4="vb1"`, `YNH_MCP_7="vb4"`} {
		if !strings.Contains(string(firstFile), want) {
			t.Errorf("env file lacks %s:\n%s", want, firstFile)
		}
	}
	for i := 0; i < 50; i++ {
		again := CodexMCP(runDir)
		if strings.Join(again.Args, "\n") != strings.Join(first.Args, "\n") || strings.Join(again.Env, "\n") != strings.Join(first.Env, "\n") {
			t.Fatalf("call %d differs from the first:\n%v\n%v", i, again, first)
		}
		file, _ := os.ReadFile(filepath.Join(runDir, ".env.ynh"))
		if string(file) != string(firstFile) {
			t.Fatalf("call %d wrote a different env file", i)
		}
	}
}

func TestCodexMCP_HeaderVariablesMatchCodexDefaultExcludes(t *testing.T) {
	runDir := t.TempDir()
	doc := `{"mcpServers":{"r":{"type":"http","url":"https://x/mcp","headers":{"Authorization":"Bearer t0ken"}}}}`
	if err := os.WriteFile(filepath.Join(runDir, ".mcp.json"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	launch := CodexMCP(runDir)
	got := strings.Join(launch.Args, "\n")
	if strings.Contains(got, "shell_environment_policy.exclude") {
		t.Errorf("user's shell_environment_policy.exclude overridden:\n%s", got)
	}
	if !strings.Contains(got, "shell_environment_policy.ignore_default_excludes=false") {
		t.Errorf("Codex's default excludes not switched on:\n%s", got)
	}
	if g := strings.Join(CodexMCP(t.TempDir()).Args, ""); strings.Contains(g, "shell_environment_policy") {
		t.Errorf("policy touched without headers: %s", g)
	}
	if len(launch.Env) != 1 {
		t.Fatalf("env = %v", launch.Env)
	}
	// Codex's default policy drops names containing KEY, SECRET or TOKEN.
	name := strings.ToUpper(strings.SplitN(launch.Env[0], "=", 2)[0])
	if !strings.Contains(name, "SECRET") {
		t.Errorf("%s would not be caught by Codex's default excludes", name)
	}
}
