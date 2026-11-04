package exporter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

// writeIncludedHooksHarness writes a root that includes the harness "guard"
// (consenting or not), which runs ./scripts/mark.sh on before_tool. Root and
// guard each ship a scripts/mark.sh, so a collision would show.
func writeIncludedHooksHarness(t *testing.T, consent bool) string {
	t.Helper()
	root := t.TempDir()
	writeJSON(t, filepath.Join(root, plugin.PluginDir, plugin.PluginFile), map[string]any{
		"name": "root", "version": "0.1.0",
		"includes": []any{map[string]any{"local": "guard", "hooks": consent}},
		"hooks": map[string]any{
			"before_tool": []any{map[string]string{"command": "./scripts/mark.sh"}},
		},
	})
	writeJSON(t, filepath.Join(root, "guard", plugin.PluginDir, plugin.PluginFile), map[string]any{
		"name": "guard", "version": "0.1.0",
		"hooks": map[string]any{
			"before_tool": []any{map[string]string{"command": "./scripts/mark.sh now"}},
		},
	})
	for dir, body := range map[string]string{root: "root", filepath.Join(root, "guard"): "guard"} {
		if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "scripts", "mark.sh"), []byte("#!/bin/sh\necho "+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// An export carries the hooks of an included harness that consented, with
// their scripts in the include's own place beside the root's, for every
// vendor whose plugin carries hooks.
func TestExport_IncludedHooksAndScripts(t *testing.T) {
	srcDir := writeIncludedHooksHarness(t, true)
	for _, c := range pluginHookCases {
		if c.hookFile == "" {
			continue
		}
		t.Run(c.vendor, func(t *testing.T) {
			outputDir := t.TempDir()
			results, err := Export(ExportOptions{SourceDir: srcDir, OutputDir: outputDir, Vendors: []string{c.vendor}, Mode: ModePerVendor})
			if err != nil {
				t.Fatalf("Export: %v", err)
			}
			if len(results[0].Warnings) != 0 {
				t.Errorf("warnings = %q, want none", results[0].Warnings)
			}
			vdir := filepath.Join(outputDir, c.vendor)
			data, err := os.ReadFile(filepath.Join(vdir, filepath.FromSlash(c.hookFile)))
			if err != nil {
				t.Fatal(err)
			}
			doc := string(data)
			guard := strings.Index(doc, "scripts/_include/guard/scripts/mark.sh now")
			root := strings.LastIndex(doc, "/scripts/mark.sh")
			if guard < 0 || root < 0 || guard > root {
				t.Errorf("hook file should run the include's script first, then the root's:\n%s", doc)
			}
			for path, want := range map[string]string{
				"scripts/mark.sh":                        "root",
				"scripts/_include/guard/scripts/mark.sh": "guard",
			} {
				got, err := os.ReadFile(filepath.Join(vdir, filepath.FromSlash(path)))
				if err != nil || !strings.Contains(string(got), "echo "+want) {
					t.Errorf("%s = %q, %v; want the %s script", path, got, err, want)
				}
			}
		})
	}

	t.Run("merged", func(t *testing.T) {
		outputDir := filepath.Join(t.TempDir(), "merged")
		if _, err := Export(ExportOptions{SourceDir: srcDir, OutputDir: outputDir, Vendors: []string{"claude", "codex", "cursor"}, Mode: ModeMerged}); err != nil {
			t.Fatalf("Export: %v", err)
		}
		if _, err := os.Stat(filepath.Join(outputDir, "scripts", "_include", "guard", "scripts", "mark.sh")); err != nil {
			t.Errorf("included script not exported: %v", err)
		}
	})
}

// Without consent the export carries nothing of the include's and warns.
func TestExport_IncludedHooksWithoutConsent(t *testing.T) {
	srcDir := writeIncludedHooksHarness(t, false)
	outputDir := t.TempDir()
	results, err := Export(ExportOptions{SourceDir: srcDir, OutputDir: outputDir, Vendors: []string{"claude", "codex"}, Mode: ModePerVendor})
	if err != nil {
		t.Fatal(err)
	}
	want := `included harness guard declares hooks (before_tool) that are not active; add "hooks": true to its include to run them`
	n := 0
	for _, r := range results {
		for _, w := range r.Warnings {
			if w == want {
				n++
			}
		}
	}
	if n != 1 {
		t.Errorf("warning appeared %d times in %v, want once", n, results)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "claude", "scripts", "_include")); !os.IsNotExist(err) {
		t.Errorf("an unconsented include's script was exported, stat err = %v", err)
	}
}
