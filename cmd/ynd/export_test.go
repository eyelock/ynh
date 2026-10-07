package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func testdataExportDir() string {
	return filepath.Join("..", "..", "testdata", "export-harness")
}

func TestCmdExportLocalSource(t *testing.T) {
	outputDir := t.TempDir()
	srcDir := testdataExportDir()

	err := cmdExport([]string{srcDir, "-o", outputDir, "-v", "claude"})
	if err != nil {
		t.Fatalf("cmdExport failed: %v", err)
	}

	// Verify output
	manifestPath := filepath.Join(outputDir, "claude", ".claude-plugin", "plugin.json")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		t.Error("expected .claude-plugin/plugin.json")
	}
}

func TestCmdExportAllVendors(t *testing.T) {
	outputDir := t.TempDir()
	srcDir := testdataExportDir()

	err := cmdExport([]string{srcDir, "-o", outputDir})
	if err != nil {
		t.Fatalf("cmdExport failed: %v", err)
	}

	// All three vendor dirs should exist
	for _, v := range []string{"claude", "cursor", "codex"} {
		dir := filepath.Join(outputDir, v)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			t.Errorf("expected vendor dir: %s", dir)
		}
	}
}

func TestCmdExportMergedFlag(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "merged-out")
	srcDir := testdataExportDir()

	err := cmdExport([]string{srcDir, "-o", outputDir, "--merged", "-v", "claude,cursor"})
	if err != nil {
		t.Fatalf("cmdExport failed: %v", err)
	}

	// Both manifests should be in same directory
	assertExists(t, filepath.Join(outputDir, ".claude-plugin", "plugin.json"))
	assertExists(t, filepath.Join(outputDir, ".cursor-plugin", "plugin.json"))
}

func TestCmdExportCleanFlag(t *testing.T) {
	outputDir := t.TempDir()
	srcDir := testdataExportDir()

	// Create stale content
	staleFile := filepath.Join(outputDir, "stale.txt")
	if err := os.WriteFile(staleFile, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Neutralise the environment: skipConfirmEnv honours CI and YNH_YES, and CI
	// is always set in Actions. Without this the prompt is skipped there and
	// the test asserts the machine it runs on rather than the behaviour.
	t.Setenv("CI", "")
	t.Setenv("YNH_YES", "")

	// --clean now asks before deleting a non-empty directory. Answer it
	// explicitly: a test that relied on the old unconditional delete would
	// otherwise pass for the wrong reason.
	restorePrompt := promptActionFunc
	t.Cleanup(func() { promptActionFunc = restorePrompt })
	asked := false
	promptActionFunc = func(_ string, _ ...string) string { asked = true; return "y" }

	err := cmdExport([]string{srcDir, "-o", outputDir, "-v", "claude", "--clean"})
	if err != nil {
		t.Fatalf("cmdExport failed: %v", err)
	}

	// Stale file should be gone (--clean removes entire output dir)
	if _, err := os.Stat(staleFile); err == nil {
		t.Error("stale file should have been removed by --clean")
	}

	if !asked {
		t.Error("--clean deleted a non-empty directory without asking")
	}

	// Fresh content should exist
	assertExists(t, filepath.Join(outputDir, "claude", ".claude-plugin", "plugin.json"))
}

func TestCmdExportDefaultOutput(t *testing.T) {
	// Get absolute path to testdata before changing dirs
	srcDir, err := filepath.Abs(testdataExportDir())
	if err != nil {
		t.Fatal(err)
	}

	// Run in a temp directory so the default ./dist/ doesn't pollute
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	err = cmdExport([]string{srcDir, "-v", "claude"})
	if err != nil {
		t.Fatalf("cmdExport failed: %v", err)
	}

	// Default output should be ./dist/<harness-name>/
	assertExists(t, filepath.Join(tmpDir, "dist", "export-test", "claude", ".claude-plugin", "plugin.json"))
}

func TestCmdExportUnknownVendor(t *testing.T) {
	srcDir := testdataExportDir()
	outputDir := t.TempDir()

	err := cmdExport([]string{srcDir, "-o", outputDir, "-v", "bogus"})
	if err == nil {
		t.Fatal("expected error for unknown vendor")
	}
	if !strings.Contains(err.Error(), "unknown vendor") {
		t.Errorf("expected 'unknown vendor' error, got: %v", err)
	}
}

func TestCmdExportVendorEnvVar(t *testing.T) {
	outputDir := t.TempDir()
	srcDir := testdataExportDir()

	t.Setenv("YNH_VENDOR", "claude")

	err := cmdExport([]string{srcDir, "-o", outputDir})
	if err != nil {
		t.Fatalf("cmdExport failed: %v", err)
	}

	// Should export for claude only
	assertExists(t, filepath.Join(outputDir, "claude"))
}

func TestCmdExportVendorFlagOverridesEnv(t *testing.T) {
	outputDir := t.TempDir()
	srcDir := testdataExportDir()

	t.Setenv("YNH_VENDOR", "cursor")

	err := cmdExport([]string{srcDir, "-o", outputDir, "-v", "claude"})
	if err != nil {
		t.Fatalf("cmdExport failed: %v", err)
	}

	// Flag should win
	assertExists(t, filepath.Join(outputDir, "claude"))
}

func TestCmdExportHarnessFlag(t *testing.T) {
	outputDir := t.TempDir()
	srcDir := testdataExportDir()

	err := cmdExport([]string{"--harness", srcDir, "-o", outputDir, "-v", "claude"})
	if err != nil {
		t.Fatalf("cmdExport with --harness failed: %v", err)
	}

	assertExists(t, filepath.Join(outputDir, "claude"))
}

func TestCmdExportHarnessEnvVar(t *testing.T) {
	outputDir := t.TempDir()
	srcDir, err := filepath.Abs(testdataExportDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("YNH_HARNESS", srcDir)

	err = cmdExport([]string{"-o", outputDir, "-v", "claude"})
	if err != nil {
		t.Fatalf("cmdExport with YNH_HARNESS failed: %v", err)
	}

	assertExists(t, filepath.Join(outputDir, "claude"))
}

func TestCmdExportMissingSource(t *testing.T) {
	err := cmdExport([]string{})
	if err == nil {
		t.Fatal("expected error for missing source")
	}
}

func TestCmdExportBadPath(t *testing.T) {
	err := cmdExport([]string{"./nonexistent-dir", "-o", t.TempDir()})
	if err == nil {
		t.Fatal("expected error for nonexistent source")
	}
}

func TestCmdExportManifestContent(t *testing.T) {
	outputDir := t.TempDir()
	srcDir := testdataExportDir()

	err := cmdExport([]string{srcDir, "-o", outputDir, "-v", "claude"})
	if err != nil {
		t.Fatalf("cmdExport failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outputDir, "claude", ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}

	var pj map[string]any
	if err := json.Unmarshal(data, &pj); err != nil {
		t.Fatalf("invalid manifest JSON: %v", err)
	}

	if pj["name"] != "export-test" {
		t.Errorf("manifest name = %q, want %q", pj["name"], "export-test")
	}
	if pj["version"] != "1.0.0" {
		t.Errorf("manifest version = %q, want %q", pj["version"], "1.0.0")
	}
}

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("expected to exist: %s", path)
	}
}

// snapshotDir records every path under dir with its contents, so a test can
// assert that a refused command left a directory exactly as it found it. A
// missing dir snapshots as nil.
func snapshotDir(t *testing.T, dir string) map[string]string {
	t.Helper()
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil
	}
	snap := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			snap[rel+"/"] = ""
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		snap[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

// outputDirStates are the three states -o can be in before a refused command
// runs: absent, an existing empty directory, and an existing directory with
// content. Each setup returns the -o path, always under t.TempDir().
var outputDirStates = []struct {
	name  string
	setup func(t *testing.T) string
}{
	{"absent", func(t *testing.T) string {
		return filepath.Join(t.TempDir(), "out")
	}},
	{"existing empty", func(t *testing.T) string {
		return t.TempDir()
	}},
	{"existing with content", func(t *testing.T) string {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("keep"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(dir, "claude"), 0o755); err != nil {
			t.Fatal(err)
		}
		return dir
	}},
}

// A refused export must create nothing (#451). It used to create -o before it
// looked at the source, so refusing a legacy tree left an empty directory
// behind. An -o that already existed must be left exactly as it was.
func TestCmdExportRefusedLeavesOutputUntouched(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	t.Setenv("YNH_FOCUS", "")
	t.Setenv("YNH_PROFILE", "")

	legacy := filepath.Join(t.TempDir(), "legacy")
	writeLegacyHarnessJSON(t, legacy)
	good, err := filepath.Abs(testdataExportDir())
	if err != nil {
		t.Fatal(err)
	}

	refusals := []struct {
		name string
		args []string
		want string
	}{
		{"legacy source", []string{legacy, "-v", "claude"}, "ynd migrate"},
		{"legacy source merged", []string{legacy, "--merged"}, "ynd migrate"},
		// --clean -y must not empty -o for an export that is then refused.
		{"legacy source with clean", []string{legacy, "--clean", "-y"}, "ynd migrate"},
		{"unknown vendor", []string{good, "-v", "bogus"}, "unknown vendor"},
		{"focus and profile", []string{good, "--focus", "f", "--profile", "p"}, "--focus and --profile"},
		{"undefined focus", []string{good, "--focus", "nope"}, "focus \"nope\" not defined"},
		{"undefined profile", []string{good, "--profile", "nope"}, "nope"},
		{"undefined profile with clean", []string{good, "--profile", "nope", "--clean", "-y"}, "nope"},
		{"merged and agent-plugin", []string{good, "--merged", "--format", "agent-plugin", "--clean", "-y"}, "different layouts"},
		{"unknown format", []string{good, "--format", "zip", "--clean", "-y"}, `unknown --format "zip"`},
	}

	for _, r := range refusals {
		for _, s := range outputDirStates {
			t.Run(r.name+"/"+s.name, func(t *testing.T) {
				out := s.setup(t)
				before := snapshotDir(t, out)

				err := cmdExport(append(append([]string{}, r.args...), "-o", out))
				if err == nil {
					t.Fatal("expected the export to be refused")
				}
				if !strings.Contains(err.Error(), r.want) {
					t.Errorf("error = %q, want it to contain %q", err, r.want)
				}
				if after := snapshotDir(t, out); !reflect.DeepEqual(before, after) {
					t.Errorf("refused export changed -o\nbefore: %v\nafter:  %v", before, after)
				}
			})
		}
	}
}

// With no -o the default is ./dist/<name>; a refused export must not create
// ./dist either.
func TestCmdExportRefusedCreatesNoDefaultOutput(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	legacy := filepath.Join(t.TempDir(), "legacy")
	writeLegacyHarnessJSON(t, legacy)
	work := t.TempDir()
	t.Chdir(work)

	if err := cmdExport([]string{legacy, "-v", "claude"}); err == nil {
		t.Fatal("expected the export to be refused")
	}
	if _, err := os.Stat(filepath.Join(work, "dist")); !os.IsNotExist(err) {
		t.Errorf("refused export created ./dist (stat err: %v)", err)
	}
}

// writeLegacyHarnessJSON writes a pre-plugin `.harness.json` tree, which every
// command now refuses with a pointer to `ynd migrate` (#417).
func writeLegacyHarnessJSON(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"$schema":"https://eyelock.github.io/ynh/schema/harness.schema.json","name":"legacy","version":"0.1.0"}`
	if err := os.WriteFile(filepath.Join(dir, ".harness.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCmdExportFormatAgentPlugin(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "pkg")
	if err := cmdExport([]string{testdataExportDir(), "-o", outputDir, "--format", "agent-plugin", "-v", "claude,copilot"}); err != nil {
		t.Fatalf("cmdExport failed: %v", err)
	}
	for _, rel := range []string{"plugin.json", "skills/dev-project/SKILL.md", ".claude-plugin/plugin.json", "com.github.copilot/agents/planner.md", "agents/planner.md"} {
		if _, err := os.Stat(filepath.Join(outputDir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("expected %s: %v", rel, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(outputDir, "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"`) || !strings.Contains(string(data), `"name": "export-test"`) {
		t.Errorf("plugin.json = %s", data)
	}
}

func TestCmdExportFormatFlagErrors(t *testing.T) {
	out := t.TempDir()
	if err := cmdExport([]string{testdataExportDir(), "-o", out, "--format", "agent-plugin", "--merged"}); err == nil || !strings.Contains(err.Error(), "different layouts") {
		t.Errorf("merged+agent-plugin: err = %v", err)
	}
	if err := cmdExport([]string{testdataExportDir(), "-o", out, "--format", "zip"}); err == nil || !strings.Contains(err.Error(), `unknown --format "zip"`) {
		t.Errorf("unknown format: err = %v", err)
	}
	if err := cmdExport([]string{testdataExportDir(), "-o", out, "--format"}); err == nil || !strings.Contains(err.Error(), "requires a value") {
		t.Errorf("missing value: err = %v", err)
	}
}
