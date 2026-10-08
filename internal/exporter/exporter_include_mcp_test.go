package exporter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

// writeHarnessWithIncludedMCP builds a root harness that includes a harness
// declaring a portable server ("free") and one that points into its own
// directory ("tied"). rootExtra is merged into the root manifest.
func writeHarnessWithIncludedMCP(t *testing.T, rootExtra map[string]any) string {
	t.Helper()
	src := t.TempDir()
	writeJSON(t, filepath.Join(src, "inc", plugin.PluginDir, plugin.PluginFile), map[string]any{
		"name": "inc", "version": "1.0.0",
		"mcp_servers": map[string]any{
			"free": map[string]any{"command": "npx", "args": []string{"-y", "pkg"}},
			"tied": map[string]any{"command": "./bin/tied"},
		},
	})
	root := map[string]any{
		"name": "root", "version": "1.0.0",
		"includes": []map[string]any{{"local": "inc"}},
	}
	for k, v := range rootExtra {
		root[k] = v
	}
	writeJSON(t, filepath.Join(src, plugin.PluginDir, plugin.PluginFile), root)
	return src
}

func TestExport_IncludedMCPServers(t *testing.T) {
	modes := []struct {
		name string
		mode ExportMode
	}{{"per-vendor", ModePerVendor}, {"merged", ModeMerged}, {"agent-plugin", ModeAgentPlugin}}

	// An included server that points into its include cannot be exported.
	for _, m := range modes {
		t.Run(m.name+"/path-relative server is refused", func(t *testing.T) {
			src := writeHarnessWithIncludedMCP(t, nil)
			_, err := Export(ExportOptions{SourceDir: src, OutputDir: filepath.Join(t.TempDir(), "out"), Vendors: []string{"claude"}, Mode: m.mode})
			want := `included MCP server "tied" from inc uses a path inside the include and cannot be exported; declare it in the root harness`
			if err == nil || err.Error() != want {
				t.Fatalf("error = %v, want %q", err, want)
			}
		})

		t.Run(m.name+"/removed or overridden server exports", func(t *testing.T) {
			src := writeHarnessWithIncludedMCP(t, map[string]any{
				"mcp_servers": map[string]any{
					"tied": nil,
					"free": map[string]any{"command": "npx", "args": []string{"-y", "mine"}},
				},
			})
			out := filepath.Join(t.TempDir(), "out")
			if _, err := Export(ExportOptions{SourceDir: src, OutputDir: out, Vendors: []string{"claude"}, Mode: m.mode}); err != nil {
				t.Fatalf("Export: %v", err)
			}
			var all strings.Builder
			for _, f := range mcpFilesUnder(t, out) {
				data, err := os.ReadFile(filepath.Join(out, f))
				if err != nil {
					t.Fatal(err)
				}
				all.Write(data)
			}
			if !strings.Contains(all.String(), `"mine"`) || strings.Contains(all.String(), "tied") {
				t.Errorf("MCP output should carry the root's free server and not tied:\n%s", all.String())
			}
		})
	}

	t.Run("portable included server is exported", func(t *testing.T) {
		src := writeHarnessWithIncludedMCP(t, map[string]any{"mcp_servers": map[string]any{"tied": nil}})
		out := filepath.Join(t.TempDir(), "out")
		if _, err := Export(ExportOptions{SourceDir: src, OutputDir: out, Vendors: []string{"claude"}, Mode: ModeMerged}); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(out, "mcp", "claude.json"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), `"free"`) || !strings.Contains(string(data), "npx") {
			t.Errorf("included server missing from export:\n%s", data)
		}
	})
}
