package assembler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/resolver"
	"github.com/eyelock/ynh/internal/vendor"
)

// hookHarness writes a harness manifest named name into dir, with hooks
// (event to commands) and includes, and the given files (path to content, all
// executable) beside it.
func hookHarness(t *testing.T, dir, name string, hooks map[string][]string, includes []map[string]any, files ...string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, plugin.PluginDir), 0o755); err != nil {
		t.Fatal(err)
	}
	hj := map[string]any{"name": name, "version": "1.0.0"}
	if len(hooks) > 0 {
		h := map[string][]map[string]string{}
		for event, cmds := range hooks {
			for _, c := range cmds {
				h[event] = append(h[event], map[string]string{"command": c})
			}
		}
		hj["hooks"] = h
	}
	if len(includes) > 0 {
		hj["includes"] = includes
	}
	data, err := json.Marshal(hj)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, plugin.PluginDir, plugin.PluginFile), data, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		path := filepath.Join(dir, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("#!/bin/sh\necho "+name+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func resolveHooks(t *testing.T, root string, sel harness.Selection) (*harness.Harness, []resolver.ResolveResult) {
	t.Helper()
	p, err := harness.LoadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	resolved, _, err := resolver.ResolveSelected(p, nil, sel)
	if err != nil {
		t.Fatal(err)
	}
	return p, resolved
}

func commandsOf(hs HookSet, event string) []string {
	var out []string
	for _, e := range hs.Hooks[event] {
		out = append(out, e.Command)
	}
	return out
}

// An included harness's hooks reach the session only when its include says
// "hooks": true.
func TestComposeHooks_Consent(t *testing.T) {
	tests := []struct {
		name       string
		consent    bool
		wantStop   []string
		wantWarned bool
	}{
		{"consent", true, []string{"echo guard", "echo root"}, false},
		{"no consent", false, []string{"echo root"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := t.TempDir()
			hookHarness(t, filepath.Join(base, "guard"), "guard", map[string][]string{"on_stop": {"echo guard"}, "before_tool": {"echo gate"}}, nil)
			inc := map[string]any{"local": "guard"}
			if tt.consent {
				inc["hooks"] = true
			}
			hookHarness(t, base, "root", map[string][]string{"on_stop": {"echo root"}}, []map[string]any{inc})
			p, resolved := resolveHooks(t, base, harness.Selection{})

			hs, err := ComposeHooks(p.Hooks, resolved)
			if err != nil {
				t.Fatal(err)
			}
			if got := commandsOf(hs, "on_stop"); !slices.Equal(got, tt.wantStop) {
				t.Errorf("on_stop = %q, want %q", got, tt.wantStop)
			}
			if got := len(hs.Hooks["before_tool"]) > 0; got != tt.consent {
				t.Errorf("before_tool carried = %v, want %v", got, tt.consent)
			}
			if !tt.wantWarned {
				if len(hs.Inactive) != 0 {
					t.Errorf("warnings = %q, want none", hs.Inactive)
				}
				return
			}
			want := `included harness guard declares hooks (before_tool, on_stop) that are not active; add "hooks": true to its include to run them`
			if !slices.Equal(hs.Inactive, []string{want}) {
				t.Errorf("warnings = %q, want %q", hs.Inactive, want)
			}
		})
	}
}

// A harness that declares no hooks is never warned about.
func TestComposeHooks_NoHooksNoWarning(t *testing.T) {
	base := t.TempDir()
	hookHarness(t, filepath.Join(base, "plain"), "plain", nil, nil)
	hookHarness(t, base, "root", nil, []map[string]any{{"local": "plain"}})
	p, resolved := resolveHooks(t, base, harness.Selection{})
	hs, err := ComposeHooks(p.Hooks, resolved)
	if err != nil {
		t.Fatal(err)
	}
	if len(hs.Inactive) != 0 || len(hs.Hooks) != 0 {
		t.Errorf("got %v, %q; want nothing", hs.Hooks, hs.Inactive)
	}
}

// Consent must hold at every link of an include chain.
func TestComposeHooks_TransitiveConsent(t *testing.T) {
	tests := []struct {
		name         string
		rootToB      bool
		bToC         bool
		wantBHooks   bool
		wantCHooks   bool
		wantWarnings int
	}{
		{"both links", true, true, true, true, 0},
		{"root to B only", true, false, true, false, 1},
		{"B to C only", false, true, false, false, 2},
		{"neither", false, false, false, false, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := t.TempDir()
			hookHarness(t, filepath.Join(base, "b", "c"), "c", map[string][]string{"on_stop": {"echo c"}}, nil)
			hookHarness(t, filepath.Join(base, "b"), "b", map[string][]string{"on_stop": {"echo b"}}, []map[string]any{{"local": "c", "hooks": tt.bToC}})
			hookHarness(t, base, "root", nil, []map[string]any{{"local": "b", "hooks": tt.rootToB}})
			p, resolved := resolveHooks(t, base, harness.Selection{})
			hs, err := ComposeHooks(p.Hooks, resolved)
			if err != nil {
				t.Fatal(err)
			}
			got := commandsOf(hs, "on_stop")
			if slices.Contains(got, "echo b") != tt.wantBHooks || slices.Contains(got, "echo c") != tt.wantCHooks {
				t.Errorf("on_stop = %q, want b=%v c=%v", got, tt.wantBHooks, tt.wantCHooks)
			}
			if len(hs.Inactive) != tt.wantWarnings {
				t.Errorf("warnings = %q, want %d", hs.Inactive, tt.wantWarnings)
			}
		})
	}
}

// A picked include brings its hooks too, when it consents.
func TestComposeHooks_PickedInclude(t *testing.T) {
	for _, consent := range []bool{true, false} {
		base := t.TempDir()
		hookHarness(t, filepath.Join(base, "guard"), "guard", map[string][]string{"on_stop": {"echo guard"}}, nil)
		hookHarness(t, base, "root", nil, []map[string]any{{"local": "guard", "pick": []string{"skills/x"}, "hooks": consent}})
		p, resolved := resolveHooks(t, base, harness.Selection{})
		hs, err := ComposeHooks(p.Hooks, resolved)
		if err != nil {
			t.Fatal(err)
		}
		if got := len(hs.Hooks["on_stop"]) == 1; got != consent {
			t.Errorf("consent=%v: carried = %v", consent, got)
		}
	}
}

// An included harness's namespaced profile replaces a hook event before the
// hooks are carried; events it does not declare are inherited.
func TestComposeHooks_NamespacedProfile(t *testing.T) {
	base := t.TempDir()
	guard := filepath.Join(base, "guard")
	hookHarness(t, guard, "guard", map[string][]string{"on_stop": {"echo base-stop"}, "before_tool": {"echo base-tool"}}, nil)
	// Add a profile that replaces on_stop.
	manifest := filepath.Join(guard, plugin.PluginDir, plugin.PluginFile)
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var hj map[string]any
	if err := json.Unmarshal(data, &hj); err != nil {
		t.Fatal(err)
	}
	hj["profiles"] = map[string]any{"strict": map[string]any{"hooks": map[string]any{"on_stop": []map[string]string{{"command": "echo strict-stop"}}}}}
	data, _ = json.Marshal(hj)
	if err := os.WriteFile(manifest, data, 0o644); err != nil {
		t.Fatal(err)
	}
	hookHarness(t, base, "root", nil, []map[string]any{{"local": "guard", "hooks": true}})

	p, resolved := resolveHooks(t, base, harness.Selection{Included: map[string]string{"guard": "strict"}})
	hs, err := ComposeHooks(p.Hooks, resolved)
	if err != nil {
		t.Fatal(err)
	}
	if got := commandsOf(hs, "on_stop"); !slices.Equal(got, []string{"echo strict-stop"}) {
		t.Errorf("on_stop = %q, want the profile's", got)
	}
	if got := commandsOf(hs, "before_tool"); !slices.Equal(got, []string{"echo base-tool"}) {
		t.Errorf("before_tool = %q, want the inherited one", got)
	}
}

// Per event, included entries come first in content order (dependencies
// before includers), the root's own last, and nothing is de-duplicated.
func TestComposeHooks_MergeOrder(t *testing.T) {
	base := t.TempDir()
	hookHarness(t, filepath.Join(base, "a", "dep"), "dep", map[string][]string{"on_stop": {"echo dup"}}, nil)
	hookHarness(t, filepath.Join(base, "a"), "a", map[string][]string{"on_stop": {"echo a", "echo dup"}}, []map[string]any{{"local": "dep", "hooks": true}})
	hookHarness(t, filepath.Join(base, "z"), "z", map[string][]string{"on_stop": {"echo z"}}, nil)
	hookHarness(t, base, "root", map[string][]string{"on_stop": {"echo root", "echo dup"}},
		[]map[string]any{{"local": "a", "hooks": true}, {"local": "z", "hooks": true}})
	p, resolved := resolveHooks(t, base, harness.Selection{})
	hs, err := ComposeHooks(p.Hooks, resolved)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"echo dup", "echo a", "echo dup", "echo z", "echo root", "echo dup"}
	if got := commandsOf(hs, "on_stop"); !slices.Equal(got, want) {
		t.Errorf("on_stop = %q, want %q", got, want)
	}
	if len(p.Hooks["on_stop"]) != 2 {
		t.Errorf("root's own hooks were changed: %v", p.Hooks)
	}
}

// A "./" script of an included harness is copied to its own subdirectory and
// the command reaches it, for each vendor that writes session hooks.
func TestWriteComposedSessionHooks_IncludeScripts(t *testing.T) {
	tests := []struct {
		vendor, hookFile, command, scriptRoot string
	}{
		{"claude", ".claude/hooks/hooks.json", `"${CLAUDE_PLUGIN_ROOT}"/scripts/_include/guard/scripts/mark.sh now`, ".claude"},
		{"codex", ".codex/hooks.json", "./scripts/_include/guard/scripts/mark.sh now", "."},
		{"cursor", ".cursor/hooks.json", "./scripts/_include/guard/scripts/mark.sh now", "."},
	}
	for _, tt := range tests {
		t.Run(tt.vendor, func(t *testing.T) {
			base := t.TempDir()
			hookHarness(t, filepath.Join(base, "guard"), "guard", map[string][]string{"on_session_start": {"./scripts/mark.sh now", "/usr/bin/env true"}}, nil, "scripts/mark.sh")
			hookHarness(t, base, "root", map[string][]string{"on_session_start": {"./scripts/mark.sh"}}, []map[string]any{{"local": "guard", "hooks": true}}, "scripts/mark.sh")
			p, resolved := resolveHooks(t, base, harness.Selection{})
			adapter, err := vendor.Get(tt.vendor)
			if err != nil {
				t.Fatal(err)
			}
			runDir := t.TempDir()
			warnings, err := WriteComposedSessionHooks(runDir, adapter, p, resolved)
			if err != nil {
				t.Fatal(err)
			}
			if len(warnings) != 0 {
				t.Errorf("warnings = %q", warnings)
			}
			data, err := os.ReadFile(filepath.Join(runDir, filepath.FromSlash(tt.hookFile)))
			if err != nil {
				t.Fatal(err)
			}
			rootCmd := "./scripts/mark.sh"
			if tt.vendor == "claude" {
				rootCmd = `"${CLAUDE_PLUGIN_ROOT}"/scripts/mark.sh`
			}
			want := []string{tt.command, "/usr/bin/env true", rootCmd}
			if got := sessionHookCommands(t, data); !slices.Equal(got, want) {
				t.Errorf("commands = %q, want %q", got, want)
			}
			// Both scripts exist, apart, and the include's keeps its own content.
			incScript := filepath.Join(runDir, tt.scriptRoot, "scripts", "_include", "guard", "scripts", "mark.sh")
			got, err := os.ReadFile(incScript)
			if err != nil {
				t.Fatalf("included script not carried: %v", err)
			}
			if !strings.Contains(string(got), "echo guard") {
				t.Errorf("included script content = %q", got)
			}
			if info, _ := os.Stat(incScript); info == nil || info.Mode().Perm()&0o111 == 0 {
				t.Errorf("included script is not executable")
			}
			rootScript, err := os.ReadFile(filepath.Join(runDir, tt.scriptRoot, "scripts", "mark.sh"))
			if err != nil || !strings.Contains(string(rootScript), "echo root") {
				t.Errorf("root script = %q, %v", rootScript, err)
			}
		})
	}
}

// Without consent nothing is written for the include, and the warning comes
// back from the writer.
func TestWriteComposedSessionHooks_NoConsent(t *testing.T) {
	base := t.TempDir()
	hookHarness(t, filepath.Join(base, "guard"), "guard", map[string][]string{"on_stop": {"./scripts/mark.sh"}}, nil, "scripts/mark.sh")
	hookHarness(t, base, "root", nil, []map[string]any{{"local": "guard"}})
	p, resolved := resolveHooks(t, base, harness.Selection{})
	adapter, _ := vendor.Get("claude")
	runDir := t.TempDir()
	warnings, err := WriteComposedSessionHooks(runDir, adapter, p, resolved)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 || !strings.HasPrefix(warnings[0], "included harness guard declares hooks (on_stop) that are not active") {
		t.Errorf("warnings = %q", warnings)
	}
	if got := filesUnder(t, runDir); len(got) != 0 {
		t.Errorf("run dir files = %q, want none", got)
	}
}

// Two includes with a script of the same name do not collide, and two
// harnesses sharing a namespace get a stable disambiguator.
func TestComposeHooks_ScriptsDoNotCollide(t *testing.T) {
	base := t.TempDir()
	hookHarness(t, filepath.Join(base, "one"), "same", map[string][]string{"on_stop": {"./scripts/x.sh"}}, nil, "scripts/x.sh")
	hookHarness(t, filepath.Join(base, "two"), "same", map[string][]string{"on_stop": {"./scripts/x.sh"}}, nil, "scripts/x.sh")
	hookHarness(t, filepath.Join(base, "three"), "other", map[string][]string{"on_stop": {"./scripts/x.sh"}}, nil, "scripts/x.sh")
	hookHarness(t, base, "root", nil, []map[string]any{
		{"local": "one", "hooks": true}, {"local": "two", "hooks": true}, {"local": "three", "hooks": true}})
	p, resolved := resolveHooks(t, base, harness.Selection{})

	hs, err := ComposeHooks(p.Hooks, resolved)
	if err != nil {
		t.Fatal(err)
	}
	cmds := commandsOf(hs, "on_stop")
	if len(cmds) != 3 {
		t.Fatalf("commands = %q", cmds)
	}
	seen := map[string]bool{}
	for _, c := range cmds {
		if seen[c] {
			t.Errorf("command %q appears twice", c)
		}
		seen[c] = true
		if !strings.HasPrefix(c, "./scripts/_include/") {
			t.Errorf("command %q is not under the include directory", c)
		}
	}
	if !slices.Contains(cmds, "./scripts/_include/other/scripts/x.sh") {
		t.Errorf("unshared namespace should be used as is: %q", cmds)
	}

	// Stable: composing again names the same places.
	again, err := ComposeHooks(p.Hooks, resolved)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cmds, commandsOf(again, "on_stop")) {
		t.Errorf("not stable: %q then %q", cmds, commandsOf(again, "on_stop"))
	}

	// Each copy comes from its own harness.
	adapter, _ := vendor.Get("codex")
	runDir := t.TempDir()
	if _, err := WriteComposedSessionHooks(runDir, adapter, p, resolved); err != nil {
		t.Fatal(err)
	}
	if got := filesUnder(t, runDir); len(got) != 4 {
		t.Errorf("run dir files = %q, want the hook file and three scripts", got)
	}
}

// A script that climbs out of the included harness is refused, and nothing is
// written.
func TestComposeHooks_ScriptEscapeRefused(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "secret.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	hookHarness(t, filepath.Join(base, "guard"), "guard", map[string][]string{"on_stop": {"./../secret.sh"}}, nil)
	hookHarness(t, base, "root", nil, []map[string]any{{"local": "guard", "hooks": true}})
	p, resolved := resolveHooks(t, base, harness.Selection{})
	adapter, _ := vendor.Get("codex")
	runDir := t.TempDir()
	_, err := WriteComposedSessionHooks(runDir, adapter, p, resolved)
	if err == nil || !strings.Contains(err.Error(), "outside the harness") || !strings.Contains(err.Error(), "guard") {
		t.Fatalf("err = %v, want a refusal naming the include", err)
	}
	if got := filesUnder(t, runDir); len(got) != 0 {
		t.Errorf("run dir files = %q, want none", got)
	}
}

// A script an included harness names but does not ship is a warning that
// names the include, as for the root.
func TestWriteComposedSessionHooks_MissingIncludeScript(t *testing.T) {
	base := t.TempDir()
	hookHarness(t, filepath.Join(base, "guard"), "guard", map[string][]string{"on_stop": {"./scripts/gone.sh"}}, nil)
	hookHarness(t, base, "root", nil, []map[string]any{{"local": "guard", "hooks": true}})
	p, resolved := resolveHooks(t, base, harness.Selection{})
	adapter, _ := vendor.Get("codex")
	warnings, err := WriteComposedSessionHooks(t.TempDir(), adapter, p, resolved)
	if err != nil {
		t.Fatal(err)
	}
	want := "hook script ./scripts/gone.sh is not a file in included harness guard, so the session does not carry it"
	if !slices.Equal(warnings, []string{want}) {
		t.Errorf("warnings = %q, want %q", warnings, want)
	}
}
