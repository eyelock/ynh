package permission

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestOrderAndNarrow(t *testing.T) {
	if !None.Narrower(Edits) || !Edits.Narrower(All) || !None.Narrower(All) {
		t.Fatal("order must be all > edits > none")
	}
	if Narrow(All, Edits) != Edits || Narrow(None, All) != None || Narrow(Edits, Edits) != Edits {
		t.Error("Narrow must return the narrower level")
	}
	if _, err := Parse("Edits"); err == nil {
		t.Error("levels are lower case words")
	}
	if l, err := Parse("none"); err != nil || l != None {
		t.Errorf("Parse(none) = %v, %v", l, err)
	}
}

func TestRequest(t *testing.T) {
	if l, err := Request(Edits, ""); err != nil || l != Edits {
		t.Errorf("no request keeps the ceiling: %v %v", l, err)
	}
	if l, err := Request(All, None); err != nil || l != None {
		t.Errorf("narrowing: %v %v", l, err)
	}
	if _, err := Request(Edits, All); err == nil || !strings.Contains(err.Error(), "wider than the ceiling") {
		t.Errorf("wider request must be refused, got %v", err)
	}
	if _, err := Request(All, "most"); err == nil {
		t.Error("unknown request must be refused")
	}
}

// The golden table: every vendor and level, so a P0 correction is one edit.
func TestFor_Golden(t *testing.T) {
	type cell struct {
		args    []string
		env     bool
		refused bool
	}
	want := map[string]map[Level]cell{
		Claude: {
			None: {args: []string{"--permission-mode", "default"}}, Edits: {args: []string{"--permission-mode", "acceptEdits", "--disallowedTools", "Bash"}},
			All: {args: []string{"--permission-mode", "bypassPermissions"}},
		},
		Codex: {
			None: {args: []string{"--sandbox", "read-only"}}, Edits: {refused: true},
			All: {args: []string{"--dangerously-bypass-approvals-and-sandbox"}},
		},
		Cursor: {None: {refused: true}, Edits: {refused: true}, All: {args: []string{"--force"}}},
		OpenCode: {
			None: {args: []string{"--standalone"}, env: true}, Edits: {args: []string{"--standalone"}, env: true},
			All: {args: []string{"--auto"}},
		},
		Copilot: {
			None:  {args: []string{"--no-ask-user", "--deny-tool", "shell", "--deny-tool", "write"}},
			Edits: {args: []string{"--no-ask-user", "--allow-tool", "write", "--deny-tool", "shell"}},
			All:   {args: []string{"--no-ask-user", "--allow-all-tools"}},
		},
	}
	for v, levels := range want {
		for l, c := range levels {
			m, err := For(v, l)
			if c.refused {
				if _, ok := err.(*RefusedError); !ok {
					t.Errorf("%s/%s: want a RefusedError, got %v", v, l, err)
				}
				continue
			}
			if err != nil {
				t.Errorf("%s/%s: %v", v, l, err)
				continue
			}
			if !slices.Equal(m.Args, c.args) || (len(m.Env) > 0) != c.env {
				t.Errorf("%s/%s: args %q env %q, want %q (env %v)", v, l, m.Args, m.Env, c.args, c.env)
			}
		}
	}
	if _, err := For("vim", All); err == nil {
		t.Error("unknown vendor must error")
	}
	if _, err := For(Claude, "x"); err == nil {
		t.Error("unknown level must error")
	}
}

func TestCheckRoot(t *testing.T) {
	none := func(string) string { return "" }
	if err := CheckRoot(Claude, All, 0, none); err == nil || !strings.Contains(err.Error(), "--auto-approve edits") {
		t.Errorf("root + claude + all must be refused naming the fixes, got %v", err)
	}
	for _, c := range []struct {
		v      string
		l      Level
		euid   int
		getenv func(string) string
	}{
		{Claude, Edits, 0, none}, {Claude, All, 1001, none}, {Codex, All, 0, none},
		{Claude, All, 0, func(k string) string {
			if k == "IS_SANDBOX" {
				return "1"
			}
			return ""
		}},
		{Claude, All, 0, func(k string) string {
			if k == "CLAUDE_CODE_BUBBLEWRAP" {
				return "1"
			}
			return ""
		}},
	} {
		if err := CheckRoot(c.v, c.l, c.euid, c.getenv); err != nil {
			t.Errorf("%v: unexpected refusal %v", c, err)
		}
	}
}

func TestCheckOpened(t *testing.T) {
	if err := CheckOpened(Claude, All, "bypassPermissions"); err != nil {
		t.Error(err)
	}
	if err := CheckOpened(Claude, All, "auto"); err == nil || !strings.Contains(err.Error(), `started in "auto"`) {
		t.Errorf("mismatch must stop: %v", err)
	}
	if err := CheckOpened(Claude, Edits, ""); err != nil {
		t.Error("a vendor that reports nothing passes")
	}
	if err := CheckOpened(Codex, All, "whatever"); err != nil {
		t.Error("only Claude reports its mode")
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestProject(t *testing.T) {
	tests := []struct {
		name, vendor string
		files        map[string]string
		wantLevel    Level // "" means no project mode
		wantSub      string
		wantErr      bool
	}{
		{name: "no settings", vendor: Claude},
		{"plan", Claude, map[string]string{".claude/settings.json": `{"permissions":{"defaultMode":"plan"}}`}, None, `permissions.defaultMode "plan"`, false},
		{"default", Claude, map[string]string{".claude/settings.json": `{"permissions":{"defaultMode":"default"}}`}, None, "", false},
		{"local acceptEdits", Claude, map[string]string{".claude/settings.local.json": `{"permissions":{"defaultMode":"acceptEdits"}}`}, Edits, "settings.local.json", false},
		{"bypass", Claude, map[string]string{".claude/settings.json": `{"permissions":{"defaultMode":"bypassPermissions"}}`}, All, "", false},
		{"unplaced mode is none", Claude, map[string]string{".claude/settings.json": `{"permissions":{"defaultMode":"mystery"}}`}, None, "", false},
		{"auto is not placed at edits", Claude, map[string]string{".claude/settings.json": `{"permissions":{"defaultMode":"auto"}}`}, None, "", false},
		{name: "no mode set", vendor: Claude, files: map[string]string{".claude/settings.json": `{"permissions":{"allow":["x"]}}`}},
		{"unparsable", Claude, map[string]string{".claude/settings.json": `{`}, "", "", true},
		{"codex read-only", Codex, map[string]string{".codex/config.toml": "sandbox_mode = \"read-only\"\n"}, None, `sandbox_mode = "read-only"`, false},
		{"codex workspace-write", Codex, map[string]string{".codex/config.toml": "sandbox_mode = \"workspace-write\"\n"}, Edits, "", false},
		{"codex full access", Codex, map[string]string{".codex/config.toml": "sandbox_mode = \"danger-full-access\"\n"}, All, "", false},
		{"codex untrusted policy", Codex, map[string]string{".codex/config.toml": "model=\"x\"\napproval_policy = \"untrusted\"\n"}, None, "approval_policy", false},
		{"codex never asks, no bound", Codex, map[string]string{".codex/config.toml": "approval_policy = \"never\"\n"}, "", "", false},
		{"codex narrowest of two keys", Codex, map[string]string{".codex/config.toml": "sandbox_mode = \"workspace-write\"\napproval_policy = \"untrusted\"\n"}, None, "", false},
		{"codex tables and comments ignored", Codex, map[string]string{".codex/config.toml": "# sandbox_mode = \"read-only\"\n[profiles.ci]\nsandbox_mode = \"read-only\"\n"}, "", "", false},
		{"cursor has none", Cursor, map[string]string{".cursor/cli.json": `{"permissions":{"allow":[]}}`}, "", "", false},
		{"other vendor's file", Codex, map[string]string{".claude/settings.json": `{"permissions":{"defaultMode":"plan"}}`}, "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for rel, c := range tt.files {
				write(t, filepath.Join(dir, rel), c)
			}
			pm, err := Project(dir, tt.vendor)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v", err)
			}
			if tt.wantLevel == "" {
				if pm != nil {
					t.Fatalf("want no project mode, got %+v", pm)
				}
				return
			}
			if pm == nil || pm.Level != tt.wantLevel || !strings.Contains(pm.Describe(), tt.wantSub) {
				t.Errorf("got %+v, want level %s containing %q", pm, tt.wantLevel, tt.wantSub)
			}
		})
	}
}

func TestProject_IgnoresUserSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	write(t, filepath.Join(home, ".claude", "settings.json"), `{"permissions":{"defaultMode":"plan"}}`)
	for _, v := range []string{Claude, Codex, Cursor, OpenCode} {
		if pm, err := Project(t.TempDir(), v); pm != nil || err != nil {
			t.Errorf("%s: %v %v", v, pm, err)
		}
	}
}

func TestResolve(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, ".claude/settings.json"), `{"permissions":{"defaultMode":"acceptEdits"}}`)
	r, err := Resolve(dir, Claude, All)
	if err != nil || r.Level != Edits || !r.FromProject() || !strings.Contains(r.Line, "settings.json") {
		t.Errorf("narrower project must win with a line: %+v %v", r, err)
	}
	r, _ = Resolve(dir, Claude, None)
	if r.Level != None || r.FromProject() || r.Line != "" {
		t.Errorf("a wider project mode loses: %+v", r)
	}
	r, _ = Resolve(t.TempDir(), Claude, All)
	if r.Level != All || r.FromProject() {
		t.Errorf("no project mode: %+v", r)
	}
}

func TestProject_OpensExplicitly(t *testing.T) {
	tests := []struct {
		name, vendor, file, content string
		wantArgs                    []string
		wantOpened                  string
	}{
		{"claude plan", Claude, ".claude/settings.json", `{"permissions":{"defaultMode":"plan"}}`, []string{"--permission-mode", "plan"}, "plan"},
		{"claude acceptEdits is exactly the edits mapping", Claude, ".claude/settings.json", `{"permissions":{"defaultMode":"acceptEdits"}}`, []string{"--permission-mode", "acceptEdits", "--disallowedTools", "Bash"}, "acceptEdits"},
		{"claude unplaced opens default", Claude, ".claude/settings.json", `{"permissions":{"defaultMode":"auto"}}`, []string{"--permission-mode", "default"}, "default"},
		{"codex sandbox", Codex, ".codex/config.toml", "sandbox_mode = \"read-only\"\n", []string{"--sandbox", "read-only"}, ""},
		{"codex approval alone still passes --sandbox", Codex, ".codex/config.toml", "approval_policy = \"untrusted\"\n", []string{"-c", `approval_policy="untrusted"`, "--sandbox", "read-only"}, ""},
		{"codex on-request approval alone is held at read-only", Codex, ".codex/config.toml", "approval_policy = \"on-request\"\n", []string{"-c", `approval_policy="on-request"`, "--sandbox", "read-only"}, ""},
		{"codex both keys", Codex, ".codex/config.toml", "sandbox_mode = \"workspace-write\"\napproval_policy = \"untrusted\"\n", []string{"--sandbox", "workspace-write", "-c", `approval_policy="untrusted"`}, ""},
		{"codex unplaced opens read-only", Codex, ".codex/config.toml", "sandbox_mode = \"strange\"\n", []string{"--sandbox", "read-only"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, filepath.Join(dir, tt.file), tt.content)
			pm, err := Project(dir, tt.vendor)
			if err != nil || pm == nil {
				t.Fatalf("%v %v", pm, err)
			}
			if !slices.Equal(pm.Args, tt.wantArgs) || pm.Opened != tt.wantOpened {
				t.Errorf("args %q opened %q, want %q %q", pm.Args, pm.Opened, tt.wantArgs, tt.wantOpened)
			}
		})
	}
}

// A project-narrowed run is passed the project's mode explicitly, and Claude's
// opened-mode check compares against it, so a wider user-level setting cannot
// widen the run unseen.
func TestResolve_ProjectModeIsPassedExplicitly(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, ".codex/config.toml"), "sandbox_mode = \"read-only\"\n")
	r, err := Resolve(dir, Codex, All)
	if err != nil {
		t.Fatal(err)
	}
	m, err := r.Mapping(Codex)
	if err != nil || !slices.Equal(m.Args, []string{"--sandbox", "read-only"}) {
		t.Errorf("mapping = %+v %v", m, err)
	}
	cdir := t.TempDir()
	write(t, filepath.Join(cdir, ".claude/settings.json"), `{"permissions":{"defaultMode":"plan"}}`)
	r, _ = Resolve(cdir, Claude, All)
	if got := r.OpenedMode(Claude); got != "plan" {
		t.Errorf("opened mode = %q, want plan", got)
	}
	if err := OpenedMismatch(r.OpenedMode(Claude), "bypassPermissions"); err == nil {
		t.Error("a session that opened wider than the project's mode must be stopped")
	}
	// No project mode: the level's own mapping.
	r, _ = Resolve(t.TempDir(), Claude, Edits)
	if m, _ := r.Mapping(Claude); m.Args[1] != "acceptEdits" || r.OpenedMode(Claude) != "acceptEdits" {
		t.Errorf("%+v", m)
	}
}

func TestMergeEnv_KeepsIsolationAndTheCallersContent(t *testing.T) {
	isolation := `{"mcp":{"servers":{"off":{"enabled":false}}},"theme":"dark"}`
	env := []string{"A=1", ConfigContentEnv + "=" + `{"theme":"light"}`, ConfigContentEnv + "=" + isolation}
	m, err := For(OpenCode, Edits)
	if err != nil {
		t.Fatal(err)
	}
	out, err := MergeEnv(env, m.Env)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	var content string
	for _, kv := range out {
		if v, ok := strings.CutPrefix(kv, ConfigContentEnv+"="); ok {
			n++
			content = v
		}
	}
	if n != 1 {
		t.Fatalf("%d copies of %s, want exactly one: %q", n, ConfigContentEnv, out)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(content), &doc); err != nil {
		t.Fatal(err)
	}
	servers := doc["mcp"].(map[string]any)["servers"].(map[string]any)
	if off, _ := servers["off"].(map[string]any); off == nil || off["enabled"] != false {
		t.Errorf("a server isolation turned off came back: %s", content)
	}
	perm := doc["permission"].(map[string]any)
	if perm["edit"] != "allow" || perm["bash"] != "deny" || doc["theme"] != "dark" {
		t.Errorf("permission or content lost: %s", content)
	}
	if !slices.Contains(out, "A=1") {
		t.Error("other variables are kept")
	}
	// Permission wins on its own keys.
	out, _ = MergeEnv([]string{ConfigContentEnv + `={"permission":{"bash":"allow","x":"y"}}`}, m.Env)
	if !strings.Contains(out[0], `"bash":"deny"`) || !strings.Contains(out[0], `"x":"y"`) {
		t.Errorf("%s", out[0])
	}
	if _, err := MergeEnv([]string{ConfigContentEnv + "=["}, m.Env); err == nil {
		t.Error("a non-object must fail")
	}
}

// Only Claude Code's none is still provisional against ynh #1059's final
// results (its project allow rules were not shown to be read in a trusted
// folder). Clearing it is one edit in For.
func TestFor_ProvisionalCells(t *testing.T) {
	var got []string
	for _, v := range []string{Claude, Codex, Cursor, OpenCode, Copilot} {
		for _, l := range []Level{None, Edits, All} {
			if m, err := For(v, l); err == nil && m.Provisional {
				got = append(got, v+"/"+string(l))
			}
		}
	}
	if !slices.Equal(got, []string{"claude/none"}) {
		t.Errorf("provisional cells = %v, want [claude/none]", got)
	}
}

// A Codex project that sets only approval_policy is still passed --sandbox,
// and is reported at the level that sandbox gives: none.
func TestResolve_CodexApprovalOnlyProjectGetsASandbox(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, ".codex/config.toml"), "approval_policy = \"on-request\"\n")
	r, err := Resolve(dir, Codex, All)
	if err != nil || !r.FromProject() || r.Level != None {
		t.Fatalf("%+v %v", r, err)
	}
	m, _ := r.Mapping(Codex)
	if !slices.Contains(m.Args, "--sandbox") || slices.Contains(m.Args, "--dangerously-bypass-approvals-and-sandbox") {
		t.Errorf("args = %q", m.Args)
	}
}
