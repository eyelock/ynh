package vendor

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eyelock/ynh/internal/plugin"
)

func TestOpenCodeIdentity(t *testing.T) {
	o := &OpenCode{}
	if o.Name() != "opencode" || o.DisplayName() != "OpenCode" || o.CLIName() != "opencode" {
		t.Errorf("identity = %q %q %q", o.Name(), o.DisplayName(), o.CLIName())
	}
	if o.ConfigDir() != ".opencode" || o.InstructionsFile() != "AGENTS.md" {
		t.Errorf("layout = %q %q", o.ConfigDir(), o.InstructionsFile())
	}
	if !reflect.DeepEqual(o.ArtifactDirs(), DefaultArtifactDirs()) {
		t.Error("ArtifactDirs is not the default")
	}
	if o.NeedsSymlinks() || !o.SupportsInitialPrompt() || !o.SupportsResume() {
		t.Error("symlinks/initial-prompt/resume flags wrong")
	}
	entries, err := o.Install(t.TempDir(), t.TempDir())
	if err != nil || entries != nil {
		t.Errorf("Install = %v, %v", entries, err)
	}
	if err := o.Clean(nil); err != nil {
		t.Errorf("Clean: %v", err)
	}
	if got, err := Get("opencode"); err != nil || got.Name() != "opencode" {
		t.Errorf("not registered: %v", err)
	}
}

func TestOpenCodeNoExportSurface(t *testing.T) {
	o := &OpenCode{}
	files, err := o.GeneratePluginManifest(&plugin.HarnessJSON{Name: "x"}, t.TempDir())
	if files != nil || err != nil {
		t.Errorf("manifest = %v, %v", files, err)
	}
	idx, err := o.GenerateMarketplaceIndex(MarketplaceIndexConfig{}, nil)
	if idx != nil || err != nil {
		t.Errorf("index = %q, %v", idx, err)
	}
	if o.PluginManifestDir() != "" || o.MarketplaceManifestDir() != "" ||
		o.ExportArtifactDirs() != nil || o.SupportsExportDelegates() ||
		o.AgentPluginLayout() != (AgentPluginLayout{}) {
		t.Error("OpenCode must declare no export, marketplace or Agent Plugins surface")
	}
}

func TestOpenCodeGenerateSystemPrompt(t *testing.T) {
	files := (&OpenCode{}).GenerateSystemPrompt([]byte("body"))
	if len(files) != 1 || string(files["AGENTS.md"]) != "body" {
		t.Errorf("files = %v", keysOf(files))
	}
}

func TestOpenCodeResume(t *testing.T) {
	if _, err := (&OpenCode{}).ResolveLastSession("/x", time.Time{}); !errors.Is(err, ErrSessionLookupUnavailable) {
		t.Errorf("err = %v, want ErrSessionLookupUnavailable", err)
	}
}

// fakeOpencode puts an executable named opencode on PATH and stubs the exec
// and version probe, returning a pointer to the recorded exec call.
type opencodeCall struct {
	bin  string
	args []string
	env  []string
}

func stubOpencode(t *testing.T, version string) *opencodeCall {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "opencode"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	call := &opencodeCall{}
	oldExec, oldProbe := opencodeExec, opencodeVersionProbe
	t.Cleanup(func() { opencodeExec, opencodeVersionProbe = oldExec, oldProbe })
	opencodeVersionProbe = func(string) (string, error) { return version, nil }
	opencodeExec = func(b string, a []string, e []string) error {
		call.bin, call.args, call.env = b, a, e
		return nil
	}
	return call
}

func TestOpenCodeLaunch(t *testing.T) {
	cases := []struct {
		name string
		run  func(o *OpenCode, cfg string) error
		want []string
	}{
		{"interactive", func(o *OpenCode, c string) error { return o.LaunchInteractive(c, []string{"-m", "p/m"}) },
			[]string{"opencode", "--standalone", "-m", "p/m"}},
		{"initial prompt", func(o *OpenCode, c string) error { return o.LaunchWithInitialPrompt(c, "do it", []string{"--auto"}) },
			[]string{"opencode", "--standalone", "--prompt", "do it", "--auto"}},
		{"non-interactive", func(o *OpenCode, c string) error {
			return o.LaunchNonInteractive(c, "go", []string{"--format", "json"})
		},
			[]string{"opencode", "run", "--standalone", "--format", "json", "--", "go"}},
		{"resume id", func(o *OpenCode, c string) error { return o.LaunchResume(c, "ses_1", "", true, []string{"--auto"}) },
			[]string{"opencode", "--standalone", "-s", "ses_1", "--auto"}},
		{"resume last", func(o *OpenCode, c string) error { return o.LaunchResume(c, "", "", false, nil) },
			[]string{"opencode", "--standalone", "-c"}},
		{"resume id with prompt, one-shot", func(o *OpenCode, c string) error {
			return o.LaunchResume(c, "ses_1", "-go on", false, []string{"--auto"})
		},
			[]string{"opencode", "run", "--standalone", "-s", "ses_1", "--auto", "--", "-go on"}},
		{"resume last with prompt, one-shot", func(o *OpenCode, c string) error { return o.LaunchResume(c, "", "go on", false, nil) },
			[]string{"opencode", "run", "--standalone", "-c", "--", "go on"}},
		{"resume id with prompt, interactive", func(o *OpenCode, c string) error {
			return o.LaunchResume(c, "ses_1", "go on", true, []string{"--auto"})
		},
			[]string{"opencode", "--standalone", "-s", "ses_1", "--prompt", "go on", "--auto"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			call := stubOpencode(t, "opencode v2.0.27\n")
			cfg := t.TempDir()
			if err := tc.run(&OpenCode{}, cfg); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(call.args, tc.want) {
				t.Errorf("args = %v, want %v", call.args, tc.want)
			}
			want := "OPENCODE_CONFIG_DIR=" + filepath.Join(cfg, ".opencode")
			if !slices.Contains(call.env, want) {
				t.Errorf("env lacks %s", want)
			}
		})
	}
}

func TestOpenCodeLaunchReplacesInheritedConfigDir(t *testing.T) {
	call := stubOpencode(t, "opencode v2.0.27")
	t.Setenv("OPENCODE_CONFIG_DIR", "/elsewhere")
	cfg := t.TempDir()
	if err := (&OpenCode{}).LaunchInteractive(cfg, nil); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range call.env {
		if strings.HasPrefix(e, "OPENCODE_CONFIG_DIR=") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("OPENCODE_CONFIG_DIR appears %d times", n)
	}
}

func TestOpenCodeLaunchErrors(t *testing.T) {
	t.Run("not installed", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		if err := (&OpenCode{}).LaunchInteractive(t.TempDir(), nil); err == nil {
			t.Error("expected an error with no opencode on PATH")
		}
	})
	t.Run("compose failure", func(t *testing.T) {
		cfg := t.TempDir()
		rules := filepath.Join(cfg, ".opencode", "rules")
		if err := os.MkdirAll(filepath.Join(rules, "bad.md"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(rules, "ok.md"), []byte("x"), 0o000); err != nil {
			t.Fatal(err)
		}
		if os.Geteuid() == 0 {
			t.Skip("root reads unreadable files")
		}
		// The compose is the prepare step, run before the snapshot is sealed.
		if err := (&OpenCode{}).PrepareLaunch(cfg); err == nil {
			t.Error("expected a compose error from PrepareLaunch")
		}
	})
}

func TestCheckOpencodeVersion(t *testing.T) {
	cases := []struct {
		out     string
		probe   error
		wantErr string
	}{
		{out: "opencode v2.0.27\n"},
		{out: "2.1.0"},
		{out: "opencode v1.14.3\n", wantErr: "2.x only"},
		{out: "opencode vx.y\n", wantErr: "2.x only"},
		{out: "opencode v2.0.27\nnew version available: 3.0.0\n"},
		{out: "v10.0.0", wantErr: "2.x only"},
		{out: "v2-rc", wantErr: "2.x only"},
		{out: "garbage", wantErr: "2.x only"},
		{out: "opencode 2.0.27 (build 1.9.0)"},
		{out: "  \n", wantErr: "printed nothing"},
		{probe: errors.New("boom"), wantErr: "boom"},
	}
	for _, tc := range cases {
		t.Run(tc.out, func(t *testing.T) {
			old := opencodeVersionProbe
			t.Cleanup(func() { opencodeVersionProbe = old })
			opencodeVersionProbe = func(string) (string, error) { return tc.out, tc.probe }
			err := checkOpencodeVersion("opencode")
			switch {
			case tc.wantErr == "" && err != nil:
				t.Errorf("unexpected error: %v", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Errorf("err = %v, want %q", err, tc.wantErr)
			}
			if tc.wantErr == "2.x only" && !errors.Is(err, errOpencodeVersion) {
				t.Errorf("err does not wrap errOpencodeVersion: %v", err)
			}
		})
	}
}

func TestOpenCodeVersionProbeRunsBinary(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "opencode")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho \"opencode v2.9.9\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := opencodeVersionProbe(bin)
	if err != nil || !strings.Contains(out, "v2.9.9") {
		t.Errorf("probe = %q, %v", out, err)
	}
}

func TestOpenCodeApplyRuntimeInstructionsAndCompose(t *testing.T) {
	o := &OpenCode{}
	run := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(run, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("AGENTS.md", "Base.\n")
	write(".opencode/rules/b.md", "---\npaths: [\"*.go\"]\n---\nRule B.\n")
	write(".opencode/rules/a.md", "Rule A.\n")
	write(".opencode/rules/notes.txt", "ignored")
	write(".opencode/rules/empty.md", "---\nx: y\n---\n")

	args, err := o.ApplyRuntimeInstructions(run, "Runtime one.")
	if err != nil || args != nil {
		t.Fatalf("ApplyRuntimeInstructions = %v, %v", args, err)
	}

	composed := func() string {
		t.Helper()
		if err := composeOpencodeAgents(run); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(run, ".opencode", "AGENTS.md"))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	want := "Base.\n\nRule A.\n\nRule B.\n\nRuntime one.\n"
	if got := composed(); got != want {
		t.Errorf("composed =\n%q\nwant\n%q", got, want)
	}
	if got := composed(); got != want {
		t.Errorf("second compose differs (not idempotent):\n%q", got)
	}
	if b, _ := os.ReadFile(filepath.Join(run, "AGENTS.md")); string(b) != "Base.\n" {
		t.Errorf("root AGENTS.md was modified: %q", b)
	}
}

func TestComposeOpencodeAgentsNothingToCompose(t *testing.T) {
	run := t.TempDir()
	if err := composeOpencodeAgents(run); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(run, ".opencode", "AGENTS.md")); !os.IsNotExist(err) {
		t.Errorf("AGENTS.md written with nothing to compose: %v", err)
	}
}

func TestOpenCodeApplyRuntimeInstructionsErrors(t *testing.T) {
	run := t.TempDir()
	if err := os.WriteFile(filepath.Join(run, ".opencode"), []byte("a file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (&OpenCode{}).ApplyRuntimeInstructions(run, "x"); err == nil {
		t.Error("expected an error when .opencode is a file")
	}
	run2 := t.TempDir()
	if err := os.MkdirAll(filepath.Join(run2, ".opencode", opencodeRuntimeFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := (&OpenCode{}).ApplyRuntimeInstructions(run2, "x"); err == nil {
		t.Error("expected an error when the runtime file is a directory")
	}
}

func TestOpenCodeGenerateHookConfig(t *testing.T) {
	o := &OpenCode{}

	// Only a session-start hook: no plugin, just the list ynh runs at launch.
	if only, err := o.GenerateHookConfig(map[string][]plugin.HookEntry{"on_session_start": {{Command: "s"}}}); err != nil ||
		len(only) != 1 || only[filepath.Join(".opencode", opencodeSessionStartFile)] == nil {
		t.Errorf("session-start only = %v, %v", keysOf(only), err)
	}

	if files, err := o.GenerateHookConfig(nil); files != nil || err != nil {
		t.Errorf("nil hooks = %v, %v", files, err)
	}
	if files, _ := o.GenerateHookConfig(map[string][]plugin.HookEntry{"bogus": {{Command: "x"}}}); files != nil {
		t.Errorf("unmapped event produced %v", keysOf(files))
	}

	nasty := "echo \"$(cat)\"; echo '`x`' ${Y} \\ </script> `rm`"
	files, err := o.GenerateHookConfig(map[string][]plugin.HookEntry{
		"before_tool":      {{Matcher: "Bash|Edit", Command: nasty}, {Command: "./scripts/guard.sh --strict"}},
		"after_tool":       {{Matcher: "Edit", Command: "lint"}},
		"before_prompt":    {{Command: "p"}},
		"on_session_start": {{Command: "s"}},
		"on_stop":          {{Command: "e"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := keysOf(files); len(got) != 2 || got[0] != filepath.Join(".opencode", "plugins", "ynh-hooks.ts") ||
		got[1] != filepath.Join(".opencode", opencodeSessionStartFile) {
		t.Fatalf("files = %v", got)
	}
	src := string(files[".opencode/plugins/ynh-hooks.ts"])

	// The hook table is a JSON literal: recover it and check the commands
	// survived byte for byte, so nothing was concatenated into the source.
	start := strings.Index(src, "= {\n  \"after_tool\"")
	end := strings.Index(src, "\n}\n\nconst ROOT")
	if start < 0 || end < 0 {
		t.Fatalf("hook table not found in plugin:\n%s", src)
	}
	var table map[string][]opencodeHookEntry
	if err := json.Unmarshal([]byte(src[start+2:end+2]), &table); err != nil {
		t.Fatalf("hook table is not JSON: %v", err)
	}
	if table["before_tool"][0].Command != nasty || table["before_tool"][0].Matcher != "Bash|Edit" {
		t.Errorf("command mangled: %+v", table["before_tool"][0])
	}
	if got := table["before_tool"][1].Command; got != `"${YNH_HOOK_ROOT}"/scripts/guard.sh --strict` {
		t.Errorf("./ script not anchored: %q", got)
	}
	for _, want := range []string{
		`id: "ynh.hooks"`,
		`ctx.tool.hook("execute.before"`,
		`ctx.tool.hook("execute.after"`,
		`ctx.session.hook("prompt"`,
		`ctx.event.subscribe`,
		`"session.execution.succeeded"`,
		`"session.execution.failed"`,
		`shell: ["Bash"]`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("plugin lacks %s", want)
		}
	}
	if strings.Contains(src, "session.created") || strings.Contains(src, "on_session_start") {
		t.Error("the plugin must not carry on_session_start: ynh runs it once before launch")
	}
	if strings.Contains(src, "__HOOKS__") {
		t.Error("placeholder left in plugin")
	}
	if o.SessionHookScriptDir() != ".opencode" {
		t.Errorf("SessionHookScriptDir = %q", o.SessionHookScriptDir())
	}
}

func TestOpenCodeGenerateMCPConfig(t *testing.T) {
	o := &OpenCode{}
	if files, err := o.GenerateMCPConfig(nil); files != nil || err != nil {
		t.Errorf("nil servers = %v, %v", files, err)
	}

	files, err := o.GenerateMCPConfig(map[string]plugin.MCPServer{
		"local":  {Command: "npx", Args: []string{"-y", "pkg", "${ROOT}/x"}, Env: map[string]string{"K": "${KEY}"}, Cwd: "."},
		"http":   {URL: "https://h/mcp", Headers: map[string]string{"Authorization": "Bearer ${TOKEN}"}},
		"sse":    {Type: plugin.MCPTypeSSE, URL: "https://s/sse"},
		"plain":  {Command: "tool"},
		"noauth": {URL: "https://n/mcp"},
		"tenant": {URL: "https://t/mcp", Headers: map[string]string{"X-Tenant": "acme"}},
		"lower":  {URL: "https://l/mcp", Headers: map[string]string{"authorization": "Bearer x"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	data := files[".opencode/opencode.json"]
	if len(files) != 1 || data == nil {
		t.Fatalf("files = %v", keysOf(files))
	}
	var doc struct {
		Schema string `json:"$schema"`
		MCP    struct {
			Servers map[string]map[string]any `json:"servers"`
		} `json:"mcp"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Schema != "https://opencode.ai/config.json" {
		t.Errorf("schema = %q", doc.Schema)
	}
	s := doc.MCP.Servers
	if !reflect.DeepEqual(s["local"]["command"], []any{"npx", "-y", "pkg", "{env:ROOT}/x"}) ||
		s["local"]["type"] != "local" || s["local"]["cwd"] != "." ||
		!reflect.DeepEqual(s["local"]["environment"], map[string]any{"K": "{env:KEY}"}) {
		t.Errorf("local = %v", s["local"])
	}
	if s["http"]["type"] != "remote" || s["http"]["url"] != "https://h/mcp" || s["http"]["oauth"] != false ||
		!reflect.DeepEqual(s["http"]["headers"], map[string]any{"Authorization": "Bearer {env:TOKEN}"}) {
		t.Errorf("http = %v", s["http"])
	}
	if s["sse"]["type"] != "remote" || s["sse"]["url"] != "https://s/sse" {
		t.Errorf("sse = %v", s["sse"])
	}
	if _, has := s["noauth"]["oauth"]; has {
		t.Errorf("oauth set without headers: %v", s["noauth"])
	}
	if _, has := s["tenant"]["oauth"]; has {
		t.Errorf("oauth disabled by a non-credential header: %v", s["tenant"])
	}
	if s["lower"]["oauth"] != false {
		t.Errorf("a lower-case authorization header did not disable oauth: %v", s["lower"])
	}
	if _, has := s["plain"]["environment"]; has {
		t.Errorf("empty environment emitted: %v", s["plain"])
	}
}

func TestOpenCodeTransformArtifact(t *testing.T) {
	o := &OpenCode{}
	rule := func(action, resource, effect string) string {
		return "  - action: " + action + "\n    resource: \"" + resource + "\"\n    effect: " + effect + "\n"
	}
	allow := func(a, r string) string { return rule(a, r, "allow") }
	deny := func(a, r string) string { return rule(a, r, "deny") }
	denyAll := "  - action: \"*\"\n    resource: \"*\"\n    effect: deny\n"
	cases := []struct {
		name, artifact, file, in, want string
	}{
		{"skill untouched", "skills", "s.md", "---\ntools: x\n---\nb", "---\ntools: x\n---\nb"},
		{"non-md untouched", "agents", "a.txt", "x", "x"},
		{
			"claude agent", "agents", "code-reviewer.md",
			"---\nname: code-reviewer\ndescription: Reviews code\nmodel: sonnet\ncolor: blue\n---\nBody\n",
			"---\ndescription: Reviews code\nmode: all\n---\nBody\n",
		},
		{
			"keeps mode and provider model", "agents", "a.md",
			"---\ndescription: d\nmode: subagent\nmodel: opencode/free\n---\n\nBody\n",
			"---\ndescription: d\nmode: subagent\nmodel: opencode/free\n---\nBody\n",
		},
		{
			"tools comma string", "agents", "a.md",
			"---\ndescription: d\ntools: Read, Grep\n---\nB\n",
			"---\ndescription: d\nmode: all\npermissions:\n" + denyAll + allow("read", "*") + allow("grep", "*") + "---\nB\n",
		},
		{
			"tools yaml list, dupes and unknown", "agents", "a.md",
			"---\ndescription: d\ntools:\n  - Edit\n  - Write\n  - mcp__x__y\n  - Bash\n---\nB\n",
			"---\ndescription: d\nmode: all\npermissions:\n" + denyAll + allow("edit", "*") + allow("x_y", "*") + allow("shell", "*") + "---\nB\n",
		},
		{
			"granted MCP tool is allowed, not denied", "agents", "a.md",
			"---\ndescription: d\ntools: [Read, mcp__srv__do_it, mcp__bad, mcp__srv__x(y)]\n---\nB\n",
			"---\ndescription: d\nmode: all\npermissions:\n" + denyAll + allow("read", "*") + allow("srv_do_it", "*") + "---\nB\n",
		},
		{
			"granted MCP tool with existing permissions is not denied", "agents", "a.md",
			"---\ndescription: d\ntools: Read, Glob, Grep, Edit, Bash, WebFetch, WebSearch, Task, Skill, mcp__srv__do_it\npermissions:\n  - action: edit\n    resource: \"*\"\n    effect: ask\n---\nB\n",
			"---\ndescription: d\nmode: all\npermissions:\n  - action: edit\n    resource: \"*\"\n    effect: ask\n---\nB\n",
		},
		{
			"tools flow list, only unknown", "agents", "a.md",
			"---\ndescription: d\ntools: [Foo, Bar]\n---\nB\n",
			"---\ndescription: d\nmode: all\npermissions:\n" + denyAll + "---\nB\n",
		},
		{
			"scoped Bash keeps commas in parens and maps to a command pattern", "agents", "a.md",
			"---\ndescription: d\ntools: Read, Bash(git status:*), Bash(git add -A, x), Read(./src/**)\n---\nB\n",
			"---\ndescription: d\nmode: all\npermissions:\n" + denyAll + allow("read", "*") + allow("shell", "git status *") + allow("shell", "git add -A, x") + "---\nB\n",
		},
		{
			"empty tools inherits", "agents", "a.md",
			"---\ndescription: d\ntools:\n---\nB\n",
			"---\ndescription: d\nmode: all\n---\nB\n",
		},
		{
			"disallowedTools alone denies after the default allow-all", "agents", "a.md",
			"---\ndescription: d\ndisallowedTools: Bash, Write, mcp__srv__do_it, Foo\n---\nB\n",
			"---\ndescription: d\nmode: all\npermissions:\n" + deny("shell", "*") + deny("edit", "*") + deny("srv_do_it", "*") + "---\nB\n",
		},
		{
			"disallowed-tools scoped Bash and scoped Read", "agents", "a.md",
			"---\ndescription: d\ndisallowed-tools: Bash(rm:*), Read(./.env)\n---\nB\n",
			"---\ndescription: d\nmode: all\npermissions:\n" + deny("shell", "rm *") + deny("read", "*") + "---\nB\n",
		},
		{
			"disallowed denies come after the allows", "agents", "a.md",
			"---\ndescription: d\ntools: Bash\ndisallowedTools: Bash(rm:*)\n---\nB\n",
			"---\ndescription: d\nmode: all\npermissions:\n" + denyAll + allow("shell", "*") + deny("shell", "rm *") + "---\nB\n",
		},
		{
			"existing permissions are kept and only denies are appended", "agents", "a.md",
			"---\ndescription: d\ntools: Bash\ndisallowedTools: Write\npermissions:\n  - action: edit\n    resource: \"*\"\n    effect: ask\n---\nB\n",
			"---\ndescription: d\nmode: all\npermissions:\n  - action: edit\n    resource: \"*\"\n    effect: ask\n" +
				"  - action: read\n    resource: \"*\"\n    effect: deny\n" +
				"  - action: edit\n    resource: \"*\"\n    effect: deny\n" +
				"  - action: glob\n    resource: \"*\"\n    effect: deny\n" +
				"  - action: grep\n    resource: \"*\"\n    effect: deny\n" +
				"  - action: webfetch\n    resource: \"*\"\n    effect: deny\n" +
				"  - action: websearch\n    resource: \"*\"\n    effect: deny\n" +
				"  - action: subagent\n    resource: \"*\"\n    effect: deny\n" +
				"  - action: skill\n    resource: \"*\"\n    effect: deny\n" +
				"  - action: edit\n    resource: \"*\"\n    effect: deny\n---\nB\n",
		},
		{
			"existing permissions untouched without tool lists", "agents", "a.md",
			"---\ndescription: d\npermissions:\n    - action: edit\n      resource: \"*\"\n      effect: deny\n---\nB\n",
			"---\ndescription: d\nmode: all\npermissions:\n    - action: edit\n      resource: \"*\"\n      effect: deny\n---\nB\n",
		},
		{
			"empty inline permissions are no permissions, so the lists still apply", "agents", "a.md",
			"---\ndescription: d\ntools: Read\ndisallowedTools: Bash\npermissions: [ ]\n---\nB\n",
			"---\ndescription: d\nmode: all\npermissions:\n" + denyAll + allow("read", "*") + deny("shell", "*") + "---\nB\n",
		},
		{
			"empty inline permissions without tool lists are dropped", "agents", "a.md",
			"---\ndescription: d\npermissions: []\n---\nB\n",
			"---\ndescription: d\nmode: all\n---\nB\n",
		},
		{
			"inline permissions without tool lists are kept as written", "agents", "a.md",
			"---\ndescription: d\npermissions: [{action: edit, resource: \"*\", effect: ask}]\n---\nB\n",
			"---\ndescription: d\nmode: all\npermissions: [{action: edit, resource: \"*\", effect: ask}]\n---\nB\n",
		},
		{
			"no description", "agents", "plan-helper.md",
			"---\nname: x\ntools: Read\n---\nBody\n",
			"---\ndescription: \"Plan Helper\"\nmode: all\npermissions:\n" + denyAll + allow("read", "*") + "---\nBody\n",
		},
		{
			"empty description", "agents", "e.md",
			"---\ndescription:\n---\nBody\n",
			"---\ndescription: \"E\"\nmode: all\n---\nBody\n",
		},
		{
			"no frontmatter", "agents", "bare.md", "Just a body\n",
			"---\ndescription: \"Bare\"\nmode: all\n---\nJust a body\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			name, got, err := o.TransformArtifact(tc.artifact, tc.file, []byte(tc.in))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if name != tc.file {
				t.Errorf("name = %q", name)
			}
			if string(got) != tc.want {
				t.Errorf("got\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

func TestOpenCodeTransformArtifactRefusesInlinePermissionsWithToolLists(t *testing.T) {
	inline := "permissions: [{action: edit, resource: \"*\", effect: ask}]\n"
	for _, lists := range []string{
		"tools: Read\n",
		"disallowedTools: Bash\n",
		"disallowed-tools: [Bash]\n",
		"tools: Read\ndisallowedTools: Bash\n",
	} {
		t.Run(strings.TrimSpace(lists), func(t *testing.T) {
			in := "---\ndescription: d\n" + lists + inline + "---\nB\n"
			_, got, err := (&OpenCode{}).TransformArtifact("agents", "a.md", []byte(in))
			if !errors.Is(err, errOpencodeInlinePermissions) {
				t.Fatalf("err = %v, want the inline-permissions refusal", err)
			}
			if got != nil {
				t.Errorf("refused agent still produced output: %q", got)
			}
			for _, want := range []string{"tools: / disallowedTools", "inline", "block list"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not say %q", err, want)
				}
			}
		})
	}
}

func TestOpenCodeGenerateHookConfigRejectsBadMatcher(t *testing.T) {
	// RE2 rejects the first four. JavaScript throws "Invalid group" on the next
	// five. The rest JavaScript compiles but reads differently from RE2.
	for _, m := range []string{"Bash(", "[", "*x(", "a**",
		"(?i)bash", "(?P<x>a)", "(?s:a)", "Bash|(?i)edit", "(?<x>a)",
		"[[:alpha:]]+", "[a[:digit:]]", "[[:^space:]]", `Bash\z`, `\ABash`, `\QBash\E`,
		`\a`, `\pL+`, `\p{Greek}`, `\PL`, `\x{41}`, `\01`, `[\pL]`,
		"[]a]", "[^]a]", "Edit|[]]"} {
		_, err := (&OpenCode{}).GenerateHookConfig(map[string][]plugin.HookEntry{
			"before_tool": {{Matcher: m, Command: "c"}},
		})
		if err == nil || !strings.Contains(err.Error(), "invalid matcher") {
			t.Errorf("matcher %q: err = %v, want an invalid-matcher error", m, err)
		}
	}
	// "*" and the empty matcher mean every tool; a session event's matcher is
	// not a tool matcher and is not checked.
	if _, err := (&OpenCode{}).GenerateHookConfig(map[string][]plugin.HookEntry{
		"before_tool": {{Matcher: "*", Command: "c"}, {Command: "c"}, {Matcher: "Edit|Write", Command: "c"},
			// Escaped and bracketed "(?" are literals, not groups.
			{Matcher: "(?:Edit|Write)", Command: "c"}, {Matcher: `\(?i`, Command: "c"}, {Matcher: "[(?]i", Command: "c"}, {Matcher: `[\]](?:a)`, Command: "c"},
			// Escapes that mean the same in RE2 and JavaScript.
			{Matcher: `\bBash\b|\d+\s\w\D\S\W|[\d\w\-.]|\x41|\.\*\\|\[:alpha:\]|[\t\n\r\f\v]`, Command: "c"}},
		"on_session_start": {{Matcher: "startup(", Command: "c"}},
	}); err != nil {
		t.Errorf("valid matchers rejected: %v", err)
	}
}

func TestOpenCodeVersionProbeTimesOut(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "opencode")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := opencodeVersionTimeout
	t.Cleanup(func() { opencodeVersionTimeout = old })
	opencodeVersionTimeout = 200 * time.Millisecond
	start := time.Now()
	if _, err := opencodeVersionProbe(bin); err == nil {
		t.Error("expected the probe to be killed")
	}
	if time.Since(start) > 10*time.Second {
		t.Errorf("probe took %v", time.Since(start))
	}
}

func TestPrepareOpenCode(t *testing.T) {
	old := opencodeVersionProbe
	t.Cleanup(func() { opencodeVersionProbe = old })
	opencodeVersionProbe = func(string) (string, error) { return "opencode v1.2.3", nil }
	cfg := t.TempDir()
	if err := os.WriteFile(filepath.Join(cfg, "AGENTS.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := PrepareOpenCode(cfg, "opencode"); !errors.Is(err, errOpencodeVersion) {
		t.Fatalf("err = %v, want the version refusal", err)
	}
	if _, err := os.Stat(filepath.Join(cfg, ".opencode", "AGENTS.md")); !os.IsNotExist(err) {
		t.Error("composed before the version was checked")
	}
	opencodeVersionProbe = func(string) (string, error) { return "opencode v2.0.27", nil }
	if err := PrepareOpenCode(cfg, "opencode"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(cfg, ".opencode", "AGENTS.md")); string(b) != "x\n" {
		t.Errorf("composed = %q", b)
	}
	if err := PrepareOpenCode("", "opencode"); err != nil {
		t.Errorf("empty configPath: %v", err)
	}
}

// TestPrepareOpenCodeSessionContext: the on_session_start hook runs before
// launch and its stdout lands in the composed AGENTS.md, once per launch.
func TestPrepareOpenCodeSessionContext(t *testing.T) {
	old := opencodeVersionProbe
	t.Cleanup(func() { opencodeVersionProbe = old })
	opencodeVersionProbe = func(string) (string, error) { return "opencode v2.0.27", nil }

	cfg := t.TempDir()
	if err := os.WriteFile(filepath.Join(cfg, "AGENTS.md"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := (&OpenCode{}).GenerateHookConfig(map[string][]plugin.HookEntry{
		"on_session_start": {
			{Command: "echo ctx-one; cat >/dev/null"},
			{Command: "echo broken; exit 1"},
			{Command: "echo ctx-two"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for rel, data := range files {
		p := filepath.Join(cfg, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	agents := filepath.Join(cfg, ".opencode", "AGENTS.md")
	for i := 0; i < 2; i++ {
		if err := PrepareOpenCode(cfg, "opencode"); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(agents)
		if got, want := string(b), "base\n\nctx-one\n\nctx-two\n"; got != want {
			t.Fatalf("launch %d: AGENTS.md = %q, want %q (a failing hook's output must not be kept)", i+1, got, want)
		}
	}

	// A later launch whose hooks print nothing carries no stale context.
	if err := os.WriteFile(filepath.Join(cfg, ".opencode", opencodeSessionStartFile), []byte(`[{"command":"true"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := PrepareOpenCode(cfg, "opencode"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(agents); string(b) != "base\n" {
		t.Errorf("stale context kept: %q", b)
	}
}

// TestPrepareOpenCodeSessionStartBackgroundChild: a hook that leaves a child
// running does not hold the launch until the child exits, and its output is
// kept.
func TestPrepareOpenCodeSessionStartBackgroundChild(t *testing.T) {
	old := opencodeVersionProbe
	t.Cleanup(func() { opencodeVersionProbe = old })
	opencodeVersionProbe = func(string) (string, error) { return "opencode v2.0.27", nil }

	cfg := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cfg, ".opencode"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg, ".opencode", opencodeSessionStartFile), []byte(`[{"command":"sleep 20 & echo hi"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := PrepareOpenCode(cfg, "opencode"); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("launch waited %v for a backgrounded child", d)
	}
	if b, _ := os.ReadFile(filepath.Join(cfg, ".opencode", "AGENTS.md")); string(b) != "hi\n" {
		t.Errorf("AGENTS.md = %q, want the hook's output", b)
	}
}

// TestOpenCodeHooksPluginBehaviour runs the generated plugin under Node
// (which strips TypeScript types) against a stub of the plugin runtime and a
// fake ctx that records the hooks it installs. Everything lives under
// t.TempDir().
func TestOpenCodeHooksPluginBehaviour(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not on PATH")
	}
	if out, err := exec.Command(node, "--experimental-strip-types", "-e", "const x: number = 1").CombinedOutput(); err != nil {
		t.Skipf("this node cannot run TypeScript: %s", out)
	}

	dir := t.TempDir()
	marker := filepath.Join(dir, "edit-marker")
	stdinLog := filepath.Join(dir, "stdin.json")
	mcpLog := filepath.Join(dir, "mcp.json")
	underLog := filepath.Join(dir, "under.json")
	otherLog := filepath.Join(dir, "other.jsonl")
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("package.json", `{"type":"module"}`)
	write("node_modules/@opencode/plugin/package.json", `{"name":"@opencode/plugin","type":"module","main":"index.js"}`)
	write("node_modules/@opencode/plugin/index.js", `export const Plugin = { define: (p) => p }`)

	files, err := (&OpenCode{}).GenerateHookConfigForServers(map[string][]plugin.HookEntry{
		"before_tool": {
			{Matcher: "Edit", Command: "touch '" + marker + "'"},
			{Matcher: "Bash", Command: "cat > '" + stdinLog + "'; exit 1"},
			{Matcher: "mcp__echo__ynh_ping", Command: "cat > '" + mcpLog + "'"},
			{Matcher: "mcp__my_srv__do_it", Command: "cat > '" + underLog + "'"},
			{Matcher: "opencode_list_mcp_resources|unknown_tool", Command: "cat >> '" + otherLog + "'"},
			{Matcher: "Bash|Task", Command: "echo 'use make instead' >&2; exit 2"},
		},
		"after_tool": {{Matcher: "mcp__srv__do_it", Command: "exit 0"}},
	}, []string{"echo", "my_srv", "my"})
	if err != nil {
		t.Fatal(err)
	}
	write("plugins/ynh-hooks.ts", string(files[".opencode/plugins/ynh-hooks.ts"]))
	write("driver.mjs", `
import plugin from "./plugins/ynh-hooks.ts"
const hooks = {}
const ctx = {
  location: { directory: process.argv[2] },
  tool: { hook: (n, fn) => { hooks[n] = fn } },
  session: { hook: () => {} },
  event: { subscribe: () => (async function* () {})() },
}
const cleanup = plugin.setup(ctx)
const out = {}
for (const tool of ["shell", "read", "edit", "subagent", "echo_ynh_ping", "my_srv_do_it", "opencode_list_mcp_resources", "unknown_tool"]) {
  try { hooks["execute.before"]({ tool, sessionID: "s1", input: { command: "ls", path: "/a" } }); out[tool] = "ok" }
  catch (e) { out[tool] = "threw: " + e.message }
}
out.after = typeof hooks["execute.after"]
cleanup()
console.log(JSON.stringify(out))
`)

	cmd := exec.Command(node, "--experimental-strip-types", "--no-warnings", filepath.Join(dir, "driver.mjs"), dir)
	cmd.Dir = dir
	raw, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("driver failed: %v\n%s", err, raw)
	}
	var got map[string]string
	if err := json.Unmarshal(raw[bytes.LastIndexByte(bytes.TrimSpace(raw), '\n')+1:], &got); err != nil {
		t.Fatalf("driver output %q: %v", raw, err)
	}
	if got["shell"] != "threw: use make instead" {
		t.Errorf("shell = %q, want the exit-2 hook's message to throw", got["shell"])
	}
	if got["subagent"] != "threw: use make instead" {
		t.Errorf("subagent (Task) = %q, want the Bash|Task matcher to match it", got["subagent"])
	}
	if got["read"] != "ok" {
		t.Errorf("read = %q, want no hook to match", got["read"])
	}
	if got["edit"] != "ok" {
		t.Errorf("edit = %q", got["edit"])
	}
	if got["after"] != "function" {
		t.Errorf("execute.after hook installed = %q", got["after"])
	}
	mcpData, err := os.ReadFile(mcpLog)
	if err != nil {
		t.Fatalf("the mcp__echo__ynh_ping hook did not run: %v", err)
	}
	var mcpStdin map[string]any
	if err := json.Unmarshal(mcpData, &mcpStdin); err != nil {
		t.Fatal(err)
	}
	if mcpStdin["tool_name"] != "mcp__echo__ynh_ping" {
		t.Errorf("MCP tool_name = %v, want Claude's mcp__echo__ynh_ping", mcpStdin["tool_name"])
	}
	// A server whose name holds an underscore maps exactly, on its full name
	// rather than its first underscore.
	underData, err := os.ReadFile(underLog)
	if err != nil {
		t.Fatalf("the mcp__my_srv__do_it hook did not run: %v", err)
	}
	var underStdin map[string]any
	if err := json.Unmarshal(underData, &underStdin); err != nil {
		t.Fatal(err)
	}
	if underStdin["tool_name"] != "mcp__my_srv__do_it" {
		t.Errorf("tool_name for server my_srv = %v, want mcp__my_srv__do_it", underStdin["tool_name"])
	}
	// An id no known server prefixes, and OpenCode's own opencode_* tools,
	// are left as they are.
	otherData, err := os.ReadFile(otherLog)
	if err != nil {
		t.Fatalf("the hook for ids no server owns did not run: %v", err)
	}
	var names []string
	dec := json.NewDecoder(bytes.NewReader(otherData))
	for dec.More() {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			t.Fatal(err)
		}
		names = append(names, m["tool_name"].(string))
	}
	if !slices.Equal(names, []string{"opencode_list_mcp_resources", "unknown_tool"}) {
		t.Errorf("tool_name for ids no server owns = %v, want them unchanged", names)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("the Edit hook did not run for the edit tool: %v", err)
	}
	var stdin map[string]any
	data, err := os.ReadFile(stdinLog)
	if err != nil {
		t.Fatalf("the exit-1 Bash hook did not run (a non-2 exit must not stop later hooks): %v", err)
	}
	if err := json.Unmarshal(data, &stdin); err != nil {
		t.Fatal(err)
	}
	if stdin["hook_event_name"] != "PreToolUse" || stdin["tool_name"] != "Bash" || stdin["session_id"] != "s1" {
		t.Errorf("stdin = %v", stdin)
	}
}

// TestOpenCodeHookTimeout: a per-hook timeout rides in the generated plugin's
// HOOKS table, in seconds, and the plugin hands it to spawnSync in ms; a hook
// without one carries none and gets the 60 s default.
func TestOpenCodeHookTimeout(t *testing.T) {
	hooks := map[string][]plugin.HookEntry{
		"before_tool": {{Command: "slow", Timeout: 12}, {Command: "plain"}},
		"on_stop":     {{Command: "stop", Timeout: 300}},
	}
	files, err := (&OpenCode{}).GenerateHookConfig(hooks)
	if err != nil {
		t.Fatal(err)
	}
	src := string(files[filepath.Join(".opencode", "plugins", "ynh-hooks.ts")])
	start := strings.Index(src, "= {\n")
	end := strings.Index(src, "\n}\n\nconst ROOT")
	if start < 0 || end < 0 {
		t.Fatalf("hook table not found:\n%s", src)
	}
	var table map[string][]opencodeHookEntry
	if err := json.Unmarshal([]byte(src[start+2:end+2]), &table); err != nil {
		t.Fatal(err)
	}
	if got := table["before_tool"]; got[0].Timeout != 12 || got[1].Timeout != 0 {
		t.Errorf("before_tool timeouts = %+v, want 12 then none", got)
	}
	if got := table["on_stop"][0].Timeout; got != 300 {
		t.Errorf("on_stop timeout = %d, want 300 (no cap)", got)
	}
	if strings.Contains(src, "timeout: 60000") || !strings.Contains(src, "timeout: (timeout ?? 60) * 1000") {
		t.Error("the plugin must run each hook with its own timeout, 60 s by default")
	}
	if w := HookWarnings("opencode", hooks); len(w) != 0 {
		t.Errorf("warnings = %q, want none", w)
	}
}

// TestOpenCodeHookDeclarations pins the contract declarations the parity
// scripts read.
func TestOpenCodeHookDeclarations(t *testing.T) {
	if _, ok := opencodeUnsupportedHookEvents["on_session_end"]; !ok {
		t.Error("on_session_end must be declared unsupported, with a reason")
	}
	if sessionStartContext["opencode"] != sessionStartPlain {
		t.Errorf("session-start context = %q, want plain", sessionStartContext["opencode"])
	}
	if sessionContextMode("opencode") != "" {
		t.Error("a plain vendor gets no wrapper")
	}
	field, reason, _, ok := hookTimeoutSupport("opencode")
	if !ok || field != "timeout" || reason != "" {
		t.Errorf("timeout support = %q %q %v", field, reason, ok)
	}
	// on_session_end is not written into the plugin.
	files, err := (&OpenCode{}).GenerateHookConfig(map[string][]plugin.HookEntry{"on_session_end": {{Command: "bye"}}})
	if err != nil || files != nil {
		t.Errorf("on_session_end alone = %v, %v; want no plugin", keysOf(files), err)
	}
}

// TestOpenCodeLaunchWritesNothing: a launch only reads the run dir. The tree is
// prepared first (PrepareLaunch, before the seal) and then made read-only, as a
// sealed snapshot is, and every launch path still starts OpenCode and leaves the
// tree as it was (ynh #765).
func TestOpenCodeLaunchWritesNothing(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes through read-only folders")
	}
	runs := map[string]func(o *OpenCode, c string) error{
		"interactive":     func(o *OpenCode, c string) error { return o.LaunchInteractive(c, nil) },
		"initial prompt":  func(o *OpenCode, c string) error { return o.LaunchWithInitialPrompt(c, "go", nil) },
		"non-interactive": func(o *OpenCode, c string) error { return o.LaunchNonInteractive(c, "go", nil) },
		"resume":          func(o *OpenCode, c string) error { return o.LaunchResume(c, "ses_1", "", true, nil) },
	}
	for name, run := range runs {
		t.Run(name, func(t *testing.T) {
			call := stubOpencode(t, "opencode v2.0.27")
			cfg := t.TempDir()
			oc := filepath.Join(cfg, ".opencode")
			if err := os.MkdirAll(oc, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(cfg, "AGENTS.md"), []byte("base"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(oc, opencodeSessionStartFile), []byte(`[{"command":"echo ctx"}]`), 0o644); err != nil {
				t.Fatal(err)
			}
			o := &OpenCode{}
			if err := o.PrepareLaunch(cfg); err != nil {
				t.Fatal(err)
			}
			before := treeDigest(t, cfg)
			for _, d := range []string{cfg, oc} {
				if err := os.Chmod(d, 0o555); err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(func() {
				_ = os.Chmod(cfg, 0o755)
				_ = os.Chmod(oc, 0o755)
			})
			if err := run(o, cfg); err != nil {
				t.Fatalf("launch on a read-only run dir: %v", err)
			}
			if call.bin == "" {
				t.Error("OpenCode was not started")
			}
			if after := treeDigest(t, cfg); after != before {
				t.Errorf("the launch changed the run dir:\nbefore %s\nafter  %s", before, after)
			}
		})
	}
}

// treeDigest lists every path under root with its size and content.
func treeDigest(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if d.IsDir() {
			fmt.Fprintf(&b, "%s/\n", rel)
			return nil
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		fmt.Fprintf(&b, "%s %d %q\n", rel, len(data), data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}
