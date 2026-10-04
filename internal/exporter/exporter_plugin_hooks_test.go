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
