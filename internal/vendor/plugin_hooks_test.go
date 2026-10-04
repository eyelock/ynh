package vendor

import (
	"encoding/json"
	"os"
	"path/filepath"
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
// one file, and both render the same document.
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
// or whichever writes last decides whether Claude's hooks pointer survives.
func TestCopilotManifestMatchesClaude(t *testing.T) {
	hj := &plugin.HarnessJSON{Name: "h", Version: "1.0.0", Description: "d", Keywords: []string{"k"}}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hooks", "claude.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
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
