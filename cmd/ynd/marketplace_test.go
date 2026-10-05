package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// setupMarketplaceTest creates a marketplace config and sources in a temp dir.
func setupMarketplaceTest(t *testing.T) (configFile string) {
	t.Helper()
	dir := t.TempDir()

	// Create harness source (symlink to export-harness testdata)
	harnessDir := filepath.Join(dir, "harnesses", "david")
	if err := os.MkdirAll(filepath.Dir(harnessDir), 0o755); err != nil {
		t.Fatal(err)
	}
	srcHarness, _ := filepath.Abs(filepath.Join("..", "..", "testdata", "export-harness"))
	if err := os.Symlink(srcHarness, harnessDir); err != nil {
		t.Fatal(err)
	}

	// Create plugin source (vendor-native format with .claude-plugin/plugin.json)
	pluginDir := filepath.Join(dir, "plugins", "my-tool")
	manifestDir := filepath.Join(pluginDir, ".claude-plugin")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestJSON(t, filepath.Join(manifestDir, "plugin.json"), map[string]any{
		"name":        "my-tool",
		"version":     "0.2.0",
		"description": "A standalone tool",
	})

	// Write marketplace.json
	configFile = filepath.Join(dir, "marketplace.json")
	writeTestJSON(t, configFile, map[string]any{
		"name":        "cli-test-marketplace",
		"owner":       map[string]string{"name": "tester"},
		"description": "CLI test",
		"harnesses": []map[string]string{
			{"type": "harness", "source": "./harnesses/david"},
			{"type": "plugin", "source": "./plugins/my-tool"},
		},
	})

	return configFile
}

func TestCmdMarketplaceBuild(t *testing.T) {
	configFile := setupMarketplaceTest(t)
	outputDir := t.TempDir()

	err := cmdMarketplace([]string{"build", configFile, "-o", outputDir})
	if err != nil {
		t.Fatalf("cmdMarketplace build: %v", err)
	}

	// Verify marketplace index
	assertExists(t, filepath.Join(outputDir, ".claude-plugin", "marketplace.json"))
	assertExists(t, filepath.Join(outputDir, ".cursor-plugin", "marketplace.json"))

	// Verify plugins
	assertExists(t, filepath.Join(outputDir, "plugins", "export-test"))
	assertExists(t, filepath.Join(outputDir, "plugins", "my-tool"))

	// Verify README
	assertExists(t, filepath.Join(outputDir, "README.md"))
}

func TestCmdMarketplaceBuildClean(t *testing.T) {
	configFile := setupMarketplaceTest(t)
	outputDir := t.TempDir()

	// Create stale file
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

	err := cmdMarketplace([]string{"build", configFile, "-o", outputDir, "--clean"})
	if err != nil {
		t.Fatalf("cmdMarketplace build: %v", err)
	}

	// Stale file should be gone
	if _, err := os.Stat(staleFile); err == nil {
		t.Error("stale file should have been removed by --clean")
	}
	if !asked {
		t.Error("--clean deleted a non-empty directory without asking")
	}

	// Fresh content should exist
	assertExists(t, filepath.Join(outputDir, ".claude-plugin", "marketplace.json"))
}

func TestCmdMarketplaceBuildVendorFilter(t *testing.T) {
	configFile := setupMarketplaceTest(t)
	outputDir := t.TempDir()

	err := cmdMarketplace([]string{"build", configFile, "-o", outputDir, "-v", "claude"})
	if err != nil {
		t.Fatalf("cmdMarketplace build: %v", err)
	}

	assertExists(t, filepath.Join(outputDir, ".claude-plugin", "marketplace.json"))

	// Cursor should not be generated
	cursorPath := filepath.Join(outputDir, ".cursor-plugin", "marketplace.json")
	if _, err := os.Stat(cursorPath); err == nil {
		t.Error(".cursor-plugin should not exist for claude-only build")
	}
}

func TestCmdMarketplaceIndexContent(t *testing.T) {
	configFile := setupMarketplaceTest(t)
	outputDir := t.TempDir()

	err := cmdMarketplace([]string{"build", configFile, "-o", outputDir})
	if err != nil {
		t.Fatalf("cmdMarketplace build: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outputDir, ".claude-plugin", "marketplace.json"))
	if err != nil {
		t.Fatal(err)
	}

	var idx struct {
		Name    string `json:"name"`
		Plugins []struct {
			Name   string `json:"name"`
			Source string `json:"source"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatal(err)
	}

	if idx.Name != "cli-test-marketplace" {
		t.Errorf("name = %q, want cli-test-marketplace", idx.Name)
	}
	if len(idx.Plugins) != 2 {
		t.Fatalf("plugins = %d, want 2", len(idx.Plugins))
	}
	for _, p := range idx.Plugins {
		if !strings.HasPrefix(p.Source, "./plugins/") {
			t.Errorf("source %q should start with ./plugins/", p.Source)
		}
	}
}

func TestCmdMarketplaceMissingSubcommand(t *testing.T) {
	err := cmdMarketplace([]string{})
	if err == nil {
		t.Fatal("expected error for missing subcommand")
	}
}

func TestCmdMarketplaceUnknownSubcommand(t *testing.T) {
	err := cmdMarketplace([]string{"destroy"})
	if err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
	if !strings.Contains(err.Error(), "unknown marketplace subcommand") {
		t.Errorf("error = %q", err.Error())
	}
}

func writeTestJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// The guard at marketplace.go skipped only vendor.Get validation — which would
// have passed, since codex is a registered adapter — while codex stayed in
// vendorList and reached Build regardless. Codex's manifest dir is
// .agents/plugins, so it genuinely got an index all along.
//
// The code said one thing, did another, and docs/ynd.md said a third. This
// asserts what actually happens.
func TestCmdMarketplaceBuild_CodexGetsAnIndex(t *testing.T) {
	configFile := setupMarketplaceTest(t)
	outputDir := t.TempDir()

	if err := cmdMarketplace([]string{"build", configFile, "-o", outputDir, "-v", "codex"}); err != nil {
		t.Fatalf("cmdMarketplace build -v codex: %v", err)
	}
	assertExists(t, filepath.Join(outputDir, ".agents", "plugins", "marketplace.json"))
}

// An unknown vendor must still be rejected — deleting the codex branch must not
// weaken validation.
func TestCmdMarketplaceBuild_UnknownVendorStillRejected(t *testing.T) {
	configFile := setupMarketplaceTest(t)
	outputDir := t.TempDir()

	err := cmdMarketplace([]string{"build", configFile, "-o", outputDir, "-v", "not-a-vendor"})
	if err == nil {
		t.Fatal("an unknown vendor must be rejected")
	}
}

// config.Load returns an empty config for an absent file, so an error there
// means the file exists and is malformed. Swallowing it produced a confusing
// resolution failure several steps later instead of the parse error.
func TestCmdMarketplaceBuild_MalformedGlobalConfigFailsImmediately(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	configFile := setupMarketplaceTest(t)
	err := cmdMarketplace([]string{"build", configFile, "-o", t.TempDir()})
	if err == nil {
		t.Fatal("a malformed global config must fail the build, not be replaced with an empty one")
	}
	if !strings.Contains(err.Error(), "global config") {
		t.Errorf("the error should name what failed, got: %v", err)
	}
}

// A genuinely absent global config must keep working — the fix must not turn
// "no config yet" into an error.
func TestCmdMarketplaceBuild_AbsentGlobalConfigStillWorks(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	configFile := setupMarketplaceTest(t)
	if err := cmdMarketplace([]string{"build", configFile, "-o", t.TempDir()}); err != nil {
		t.Fatalf("an absent global config must not fail the build: %v", err)
	}
}

// Issue #399: `build` always makes its output a git repository, and --clean
// refused every git repository, so a rebuild with --clean into the same -o
// could never work. Now --clean empties the repository ynd created, keeps its
// .git, and the rebuild commits on top, so the build history survives.
func TestCmdMarketplaceBuildClean_RebuildsIntoItsOwnRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	configFile := setupMarketplaceTest(t)
	outputDir := filepath.Join(t.TempDir(), "out")

	if err := cmdMarketplace([]string{"build", configFile, "-o", outputDir}); err != nil {
		t.Fatalf("first build: %v", err)
	}
	stale := filepath.Join(outputDir, "plugins", "stale-plugin", "README.md")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("left over from an earlier build"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Change the marketplace between builds so the second build has
	// something to commit.
	raw, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(string(raw), `"description": "CLI test"`, `"description": "CLI test, rebuilt"`, 1)
	if changed == string(raw) {
		t.Fatal("fixture no longer has the description this test edits")
	}
	if err := os.WriteFile(configFile, []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := cmdMarketplace([]string{"build", configFile, "-o", outputDir, "--clean", "-y"}); err != nil {
		t.Fatalf("rebuild with --clean -y into ynd's own repo: %v", err)
	}

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("--clean should have removed the stale plugin from the previous build")
	}
	if _, err := os.Stat(filepath.Join(outputDir, ".git")); err != nil {
		t.Error(".git must survive: the repository is what Claude Code needs")
	}
	assertExists(t, filepath.Join(outputDir, ".claude-plugin", "marketplace.json"))

	log := exec.Command("git", "log", "--format=%s")
	log.Dir = outputDir
	out, err := log.CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %v\n%s", err, out)
	}
	commits := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(commits) < 2 {
		t.Errorf("the rebuild should commit on top of the first build, got %d commit(s):\n%s", len(commits), out)
	}
}

// A repository the user made themselves is still refused, -y or not. The
// output directory in this test is a git repo with no marker, exactly the
// "typed the output path one directory too high" accident.
func TestCmdMarketplaceBuildClean_StillRefusesAForeignRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	configFile := setupMarketplaceTest(t)
	outputDir := filepath.Join(t.TempDir(), "theirs")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatal(err)
	}
	init := exec.Command("git", "init")
	init.Dir = outputDir
	if out, err := init.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	source := filepath.Join(outputDir, "main.go")
	if err := os.WriteFile(source, []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := cmdMarketplace([]string{"build", configFile, "-o", outputDir, "--clean", "-y"})
	if err == nil {
		t.Fatal("--clean -y emptied a git repository ynd did not create")
	}
	if !strings.Contains(err.Error(), "not created by ynd") {
		t.Errorf("the refusal should say the repo is not ynd's, got %v", err)
	}
	if _, statErr := os.Stat(source); statErr != nil {
		t.Error("the source file was deleted despite the refusal")
	}
}

// A refused marketplace build must create nothing (#451). It created -o, and
// then -o/plugins, before it looked at a single entry, so a legacy entry or a
// malformed global config left empty directories behind. An -o that already
// existed must be left exactly as it was.
func TestCmdMarketplaceBuildRefusedLeavesOutputUntouched(t *testing.T) {
	refusals := []struct {
		name  string
		setup func(t *testing.T) (configFile string, extra []string)
		want  string
	}{
		{"legacy entry after valid ones", func(t *testing.T) (string, []string) {
			t.Setenv("YNH_HOME", t.TempDir())
			configFile := setupMarketplaceTest(t)
			dir := filepath.Dir(configFile)
			writeLegacyHarnessJSON(t, filepath.Join(dir, "harnesses", "legacy"))
			writeTestJSON(t, configFile, map[string]any{
				"name":  "cli-test-marketplace",
				"owner": map[string]string{"name": "tester"},
				"harnesses": []map[string]string{
					{"type": "harness", "source": "./harnesses/david"},
					{"type": "plugin", "source": "./plugins/my-tool"},
					{"type": "harness", "source": "./harnesses/legacy"},
				},
			})
			return configFile, nil
		}, "ynd migrate"},
		{"legacy entry with clean", func(t *testing.T) (string, []string) {
			t.Setenv("YNH_HOME", t.TempDir())
			configFile := setupMarketplaceTest(t)
			dir := filepath.Dir(configFile)
			writeLegacyHarnessJSON(t, filepath.Join(dir, "harnesses", "legacy"))
			writeTestJSON(t, configFile, map[string]any{
				"name":      "cli-test-marketplace",
				"owner":     map[string]string{"name": "tester"},
				"harnesses": []map[string]string{{"type": "harness", "source": "./harnesses/legacy"}},
			})
			return configFile, []string{"--clean", "-y"}
		}, "ynd migrate"},
		{"malformed global config", func(t *testing.T) (string, []string) {
			home := t.TempDir()
			t.Setenv("YNH_HOME", home)
			if err := os.WriteFile(filepath.Join(home, "config.json"), []byte("{ not json"), 0o644); err != nil {
				t.Fatal(err)
			}
			return setupMarketplaceTest(t), nil
		}, "global config"},
		{"unknown vendor", func(t *testing.T) (string, []string) {
			t.Setenv("YNH_HOME", t.TempDir())
			return setupMarketplaceTest(t), []string{"-v", "bogus"}
		}, "unknown vendor"},
		{"unknown format", func(t *testing.T) (string, []string) {
			t.Setenv("YNH_HOME", t.TempDir())
			return setupMarketplaceTest(t), []string{"--format", "tarball", "--clean", "-y"}
		}, `unknown --format "tarball"`},
	}

	for _, r := range refusals {
		for _, s := range outputDirStates {
			t.Run(r.name+"/"+s.name, func(t *testing.T) {
				configFile, extra := r.setup(t)
				out := s.setup(t)
				before := snapshotDir(t, out)

				args := append([]string{"build", configFile, "-o", out}, extra...)
				err := cmdMarketplace(args)
				if err == nil {
					t.Fatal("expected the build to be refused")
				}
				if !strings.Contains(err.Error(), r.want) {
					t.Errorf("error = %q, want it to contain %q", err, r.want)
				}
				if after := snapshotDir(t, out); !reflect.DeepEqual(before, after) {
					t.Errorf("refused build changed -o\nbefore: %v\nafter:  %v", before, after)
				}
			})
		}
	}
}

// Each harness entry's export warnings go to stderr, one "warning: " line
// each, prefixed with the entry's name, and the build still succeeds. An entry
// that exports cleanly prints nothing there (#496).
func TestCmdMarketplaceBuild_PrintsEntryWarnings(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    string
	}{
		{
			name:    "missing hook script",
			command: "./scripts/missing.sh",
			want:    "warning: reviewer: hook script ./scripts/missing.sh is not a file in the harness, so the plugin does not carry it\n",
		},
		{name: "clean entry", command: "echo before"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			harnessDir := filepath.Join(dir, "harnesses", "reviewer")
			if err := os.MkdirAll(filepath.Join(harnessDir, ".agents", "harness"), 0o755); err != nil {
				t.Fatal(err)
			}
			writeTestJSON(t, filepath.Join(harnessDir, ".agents", "harness", "plugin.json"), map[string]any{
				"name":    "reviewer",
				"version": "0.1.0",
				"hooks": map[string]any{
					"before_tool": []any{map[string]string{"matcher": "Bash", "command": tt.command}},
				},
			})
			configFile := filepath.Join(dir, "marketplace.json")
			writeTestJSON(t, configFile, map[string]any{
				"name":      "warnings-marketplace",
				"owner":     map[string]string{"name": "tester"},
				"harnesses": []map[string]string{{"type": "harness", "source": "./harnesses/reviewer"}},
			})

			var stderr bytes.Buffer
			if err := cmdMarketplaceBuild([]string{configFile, "-o", filepath.Join(dir, "out")}, &stderr); err != nil {
				t.Fatalf("cmdMarketplaceBuild: %v", err)
			}
			if got := stderr.String(); got != tt.want {
				t.Errorf("stderr = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCmdMarketplaceBuild_FormatAgentPlugin(t *testing.T) {
	configPath := setupMarketplaceTest(t)
	outputDir := filepath.Join(t.TempDir(), "out")
	if err := cmdMarketplace([]string{"build", configPath, "-o", outputDir, "--format", "agent-plugin"}); err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outputDir, "plugins", "export-test", "plugin.json")); err != nil {
		t.Errorf("expected a portable manifest: %v", err)
	}
	if err := cmdMarketplace([]string{"build", configPath, "-o", outputDir, "--format", "tarball"}); err == nil || !strings.Contains(err.Error(), `unknown --format "tarball"`) {
		t.Errorf("err = %v", err)
	}
	if err := cmdMarketplace([]string{"build", configPath, "-o", outputDir, "--format"}); err == nil || !strings.Contains(err.Error(), "requires a value") {
		t.Errorf("err = %v", err)
	}
}
