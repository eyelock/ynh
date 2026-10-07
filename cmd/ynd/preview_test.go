package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/harness"
)

func createPreviewHarness(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Create plugin.json with hooks and MCP servers
	hj := map[string]any{
		"name":           "preview-test",
		"version":        "1.0.0",
		"description":    "Test harness for preview",
		"default_vendor": "claude",
		"hooks": map[string]any{
			"before_tool": []map[string]any{
				{"command": "echo before"},
			},
		},
		"mcp_servers": map[string]any{
			"test-server": map[string]any{
				"command": "node",
				"args":    []string{"server.js"},
			},
		},
	}
	data, _ := json.MarshalIndent(hj, "", "  ")
	if err := writePluginJSONFile(dir, data); err != nil {
		t.Fatal(err)
	}

	// Create a skill
	skillDir := filepath.Join(dir, "skills", "test-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: test-skill\n---\nTest skill content.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create instructions
	if err := os.WriteFile(filepath.Join(dir, "instructions.md"), []byte("# Preview Test\nInstructions here.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestCmdPreviewDefault(t *testing.T) {
	srcDir := createPreviewHarness(t)

	// Preview to stdout (capture by just running without error)
	err := cmdPreview([]string{srcDir})
	if err != nil {
		t.Fatalf("cmdPreview failed: %v", err)
	}
}

func TestCmdPreviewClaude(t *testing.T) {
	srcDir := createPreviewHarness(t)

	err := cmdPreview([]string{srcDir, "-v", "claude"})
	if err != nil {
		t.Fatalf("cmdPreview failed: %v", err)
	}
}

func TestCmdPreviewCursor(t *testing.T) {
	srcDir := createPreviewHarness(t)

	err := cmdPreview([]string{srcDir, "-v", "cursor"})
	if err != nil {
		t.Fatalf("cmdPreview failed: %v", err)
	}
}

func TestCmdPreviewCodex(t *testing.T) {
	srcDir := createPreviewHarness(t)

	err := cmdPreview([]string{srcDir, "-v", "codex"})
	if err != nil {
		t.Fatalf("cmdPreview failed: %v", err)
	}
}

func TestCmdPreviewWithOutput(t *testing.T) {
	srcDir := createPreviewHarness(t)
	outputDir := filepath.Join(t.TempDir(), "preview-out")

	err := cmdPreview([]string{srcDir, "-v", "claude", "-o", outputDir})
	if err != nil {
		t.Fatalf("cmdPreview failed: %v", err)
	}

	// Verify output contains assembled files
	assertExists(t, filepath.Join(outputDir, "CLAUDE.md"))

	// Should have skill
	entries, err := os.ReadDir(filepath.Join(outputDir, ".claude", "skills"))
	if err != nil {
		t.Fatalf("reading skills dir: %v", err)
	}
	if len(entries) == 0 {
		t.Error("expected skills in output")
	}
}

func TestCmdPreviewWithHooks(t *testing.T) {
	srcDir := createPreviewHarness(t)
	outputDir := filepath.Join(t.TempDir(), "preview-hooks")

	err := cmdPreview([]string{srcDir, "-v", "claude", "-o", outputDir})
	if err != nil {
		t.Fatalf("cmdPreview failed: %v", err)
	}

	// Claude hooks go in .claude/hooks/hooks.json (plugin format)
	hooksPath := filepath.Join(outputDir, ".claude", "hooks", "hooks.json")
	assertExists(t, hooksPath)

	data, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "hooks") {
		t.Error("expected hooks.json to contain hooks config")
	}
}

// TestCmdPreviewCursorHooksOnlyWhereCursorReads locks #454: a Cursor project
// session reads hooks from .cursor/hooks.json alone (cursor.com/docs/hooks),
// so the preview of a run assembly carries that file and no root
// hooks/hooks.json, which only a Cursor plugin reads.
func TestCmdPreviewCursorHooksOnlyWhereCursorReads(t *testing.T) {
	srcDir := createPreviewHarness(t)
	outputDir := filepath.Join(t.TempDir(), "preview-cursor-hooks")

	if err := cmdPreview([]string{srcDir, "-v", "cursor", "-o", outputDir}); err != nil {
		t.Fatalf("cmdPreview failed: %v", err)
	}

	assertExists(t, filepath.Join(outputDir, ".cursor", "hooks.json"))
	if _, err := os.Stat(filepath.Join(outputDir, "hooks")); !os.IsNotExist(err) {
		t.Errorf("expected no root hooks/ in cursor preview, stat err = %v", err)
	}
}

// TestCmdPreviewCursorMCPOnlyWhereCursorReads locks #470: a Cursor project
// session reads MCP servers from .cursor/mcp.json alone
// (cursor.com/docs/context/mcp), so the preview of a run assembly carries
// that file and no root mcp.json, which only a Cursor plugin reads.
func TestCmdPreviewCursorMCPOnlyWhereCursorReads(t *testing.T) {
	srcDir := createPreviewHarness(t)
	outputDir := filepath.Join(t.TempDir(), "preview-cursor-mcp")

	if err := cmdPreview([]string{srcDir, "-v", "cursor", "-o", outputDir}); err != nil {
		t.Fatalf("cmdPreview failed: %v", err)
	}

	assertExists(t, filepath.Join(outputDir, ".cursor", "mcp.json"))
	if _, err := os.Stat(filepath.Join(outputDir, "mcp.json")); !os.IsNotExist(err) {
		t.Errorf("expected no root mcp.json in cursor preview, stat err = %v", err)
	}
}

// TestCmdPreviewCopilotRunLayout locks the run-dir layout #471 must leave
// alone: `ynh run` points --plugin-dir at .copilot/, so the preview nests the
// manifest and MCP file there, beside the skills.
func TestCmdPreviewCopilotRunLayout(t *testing.T) {
	srcDir := createPreviewHarness(t)
	outputDir := filepath.Join(t.TempDir(), "preview-copilot")

	if err := cmdPreview([]string{srcDir, "-v", "copilot", "-o", outputDir}); err != nil {
		t.Fatalf("cmdPreview failed: %v", err)
	}

	assertExists(t, filepath.Join(outputDir, ".copilot", ".claude-plugin", "plugin.json"))
	assertExists(t, filepath.Join(outputDir, ".copilot", ".mcp.json"))
	for _, rel := range []string{".claude-plugin", filepath.Join(".github", "mcp.json")} {
		if _, err := os.Stat(filepath.Join(outputDir, rel)); !os.IsNotExist(err) {
			t.Errorf("expected no %s at the copilot run-dir root, stat err = %v", rel, err)
		}
	}
}

func TestCmdPreviewWithMCP(t *testing.T) {
	srcDir := createPreviewHarness(t)
	outputDir := filepath.Join(t.TempDir(), "preview-mcp")

	err := cmdPreview([]string{srcDir, "-v", "claude", "-o", outputDir})
	if err != nil {
		t.Fatalf("cmdPreview failed: %v", err)
	}

	// Claude MCP goes in .claude/.mcp.json (plugin format)
	mcpPath := filepath.Join(outputDir, ".claude", ".mcp.json")
	assertExists(t, mcpPath)

	data, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "test-server") {
		t.Error("expected .mcp.json to contain test-server")
	}

	// A preview is a session layout: the plugin MCP file and the manifest
	// pointer to it belong to an export only (#481).
	if _, err := os.Stat(filepath.Join(outputDir, "mcp")); !os.IsNotExist(err) {
		t.Errorf("expected no mcp/ in a preview, stat err = %v", err)
	}
	manifest, err := os.ReadFile(filepath.Join(outputDir, ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(manifest), "mcpServers") {
		t.Errorf("preview manifest must not name an MCP file:\n%s", manifest)
	}
}

func TestCmdPreviewBareAGENTS(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"),
		[]byte("# My Project\n\nDo stuff.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	outputDir := filepath.Join(t.TempDir(), "preview-bare")
	err := cmdPreview([]string{dir, "-v", "claude", "-o", outputDir})
	if err != nil {
		t.Fatalf("cmdPreview failed: %v", err)
	}

	// Should have CLAUDE.md with the AGENTS.md content
	assertExists(t, filepath.Join(outputDir, "CLAUDE.md"))

	// Source directory should NOT have been mutated
	if _, err := os.Stat(filepath.Join(dir, ".claude-plugin")); !os.IsNotExist(err) {
		t.Error("source directory should not have .claude-plugin after preview")
	}
}

func TestCmdPreviewMissingSource(t *testing.T) {
	err := cmdPreview([]string{})
	if err == nil {
		t.Fatal("expected error for missing source")
	}
}

func TestCmdPreviewBadSource(t *testing.T) {
	err := cmdPreview([]string{"./nonexistent-dir"})
	if err == nil {
		t.Fatal("expected error for nonexistent source")
	}
}

func TestCmdPreviewBadVendor(t *testing.T) {
	srcDir := createPreviewHarness(t)
	err := cmdPreview([]string{srcDir, "-v", "bogus"})
	if err == nil {
		t.Fatal("expected error for unknown vendor")
	}
	if !strings.Contains(err.Error(), "unknown vendor") {
		t.Errorf("expected 'unknown vendor' error, got: %v", err)
	}
}

func TestCmdPreviewNoHarnessOrInstructions(t *testing.T) {
	dir := t.TempDir()
	err := cmdPreview([]string{dir})
	if err == nil {
		t.Fatal("expected error for dir with no harness or AGENTS.md")
	}
}

func TestCmdPreviewVendorEnvVar(t *testing.T) {
	srcDir := createPreviewHarness(t)
	outputDir := filepath.Join(t.TempDir(), "preview-env")

	t.Setenv("YNH_VENDOR", "cursor")

	err := cmdPreview([]string{srcDir, "-o", outputDir})
	if err != nil {
		t.Fatalf("cmdPreview failed: %v", err)
	}

	// Cursor output should have .cursor/ dir
	assertExists(t, filepath.Join(outputDir, ".cursor"))
}

func TestCmdPreviewVendorFlagOverridesEnv(t *testing.T) {
	srcDir := createPreviewHarness(t)
	outputDir := filepath.Join(t.TempDir(), "preview-flag-override")

	t.Setenv("YNH_VENDOR", "cursor")

	err := cmdPreview([]string{srcDir, "-v", "claude", "-o", outputDir})
	if err != nil {
		t.Fatalf("cmdPreview failed: %v", err)
	}

	// -v flag should win over YNH_VENDOR
	assertExists(t, filepath.Join(outputDir, ".claude"))
}

func TestCmdPreviewHarnessFlag(t *testing.T) {
	srcDir := createPreviewHarness(t)

	err := cmdPreview([]string{"--harness", srcDir})
	if err != nil {
		t.Fatalf("cmdPreview with --harness failed: %v", err)
	}
}

func TestCmdPreviewHarnessEnvVar(t *testing.T) {
	srcDir := createPreviewHarness(t)

	t.Setenv("YNH_HARNESS", srcDir)

	err := cmdPreview(nil)
	if err != nil {
		t.Fatalf("cmdPreview with YNH_HARNESS failed: %v", err)
	}
}

func TestCmdPreviewHarnessFlagOverridesEnv(t *testing.T) {
	srcDir := createPreviewHarness(t)
	badDir := "/nonexistent/path"

	t.Setenv("YNH_HARNESS", badDir)

	// --harness flag should take priority over env var
	err := cmdPreview([]string{"--harness", srcDir})
	if err != nil {
		t.Fatalf("--harness flag should override YNH_HARNESS: %v", err)
	}
}

func TestCmdPreviewSkillsOnly(t *testing.T) {
	// Harness with skills but no hooks or MCP
	dir := t.TempDir()
	hj := map[string]any{"name": "skills-only", "version": "1.0.0"}
	data, _ := json.MarshalIndent(hj, "", "  ")
	if err := writePluginJSONFile(dir, data); err != nil {
		t.Fatal(err)
	}

	skillDir := filepath.Join(dir, "skills", "simple")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: simple\n---\nSimple skill.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	outputDir := filepath.Join(t.TempDir(), "out")
	err := cmdPreview([]string{dir, "-v", "claude", "-o", outputDir})
	if err != nil {
		t.Fatalf("cmdPreview failed: %v", err)
	}

	assertExists(t, filepath.Join(outputDir, ".claude", "skills", "simple", "SKILL.md"))
}

// `ynd preview` exists to show what will be assembled, and `ynh run` assembles
// the real thing. Both synthesized a manifest with a hardcoded "0.0.0" and no
// author or keywords, so the same harness produced two different plugin.json
// files depending on which command ran — and the live launch path shipped the
// lossy one.
func TestPreview_ManifestMatchesTheSource(t *testing.T) {
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, ".agents/harness"), 0o755); err != nil {
		t.Fatal(err)
	}
	const manifest = `{
      "name": "fidelity", "version": "2.3.4", "default_vendor": "claude",
      "description": "carries its own identity",
      "author": {"name": "A Person", "url": "https://example.com"},
      "keywords": ["one", "two", "three"]
    }`
	if err := os.WriteFile(filepath.Join(src, ".agents/harness", "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := cmdPreview([]string{src, "-v", "claude", "-o", out}); err != nil {
		t.Fatalf("preview: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(out, ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatalf("reading assembled manifest: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got["version"] != "2.3.4" {
		t.Errorf("version = %v, want 2.3.4 — a synthesized 0.0.0 disagrees with what ships", got["version"])
	}
	if got["author"] == nil {
		t.Error("author dropped: a *Harness that cannot carry it cannot reproduce its own manifest")
	}
	if kw, _ := got["keywords"].([]any); len(kw) != 3 {
		t.Errorf("keywords = %v, want 3", got["keywords"])
	}
}

// TestCmdPreviewShipsHookScripts locks #495: a session carries the scripts its
// hooks run by a "./" path, and the session hook command reaches the copy.
// Claude reads the session hooks as a --plugin-dir plugin rooted at .claude/;
// Cursor and Codex run hooks from the run directory, where the session starts.
func TestCmdPreviewShipsHookScripts(t *testing.T) {
	tests := []struct {
		vendor   string
		hookFile string
		command  string
		script   string
	}{
		{"claude", ".claude/hooks/hooks.json", `\"${CLAUDE_PLUGIN_ROOT}\"/scripts/guard.sh --strict`, ".claude/scripts/guard.sh"},
		{"codex", ".codex/hooks.json", "./scripts/guard.sh --strict", "scripts/guard.sh"},
		{"cursor", ".cursor/hooks.json", "./scripts/guard.sh --strict", "scripts/guard.sh"},
	}
	for _, tt := range tests {
		t.Run(tt.vendor, func(t *testing.T) {
			srcDir := t.TempDir()
			hj := map[string]any{
				"name":    "preview-hook-scripts",
				"version": "1.0.0",
				"hooks": map[string]any{
					"on_stop": []map[string]any{{"command": "./scripts/guard.sh --strict"}},
				},
			}
			data, err := json.Marshal(hj)
			if err != nil {
				t.Fatal(err)
			}
			if err := writePluginJSONFile(srcDir, data); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(srcDir, "scripts"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(srcDir, "scripts", "guard.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
				t.Fatal(err)
			}

			outputDir := filepath.Join(t.TempDir(), "out")
			if err := cmdPreview([]string{srcDir, "-v", tt.vendor, "-o", outputDir}); err != nil {
				t.Fatalf("cmdPreview failed: %v", err)
			}

			hooks, err := os.ReadFile(filepath.Join(outputDir, filepath.FromSlash(tt.hookFile)))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(hooks), `"command": "`+tt.command+`"`) {
				t.Errorf("%s does not run %s:\n%s", tt.hookFile, tt.command, hooks)
			}
			info, err := os.Stat(filepath.Join(outputDir, filepath.FromSlash(tt.script)))
			if err != nil {
				t.Fatalf("hook script not in the session: %v", err)
			}
			if info.Mode().Perm()&0o111 == 0 {
				t.Errorf("hook script mode = %v, want executable", info.Mode())
			}
		})
	}
}

// A server declared by an included harness is carried into the preview's MCP
// file, expanded in the include's own directory, and reported with where it
// came from. The root's own servers are not listed as included.
func TestPreviewCarriesMCPServersOfIncludedHarness(t *testing.T) {
	root := t.TempDir()
	inc := filepath.Join(root, "inc")
	for dir, hj := range map[string]map[string]any{
		root: {
			"name": "root", "version": "1.0.0",
			"includes":    []map[string]any{{"local": "inc"}},
			"mcp_servers": map[string]any{"own": map[string]any{"command": "node"}},
		},
		inc: {
			"name": "inc", "version": "1.0.0",
			"mcp_servers": map[string]any{"db": map[string]any{"command": "./bin/db"}},
		},
	} {
		data, _ := json.Marshal(hj)
		if err := writePluginJSONFile(dir, data); err != nil {
			t.Fatal(err)
		}
	}

	tmp, report, err := assembleForVendorSources(root, "claude", harness.Selection{})
	if err != nil {
		t.Fatalf("assembleForVendorSources: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	data, err := os.ReadFile(filepath.Join(tmp, ".claude", ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), filepath.Join(inc, "bin", "db")) || !strings.Contains(string(data), `"own"`) {
		t.Errorf("MCP file should carry both servers, the included one expanded against its directory:\n%s", data)
	}

	var out strings.Builder
	printMCPSources(&out, report.mcp)
	if got := out.String(); !strings.Contains(got, "db (from inc)") || strings.Contains(got, "own") {
		t.Errorf("sources listing = %q", got)
	}
}

// namespaceFixture writes a root that includes a harness named "github",
// which declares a profile, a focus bound to it, and a server of its own.
func namespaceFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for dir, hj := range map[string]map[string]any{
		root: {
			"name": "root", "version": "1.0.0",
			"includes": []map[string]any{{"local": "gh"}},
		},
		filepath.Join(root, "gh"): {
			"name": "github", "version": "1.0.0",
			"mcp_servers": map[string]any{"base": map[string]any{"command": "node"}},
			"profiles": map[string]any{"ci": map[string]any{
				"mcp_servers": map[string]any{"ci-only": map[string]any{"command": "node"}},
			}},
			"focuses": map[string]any{"triage": map[string]any{"profile": "ci", "prompt": "triage"}},
		},
	} {
		data, _ := json.Marshal(hj)
		if err := writePluginJSONFile(dir, data); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestPreviewNamespacedProfileAndFocus(t *testing.T) {
	root := namespaceFixture(t)
	t.Setenv("YNH_PROFILE", "")
	t.Setenv("YNH_FOCUS", "")

	mcpFile := func(t *testing.T, args ...string) string {
		t.Helper()
		out := filepath.Join(t.TempDir(), "out")
		if err := cmdPreview(append([]string{root, "-v", "claude", "-o", out}, args...)); err != nil {
			t.Fatalf("cmdPreview %v: %v", args, err)
		}
		data, err := os.ReadFile(filepath.Join(out, ".claude", ".mcp.json"))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	if got := mcpFile(t); strings.Contains(got, "ci-only") {
		t.Errorf("no selection should not apply the include's profile:\n%s", got)
	}
	if got := mcpFile(t, "--profile", "github:ci"); !strings.Contains(got, "ci-only") || !strings.Contains(got, "base") {
		t.Errorf("--profile github:ci should add its server:\n%s", got)
	}
	if got := mcpFile(t, "--focus", "github:triage"); !strings.Contains(got, "ci-only") {
		t.Errorf("--focus github:triage should apply the focus's profile:\n%s", got)
	}
}

func TestPreviewNamespacedSelectionErrors(t *testing.T) {
	root := namespaceFixture(t)
	t.Setenv("YNH_PROFILE", "")
	t.Setenv("YNH_FOCUS", "")
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"duplicate namespace", []string{"--profile", "github:ci", "--profile", "github:ci"}, `namespace "github"`},
		{"duplicate root", []string{"--profile", "a", "--profile", "b"}, "at most one unqualified profile"},
		{"focus with profile", []string{"--focus", "github:triage", "--profile", "github:ci"}, "cannot use --focus and --profile together (focus includes a profile)"},
		{"unknown namespace", []string{"--profile", "nope:ci"}, `no included harness has namespace "nope" (available: github)`},
		{"unknown profile", []string{"--profile", "github:nope"}, `profile "nope" not defined in included harness "github" (available: [ci])`},
		{"unknown focus", []string{"--focus", "github:nope"}, `focus "nope" not defined in included harness "github"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := cmdPreview(append([]string{root, "-v", "claude", "-o", filepath.Join(t.TempDir(), "out")}, tt.args...))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestPreviewListsFocusesAndProfilesOfIncludedHarnesses(t *testing.T) {
	root := namespaceFixture(t)
	tmp, report, err := assembleForVendorSources(root, "claude", harness.Selection{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	var out strings.Builder
	printIncludedSelectables(&out, report.included)
	for _, want := range []string{"Focuses from included harnesses:\n  github:triage\n", "Profiles from included harnesses:\n  github:ci\n"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("listing = %q, want containing %q", out.String(), want)
		}
	}

	var none strings.Builder
	printIncludedSelectables(&none, nil)
	if none.Len() != 0 {
		t.Errorf("no included harnesses should print nothing, got %q", none.String())
	}
}
