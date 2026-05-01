package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
)

func TestParseRunArgs(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantName    string
		wantVendor  string
		wantProfile string
		wantFocus   string
		wantFile    string
		wantSession string
		wantPrompt  string
		wantArgs    []string
		wantAction  string
	}{
		{
			name:     "harness name as first positional",
			args:     []string{"my-harness"},
			wantName: "my-harness",
		},
		{
			name:       "harness name then prompt via separator",
			args:       []string{"my-harness", "--", "fix the bug"},
			wantName:   "my-harness",
			wantPrompt: "fix the bug",
		},
		{
			name:       "vendor flag only",
			args:       []string{"-v", "codex"},
			wantVendor: "codex",
		},
		{
			name:       "harness name and vendor",
			args:       []string{"my-harness", "-v", "codex"},
			wantName:   "my-harness",
			wantVendor: "codex",
		},
		{
			name:       "name vendor passthrough and prompt via separator",
			args:       []string{"my-harness", "-v", "claude", "--model", "opus", "--", "fix the bug"},
			wantName:   "my-harness",
			wantVendor: "claude",
			wantPrompt: "fix the bug",
			wantArgs:   []string{"--model", "opus"},
		},
		{
			name:       "passthrough with prompt via separator — positional becomes name",
			args:       []string{"--model", "opus", "--", "do something"},
			wantName:   "opus",
			wantPrompt: "do something",
			wantArgs:   []string{"--model"},
		},
		{
			name:       "double dash separator no name",
			args:       []string{"--verbose", "--full-auto", "--", "refactor auth"},
			wantPrompt: "refactor auth",
			wantArgs:   []string{"--verbose", "--full-auto"},
		},
		{
			name:       "name vendor passthrough and prompt via double dash",
			args:       []string{"my-harness", "-v", "codex", "--full-auto", "--model", "gpt-5", "--", "deploy it"},
			wantName:   "my-harness",
			wantVendor: "codex",
			wantPrompt: "deploy it",
			wantArgs:   []string{"--full-auto", "--model", "gpt-5"},
		},
		{
			name:     "boolean flags then harness name",
			args:     []string{"--verbose", "--full-auto", "my-harness"},
			wantName: "my-harness",
			wantArgs: []string{"--verbose", "--full-auto"},
		},
		{
			name:     "flags only no name",
			args:     []string{"--verbose", "--full-auto"},
			wantArgs: []string{"--verbose", "--full-auto"},
		},
		{
			name: "no args",
			args: []string{},
		},
		{
			name:     "double dash with no prompt",
			args:     []string{"--verbose", "--"},
			wantArgs: []string{"--verbose"},
		},
		{
			name:     "dangling vendor flag",
			args:     []string{"-v"},
			wantArgs: []string{"-v"},
		},
		{
			name:       "install action",
			args:       []string{"-v", "cursor", "--install"},
			wantVendor: "cursor",
			wantAction: "install",
		},
		{
			name:       "clean action",
			args:       []string{"-v", "cursor", "--clean"},
			wantVendor: "cursor",
			wantAction: "clean",
		},
		{
			name:       "install not passed to vendor",
			args:       []string{"--verbose", "--install", "-v", "codex"},
			wantVendor: "codex",
			wantAction: "install",
			wantArgs:   []string{"--verbose"},
		},
		{
			name:      "focus flag",
			args:      []string{"my-harness", "--focus", "review"},
			wantName:  "my-harness",
			wantFocus: "review",
		},
		{
			name:        "profile flag",
			args:        []string{"my-harness", "--profile", "ci"},
			wantName:    "my-harness",
			wantProfile: "ci",
		},
		{
			name:     "harness-file flag",
			args:     []string{"--harness-file", "/tmp/test.json"},
			wantFile: "/tmp/test.json",
		},
		{
			name:        "session-name flag",
			args:        []string{"my-harness", "--session-name", "termq-abc12345"},
			wantName:    "my-harness",
			wantSession: "termq-abc12345",
		},
		{
			name:        "session-name with vendor and prompt",
			args:        []string{"my-harness", "-v", "claude", "--session-name", "termq-abc12345", "--", "review this"},
			wantName:    "my-harness",
			wantVendor:  "claude",
			wantSession: "termq-abc12345",
			wantPrompt:  "review this",
		},
		{
			name:        "session-name value not confused as prompt",
			args:        []string{"my-harness", "--session-name", "termq-abc12345"},
			wantName:    "my-harness",
			wantSession: "termq-abc12345",
			wantPrompt:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("YNH_FOCUS", "")
			t.Setenv("YNH_HARNESS_FILE", "")
			ra := parseRunArgs(tt.args)
			if ra.HarnessName != tt.wantName {
				t.Errorf("HarnessName = %q, want %q", ra.HarnessName, tt.wantName)
			}
			if ra.VendorFlag != tt.wantVendor {
				t.Errorf("VendorFlag = %q, want %q", ra.VendorFlag, tt.wantVendor)
			}
			if ra.ProfileFlag != tt.wantProfile {
				t.Errorf("ProfileFlag = %q, want %q", ra.ProfileFlag, tt.wantProfile)
			}
			if ra.FocusFlag != tt.wantFocus {
				t.Errorf("FocusFlag = %q, want %q", ra.FocusFlag, tt.wantFocus)
			}
			if ra.HarnessFile != tt.wantFile {
				t.Errorf("HarnessFile = %q, want %q", ra.HarnessFile, tt.wantFile)
			}
			if ra.SessionName != tt.wantSession {
				t.Errorf("SessionName = %q, want %q", ra.SessionName, tt.wantSession)
			}
			if ra.Prompt != tt.wantPrompt {
				t.Errorf("Prompt = %q, want %q", ra.Prompt, tt.wantPrompt)
			}
			if ra.Action != tt.wantAction {
				t.Errorf("Action = %q, want %q", ra.Action, tt.wantAction)
			}
			if len(ra.VendorArgs) != len(tt.wantArgs) {
				t.Errorf("VendorArgs = %v, want %v", ra.VendorArgs, tt.wantArgs)
			} else {
				for i := range ra.VendorArgs {
					if ra.VendorArgs[i] != tt.wantArgs[i] {
						t.Errorf("VendorArgs[%d] = %q, want %q", i, ra.VendorArgs[i], tt.wantArgs[i])
					}
				}
			}
		})
	}
}

func TestParseRunArgs_FocusEnvVar(t *testing.T) {
	t.Setenv("YNH_FOCUS", "review")
	t.Setenv("YNH_HARNESS_FILE", "")
	ra := parseRunArgs([]string{"my-harness"})
	if ra.FocusFlag != "review" {
		t.Errorf("FocusFlag = %q, want review", ra.FocusFlag)
	}
}

func TestCmdRun_FocusAndProfileError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("YNH_HOME", "")
	t.Setenv("YNH_FOCUS", "")
	t.Setenv("YNH_PROFILE", "")
	t.Setenv("YNH_HARNESS_FILE", "")

	err := cmdRun([]string{"my-harness", "--focus", "review", "--profile", "ci"})
	if err == nil {
		t.Fatal("expected error for --focus + --profile")
	}
	if !strings.Contains(err.Error(), "cannot use --focus and --profile together") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCmdRun_FocusAndPromptError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("YNH_HOME", "")
	t.Setenv("YNH_FOCUS", "")
	t.Setenv("YNH_PROFILE", "")
	t.Setenv("YNH_HARNESS_FILE", "")

	err := cmdRun([]string{"my-harness", "--focus", "review", "--", "do something"})
	if err == nil {
		t.Fatal("expected error for --focus + trailing prompt")
	}
	if !strings.Contains(err.Error(), "cannot use --focus and a trailing prompt together") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCmdRun_FocusEnvPlusProfileEnvError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("YNH_HOME", "")
	t.Setenv("YNH_FOCUS", "review")
	t.Setenv("YNH_PROFILE", "ci")
	t.Setenv("YNH_HARNESS_FILE", "")

	err := cmdRun([]string{"my-harness"})
	if err == nil {
		t.Fatal("expected error for YNH_FOCUS + YNH_PROFILE both set")
	}
	if !strings.Contains(err.Error(), "cannot use --focus and --profile together") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseRunArgs_HarnessFileEnvVar(t *testing.T) {
	t.Setenv("YNH_FOCUS", "")
	t.Setenv("YNH_HARNESS_FILE", "/tmp/test.json")
	ra := parseRunArgs([]string{})
	if ra.HarnessFile != "/tmp/test.json" {
		t.Errorf("HarnessFile = %q, want /tmp/test.json", ra.HarnessFile)
	}
}

func TestResolveVendor(t *testing.T) {
	tests := []struct {
		name          string
		flag          string
		envVar        string
		harnessVendor string
		want          string
	}{
		{"flag takes priority", "claude", "", "codex", "claude"},
		{"harness default", "", "", "codex", "codex"},
		{"global config fallback", "", "", "", "claude"},
		{"env var", "", "codex", "claude", "codex"},
		{"flag beats env var", "claude", "codex", "cursor", "claude"},
		{"empty env var falls through", "", "", "codex", "codex"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("HOME", dir)
			if tt.envVar != "" {
				t.Setenv("YNH_VENDOR", tt.envVar)
			} else {
				t.Setenv("YNH_VENDOR", "")
			}

			p := &harness.Harness{
				Name:          "test",
				DefaultVendor: tt.harnessVendor,
			}

			got, err := resolveVendor(tt.flag, p)
			if err != nil {
				t.Fatalf("resolveVendor failed: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGenerateLauncher(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	if err := generateLauncher("david"); err != nil {
		t.Fatalf("generateLauncher failed: %v", err)
	}

	launcherPath := filepath.Join(dir, ".ynh", "bin", "david")
	info, err := os.Stat(launcherPath)
	if err != nil {
		t.Fatalf("launcher not created: %v", err)
	}

	// Should be executable
	if info.Mode()&0o111 == 0 {
		t.Error("launcher is not executable")
	}
}

// installTestHarness creates a fake installed harness with a launcher and run dir.
func installTestHarness(t *testing.T, name string) {
	t.Helper()

	// Create harness directory with .harness.json
	installDir := harness.InstalledDir(name)
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatal(err)
	}
	harnessJSON := fmt.Sprintf(`{"name":%q,"version":"0.1.0","default_vendor":"claude"}`, name)
	if err := os.WriteFile(filepath.Join(installDir, ".harness.json"), []byte(harnessJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create launcher
	launcherPath := filepath.Join(config.BinDir(), name)
	if err := os.MkdirAll(config.BinDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(launcherPath, []byte("#!/bin/bash\nexec ynh run "+name+" \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Create run directory
	runDir := filepath.Join(config.RunDir(), name)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestCmdUninstall_RemovesEverything(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	installTestHarness(t, "david")

	// Verify everything exists before uninstall
	installDir := harness.InstalledDir("david")
	if _, err := os.Stat(installDir); err != nil {
		t.Fatalf("harness not installed: %v", err)
	}

	if err := cmdUninstall([]string{"david"}); err != nil {
		t.Fatalf("cmdUninstall failed: %v", err)
	}

	// Harness directory should be gone
	if _, err := os.Stat(installDir); !os.IsNotExist(err) {
		t.Error("harness directory still exists after uninstall")
	}

	// Launcher should be gone
	launcherPath := filepath.Join(config.BinDir(), "david")
	if _, err := os.Stat(launcherPath); !os.IsNotExist(err) {
		t.Error("launcher still exists after uninstall")
	}

	// Run dir should be gone
	runDir := filepath.Join(config.RunDir(), "david")
	if _, err := os.Stat(runDir); !os.IsNotExist(err) {
		t.Error("run directory still exists after uninstall")
	}
}

func TestCmdUninstall_NamespacedHarness(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("YNH_HOME", "")

	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	// Install harness at namespaced path (eyelock/assistants → eyelock--assistants)
	nsDir := filepath.Join(config.HarnessesDir(), "eyelock--assistants", "planner")
	if err := os.MkdirAll(nsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	harnessJSON := `{"name":"planner","version":"1.0.0","default_vendor":"claude"}`
	if err := os.WriteFile(filepath.Join(nsDir, ".harness.json"), []byte(harnessJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := cmdUninstall([]string{"planner"}); err != nil {
		t.Fatalf("cmdUninstall failed: %v", err)
	}

	if _, err := os.Stat(nsDir); !os.IsNotExist(err) {
		t.Error("namespaced harness directory still exists after uninstall")
	}
}

func TestCmdUninstall_NotInstalled(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	err := cmdUninstall([]string{"nonexistent"})
	if err == nil {
		t.Fatal("expected error for uninstalling nonexistent harness")
	}
	if !strings.Contains(err.Error(), "not installed") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCmdUninstall_NoArgs(t *testing.T) {
	err := cmdUninstall([]string{})
	if err == nil {
		t.Fatal("expected error for no args")
	}
	if !strings.Contains(err.Error(), "usage") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCmdUninstall_RemovesSourcesEntry(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("YNH_HOME", "")

	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	// Pre-populate config with a sources entry for the harness being uninstalled
	// and an unrelated entry that must survive.
	cfg := &config.Config{
		DefaultVendor: "claude",
		Sources: []config.Source{
			{Name: "david", Path: "/some/path"},
			{Name: "other", Path: "/other/path"},
		},
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	installTestHarness(t, "david")

	if err := cmdUninstall([]string{"david"}); err != nil {
		t.Fatalf("cmdUninstall failed: %v", err)
	}

	loaded, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range loaded.Sources {
		if s.Name == "david" {
			t.Error("sources entry for uninstalled harness still present in config")
		}
	}
	if len(loaded.Sources) != 1 || loaded.Sources[0].Name != "other" {
		t.Errorf("unexpected sources after uninstall: %+v", loaded.Sources)
	}
}

func TestCmdList_Empty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	// Should not error when no harnesses installed
	if err := cmdList(nil); err != nil {
		t.Fatalf("cmdList failed: %v", err)
	}
}

func TestCmdList_WithHarnesses(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	installTestHarness(t, "alice")
	installTestHarness(t, "bob")

	if err := cmdList(nil); err != nil {
		t.Fatalf("cmdList failed: %v", err)
	}
}

func TestFormatProvenance(t *testing.T) {
	tests := []struct {
		name string
		prov *harness.Provenance
		want string
	}{
		{"nil", nil, "-"},
		{"local", &harness.Provenance{SourceType: "local", Source: "./my-harness"}, "./my-harness"},
		{"git no path", &harness.Provenance{SourceType: "git", Source: "github.com/eyelock/assistants"}, "eyelock/assistants"},
		{"git with path", &harness.Provenance{SourceType: "git", Source: "github.com/eyelock/assistants", Path: "ynh/david"}, "eyelock/assistants/ynh/david"},
		{"registry", &harness.Provenance{SourceType: "registry", Source: "github.com/eyelock/assistants", RegistryName: "my-reg"}, "eyelock/assistants (my-reg)"},
		{"registry with path", &harness.Provenance{SourceType: "registry", Source: "github.com/eyelock/assistants", Path: "ynh/david", RegistryName: "my-reg"}, "eyelock/assistants/ynh/david (my-reg)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatProvenance(tt.prov)
			if got != tt.want {
				t.Errorf("formatProvenance() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatIncludes(t *testing.T) {
	tests := []struct {
		name     string
		includes []harness.Include
		want     string
	}{
		{"empty", nil, "0"},
		{"single no pick", []harness.Include{
			{GitSource: harness.GitSource{Git: "github.com/example/skills", Path: "dev"}},
		}, "example/skills/dev"},
		{"single with pick", []harness.Include{
			{GitSource: harness.GitSource{Git: "github.com/example/skills", Path: "dev"}, Pick: []string{"a", "b"}},
		}, "example/skills/dev [2]"},
		{"with ref", []harness.Include{
			{GitSource: harness.GitSource{Git: "github.com/example/skills", Path: "dev", Ref: "v1.2.0"}},
		}, "example/skills/dev@v1.2.0"},
		{"main ref omitted", []harness.Include{
			{GitSource: harness.GitSource{Git: "github.com/example/skills", Ref: "main"}},
		}, "example/skills"},
		{"multiple", []harness.Include{
			{GitSource: harness.GitSource{Git: "github.com/example/skills", Path: "dev"}, Pick: []string{"a", "b"}},
			{GitSource: harness.GitSource{Git: "github.com/example/skills", Path: "infra"}, Pick: []string{"c"}},
		}, "example/skills/dev [2], example/skills/infra [1]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatIncludes(tt.includes)
			if got != tt.want {
				t.Errorf("formatIncludes() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatDelegates(t *testing.T) {
	tests := []struct {
		name      string
		delegates []harness.Delegate
		want      string
	}{
		{"empty", nil, "0"},
		{"single", []harness.Delegate{
			{GitSource: harness.GitSource{Git: "github.com/example/team"}},
		}, "example/team"},
		{"with path and ref", []harness.Delegate{
			{GitSource: harness.GitSource{Git: "github.com/example/mono", Path: "harnesses/ops", Ref: "v2"}},
		}, "example/mono/harnesses/ops@v2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDelegates(tt.delegates)
			if got != tt.want {
				t.Errorf("formatDelegates() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatArtifactSummary(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("YNH_HOME", "")

	// Create a harness with artifacts
	harnessDir := filepath.Join(dir, ".ynh", "harnesses", "artfmt")
	if err := os.MkdirAll(harnessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(harnessDir, ".harness.json"),
		[]byte(`{"name":"artfmt","version":"0.1.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Add 2 skills, 1 agent
	for _, skill := range []string{"a", "b"} {
		sd := filepath.Join(harnessDir, "skills", skill)
		if err := os.MkdirAll(sd, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sd, "SKILL.md"), []byte("#"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	agentDir := filepath.Join(harnessDir, "agents")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "x.md"), []byte("#"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := formatArtifactSummary("artfmt")
	if got != "2s 1a" {
		t.Errorf("formatArtifactSummary() = %q, want %q", got, "2s 1a")
	}
}

func TestFormatArtifactSummary_Empty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("YNH_HOME", "")

	installTestHarness(t, "neart")
	got := formatArtifactSummary("neart")
	if got != "0" {
		t.Errorf("formatArtifactSummary() = %q, want %q", got, "0")
	}
}

func TestCmdInfo_Success(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	installTestHarness(t, "david")

	err := cmdInfo([]string{"david"})
	if err != nil {
		t.Fatalf("cmdInfo failed: %v", err)
	}
}

func TestCmdInfo_NotFound(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	if err := os.MkdirAll(filepath.Join(dir, ".ynh", "harnesses"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := cmdInfo([]string{"nonexistent"})
	if err == nil {
		t.Fatal("expected error for nonexistent harness")
	}
	if !errors.Is(err, harness.ErrNotFound) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCmdInfo_NoArgs(t *testing.T) {
	err := cmdInfo([]string{})
	if err == nil {
		t.Fatal("expected error for no args")
	}
	if !strings.Contains(err.Error(), "usage") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCmdInstall_WritesProvenance(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("YNH_HOME", "")

	// Create a local harness source
	srcDir := filepath.Join(dir, "my-harness")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, ".harness.json"),
		[]byte(`{"name":"provtest","version":"0.1.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	err := cmdInstall([]string{srcDir})
	if err != nil {
		t.Fatalf("cmdInstall failed: %v", err)
	}

	// Load installed harness and check provenance
	p, err := harness.Load("provtest")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if p.InstalledFrom == nil {
		t.Fatal("InstalledFrom is nil after install")
	}
	if p.InstalledFrom.SourceType != "local" {
		t.Errorf("SourceType = %q, want %q", p.InstalledFrom.SourceType, "local")
	}
	if p.InstalledFrom.Source != srcDir {
		t.Errorf("Source = %q, want %q", p.InstalledFrom.Source, srcDir)
	}
	if p.InstalledFrom.InstalledAt == "" {
		t.Error("InstalledAt is empty")
	}
}

func TestCmdUpdate_NoArgs(t *testing.T) {
	err := cmdUpdate([]string{})
	if err == nil {
		t.Fatal("expected error for no args")
	}
	if !strings.Contains(err.Error(), "usage") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCmdUpdate_NotFound(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	err := cmdUpdate([]string{"nonexistent"})
	if err == nil {
		t.Fatal("expected error for nonexistent harness")
	}
	if !errors.Is(err, harness.ErrNotFound) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCmdUpdate_NoGitSources(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	// Install a harness with no includes/delegates
	installTestHarness(t, "minimal")

	err := cmdUpdate([]string{"minimal"})
	if err != nil {
		t.Fatalf("cmdUpdate should succeed for harness with no git sources: %v", err)
	}
}

func TestCmdInstall_PathFlag(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("YNH_HOME", "")

	// Create a monorepo-style layout with a harness in a subdirectory
	monoDir := filepath.Join(dir, "monorepo")
	aliceDir := filepath.Join(monoDir, "harnesses", "alice")
	if err := os.MkdirAll(aliceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(aliceDir, ".harness.json"),
		[]byte(`{"name":"alice","version":"0.1.0","description":"test harness"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Install with --path flag
	err := cmdInstall([]string{monoDir, "--path", "harnesses/alice"})
	if err != nil {
		t.Fatalf("cmdInstall with --path failed: %v", err)
	}

	// Verify harness was installed (format migrated to .ynh-plugin/plugin.json)
	installDir := harness.InstalledDir("alice")
	if harness.DetectFormat(installDir) == "" {
		t.Fatal("harness not found after install")
	}
}

func TestCmdInstall_PathFlag_NotFound(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("YNH_HOME", "")

	monoDir := filepath.Join(dir, "monorepo")
	if err := os.MkdirAll(monoDir, 0o755); err != nil {
		t.Fatal(err)
	}

	err := cmdInstall([]string{monoDir, "--path", "nonexistent/path"})
	if err == nil {
		t.Fatal("expected error for nonexistent --path")
	}
	if !strings.Contains(err.Error(), "not found in source") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestCmdInstall_PathFlag_NoSource(t *testing.T) {
	err := cmdInstall([]string{"--path", "some/dir"})
	if err == nil {
		t.Fatal("expected error when --path consumes the source arg")
	}
}

func TestCmdInstall_PathFlag_TraversalBlocked(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("YNH_HOME", "")

	for _, p := range []string{"../../etc", "../secret", "/etc/passwd", "a/../../etc"} {
		err := cmdInstall([]string{dir, "--path", p})
		if err == nil {
			t.Errorf("--path %q: expected error, got nil", p)
			continue
		}
		if !strings.Contains(err.Error(), "invalid --path") {
			t.Errorf("--path %q: unexpected error: %v", p, err)
		}
	}
}

func TestCmdInstall_SourceInsideHarnessesDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("YNH_HOME", "")

	// Install a harness whose source is already the install location (e.g. the
	// marketplace browser pointing at an already-installed harness). ynh should
	// skip the clean+copy and succeed without deleting anything.
	srcDir := harness.InstalledDir("already-there")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, ".harness.json"),
		[]byte(`{"name":"already-there","version":"0.1.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := cmdInstall([]string{srcDir}); err != nil {
		t.Fatalf("install from already-installed location should succeed, got: %v", err)
	}

	// Harness must still be loadable after install from already-installed location
	if harness.DetectFormat(srcDir) == "" {
		t.Error("harness missing after install from already-installed location")
	}
}

func TestCmdStatus_Empty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("YNH_HOME", "")

	if err := cmdStatus(); err != nil {
		t.Fatalf("cmdStatus failed: %v", err)
	}
}

func TestCmdPrune_NoOrphans(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("YNH_HOME", "")

	if err := cmdPrune(); err != nil {
		t.Fatalf("cmdPrune failed: %v", err)
	}
}

// Pointer registers a fork; user later deletes the source tree without
// uninstalling. ynh uninstall must still remove the pointer — the operation
// is metadata, not a delete of user-owned files.
func TestCmdUninstall_OrphanPointerSourceMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)

	missing := filepath.Join(t.TempDir(), "gone")
	if err := harness.SavePointer(&harness.Pointer{
		Name: "stranded", SourceType: "local",
		Source: missing, InstalledAt: "2026-05-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}

	if err := cmdUninstall([]string{"stranded"}); err != nil {
		t.Fatalf("cmdUninstall failed: %v", err)
	}

	if _, err := os.Stat(harness.PointerPath("stranded")); !os.IsNotExist(err) {
		t.Errorf("pointer file still exists after uninstall: err=%v", err)
	}
}

// Prune must remove pointers whose source tree no longer exists, alongside
// the existing symlink-orphan pass. Healthy pointers must survive.
func TestCmdPrune_OrphanPointer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("YNH_HOME", home)
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	healthySrc := t.TempDir()
	if err := harness.SavePointer(&harness.Pointer{
		Name: "healthy", SourceType: "local",
		Source: healthySrc, InstalledAt: "2026-05-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	if err := harness.SavePointer(&harness.Pointer{
		Name: "stranded", SourceType: "local",
		Source: filepath.Join(t.TempDir(), "gone"), InstalledAt: "2026-05-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}

	if err := cmdPrune(); err != nil {
		t.Fatalf("cmdPrune failed: %v", err)
	}

	if _, err := os.Stat(harness.PointerPath("stranded")); !os.IsNotExist(err) {
		t.Errorf("orphan pointer still present after prune: err=%v", err)
	}
	if _, err := os.Stat(harness.PointerPath("healthy")); err != nil {
		t.Errorf("healthy pointer was removed by prune: %v", err)
	}
}
