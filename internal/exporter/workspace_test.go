package exporter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// workspace is a request only the operator running the harness can grant, so
// no export carries it, and each export says so rather than dropping it quietly.
func TestExportWarnsWorkspaceIsNotCarried(t *testing.T) {
	const warning = "workspace is not carried into exports: 2 requested folder(s) dropped"
	write := func(t *testing.T, workspace string) string {
		dir := t.TempDir()
		manifest := `{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"seat","version":"0.1.0"` + workspace + `}`
		path := filepath.Join(dir, ".agents", "harness", "plugin.json")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
		skill := filepath.Join(dir, "skills", "a", "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(skill), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(skill, []byte("---\nname: a\ndescription: d\n---\nbody\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	has := func(results []ExportResult) bool {
		for _, r := range results {
			for _, w := range r.Warnings {
				if strings.Contains(w, warning) {
					return true
				}
			}
		}
		return false
	}
	for _, mode := range []struct {
		name string
		mode ExportMode
	}{{"per vendor", ModePerVendor}, {"merged", ModeMerged}, {"agent plugin", ModeAgentPlugin}} {
		t.Run(mode.name, func(t *testing.T) {
			opts := ExportOptions{OutputDir: filepath.Join(t.TempDir(), "out"), Vendors: []string{"claude", "cursor"}, Mode: mode.mode}

			opts.SourceDir = write(t, `,"workspace":["../ynh","../ynm"]`)
			results, err := Export(opts)
			if err != nil {
				t.Fatal(err)
			}
			if !has(results) {
				t.Errorf("no workspace warning in %+v", results)
			}

			opts.SourceDir = write(t, "")
			opts.OutputDir = filepath.Join(t.TempDir(), "out")
			results, err = Export(opts)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range results {
				for _, w := range r.Warnings {
					if strings.Contains(w, "workspace") {
						t.Errorf("warning without a workspace: %s", w)
					}
				}
			}
		})
	}
}
