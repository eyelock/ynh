package main

import (
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
		name       string
		args       []string
		wantVendor string
		wantPrompt string
		wantArgs   []string
		wantAction string
	}{
		{
			name:       "just prompt",
			args:       []string{"review this PR"},
			wantPrompt: "review this PR",
		},
		{
			name:       "vendor flag only",
			args:       []string{"-v", "codex"},
			wantVendor: "codex",
		},
		{
			name:       "vendor and prompt",
			args:       []string{"-v", "codex", "fix the bug"},
			wantVendor: "codex",
			wantPrompt: "fix the bug",
		},
		{
			name:       "vendor flag with passthrough via separator",
			args:       []string{"-v", "claude", "--model", "opus", "--", "fix the bug"},
			wantVendor: "claude",
			wantPrompt: "fix the bug",
			wantArgs:   []string{"--model", "opus"},
		},
		{
			name:       "passthrough with prompt via separator",
			args:       []string{"--model", "opus", "--", "do something"},
			wantPrompt: "do something",
			wantArgs:   []string{"--model", "opus"},
		},
		{
			name:       "double dash separator",
			args:       []string{"--verbose", "--full-auto", "--", "refactor auth"},
			wantPrompt: "refactor auth",
			wantArgs:   []string{"--verbose", "--full-auto"},
		},
		{
			name:       "mixed flags vendor and double dash",
			args:       []string{"-v", "codex", "--full-auto", "--model", "gpt-5", "--", "deploy it"},
			wantVendor: "codex",
			wantPrompt: "deploy it",
			wantArgs:   []string{"--full-auto", "--model", "gpt-5"},
		},
		{
			name:       "boolean flags then prompt",
			args:       []string{"--verbose", "--full-auto", "fix this"},
			wantPrompt: "fix this",
			wantArgs:   []string{"--verbose", "--full-auto"},
		},
		{
			name:     "flags only no prompt",
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vendorFlag, _, prompt, args, action := parseRunArgs(tt.args)
			if vendorFlag != tt.wantVendor {
				t.Errorf("vendor = %q, want %q", vendorFlag, tt.wantVendor)
			}
			if prompt != tt.wantPrompt {
				t.Errorf("prompt = %q, want %q", prompt, tt.wantPrompt)
			}
			if action != tt.wantAction {
				t.Errorf("action = %q, want %q", action, tt.wantAction)
			}
			if len(args) != len(tt.wantArgs) {
				t.Errorf("vendorArgs = %v, want %v", args, tt.wantArgs)
			} else {
				for i := range args {
					if args[i] != tt.wantArgs[i] {
						t.Errorf("vendorArgs[%d] = %q, want %q", i, args[i], tt.wantArgs[i])
					}
				}
			}
		})
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

	// Create harness directory with harness.json
	installDir := harness.InstalledDir(name)
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatal(err)
	}
	harnessJSON := fmt.Sprintf(`{"name":%q,"version":"0.1.0","default_vendor":"claude"}`, name)
	if err := os.WriteFile(filepath.Join(installDir, "harness.json"), []byte(harnessJSON), 0o644); err != nil {
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

func TestCmdList_Empty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}

	// Should not error when no harnesses installed
	if err := cmdList(); err != nil {
		t.Fatalf("cmdList failed: %v", err)
	}
}

func TestCmdList_WithHarnesses(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	installTestHarness(t, "alice")
	installTestHarness(t, "bob")

	if err := cmdList(); err != nil {
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
	if err := os.WriteFile(filepath.Join(harnessDir, "harness.json"),
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
	if !strings.Contains(err.Error(), "not found") {
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
	if err := os.WriteFile(filepath.Join(srcDir, "harness.json"),
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
	if !strings.Contains(err.Error(), "not found") {
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
	if err := os.WriteFile(filepath.Join(aliceDir, "harness.json"),
		[]byte(`{"name":"alice","version":"0.1.0","description":"test harness"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Install with --path flag
	err := cmdInstall([]string{monoDir, "--path", "harnesses/alice"})
	if err != nil {
		t.Fatalf("cmdInstall with --path failed: %v", err)
	}

	// Verify harness was installed
	installDir := harness.InstalledDir("alice")
	if _, err := os.Stat(filepath.Join(installDir, "harness.json")); err != nil {
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
