package exporter

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

// pluginMCPCase is what one vendor's plugin must carry for MCP servers: the
// file it reads, the manifest it reads, and the "mcpServers" pointer that
// manifest carries ("" when the vendor reads its file by default).
type pluginMCPCase struct {
	vendor   string
	manifest string
	mcpFile  string
	pointer  string
}

var pluginMCPCases = []pluginMCPCase{
	// A Claude plugin reads .mcp.json at its root, which is Codex's in a
	// merged package, or what "mcpServers" names (#481).
	{vendor: "claude", manifest: ".claude-plugin/plugin.json", mcpFile: "mcp/claude.json", pointer: "./mcp/claude.json"},
	{vendor: "codex", manifest: ".codex-plugin/plugin.json", mcpFile: ".mcp.json", pointer: "./.mcp.json"},
	// Cursor discovers mcp.json at its plugin root; its manifest names nothing.
	{vendor: "cursor", manifest: ".cursor-plugin/plugin.json", mcpFile: "mcp.json"},
	// Copilot reads .github/mcp.json by default. It shares Claude's manifest,
	// which names Claude's file when Claude is in the package.
	{vendor: "copilot", manifest: ".claude-plugin/plugin.json", mcpFile: ".github/mcp.json"},
}

func writeMCPHarness(t *testing.T) string {
	t.Helper()
	srcDir := t.TempDir()
	writeJSON(t, filepath.Join(srcDir, plugin.PluginDir, plugin.PluginFile), map[string]any{
		"name":    "plugin-mcp",
		"version": "0.1.0",
		"mcp_servers": map[string]any{
			"github": map[string]any{"command": "npx", "args": []string{"-y", "server"}},
		},
	})
	if err := os.MkdirAll(filepath.Join(srcDir, "skills", "s"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "skills", "s", "SKILL.md"), []byte("---\nname: s\ndescription: d\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return srcDir
}

// mcpFilesUnder lists every file under root whose name mentions mcp,
// relative to root and slash-separated.
func mcpFilesUnder(t *testing.T, root string) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if !d.IsDir() && strings.Contains(filepath.ToSlash(rel), "mcp") {
			found = append(found, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	sort.Strings(found)
	return found
}

// manifestMCP returns the manifest's "mcpServers" field, or "" when it has none.
func manifestMCP(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading manifest: %v", err)
	}
	var m struct {
		MCPServers string `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	return m.MCPServers
}

// assertPluginMCP checks that root carries exactly the given vendors' MCP
// files, one per vendor, each declaring the harness's server, and that every
// manifest pointer names a file that is there.
func assertPluginMCP(t *testing.T, root string, cases []pluginMCPCase) {
	t.Helper()
	var want []string
	hasClaude := false
	for _, c := range cases {
		want = append(want, c.mcpFile)
		hasClaude = hasClaude || c.vendor == "claude"
	}
	sort.Strings(want)
	if got := mcpFilesUnder(t, root); !slices.Equal(got, want) {
		t.Errorf("MCP files = %v, want %v", got, want)
	}

	for _, c := range cases {
		data, err := os.ReadFile(filepath.Join(root, c.mcpFile))
		if err != nil {
			t.Errorf("%s: %v", c.vendor, err)
			continue
		}
		var doc struct {
			MCPServers map[string]json.RawMessage `json:"mcpServers"`
		}
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Errorf("%s: parsing %s: %v", c.vendor, c.mcpFile, err)
		} else if _, ok := doc.MCPServers["github"]; !ok {
			t.Errorf("%s: %s has no github server:\n%s", c.vendor, c.mcpFile, data)
		}

		wantPointer := c.pointer
		if c.vendor == "copilot" && hasClaude {
			wantPointer = "./mcp/claude.json"
		}
		got := manifestMCP(t, filepath.Join(root, c.manifest))
		if got != wantPointer {
			t.Errorf("%s: %s mcpServers = %q, want %q", c.vendor, c.manifest, got, wantPointer)
		}
		if got != "" {
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(got))); err != nil {
				t.Errorf("%s: mcpServers names a missing file: %v", c.vendor, err)
			}
		}
	}
}

// A plugin export carries MCP servers where that vendor's plugin reads them,
// and its manifest names the file when the vendor reads it through the
// manifest. Nothing lands at a session-only path such as .claude/.mcp.json
// (#481) or .copilot/.mcp.json.
func TestExportPerVendor_PluginMCP(t *testing.T) {
	srcDir := writeMCPHarness(t)
	for _, c := range pluginMCPCases {
		t.Run(c.vendor, func(t *testing.T) {
			outputDir := t.TempDir()
			if _, err := Export(ExportOptions{
				SourceDir: srcDir,
				OutputDir: outputDir,
				Vendors:   []string{c.vendor},
				Mode:      ModePerVendor,
			}); err != nil {
				t.Fatalf("Export: %v", err)
			}
			assertPluginMCP(t, filepath.Join(outputDir, c.vendor), []pluginMCPCase{c})
		})
	}
}

// A merged package shares one plugin root, so every vendor's MCP file must
// be its own and every manifest pointer must be written after the files it
// names (#482: Codex's pointer was missing because its manifest came first).
func TestExportMerged_PluginMCP(t *testing.T) {
	srcDir := writeMCPHarness(t)
	tests := []struct {
		name    string
		vendors []string
	}{
		{"codex only", []string{"codex"}},
		{"claude and codex", []string{"claude", "codex"}},
		{"all vendors", []string{"claude", "codex", "copilot", "cursor"}},
		{"copilot before claude", []string{"copilot", "cursor", "codex", "claude"}},
		{"copilot without claude", []string{"copilot", "cursor"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputDir := filepath.Join(t.TempDir(), "merged")
			if _, err := Export(ExportOptions{
				SourceDir: srcDir,
				OutputDir: outputDir,
				Vendors:   tt.vendors,
				Mode:      ModeMerged,
			}); err != nil {
				t.Fatalf("Export: %v", err)
			}
			var cases []pluginMCPCase
			for _, c := range pluginMCPCases {
				if slices.Contains(tt.vendors, c.vendor) {
					cases = append(cases, c)
				}
			}
			assertPluginMCP(t, outputDir, cases)
		})
	}
}

// A harness without MCP servers gets no MCP file and no pointer.
func TestExport_NoMCPNoManifestPointer(t *testing.T) {
	srcDir := t.TempDir()
	writeJSON(t, filepath.Join(srcDir, plugin.PluginDir, plugin.PluginFile), map[string]any{
		"name":    "no-mcp",
		"version": "0.1.0",
	})
	outputDir := filepath.Join(t.TempDir(), "merged")
	if _, err := Export(ExportOptions{
		SourceDir: srcDir,
		OutputDir: outputDir,
		Vendors:   []string{"claude", "codex", "copilot", "cursor"},
		Mode:      ModeMerged,
	}); err != nil {
		t.Fatalf("Export: %v", err)
	}
	for _, m := range []string{".claude-plugin/plugin.json", ".codex-plugin/plugin.json"} {
		if got := manifestMCP(t, filepath.Join(outputDir, m)); got != "" {
			t.Errorf("%s mcpServers = %q, want none", m, got)
		}
	}
	if got := mcpFilesUnder(t, outputDir); len(got) != 0 {
		t.Errorf("MCP files = %v, want none", got)
	}
}
