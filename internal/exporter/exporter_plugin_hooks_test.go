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
}

var pluginHookCases = []pluginHookCase{
	{vendor: "claude", manifest: ".claude-plugin/plugin.json", hookFile: "hooks/claude.json", event: "PreToolUse"},
	{vendor: "codex", manifest: ".codex-plugin/plugin.json", hookFile: "hooks/codex.json", event: "PreToolUse"},
	{vendor: "cursor", manifest: ".cursor-plugin/plugin.json", hookFile: "hooks/cursor.json", event: "beforeShellExecution"},
	// Copilot emits no hooks of its own (see Copilot.GenerateHookConfig).
	{vendor: "copilot", manifest: ".claude-plugin/plugin.json"},
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
	for _, c := range cases {
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
// cannot (not in the harness, or outside it). A vendor whose plugin carries
// no hooks (Copilot alone) gets no scripts and no warnings.
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
