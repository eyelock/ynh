package exporter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestExportSingleVendorCodex(t *testing.T) {
	srcDir := filepath.Join(testdataDir(), "export-harness")
	outputDir := t.TempDir()

	results, err := Export(ExportOptions{
		SourceDir: srcDir,
		OutputDir: outputDir,
		Vendors:   []string{"codex"},
		Mode:      ModePerVendor,
	})
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	r := results[0]
	codexDir := filepath.Join(outputDir, "codex")

	// Check skills/ layout (at plugin root, not .agents/skills/)
	assertFileExists(t, filepath.Join(codexDir, "skills", "dev-project", "SKILL.md"))
	assertFileExists(t, filepath.Join(codexDir, "skills", "dev-quality", "SKILL.md"))

	// Check .codex-plugin/plugin.json manifest
	assertFileExists(t, filepath.Join(codexDir, ".codex-plugin", "plugin.json"))

	// Check AGENTS.md present
	assertFileExists(t, filepath.Join(codexDir, "AGENTS.md"))

	// No agents/rules/commands directories at top level
	assertFileNotExists(t, filepath.Join(codexDir, "agents"))
	assertFileNotExists(t, filepath.Join(codexDir, "rules"))
	assertFileNotExists(t, filepath.Join(codexDir, "commands"))

	// No plugin manifest
	assertFileNotExists(t, filepath.Join(codexDir, ".codex"))

	// Should have warnings about skipped artifacts
	if len(r.Warnings) == 0 {
		t.Error("expected warnings about skipped artifacts")
	}
	foundSkipWarning := false
	for _, w := range r.Warnings {
		if strings.Contains(w, "codex: skipping") {
			foundSkipWarning = true
		}
	}
	if !foundSkipWarning {
		t.Error("expected Codex skip warning")
	}
}

func TestExportCodexLayout(t *testing.T) {
	srcDir := filepath.Join(testdataDir(), "export-harness")
	outputDir := t.TempDir()

	_, err := Export(ExportOptions{
		SourceDir: srcDir,
		OutputDir: outputDir,
		Vendors:   []string{"codex"},
	})
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	codexDir := filepath.Join(outputDir, "codex")

	// Skills must be under skills/ (plugin root format)
	entries, err := os.ReadDir(filepath.Join(codexDir, "skills"))
	if err != nil {
		t.Fatalf("reading skills: %v", err)
	}

	skillNames := make(map[string]bool)
	for _, e := range entries {
		skillNames[e.Name()] = true
	}

	if !skillNames["dev-project"] {
		t.Error("missing dev-project skill")
	}
	if !skillNames["dev-quality"] {
		t.Error("missing dev-quality skill")
	}
}

func TestExportCodexSkipsAgentsRulesCommands(t *testing.T) {
	srcDir := filepath.Join(testdataDir(), "export-harness")
	outputDir := t.TempDir()

	results, err := Export(ExportOptions{
		SourceDir: srcDir,
		OutputDir: outputDir,
		Vendors:   []string{"codex"},
	})
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	r := results[0]
	codexDir := filepath.Join(outputDir, "codex")

	// These should NOT exist
	for _, name := range []string{"agents", "rules", "commands"} {
		path := filepath.Join(codexDir, name)
		if _, err := os.Stat(path); err == nil {
			t.Errorf("%s directory should not exist in Codex export", name)
		}
	}

	// Should have warnings
	if len(r.Warnings) == 0 {
		t.Error("expected warnings about skipped artifacts")
	}
}

// TestExportMergedCodex pins #479: merged export, which ynd marketplace build
// also uses, carries Codex's manifest whenever codex is a selected vendor, and
// no vendor list means all of them. The shared tree holds agents for the other
// vendors, so the Codex manifest must point at skills and nothing else.
func TestExportMergedCodex(t *testing.T) {
	srcDir := filepath.Join(testdataDir(), "export-harness")

	tests := []struct {
		name      string
		vendors   []string
		wantCodex bool
	}{
		{name: "default vendors", vendors: nil, wantCodex: true},
		{name: "codex selected", vendors: []string{"claude", "codex"}, wantCodex: true},
		{name: "codex not selected", vendors: []string{"claude", "cursor"}, wantCodex: false},
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
				t.Fatalf("Export failed: %v", err)
			}

			manifest := filepath.Join(outputDir, ".codex-plugin", "plugin.json")
			if !tt.wantCodex {
				assertFileNotExists(t, manifest)
				return
			}
			data, err := os.ReadFile(manifest)
			if err != nil {
				t.Fatalf("reading Codex manifest: %v", err)
			}
			var got map[string]any
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("parsing Codex manifest: %v", err)
			}
			if got["skills"] != "./skills/" {
				t.Errorf("skills = %v, want ./skills/", got["skills"])
			}
			for _, key := range []string{"agents", "rules", "commands"} {
				if _, ok := got[key]; ok {
					t.Errorf("Codex manifest declares %q; Codex loads skills only", key)
				}
			}
			// The shared tree still carries agents for the other vendors.
			assertFileExists(t, filepath.Join(outputDir, "agents", "planner.md"))
		})
	}
}

// TestExportMergedCodexNote pins #488: a merged package keeps agents, rules
// and commands in the shared tree for the other vendors, but Codex's manifest
// points at ./skills/ only. The export says so in the same words as a
// per-vendor Codex export, and only when Codex is selected and the harness
// has something Codex does not load.
func TestExportMergedCodexNote(t *testing.T) {
	exportHarness := filepath.Join(testdataDir(), "export-harness")
	perVendor, err := Export(ExportOptions{
		SourceDir: exportHarness,
		OutputDir: t.TempDir(),
		Vendors:   []string{"codex"},
		Mode:      ModePerVendor,
	})
	if err != nil {
		t.Fatalf("per-vendor Export: %v", err)
	}
	wantCodex := perVendor[0].Warnings
	if len(wantCodex) == 0 {
		t.Fatal("per-vendor Codex export gave no note to compare against")
	}

	tests := []struct {
		name    string
		src     string
		vendors []string
		want    []string
	}{
		{name: "codex selected", src: exportHarness, vendors: []string{"claude", "codex", "cursor"}, want: wantCodex},
		{name: "codex not selected", src: exportHarness, vendors: []string{"claude", "cursor"}},
		{name: "skills only", src: writeMCPHarness(t), vendors: []string{"claude", "codex"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := Export(ExportOptions{
				SourceDir: tt.src,
				OutputDir: filepath.Join(t.TempDir(), "merged"),
				Vendors:   tt.vendors,
				Mode:      ModeMerged,
			})
			if err != nil {
				t.Fatalf("Export: %v", err)
			}
			var codex []string
			for _, w := range results[0].Warnings {
				if strings.HasPrefix(w, "codex: ") {
					codex = append(codex, w)
				}
			}
			if !slices.Equal(codex, tt.want) {
				t.Errorf("codex notes = %q, want %q", codex, tt.want)
			}
		})
	}
}
