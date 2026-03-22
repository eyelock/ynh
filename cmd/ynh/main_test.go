package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/persona"
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
			vendorFlag, prompt, args, action := parseRunArgs(tt.args)
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

func TestResolveVendor_FlagTakesPriority(t *testing.T) {
	p := &persona.Persona{
		Name:          "test",
		DefaultVendor: "codex",
	}

	got, err := resolveVendor("claude", p)
	if err != nil {
		t.Fatalf("resolveVendor failed: %v", err)
	}
	if got != "claude" {
		t.Errorf("got %q, want %q", got, "claude")
	}
}

func TestResolveVendor_PersonaDefault(t *testing.T) {
	p := &persona.Persona{
		Name:          "test",
		DefaultVendor: "codex",
	}

	got, err := resolveVendor("", p)
	if err != nil {
		t.Fatalf("resolveVendor failed: %v", err)
	}
	if got != "codex" {
		t.Errorf("got %q, want %q", got, "codex")
	}
}

func TestResolveVendor_GlobalConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	p := &persona.Persona{
		Name: "test",
	}

	got, err := resolveVendor("", p)
	if err != nil {
		t.Fatalf("resolveVendor failed: %v", err)
	}
	// Default global config is "claude"
	if got != "claude" {
		t.Errorf("got %q, want %q", got, "claude")
	}
}

func TestResolveVendor_EnvVar(t *testing.T) {
	t.Setenv("YNH_VENDOR", "codex")

	p := &persona.Persona{
		Name:          "test",
		DefaultVendor: "claude",
	}

	got, err := resolveVendor("", p)
	if err != nil {
		t.Fatalf("resolveVendor failed: %v", err)
	}
	if got != "codex" {
		t.Errorf("got %q, want %q", got, "codex")
	}
}

func TestResolveVendor_FlagBeatsEnvVar(t *testing.T) {
	t.Setenv("YNH_VENDOR", "codex")

	p := &persona.Persona{
		Name:          "test",
		DefaultVendor: "cursor",
	}

	got, err := resolveVendor("claude", p)
	if err != nil {
		t.Fatalf("resolveVendor failed: %v", err)
	}
	if got != "claude" {
		t.Errorf("got %q, want %q", got, "claude")
	}
}

func TestResolveVendor_EnvVarFallthrough(t *testing.T) {
	t.Setenv("YNH_VENDOR", "")

	p := &persona.Persona{
		Name:          "test",
		DefaultVendor: "codex",
	}

	got, err := resolveVendor("", p)
	if err != nil {
		t.Fatalf("resolveVendor failed: %v", err)
	}
	if got != "codex" {
		t.Errorf("got %q, want %q", got, "codex")
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

// installTestPersona creates a fake installed persona with a launcher and run dir.
func installTestPersona(t *testing.T, name string) {
	t.Helper()

	// Create persona directory with plugin manifest
	installDir := persona.InstalledDir(name)
	pluginDir := filepath.Join(installDir, ".claude-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pluginJSON := fmt.Sprintf(`{"name":%q,"version":"0.1.0"}`, name)
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(pluginJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "metadata.json"), []byte(`{"ynh":{"default_vendor":"claude"}}`), 0o644); err != nil {
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

	installTestPersona(t, "david")

	// Verify everything exists before uninstall
	installDir := persona.InstalledDir("david")
	if _, err := os.Stat(installDir); err != nil {
		t.Fatalf("persona not installed: %v", err)
	}

	if err := cmdUninstall([]string{"david"}); err != nil {
		t.Fatalf("cmdUninstall failed: %v", err)
	}

	// Persona directory should be gone
	if _, err := os.Stat(installDir); !os.IsNotExist(err) {
		t.Error("persona directory still exists after uninstall")
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
		t.Fatal("expected error for uninstalling nonexistent persona")
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

	// Should not error when no personas installed
	if err := cmdList(); err != nil {
		t.Fatalf("cmdList failed: %v", err)
	}
}

func TestCmdList_WithPersonas(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	installTestPersona(t, "alice")
	installTestPersona(t, "bob")

	if err := cmdList(); err != nil {
		t.Fatalf("cmdList failed: %v", err)
	}
}

func TestFormatProvenance(t *testing.T) {
	tests := []struct {
		name string
		prov *persona.Provenance
		want string
	}{
		{"nil", nil, "-"},
		{"local", &persona.Provenance{SourceType: "local", Source: "./my-persona"}, "./my-persona"},
		{"git no path", &persona.Provenance{SourceType: "git", Source: "github.com/eyelock/assistants"}, "eyelock/assistants"},
		{"git with path", &persona.Provenance{SourceType: "git", Source: "github.com/eyelock/assistants", Path: "ynh/david"}, "eyelock/assistants/ynh/david"},
		{"registry", &persona.Provenance{SourceType: "registry", Source: "github.com/eyelock/assistants", RegistryName: "my-reg"}, "eyelock/assistants (my-reg)"},
		{"registry with path", &persona.Provenance{SourceType: "registry", Source: "github.com/eyelock/assistants", Path: "ynh/david", RegistryName: "my-reg"}, "eyelock/assistants/ynh/david (my-reg)"},
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
		includes []persona.Include
		want     string
	}{
		{"empty", nil, "0"},
		{"single no pick", []persona.Include{
			{GitSource: persona.GitSource{Git: "github.com/example/skills", Path: "dev"}},
		}, "example/skills/dev"},
		{"single with pick", []persona.Include{
			{GitSource: persona.GitSource{Git: "github.com/example/skills", Path: "dev"}, Pick: []string{"a", "b"}},
		}, "example/skills/dev [2]"},
		{"with ref", []persona.Include{
			{GitSource: persona.GitSource{Git: "github.com/example/skills", Path: "dev", Ref: "v1.2.0"}},
		}, "example/skills/dev@v1.2.0"},
		{"main ref omitted", []persona.Include{
			{GitSource: persona.GitSource{Git: "github.com/example/skills", Ref: "main"}},
		}, "example/skills"},
		{"multiple", []persona.Include{
			{GitSource: persona.GitSource{Git: "github.com/example/skills", Path: "dev"}, Pick: []string{"a", "b"}},
			{GitSource: persona.GitSource{Git: "github.com/example/skills", Path: "infra"}, Pick: []string{"c"}},
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
		delegates []persona.Delegate
		want      string
	}{
		{"empty", nil, "0"},
		{"single", []persona.Delegate{
			{GitSource: persona.GitSource{Git: "github.com/example/team"}},
		}, "example/team"},
		{"with path and ref", []persona.Delegate{
			{GitSource: persona.GitSource{Git: "github.com/example/mono", Path: "personas/ops", Ref: "v2"}},
		}, "example/mono/personas/ops@v2"},
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

	// Create a persona with artifacts
	personaDir := filepath.Join(dir, ".ynh", "personas", "artfmt")
	installDir := filepath.Join(personaDir, ".claude-plugin")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "plugin.json"),
		[]byte(`{"name":"artfmt","version":"0.1.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Add 2 skills, 1 agent
	for _, skill := range []string{"a", "b"} {
		sd := filepath.Join(personaDir, "skills", skill)
		if err := os.MkdirAll(sd, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sd, "SKILL.md"), []byte("#"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	agentDir := filepath.Join(personaDir, "agents")
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

	installTestPersona(t, "neart")
	got := formatArtifactSummary("neart")
	if got != "0" {
		t.Errorf("formatArtifactSummary() = %q, want %q", got, "0")
	}
}

func TestCmdInfo_Success(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	installTestPersona(t, "david")

	err := cmdInfo([]string{"david"})
	if err != nil {
		t.Fatalf("cmdInfo failed: %v", err)
	}
}

func TestCmdInfo_NotFound(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	if err := os.MkdirAll(filepath.Join(dir, ".ynh", "personas"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := cmdInfo([]string{"nonexistent"})
	if err == nil {
		t.Fatal("expected error for nonexistent persona")
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

	// Create a local persona source
	srcDir := filepath.Join(dir, "my-persona")
	pluginDir := filepath.Join(srcDir, ".claude-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"),
		[]byte(`{"name":"provtest","version":"0.1.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	err := cmdInstall([]string{srcDir})
	if err != nil {
		t.Fatalf("cmdInstall failed: %v", err)
	}

	// Load installed persona and check provenance
	p, err := persona.Load("provtest")
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
		t.Fatal("expected error for nonexistent persona")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCmdUpdate_NoGitSources(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	// Install a persona with no includes/delegates
	installTestPersona(t, "minimal")

	err := cmdUpdate([]string{"minimal"})
	if err != nil {
		t.Fatalf("cmdUpdate should succeed for persona with no git sources: %v", err)
	}
}

func TestCmdInstall_PathFlag(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("YNH_HOME", "")

	// Create a monorepo-style layout with a persona in a subdirectory
	monoDir := filepath.Join(dir, "monorepo")
	pluginDir := filepath.Join(monoDir, "personas", "alice", ".claude-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"),
		[]byte(`{"name":"alice","version":"0.1.0","description":"test persona"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Install with --path flag
	err := cmdInstall([]string{monoDir, "--path", "personas/alice"})
	if err != nil {
		t.Fatalf("cmdInstall with --path failed: %v", err)
	}

	// Verify persona was installed
	installDir := persona.InstalledDir("alice")
	if _, err := os.Stat(filepath.Join(installDir, ".claude-plugin", "plugin.json")); err != nil {
		t.Fatal("persona plugin.json not found after install")
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
