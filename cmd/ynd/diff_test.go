package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func createDiffHarness(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Create plugin.json with hooks and MCP
	hj := map[string]any{
		"name":           "diff-test",
		"version":        "1.0.0",
		"description":    "Test harness for diff",
		"default_vendor": "claude",
		"hooks": map[string]any{
			"after_tool": []map[string]any{
				{"command": "echo done"},
			},
		},
		"mcp_servers": map[string]any{
			"diff-server": map[string]any{
				"command": "python",
				"args":    []string{"-m", "server"},
			},
		},
	}
	data, _ := json.MarshalIndent(hj, "", "  ")
	if err := writePluginJSONFile(dir, data); err != nil {
		t.Fatal(err)
	}

	// Create a skill
	skillDir := filepath.Join(dir, "skills", "diff-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: diff-skill\n---\nDiff skill.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create instructions
	if err := os.WriteFile(filepath.Join(dir, "instructions.md"), []byte("# Diff Test\nInstructions.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestCmdDiffTwoVendors(t *testing.T) {
	srcDir := createDiffHarness(t)

	err := cmdDiff([]string{srcDir, "claude", "cursor"})
	if err != nil {
		t.Fatalf("cmdDiff failed: %v", err)
	}
}

func TestCmdDiffAllVendors(t *testing.T) {
	srcDir := createDiffHarness(t)

	err := cmdDiff([]string{srcDir})
	if err != nil {
		t.Fatalf("cmdDiff failed: %v", err)
	}
}

func TestCmdDiffClaudeCodex(t *testing.T) {
	srcDir := createDiffHarness(t)

	err := cmdDiff([]string{srcDir, "claude", "codex"})
	if err != nil {
		t.Fatalf("cmdDiff failed: %v", err)
	}
}

func TestCmdDiffVendorFlag(t *testing.T) {
	srcDir := createDiffHarness(t)

	err := cmdDiff([]string{srcDir, "-v", "claude,cursor"})
	if err != nil {
		t.Fatalf("cmdDiff with -v failed: %v", err)
	}
}

func TestCmdDiffHarnessFlag(t *testing.T) {
	srcDir := createDiffHarness(t)

	err := cmdDiff([]string{"--harness", srcDir})
	if err != nil {
		t.Fatalf("cmdDiff with --harness failed: %v", err)
	}
}

func TestCmdDiffHarnessEnvVar(t *testing.T) {
	srcDir := createDiffHarness(t)

	t.Setenv("YNH_HARNESS", srcDir)

	err := cmdDiff(nil)
	if err != nil {
		t.Fatalf("cmdDiff with YNH_HARNESS failed: %v", err)
	}
}

func TestCmdDiffMissingSource(t *testing.T) {
	err := cmdDiff([]string{})
	if err == nil {
		t.Fatal("expected error for missing source")
	}
}

func TestCmdDiffBadSource(t *testing.T) {
	err := cmdDiff([]string{"./nonexistent-dir"})
	if err == nil {
		t.Fatal("expected error for nonexistent source")
	}
}

func TestCmdDiffBadVendor(t *testing.T) {
	srcDir := createDiffHarness(t)
	err := cmdDiff([]string{srcDir, "bogus", "claude"})
	if err == nil {
		t.Fatal("expected error for unknown vendor")
	}
	if !strings.Contains(err.Error(), "unknown vendor") {
		t.Errorf("expected 'unknown vendor' error, got: %v", err)
	}
}

func TestCmdDiffSingleVendor(t *testing.T) {
	srcDir := createDiffHarness(t)
	err := cmdDiff([]string{srcDir, "claude"})
	if err == nil {
		t.Fatal("expected error for single vendor")
	}
	if !strings.Contains(err.Error(), "at least 2 vendors") {
		t.Errorf("expected '2 vendors' error, got: %v", err)
	}
}

// captureDiff runs cmdDiff and returns what it printed.
func captureDiff(t *testing.T, args []string) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	runErr := cmdDiff(args)
	os.Stdout = orig
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if runErr != nil {
		t.Fatalf("cmdDiff failed: %v", runErr)
	}
	return string(out)
}

// section returns the lines listed under heading within one pairing's block.
func section(out, pairing, heading string) []string {
	_, block, ok := strings.Cut(out, "=== "+pairing+" ===\n")
	if !ok {
		return nil
	}
	block, _, _ = strings.Cut(block, "\n\n")
	var lines []string
	in := false
	for _, line := range strings.Split(block, "\n") {
		if !strings.HasPrefix(line, "  ") {
			in = line == heading
			continue
		}
		if in {
			lines = append(lines, strings.TrimSpace(line))
		}
	}
	return lines
}

// Cursor writes .cursor/hooks.json and codex .codex/hooks.json. Both sit at
// hooks.json under their config dir, so mapping the whole config dir paired
// them and reported .cursor/hooks.json as "Different content", as though
// codex had that file (#452). Hook config is each vendor's own file and is
// reported as only in that vendor in every pairing, while a skill assembled
// under each vendor's directory still pairs.
func TestCmdDiff_VendorOnlyFilesAreNotPaired(t *testing.T) {
	srcDir := createDiffHarness(t)
	out := captureDiff(t, []string{srcDir, "claude", "cursor", "codex"})

	cases := []struct {
		pairing, heading, file string
	}{
		{"claude vs cursor", "Only in claude:", ".claude/hooks/hooks.json"},
		{"claude vs cursor", "Only in cursor:", ".cursor/hooks.json"},
		{"claude vs codex", "Only in claude:", ".claude/hooks/hooks.json"},
		{"claude vs codex", "Only in codex:", ".codex/hooks.json"},
		{"cursor vs codex", "Only in cursor:", ".cursor/hooks.json"},
		{"cursor vs codex", "Only in codex:", ".codex/hooks.json"},
		{"cursor vs codex", "Only in cursor:", ".cursor/mcp.json"},
		{"claude vs cursor", "Identical:", ".claude/skills/diff-skill/SKILL.md"},
		{"cursor vs codex", "Identical:", ".cursor/skills/diff-skill/SKILL.md"},
	}
	for _, c := range cases {
		t.Run(c.pairing+"/"+c.file, func(t *testing.T) {
			if !slices.Contains(section(out, c.pairing, c.heading), c.file) {
				t.Errorf("%s: %s not listed under %q\n%s", c.pairing, c.file, c.heading, out)
			}
		})
	}
	if got := section(out, "cursor vs codex", "Different content:"); slices.Contains(got, ".cursor/hooks.json") {
		t.Errorf(".cursor/hooks.json paired with codex's hooks file: %v", got)
	}
}

// Codex assembles .codex-plugin/plugin.json and copilot, in a run dir,
// .copilot/.claude-plugin/plugin.json, but diff looked for each vendor's plugin
// manifest in its marketplace index directory (.agents/plugins, .github/plugin).
// Neither manifest was ever paired, so every pairing with codex or copilot
// listed the manifests as only in each side (#453). The manifests are the same
// artifact in every vendor and pair; codex's carries path pointers the others
// do not, so its content differs.
func TestCmdDiff_PluginManifestsPair(t *testing.T) {
	srcDir := createDiffHarness(t)
	out := captureDiff(t, []string{srcDir, "claude", "cursor", "codex", "copilot"})

	cases := []struct {
		pairing, heading, file string
	}{
		{"claude vs cursor", "Identical:", ".claude-plugin/plugin.json"},
		{"claude vs codex", "Different content:", ".claude-plugin/plugin.json"},
		{"cursor vs codex", "Different content:", ".cursor-plugin/plugin.json"},
		{"claude vs copilot", "Identical:", ".claude-plugin/plugin.json"},
		{"cursor vs copilot", "Identical:", ".cursor-plugin/plugin.json"},
		{"codex vs copilot", "Different content:", ".codex-plugin/plugin.json"},
	}
	for _, c := range cases {
		t.Run(c.pairing+"/"+c.file, func(t *testing.T) {
			if !slices.Contains(section(out, c.pairing, c.heading), c.file) {
				t.Errorf("%s: %s not listed under %q\n%s", c.pairing, c.file, c.heading, out)
			}
		})
	}

	for _, pairing := range []string{"claude vs codex", "cursor vs codex", "claude vs copilot", "codex vs copilot"} {
		a, b, _ := strings.Cut(pairing, " vs ")
		for _, v := range []string{a, b} {
			for _, f := range section(out, pairing, "Only in "+v+":") {
				if strings.HasSuffix(f, "plugin.json") {
					t.Errorf("%s: manifest %s listed as only in %s\n%s", pairing, f, v, out)
				}
			}
		}
	}
}
