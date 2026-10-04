package marketplace

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

// writeHookedHarness writes a harness named name under dir/harnesses whose
// before_tool hook runs command, and returns its marketplace source.
func writeHookedHarness(t *testing.T, dir, name, command string) string {
	t.Helper()
	harnessDir := filepath.Join(dir, "harnesses", name)
	if err := os.MkdirAll(filepath.Join(harnessDir, plugin.PluginDir), 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(harnessDir, plugin.PluginDir, plugin.PluginFile), map[string]any{
		"name":    name,
		"version": "0.1.0",
		"hooks": map[string]any{
			"before_tool": []any{map[string]string{"matcher": "Bash", "command": command}},
		},
	})
	return "./harnesses/" + name
}

// Build returns the export warnings of each harness entry, prefixed with the
// entry's name, and still succeeds (#496). A hook script missing from the
// harness is the warning a merged export reports; an entry that exports
// cleanly and a plugin entry add nothing.
func TestBuild_ReturnsEntryExportWarnings(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    []string
	}{
		{
			name:    "missing hook script",
			command: "./scripts/missing.sh --strict",
			want: []string{
				"reviewer: hook script ./scripts/missing.sh is not a file in the harness, so the plugin does not carry it",
			},
		},
		{name: "clean entry", command: "echo before"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			source := writeHookedHarness(t, dir, "reviewer", tt.command)
			writePluginManifest(t, filepath.Join(dir, "plugins", "my-tool"), "my-tool", "0.2.0", "A plugin")
			configPath := filepath.Join(dir, "marketplace.json")
			writeJSON(t, configPath, map[string]any{
				"name":  "warnings-marketplace",
				"owner": map[string]string{"name": "tester"},
				"harnesses": []map[string]string{
					{"type": "harness", "source": source},
					{"type": "plugin", "source": "./plugins/my-tool"},
				},
			})
			cfg, err := LoadConfig(configPath)
			if err != nil {
				t.Fatal(err)
			}

			warnings, err := Build(cfg, BuildOptions{ConfigDir: dir, OutputDir: filepath.Join(t.TempDir(), "out")})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			if !reflect.DeepEqual(warnings, tt.want) {
				t.Errorf("warnings = %q, want %q", warnings, tt.want)
			}
		})
	}
}
