package marketplace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

// A marketplace package is a merged export: one plugin root that every vendor
// reads. Each vendor's manifest names its own hooks file in its own format,
// and there is no shared hooks/hooks.json for one vendor to read another's
// format from (#468, #469).
func TestMarketplaceHarnessPluginHooks(t *testing.T) {
	dir := t.TempDir()
	harnessDir := filepath.Join(dir, "harnesses", "hooked")
	if err := os.MkdirAll(filepath.Join(harnessDir, plugin.PluginDir), 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(harnessDir, plugin.PluginDir, plugin.PluginFile), map[string]any{
		"name":    "hooked",
		"version": "0.1.0",
		"hooks": map[string]any{
			"before_tool": []any{map[string]string{"matcher": "Bash", "command": "echo before"}},
		},
	})
	configPath := filepath.Join(dir, "marketplace.json")
	writeJSON(t, configPath, map[string]any{
		"name":      "hooks-marketplace",
		"owner":     map[string]string{"name": "tester"},
		"harnesses": []map[string]string{{"type": "harness", "source": "./harnesses/hooked"}},
	})

	outputDir := t.TempDir()
	buildOnce(t, configPath, dir, outputDir)
	pkg := filepath.Join(outputDir, "plugins", "hooked")

	assertFileNotExists(t, filepath.Join(pkg, "hooks", "hooks.json"))
	assertFileNotExists(t, filepath.Join(pkg, ".claude", "hooks", "hooks.json"))
	assertFileNotExists(t, filepath.Join(pkg, ".codex", "hooks.json"))
	assertFileNotExists(t, filepath.Join(pkg, ".cursor", "hooks.json"))

	for _, tt := range []struct {
		manifest, hookFile, event string
	}{
		{".claude-plugin/plugin.json", "hooks/claude.json", "PreToolUse"},
		{".codex-plugin/plugin.json", "hooks/codex.json", "PreToolUse"},
		{".cursor-plugin/plugin.json", "hooks/cursor.json", "beforeShellExecution"},
	} {
		var m struct {
			Hooks string `json:"hooks"`
		}
		readJSON(t, filepath.Join(pkg, tt.manifest), &m)
		if m.Hooks != "./"+tt.hookFile {
			t.Errorf("%s hooks = %q, want %q", tt.manifest, m.Hooks, "./"+tt.hookFile)
		}
		var doc struct {
			Hooks map[string]json.RawMessage `json:"hooks"`
		}
		readJSON(t, filepath.Join(pkg, tt.hookFile), &doc)
		if _, ok := doc.Hooks[tt.event]; !ok || len(doc.Hooks) != 1 {
			t.Errorf("%s events = %v, want only %s", tt.hookFile, doc.Hooks, tt.event)
		}
	}
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
}
