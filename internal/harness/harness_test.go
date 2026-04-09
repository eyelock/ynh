package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

func TestDetectFormat_Harness(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "x")
	if got := DetectFormat(dir); got != "harness" {
		t.Errorf("DetectFormat = %q, want %q", got, "harness")
	}
}

func TestDetectFormat_Legacy(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, ".claude-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := DetectFormat(dir); got != "legacy" {
		t.Errorf("DetectFormat = %q, want %q", got, "legacy")
	}
}

func TestDetectFormat_None(t *testing.T) {
	dir := t.TempDir()
	if got := DetectFormat(dir); got != "" {
		t.Errorf("DetectFormat = %q, want empty", got)
	}
}

func TestLoad_HarnessFormat(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("YNH_HOME", "")

	harnessDir := filepath.Join(dir, ".ynh", "harnesses", "mytest")
	writeTestHarness(t, harnessDir, "mytest")

	p, err := Load("mytest")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if p.Name != "mytest" {
		t.Errorf("Name = %q, want %q", p.Name, "mytest")
	}
	if p.DefaultVendor != "claude" {
		t.Errorf("DefaultVendor = %q, want %q", p.DefaultVendor, "claude")
	}
}

func TestLoad_LegacyFormat(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("YNH_HOME", "")

	harnessDir := filepath.Join(dir, ".ynh", "harnesses", "old")
	pluginDir := filepath.Join(harnessDir, ".claude-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"old"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load("old")
	if err == nil {
		t.Fatal("expected error for legacy format")
	}
	if got := err.Error(); !strings.Contains(got, "legacy format detected") {
		t.Errorf("error = %q, want legacy format hint", got)
	}
}

func TestLoad_NotFound(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("YNH_HOME", "")

	harnessDir := filepath.Join(dir, ".ynh", "harnesses", "empty")
	if err := os.MkdirAll(harnessDir, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := Load("empty")
	if err == nil {
		t.Fatal("expected error for directory without harness.json")
	}
}

func TestLoadDir_FullMetadata(t *testing.T) {
	dir := t.TempDir()
	hj := `{
		"name": "full",
		"version": "0.1.0",
		"default_vendor": "claude",
		"includes": [
			{"git": "github.com/example/skills", "ref": "v1.0.0", "pick": ["skills/commit", "agents/reviewer"]},
			{"git": "github.com/company/monorepo", "path": "packages/ai-config", "pick": ["skills/deploy"]}
		],
		"delegates_to": [
			{"git": "github.com/example/team-harness"},
			{"git": "github.com/company/monorepo", "path": "harnesses/team-ops"}
		]
	}`
	if err := os.WriteFile(filepath.Join(dir, ".harness.json"), []byte(hj), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir failed: %v", err)
	}

	if p.Name != "full" {
		t.Errorf("Name = %q, want %q", p.Name, "full")
	}
	if p.DefaultVendor != "claude" {
		t.Errorf("DefaultVendor = %q, want %q", p.DefaultVendor, "claude")
	}
	if len(p.Includes) != 2 {
		t.Fatalf("Includes length = %d, want 2", len(p.Includes))
	}
	if p.Includes[0].Git != "github.com/example/skills" {
		t.Errorf("Include[0] git = %q", p.Includes[0].Git)
	}
	if p.Includes[0].Ref != "v1.0.0" {
		t.Errorf("Include[0] ref = %q", p.Includes[0].Ref)
	}
	if len(p.Includes[0].Pick) != 2 {
		t.Errorf("Include[0] Pick length = %d, want 2", len(p.Includes[0].Pick))
	}
	if p.Includes[1].Path != "packages/ai-config" {
		t.Errorf("Include[1] path = %q", p.Includes[1].Path)
	}
	if len(p.DelegatesTo) != 2 {
		t.Fatalf("DelegatesTo length = %d, want 2", len(p.DelegatesTo))
	}
	if p.DelegatesTo[0].Git != "github.com/example/team-harness" {
		t.Errorf("Delegate[0] git = %q", p.DelegatesTo[0].Git)
	}
	if p.DelegatesTo[1].Path != "harnesses/team-ops" {
		t.Errorf("Delegate[1] path = %q", p.DelegatesTo[1].Path)
	}
}

func TestLoadDir_Minimal(t *testing.T) {
	dir := t.TempDir()
	writeTestHarnessMinimal(t, dir, "minimal")

	p, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir failed: %v", err)
	}
	if p.Name != "minimal" {
		t.Errorf("Name = %q, want %q", p.Name, "minimal")
	}
	if p.DefaultVendor != "" {
		t.Errorf("DefaultVendor = %q, want empty", p.DefaultVendor)
	}
}

func TestLoadDir_InvalidName(t *testing.T) {
	badNames := []string{
		"../../../etc/cron.d/evil",
		"foo; rm -rf /",
		".hidden",
		"-flag",
		"name with spaces",
		"name\tnewline",
		"/absolute/path",
	}

	for _, name := range badNames {
		dir := t.TempDir()
		hj := fmt.Sprintf(`{"name":%q,"version":"0.1.0"}`, name)
		if err := os.WriteFile(filepath.Join(dir, ".harness.json"), []byte(hj), 0o644); err != nil {
			t.Fatal(err)
		}

		_, err := LoadDir(dir)
		if err == nil {
			t.Errorf("expected error for invalid name %q", name)
		}
	}
}

func TestLoadDir_ValidNames(t *testing.T) {
	validNames := []string{
		"david",
		"team-dev",
		"my_harness",
		"v2.0",
		"CamelCase",
		"a",
	}

	for _, name := range validNames {
		dir := t.TempDir()
		writeTestHarnessMinimal(t, dir, name)

		p, err := LoadDir(dir)
		if err != nil {
			t.Errorf("unexpected error for valid name %q: %v", name, err)
			continue
		}
		if p.Name != name {
			t.Errorf("Name = %q, want %q", p.Name, name)
		}
	}
}

func TestList(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("YNH_HOME", "")

	harnessesDir := filepath.Join(dir, ".ynh", "harnesses")
	writeTestHarness(t, filepath.Join(harnessesDir, "alpha"), "alpha")
	writeTestHarness(t, filepath.Join(harnessesDir, "beta"), "beta")

	// Empty dir (no manifest)
	if err := os.MkdirAll(filepath.Join(harnessesDir, "no-manifest"), 0o755); err != nil {
		t.Fatal(err)
	}

	names, err := List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("List returned %d names, want 2: %v", len(names), names)
	}

	found := map[string]bool{}
	for _, n := range names {
		found[n] = true
	}
	if !found["alpha"] {
		t.Error("List missing 'alpha'")
	}
	if !found["beta"] {
		t.Error("List missing 'beta'")
	}
	if found["no-manifest"] {
		t.Error("List should not include dir without harness.json")
	}
}

func TestList_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("YNH_HOME", "")

	names, err := List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if names != nil {
		t.Errorf("List returned %v, want nil", names)
	}
}

func TestInstalledDir(t *testing.T) {
	dir := InstalledDir("david")
	if dir == "" {
		t.Fatal("InstalledDir returned empty")
	}
	if filepath.Base(dir) != "david" {
		t.Errorf("InstalledDir base = %q, want %q", filepath.Base(dir), "david")
	}
}

func TestLoadDir_WithProvenance(t *testing.T) {
	dir := t.TempDir()
	hj := `{
		"name": "prov",
		"version": "0.1.0",
		"default_vendor": "claude",
		"installed_from": {
			"source_type": "git",
			"source": "github.com/example/repo",
			"path": "harnesses/prov",
			"installed_at": "2026-03-22T10:30:00Z"
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, ".harness.json"), []byte(hj), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir failed: %v", err)
	}
	if p.InstalledFrom == nil {
		t.Fatal("InstalledFrom is nil")
	}
	if p.InstalledFrom.SourceType != "git" {
		t.Errorf("SourceType = %q, want %q", p.InstalledFrom.SourceType, "git")
	}
	if p.InstalledFrom.Source != "github.com/example/repo" {
		t.Errorf("Source = %q, want %q", p.InstalledFrom.Source, "github.com/example/repo")
	}
	if p.InstalledFrom.Path != "harnesses/prov" {
		t.Errorf("Path = %q, want %q", p.InstalledFrom.Path, "harnesses/prov")
	}
	if p.InstalledFrom.InstalledAt != "2026-03-22T10:30:00Z" {
		t.Errorf("InstalledAt = %q", p.InstalledFrom.InstalledAt)
	}
}

func TestLoadDir_WithoutProvenance(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "noprov")

	p, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir failed: %v", err)
	}
	if p.InstalledFrom != nil {
		t.Errorf("InstalledFrom should be nil, got %+v", p.InstalledFrom)
	}
}

func TestLoadDir_RegistryProvenance(t *testing.T) {
	dir := t.TempDir()
	hj := `{
		"name": "regprov",
		"version": "0.1.0",
		"default_vendor": "claude",
		"installed_from": {
			"source_type": "registry",
			"source": "github.com/example/repo",
			"registry_name": "my-registry",
			"installed_at": "2026-03-22T10:30:00Z"
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, ".harness.json"), []byte(hj), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir failed: %v", err)
	}
	if p.InstalledFrom == nil {
		t.Fatal("InstalledFrom is nil")
	}
	if p.InstalledFrom.RegistryName != "my-registry" {
		t.Errorf("RegistryName = %q, want %q", p.InstalledFrom.RegistryName, "my-registry")
	}
}

func TestResolveProfile_MergesMCPServers(t *testing.T) {
	h := &Harness{
		Name: "test",
		MCPServers: map[string]plugin.MCPServer{
			"github": {Command: "gh-cmd", Env: map[string]string{"TOKEN": "abc"}},
			"slack":  {Command: "slack-cmd"},
		},
		Profiles: map[string]plugin.Profile{
			"ci": {
				MCPServers: map[string]*plugin.MCPServer{
					"ci-server": {Command: "ci-cmd"},
				},
			},
		},
	}

	resolved, err := ResolveProfile(h, "ci")
	if err != nil {
		t.Fatal(err)
	}

	// Profile adds server, inherits others
	if _, ok := resolved.MCPServers["github"]; !ok {
		t.Error("expected inherited github server")
	}
	if _, ok := resolved.MCPServers["slack"]; !ok {
		t.Error("expected inherited slack server")
	}
	if _, ok := resolved.MCPServers["ci-server"]; !ok {
		t.Error("expected ci-server from profile")
	}
	if resolved.Name != "test" {
		t.Errorf("Name = %q, want test", resolved.Name)
	}
}

func TestResolveProfile_NullRemovesServer(t *testing.T) {
	h := &Harness{
		Name: "test",
		MCPServers: map[string]plugin.MCPServer{
			"postgres": {Command: "pg-cmd"},
			"github":   {Command: "gh-cmd"},
		},
		Profiles: map[string]plugin.Profile{
			"ci": {
				MCPServers: map[string]*plugin.MCPServer{
					"postgres": nil, // remove
				},
			},
		},
	}

	resolved, err := ResolveProfile(h, "ci")
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := resolved.MCPServers["postgres"]; ok {
		t.Error("expected postgres to be removed by null")
	}
	if _, ok := resolved.MCPServers["github"]; !ok {
		t.Error("expected github to be inherited")
	}
}

func TestResolveProfile_HooksPerEventReplace(t *testing.T) {
	h := &Harness{
		Name: "test",
		Hooks: map[string][]plugin.HookEntry{
			"before_tool": {{Command: "echo default-before"}},
			"on_stop":     {{Command: "echo default-stop"}},
		},
		Profiles: map[string]plugin.Profile{
			"ci": {
				Hooks: map[string][]plugin.HookEntry{
					"on_stop": {{Command: "echo ci-stop"}},
				},
			},
		},
	}

	resolved, err := ResolveProfile(h, "ci")
	if err != nil {
		t.Fatal(err)
	}

	// before_tool inherited (profile didn't declare it)
	if _, ok := resolved.Hooks["before_tool"]; !ok {
		t.Error("expected before_tool to be inherited")
	}
	// on_stop replaced by profile
	entries := resolved.Hooks["on_stop"]
	if len(entries) != 1 || entries[0].Command != "echo ci-stop" {
		t.Errorf("on_stop = %v, want ci-stop", entries)
	}
}

func TestResolveProfile_EmptyProfile(t *testing.T) {
	h := &Harness{
		Name: "test",
		Hooks: map[string][]plugin.HookEntry{
			"before_tool": {{Command: "echo default"}},
		},
		MCPServers: map[string]plugin.MCPServer{
			"github": {Command: "gh-cmd"},
		},
		Profiles: map[string]plugin.Profile{
			"empty": {},
		},
	}

	resolved, err := ResolveProfile(h, "empty")
	if err != nil {
		t.Fatal(err)
	}

	// Empty profile inherits everything unchanged
	if _, ok := resolved.Hooks["before_tool"]; !ok {
		t.Error("expected hooks inherited from empty profile")
	}
	if _, ok := resolved.MCPServers["github"]; !ok {
		t.Error("expected mcp_servers inherited from empty profile")
	}
}

func TestResolveProfile_FullOverride(t *testing.T) {
	h := &Harness{
		Name: "test",
		Hooks: map[string][]plugin.HookEntry{
			"before_tool": {{Command: "echo default"}},
		},
		MCPServers: map[string]plugin.MCPServer{
			"default-server": {Command: "default-cmd"},
		},
		Profiles: map[string]plugin.Profile{
			"ci": {
				Hooks: map[string][]plugin.HookEntry{
					"before_tool": {{Command: "echo ci"}},
					"on_stop":     {{Command: "echo ci-stop"}},
				},
				MCPServers: map[string]*plugin.MCPServer{
					"default-server": nil,
					"ci-server":      {Command: "ci-cmd"},
				},
			},
		},
	}

	resolved, err := ResolveProfile(h, "ci")
	if err != nil {
		t.Fatal(err)
	}

	// before_tool replaced by profile
	entries := resolved.Hooks["before_tool"]
	if len(entries) != 1 || entries[0].Command != "echo ci" {
		t.Errorf("before_tool = %v, want ci", entries)
	}
	// on_stop added by profile
	if _, ok := resolved.Hooks["on_stop"]; !ok {
		t.Error("expected on_stop from profile")
	}
	// default-server removed, ci-server added
	if _, ok := resolved.MCPServers["default-server"]; ok {
		t.Error("expected default-server removed by null")
	}
	if _, ok := resolved.MCPServers["ci-server"]; !ok {
		t.Error("expected ci-server from profile")
	}
}

func TestResolveProfile_MCPServerEnvMerge(t *testing.T) {
	h := &Harness{
		Name: "test",
		MCPServers: map[string]plugin.MCPServer{
			"github": {
				Command: "gh-cmd",
				Env:     map[string]string{"TOKEN": "abc", "ORG": "myorg"},
			},
		},
		Profiles: map[string]plugin.Profile{
			"ci": {
				MCPServers: map[string]*plugin.MCPServer{
					"github": {Env: map[string]string{"TOKEN": "ci-token"}},
				},
			},
		},
	}

	resolved, err := ResolveProfile(h, "ci")
	if err != nil {
		t.Fatal(err)
	}

	gh := resolved.MCPServers["github"]
	// Command inherited (profile didn't set it)
	if gh.Command != "gh-cmd" {
		t.Errorf("Command = %q, want gh-cmd", gh.Command)
	}
	// TOKEN overridden by profile
	if gh.Env["TOKEN"] != "ci-token" {
		t.Errorf("TOKEN = %q, want ci-token", gh.Env["TOKEN"])
	}
	// ORG inherited
	if gh.Env["ORG"] != "myorg" {
		t.Errorf("ORG = %q, want myorg", gh.Env["ORG"])
	}
}

func TestResolveProfile_MissingProfile(t *testing.T) {
	h := &Harness{Name: "test"}

	_, err := ResolveProfile(h, "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing profile")
	}
	if !strings.Contains(err.Error(), `profile "nonexistent" not defined`) {
		t.Errorf("error = %q", err)
	}
}

func TestResolveProfile_EmptyName(t *testing.T) {
	h := &Harness{Name: "test"}

	resolved, err := ResolveProfile(h, "")
	if err != nil {
		t.Fatal(err)
	}
	if resolved != h {
		t.Error("empty profile name should return original harness")
	}
}

func TestScanArtifacts(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("YNH_HOME", "")

	harnessDir := filepath.Join(dir, ".ynh", "harnesses", "arttest")
	writeTestHarness(t, harnessDir, "arttest")

	// Create skills (directories with SKILL.md)
	for _, skill := range []string{"greet", "review"} {
		skillDir := filepath.Join(harnessDir, "skills", skill)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# "+skill), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Create agents, rules, commands (.md files)
	for _, artType := range []string{"agents", "rules", "commands"} {
		artDir := filepath.Join(harnessDir, artType)
		if err := os.MkdirAll(artDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(artDir, "test-one.md"), []byte("# test"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	arts, err := ScanArtifacts("arttest")
	if err != nil {
		t.Fatal(err)
	}

	if len(arts.Skills) != 2 {
		t.Errorf("Skills = %v, want 2", arts.Skills)
	}
	if arts.Skills[0] != "greet" || arts.Skills[1] != "review" {
		t.Errorf("Skills = %v, want [greet review]", arts.Skills)
	}
	if len(arts.Agents) != 1 || arts.Agents[0] != "test-one" {
		t.Errorf("Agents = %v, want [test-one]", arts.Agents)
	}
	if len(arts.Rules) != 1 {
		t.Errorf("Rules = %v, want 1", arts.Rules)
	}
	if len(arts.Commands) != 1 {
		t.Errorf("Commands = %v, want 1", arts.Commands)
	}
	if arts.Total() != 5 {
		t.Errorf("Total() = %d, want 5", arts.Total())
	}
}

func TestScanArtifacts_Empty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("YNH_HOME", "")

	harnessDir := filepath.Join(dir, ".ynh", "harnesses", "empty")
	writeTestHarness(t, harnessDir, "empty")

	arts, err := ScanArtifacts("empty")
	if err != nil {
		t.Fatal(err)
	}
	if arts.Total() != 0 {
		t.Errorf("Total() = %d, want 0", arts.Total())
	}
}

func TestScanArtifacts_SkillWithoutManifest(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("YNH_HOME", "")

	harnessDir := filepath.Join(dir, ".ynh", "harnesses", "nosk")
	writeTestHarness(t, harnessDir, "nosk")

	// Create a directory in skills/ but without SKILL.md — should not be counted
	badSkill := filepath.Join(harnessDir, "skills", "bad")
	if err := os.MkdirAll(badSkill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(badSkill, "README.md"), []byte("not a skill"), 0o644); err != nil {
		t.Fatal(err)
	}

	arts, err := ScanArtifacts("nosk")
	if err != nil {
		t.Fatal(err)
	}
	if len(arts.Skills) != 0 {
		t.Errorf("Skills = %v, want empty (no SKILL.md)", arts.Skills)
	}
}

// writeTestHarness creates a minimal harness.json with default_vendor in dir.
func writeTestHarness(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	hj := fmt.Sprintf(`{"name":%q,"version":"0.1.0","default_vendor":"claude"}`, name)
	if err := os.WriteFile(filepath.Join(dir, ".harness.json"), []byte(hj), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeTestHarnessMinimal creates a minimal harness.json without default_vendor.
func writeTestHarnessMinimal(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	hj := fmt.Sprintf(`{"name":%q,"version":"0.1.0"}`, name)
	if err := os.WriteFile(filepath.Join(dir, ".harness.json"), []byte(hj), 0o644); err != nil {
		t.Fatal(err)
	}
}
