package vendor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

// sessionPluginHookGen is a vendor whose session and plugin hook files live at
// different paths.
type sessionPluginHookGen interface {
	Adapter
	GeneratePluginHookConfig(map[string][]plugin.HookEntry) (map[string][]byte, error)
}

// TestHookConfigPaths locks which file each hook generator writes. A session
// (`ynh run`, `ynd preview`, the agent loop) reads the vendor's project or
// --plugin-dir path; a plugin package carries hooks/<vendor>.json, which the
// vendor's manifest names (#454, #468, #469). Each generator writes exactly
// one file, and both render the same document for a command without a "./"
// script (see TestHookCommandRoots for one with).
func TestHookConfigPaths(t *testing.T) {
	hooks := map[string][]plugin.HookEntry{
		"on_stop": {{Command: "echo done"}},
	}
	tests := []struct {
		adapter sessionPluginHookGen
		session string
		plugin  string
	}{
		{&Claude{}, filepath.Join(".claude", "hooks", "hooks.json"), filepath.Join("hooks", "claude.json")},
		{&Codex{}, filepath.Join(".codex", "hooks.json"), filepath.Join("hooks", "codex.json")},
		{&Cursor{}, filepath.Join(".cursor", "hooks.json"), filepath.Join("hooks", "cursor.json")},
	}
	for _, tt := range tests {
		t.Run(tt.adapter.Name(), func(t *testing.T) {
			session, err := tt.adapter.GenerateHookConfig(hooks)
			if err != nil {
				t.Fatal(err)
			}
			plug, err := tt.adapter.GeneratePluginHookConfig(hooks)
			if err != nil {
				t.Fatal(err)
			}
			if len(session) != 1 || session[tt.session] == nil {
				t.Errorf("session hook files = %v, want only %s", keysOf(session), tt.session)
			}
			if len(plug) != 1 || plug[tt.plugin] == nil {
				t.Errorf("plugin hook files = %v, want only %s", keysOf(plug), tt.plugin)
			}
			if string(session[tt.session]) != string(plug[tt.plugin]) {
				t.Error("session and plugin hook documents differ")
			}

			for _, none := range []map[string][]plugin.HookEntry{nil, {"not_an_event": {{Command: "x"}}}} {
				got, err := tt.adapter.GeneratePluginHookConfig(none)
				if err != nil {
					t.Fatal(err)
				}
				if got != nil {
					t.Errorf("no mapped events: got %v, want nil", keysOf(got))
				}
			}
		})
	}
}

// TestPluginManifestHooksPointer: a manifest names the vendor's plugin hook
// file when it is in the output directory, and omits "hooks" otherwise, since
// a plugin loader rejects a path that does not exist. A session layout (no
// hooks/<vendor>.json at the root) never gets a pointer.
func TestPluginManifestHooksPointer(t *testing.T) {
	hj := &plugin.HarnessJSON{Name: "h", Version: "1.0.0"}
	tests := []struct {
		adapter  Adapter
		hookFile string
		manifest string
	}{
		{&Claude{}, "claude.json", filepath.Join(".claude-plugin", "plugin.json")},
		{&Codex{}, "codex.json", filepath.Join(".codex-plugin", "plugin.json")},
		{&Cursor{}, "cursor.json", filepath.Join(".cursor-plugin", "plugin.json")},
	}
	for _, tt := range tests {
		t.Run(tt.adapter.Name(), func(t *testing.T) {
			dir := t.TempDir()
			if got := manifestHooksField(t, tt.adapter, hj, dir, tt.manifest); got != "" {
				t.Errorf("without hooks file: hooks = %q, want none", got)
			}
			if err := os.MkdirAll(filepath.Join(dir, "hooks"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "hooks", tt.hookFile), []byte("{}"), 0o644); err != nil {
				t.Fatal(err)
			}
			if got, want := manifestHooksField(t, tt.adapter, hj, dir, tt.manifest), "./hooks/"+tt.hookFile; got != want {
				t.Errorf("with hooks file: hooks = %q, want %q", got, want)
			}
		})
	}
}

// Copilot reads .claude-plugin/plugin.json, the same file Claude writes. In a
// merged package both adapters write it, so they must render the same bytes
// or whichever writes last decides whether Claude's hooks and MCP pointers
// survive.
func TestCopilotManifestMatchesClaude(t *testing.T) {
	hj := &plugin.HarnessJSON{Name: "h", Version: "1.0.0", Description: "d", Keywords: []string{"k"}}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hooks", "claude.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "mcp", "claude.json"))
	key := filepath.Join(".claude-plugin", "plugin.json")
	claude, err := (&Claude{}).GeneratePluginManifest(hj, dir)
	if err != nil {
		t.Fatal(err)
	}
	copilot, err := (&Copilot{}).GenerateExportPluginManifest(hj, dir)
	if err != nil {
		t.Fatal(err)
	}
	if string(claude[key]) != string(copilot[key]) {
		t.Errorf("manifests differ:\nclaude:  %s\ncopilot: %s", claude[key], copilot[key])
	}
}

func manifestHooksField(t *testing.T, a Adapter, hj *plugin.HarnessJSON, dir, manifest string) string {
	t.Helper()
	files, err := a.GeneratePluginManifest(hj, dir)
	if err != nil {
		t.Fatal(err)
	}
	data, ok := files[manifest]
	if !ok {
		t.Fatalf("no %s in %v", manifest, keysOf(files))
	}
	var m struct {
		Hooks string `json:"hooks"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m.Hooks
}

// hookCommands returns every "command" string a hooks document declares, in
// document order, whatever the vendor's nesting.
func hookCommands(t *testing.T, data []byte) []string {
	t.Helper()
	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parsing hooks document: %v", err)
	}
	var cmds []string
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if c, ok := x["command"].(string); ok {
				cmds = append(cmds, c)
			}
			for _, k := range sortedKeys(x) {
				walk(x[k])
			}
		case []any:
			for _, e := range x {
				walk(e)
			}
		}
	}
	walk(doc)
	return cmds
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestHookCommandRoots: a "./" hook command names a script relative to
// something, and what that is depends on where the hooks are read (#483). A
// plugin's script ships inside the plugin, so a plugin hook file anchors it
// to the vendor's plugin-root variable. A session file keeps what it had:
// Claude anchors to $CLAUDE_PROJECT_DIR, Codex and Cursor leave it as
// written. Absolute, already-anchored and PATH-style commands are never
// touched.
func TestHookCommandRoots(t *testing.T) {
	untouched := []string{"/usr/local/bin/lint.sh", "$CLAUDE_PROJECT_DIR/x.sh", "make check", "tools/x.sh", "../x.sh"}
	var entries []plugin.HookEntry
	entries = append(entries, plugin.HookEntry{Command: "./scripts/guard.sh --strict"})
	for _, c := range untouched {
		entries = append(entries, plugin.HookEntry{Command: c})
	}
	hooks := map[string][]plugin.HookEntry{"on_stop": entries}

	tests := []struct {
		adapter sessionPluginHookGen
		session string
		plugin  string
	}{
		{&Claude{}, "$CLAUDE_PROJECT_DIR/scripts/guard.sh --strict", `"${CLAUDE_PLUGIN_ROOT}"/scripts/guard.sh --strict`},
		{&Codex{}, "./scripts/guard.sh --strict", `"${PLUGIN_ROOT}"/scripts/guard.sh --strict`},
		{&Cursor{}, "./scripts/guard.sh --strict", `"${CURSOR_PLUGIN_ROOT}"/scripts/guard.sh --strict`},
	}
	for _, tt := range tests {
		t.Run(tt.adapter.Name(), func(t *testing.T) {
			session, err := tt.adapter.GenerateHookConfig(hooks)
			if err != nil {
				t.Fatal(err)
			}
			plug, err := tt.adapter.GeneratePluginHookConfig(hooks)
			if err != nil {
				t.Fatal(err)
			}
			for name, tc := range map[string]struct {
				files map[string][]byte
				want  string
			}{"session": {session, tt.session}, "plugin": {plug, tt.plugin}} {
				if len(tc.files) != 1 {
					t.Fatalf("%s: files = %v, want one", name, keysOf(tc.files))
				}
				for _, data := range tc.files {
					want := append([]string{tc.want}, untouched...)
					if got := hookCommands(t, data); !slices.Equal(got, want) {
						t.Errorf("%s commands = %q, want %q", name, got, want)
					}
				}
			}
		})
	}
}
