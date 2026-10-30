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

// pluginHookCase is what one vendor's plugin must carry for hooks: the file
// its manifest points at, the manifest that points at it, and an event name
// that only that vendor's format uses. An empty hookFile means the vendor's
// plugin carries no hooks.
type pluginHookCase struct {
	vendor   string
	manifest string
	hookFile string
	event    string
	// unnamed: the vendor finds hookFile by default, so its manifest does not
	// name it (Copilot's hooks.json).
	unnamed bool
}

var pluginHookCases = []pluginHookCase{
	{vendor: "claude", manifest: ".claude-plugin/plugin.json", hookFile: "hooks/claude.json", event: "PreToolUse"},
	{vendor: "codex", manifest: ".codex-plugin/plugin.json", hookFile: "hooks/codex.json", event: "PreToolUse"},
	{vendor: "cursor", manifest: ".cursor-plugin/plugin.json", hookFile: "hooks/cursor.json", event: "preToolUse"},
	// Copilot reads hooks.json at the plugin root by default; its manifest is
	// Claude's and names no hooks file of Copilot's (see
	// Copilot.GeneratePluginHookConfig).
	{vendor: "copilot", manifest: ".claude-plugin/plugin.json", hookFile: "hooks.json", event: "PreToolUse", unnamed: true},
}

func writeHooksHarness(t *testing.T) string {
	t.Helper()
	srcDir := t.TempDir()
	writeJSON(t, filepath.Join(srcDir, plugin.PluginDir, plugin.PluginFile), map[string]any{
		"name":    "plugin-hooks",
		"version": "0.1.0",
		"hooks": map[string]any{
			"before_tool": []any{
				map[string]string{"matcher": "Bash", "command": "echo before"},
			},
		},
	})
	return srcDir
}

// hookFilesUnder lists every file under root whose path mentions hooks,
// relative to root and slash-separated.
func hookFilesUnder(t *testing.T, root string) []string {
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
		if !d.IsDir() && strings.Contains(rel, "hook") {
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

// manifestHooks returns the manifest's "hooks" field, or "" when it has none.
func manifestHooks(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading manifest: %v", err)
	}
	var m struct {
		Hooks string `json:"hooks"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	return m.Hooks
}

// hookEvents returns the event names a hooks file declares.
func hookEvents(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading hooks file: %v", err)
	}
	var doc struct {
		Hooks map[string]json.RawMessage `json:"hooks"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	var events []string
	for e := range doc.Hooks {
		events = append(events, e)
	}
	sort.Strings(events)
	return events
}

// assertPluginHooks checks that root carries exactly the given vendors' hook
// files, each in that vendor's format and named by that vendor's manifest.
func assertPluginHooks(t *testing.T, root string, cases []pluginHookCase) {
	t.Helper()
	var want []string
	hasClaude := slices.ContainsFunc(cases, func(o pluginHookCase) bool { return o.vendor == "claude" })
	for _, c := range cases {
		if c.unnamed && hasClaude {
			// Copilot loads the file Claude's manifest names, not its own.
			continue
		}
		if c.hookFile != "" {
			want = append(want, c.hookFile)
		}
	}
	sort.Strings(want)
	if got := hookFilesUnder(t, root); !slices.Equal(got, want) {
		t.Errorf("hook files = %v, want %v", got, want)
	}

	for _, c := range cases {
		got := manifestHooks(t, filepath.Join(root, c.manifest))
		if c.unnamed && hasClaude {
			continue
		}
		if c.unnamed {
			if events := hookEvents(t, filepath.Join(root, c.hookFile)); !slices.Equal(events, []string{c.event}) {
				t.Errorf("%s: %s events = %v, want [%s]", c.vendor, c.hookFile, events, c.event)
			}
			continue
		}
		if c.hookFile == "" {
			// Copilot shares Claude's manifest, which may legitimately
			// point at Claude's file when both are present.
			if c.vendor == "copilot" && slices.ContainsFunc(cases, func(o pluginHookCase) bool { return o.vendor == "claude" }) {
				continue
			}
			if got != "" {
				t.Errorf("%s: manifest hooks = %q, want none", c.vendor, got)
			}
			continue
		}
		if want := "./" + c.hookFile; got != want {
			t.Errorf("%s: manifest hooks = %q, want %q", c.vendor, got, want)
		}
		events := hookEvents(t, filepath.Join(root, c.hookFile))
		if !slices.Equal(events, []string{c.event}) {
			t.Errorf("%s: %s events = %v, want [%s]", c.vendor, c.hookFile, events, c.event)
		}
	}
}

// A plugin export carries hooks where that vendor's plugin loader reads them:
// a vendor-specific file under hooks/ that its manifest's "hooks" field names.
// No hooks/hooks.json, which Claude Code always loads from a plugin root and
// Copilot reads by default, so it could never hold one vendor's format
// safely, and no project-session paths (.claude/hooks, .codex/hooks.json,
// .cursor/hooks.json), which a plugin never reads (#468).
func TestExportPerVendor_PluginHooks(t *testing.T) {
	srcDir := writeHooksHarness(t)
	for _, c := range pluginHookCases {
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
			assertPluginHooks(t, filepath.Join(outputDir, c.vendor), []pluginHookCase{c})
		})
	}
}

// A merged package shares one plugin root across vendors, so each vendor's
// manifest points at its own hooks file and no vendor can read another's
// format (#469). The vendor order must not matter: Copilot writes the same
// .claude-plugin/plugin.json Claude does, and must not drop Claude's pointer.
func TestExportMerged_PluginHooks(t *testing.T) {
	srcDir := writeHooksHarness(t)
	tests := []struct {
		name    string
		vendors []string
	}{
		{"claude and cursor", []string{"claude", "cursor"}},
		{"all vendors", []string{"claude", "codex", "copilot", "cursor"}},
		{"copilot before claude", []string{"copilot", "cursor", "codex", "claude"}},
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
			var cases []pluginHookCase
			for _, c := range pluginHookCases {
				if slices.Contains(tt.vendors, c.vendor) {
					cases = append(cases, c)
				}
			}
			assertPluginHooks(t, outputDir, cases)
		})
	}
}

// A harness without hooks gets no hooks pointer in any manifest: the field
// names a file, and a plugin loader rejects a path that does not exist.
func TestExport_NoHooksNoManifestPointer(t *testing.T) {
	srcDir := t.TempDir()
	writeJSON(t, filepath.Join(srcDir, plugin.PluginDir, plugin.PluginFile), map[string]any{
		"name":    "no-hooks",
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
	for _, m := range []string{".claude-plugin/plugin.json", ".codex-plugin/plugin.json", ".cursor-plugin/plugin.json"} {
		if got := manifestHooks(t, filepath.Join(outputDir, m)); got != "" {
			t.Errorf("%s hooks = %q, want none", m, got)
		}
	}
	if got := hookFilesUnder(t, outputDir); len(got) != 0 {
		t.Errorf("hook files = %v, want none", got)
	}
}

// writeScriptHooksHarness is a harness whose hooks run a script it ships
// (scripts/guard.sh), a "./" script it does not ship, one that climbs out of
// the harness, and a PATH-style command.
func writeScriptHooksHarness(t *testing.T) string {
	t.Helper()
	srcDir := t.TempDir()
	writeJSON(t, filepath.Join(srcDir, plugin.PluginDir, plugin.PluginFile), map[string]any{
		"name":    "script-hooks",
		"version": "0.1.0",
		"hooks": map[string]any{
			"before_tool": []any{
				map[string]string{"matcher": "Bash", "command": "./scripts/guard.sh --strict"},
			},
			"on_stop": []any{
				map[string]string{"command": "./scripts/missing.sh"},
				map[string]string{"command": "./../outside.sh"},
				map[string]string{"command": "make check"},
			},
		},
	})
	if err := os.MkdirAll(filepath.Join(srcDir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "scripts", "guard.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return srcDir
}

// assertHookScripts checks that root carries the shipped script, executable
// and byte-identical, and nothing for the scripts the harness does not ship.
func assertHookScripts(t *testing.T, root string, want bool) {
	t.Helper()
	info, err := os.Stat(filepath.Join(root, "scripts", "guard.sh"))
	if !want {
		if !os.IsNotExist(err) {
			t.Errorf("scripts/guard.sh should not be exported, stat err = %v", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("hook script not exported: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("hook script mode = %v, want executable", info.Mode())
	}
	if _, err := os.Stat(filepath.Join(root, "scripts", "missing.sh")); !os.IsNotExist(err) {
		t.Errorf("missing.sh appeared in the export, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "outside.sh")); !os.IsNotExist(err) {
		t.Errorf("outside.sh appeared beside the export, stat err = %v", err)
	}
}

// assertScriptWarnings checks that the export names each "./" script it could
// not ship, and only those.
func assertScriptWarnings(t *testing.T, warnings []string) {
	t.Helper()
	joined := strings.Join(warnings, "\n")
	for _, s := range []string{"./scripts/missing.sh", "./../outside.sh"} {
		if !strings.Contains(joined, s) {
			t.Errorf("warnings %q do not name %s", warnings, s)
		}
	}
	if strings.Contains(joined, "guard.sh") || strings.Contains(joined, "make check") {
		t.Errorf("warnings %q name a command that needs none", warnings)
	}
}

// A plugin hook anchors a "./" script to the plugin root (#483), so the
// script must be in the plugin: an export copies each script a hook runs from
// the harness tree to the same path in the plugin, and warns about any it
// cannot (not in the harness, or outside it).
func TestExport_PluginHookScripts(t *testing.T) {
	srcDir := writeScriptHooksHarness(t)
	for _, c := range pluginHookCases {
		t.Run(c.vendor, func(t *testing.T) {
			outputDir := t.TempDir()
			results, err := Export(ExportOptions{
				SourceDir: srcDir,
				OutputDir: outputDir,
				Vendors:   []string{c.vendor},
				Mode:      ModePerVendor,
			})
			if err != nil {
				t.Fatalf("Export: %v", err)
			}
			if len(results) != 1 {
				t.Fatalf("results = %d, want 1", len(results))
			}
			ships := c.hookFile != ""
			assertHookScripts(t, filepath.Join(outputDir, c.vendor), ships)
			if ships {
				assertScriptWarnings(t, results[0].Warnings)
			} else if len(results[0].Warnings) != 0 {
				t.Errorf("warnings = %q, want none", results[0].Warnings)
			}
		})
	}

	t.Run("merged", func(t *testing.T) {
		outputDir := filepath.Join(t.TempDir(), "merged")
		results, err := Export(ExportOptions{
			SourceDir: srcDir,
			OutputDir: outputDir,
			Vendors:   []string{"claude", "codex", "copilot", "cursor"},
			Mode:      ModeMerged,
		})
		if err != nil {
			t.Fatalf("Export: %v", err)
		}
		assertHookScripts(t, outputDir, true)
		if len(results) != 1 {
			t.Fatalf("results = %d, want 1", len(results))
		}
		assertScriptWarnings(t, results[0].Warnings)
		if n := strings.Count(strings.Join(results[0].Warnings, "\n"), "missing.sh"); n != 1 {
			t.Errorf("missing.sh warned %d times, want once", n)
		}
	})
}

// A hook a vendor cannot carry as declared is reported on the export, never
// dropped silently: Cursor documents a matcher for tool events only, so a
// matcher on on_session_start is warned about, naming the event and the vendor.
func TestExport_HookWarnings(t *testing.T) {
	srcDir := t.TempDir()
	writeJSON(t, filepath.Join(srcDir, plugin.PluginDir, plugin.PluginFile), map[string]any{
		"name":    "hook-warn",
		"version": "0.1.0",
		"hooks": map[string]any{
			"on_session_start": []any{map[string]string{"matcher": "startup", "command": "echo hi"}},
			"on_session_end":   []any{map[string]string{"command": "echo bye"}},
		},
	})
	for _, mode := range []ExportMode{ModePerVendor, ModeMerged, ModeAgentPlugin} {
		results, err := Export(ExportOptions{SourceDir: srcDir, OutputDir: filepath.Join(t.TempDir(), "out"), Vendors: []string{"cursor", "claude"}, Mode: mode})
		if err != nil {
			t.Fatalf("mode %v: %v", mode, err)
		}
		var warnings []string
		for _, r := range results {
			warnings = append(warnings, r.Warnings...)
		}
		joined := strings.Join(warnings, "\n")
		if !strings.Contains(joined, "on_session_start") || !strings.Contains(joined, "cursor") {
			t.Errorf("mode %v: warnings = %q, want one naming on_session_start and cursor", mode, warnings)
		}
		if strings.Contains(joined, "claude") {
			t.Errorf("mode %v: claude takes a SessionStart matcher, got %q", mode, warnings)
		}
	}
}

func writeMCPScriptHarness(t *testing.T) string {
	t.Helper()
	srcDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(srcDir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "scripts", "serve.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(srcDir, plugin.PluginDir, plugin.PluginFile), map[string]any{
		"name":    "mcp-scripts",
		"version": "0.1.0",
		"hooks":   map[string]any{"on_stop": []any{map[string]string{"command": "./scripts/serve.sh"}}},
		"mcp_servers": map[string]any{
			"local":   map[string]any{"command": "./scripts/serve.sh", "cwd": "./scripts"},
			"missing": map[string]any{"command": "./scripts/missing.sh"},
		},
	})
	return srcDir
}

// An MCP server run by a "./" script is exported with the script in the
// package and the command anchored to the vendor's plugin-root placeholder, as
// hook commands are. Codex documents no placeholder, so it is left as written
// and warned about. A script that is not in the harness is warned about too.
func TestExport_MCPScripts(t *testing.T) {
	srcDir := writeMCPScriptHarness(t)
	cases := []struct {
		vendor, file, want string
	}{
		{"claude", "mcp/claude.json", "${CLAUDE_PLUGIN_ROOT}/scripts/serve.sh"},
		{"cursor", "mcp.json", "${CURSOR_PLUGIN_ROOT}/scripts/serve.sh"},
		{"copilot", ".github/mcp.json", "${PLUGIN_ROOT}/scripts/serve.sh"},
		{"codex", ".mcp.json", "./scripts/serve.sh"},
	}
	for _, c := range cases {
		t.Run(c.vendor, func(t *testing.T) {
			outputDir := t.TempDir()
			results, err := Export(ExportOptions{SourceDir: srcDir, OutputDir: outputDir, Vendors: []string{c.vendor}, Mode: ModePerVendor})
			if err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(outputDir, c.vendor)
			data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(c.file)))
			if err != nil {
				t.Fatal(err)
			}
			var doc struct {
				MCPServers map[string]struct {
					Command string `json:"command"`
					Cwd     string `json:"cwd"`
				} `json:"mcpServers"`
			}
			if err := json.Unmarshal(data, &doc); err != nil {
				t.Fatal(err)
			}
			if got := doc.MCPServers["local"].Command; got != c.want {
				t.Errorf("command = %q, want %q", got, c.want)
			}
			if _, err := os.Stat(filepath.Join(dir, "scripts", "serve.sh")); err != nil {
				t.Errorf("script not in the package: %v", err)
			}
			joined := strings.Join(results[0].Warnings, "\n")
			if !strings.Contains(joined, "mcp script ./scripts/missing.sh is not a file in the harness") {
				t.Errorf("no missing-script warning: %q", results[0].Warnings)
			}
			if gotWarn := strings.Contains(joined, "documents no plugin-root placeholder"); gotWarn != (c.vendor == "codex") {
				t.Errorf("placeholder warning = %v for %s: %q", gotWarn, c.vendor, results[0].Warnings)
			}
		})
	}

	t.Run("agent plugin keeps the specification's ./ and ships the script", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "ap")
		if _, err := Export(ExportOptions{SourceDir: srcDir, OutputDir: out, Vendors: []string{"copilot"}, Mode: ModeAgentPlugin}); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(out, "mcp.json"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), `"command": "./scripts/serve.sh"`) {
			t.Errorf("mcp.json:\n%s", data)
		}
		if _, err := os.Stat(filepath.Join(out, "scripts", "serve.sh")); err != nil {
			t.Errorf("script not in the package: %v", err)
		}
	})
}

// In a merged package Copilot loads the hooks file the shared manifest names
// (Claude's) and ignores a root hooks.json (CONFIRMED live, Copilot CLI
// 1.0.105), so none is written when Claude is in the package; a Copilot-only
// export or a merge without Claude keeps it.
func TestExportMerged_CopilotRootHooksOnlyWithoutClaude(t *testing.T) {
	srcDir := writeHooksHarness(t)
	for _, tc := range []struct {
		vendors  []string
		wantRoot bool
	}{
		{[]string{"claude", "copilot"}, false},
		{[]string{"copilot", "claude", "cursor"}, false},
		{[]string{"copilot", "cursor"}, true},
		{[]string{"copilot"}, true},
	} {
		out := filepath.Join(t.TempDir(), "m")
		if _, err := Export(ExportOptions{SourceDir: srcDir, OutputDir: out, Vendors: tc.vendors, Mode: ModeMerged}); err != nil {
			t.Fatal(err)
		}
		_, err := os.Stat(filepath.Join(out, "hooks.json"))
		if (err == nil) != tc.wantRoot {
			t.Errorf("%v: root hooks.json present = %v, want %v", tc.vendors, err == nil, tc.wantRoot)
		}
	}
}
