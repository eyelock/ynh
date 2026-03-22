package persona

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestDetectFormat_Plugin(t *testing.T) {
	dir := t.TempDir()
	writeTestPlugin(t, dir, "x")
	if got := DetectFormat(dir); got != "plugin" {
		t.Errorf("DetectFormat = %q, want %q", got, "plugin")
	}
}

func TestDetectFormat_None(t *testing.T) {
	dir := t.TempDir()
	if got := DetectFormat(dir); got != "" {
		t.Errorf("DetectFormat = %q, want empty", got)
	}
}

func TestLoad_PluginFormat(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("YNH_HOME", "")

	personaDir := filepath.Join(dir, ".ynh", "personas", "plugtest")
	writeTestPlugin(t, personaDir, "plugtest")
	if err := os.WriteFile(filepath.Join(personaDir, "metadata.json"),
		[]byte(`{"ynh":{"default_vendor":"claude"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := Load("plugtest")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if p.Name != "plugtest" {
		t.Errorf("Name = %q, want %q", p.Name, "plugtest")
	}
	if p.DefaultVendor != "claude" {
		t.Errorf("DefaultVendor = %q, want %q", p.DefaultVendor, "claude")
	}
}

func TestLoad_NotFound(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("YNH_HOME", "")

	personaDir := filepath.Join(dir, ".ynh", "personas", "empty")
	if err := os.MkdirAll(personaDir, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := Load("empty")
	if err == nil {
		t.Fatal("expected error for directory without plugin.json")
	}
}

func TestLoadPluginDir_FullMetadata(t *testing.T) {
	dir := t.TempDir()
	writeTestPlugin(t, dir, "full")
	meta := `{
		"ynh": {
			"default_vendor": "claude",
			"includes": [
				{"git": "github.com/example/skills", "ref": "v1.0.0", "pick": ["skills/commit", "agents/reviewer"]},
				{"git": "github.com/company/monorepo", "path": "packages/ai-config", "pick": ["skills/deploy"]}
			],
			"delegates_to": [
				{"git": "github.com/example/team-persona"},
				{"git": "github.com/company/monorepo", "path": "personas/team-ops"}
			]
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := LoadPluginDir(dir)
	if err != nil {
		t.Fatalf("LoadPluginDir failed: %v", err)
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
	if p.DelegatesTo[0].Git != "github.com/example/team-persona" {
		t.Errorf("Delegate[0] git = %q", p.DelegatesTo[0].Git)
	}
	if p.DelegatesTo[1].Path != "personas/team-ops" {
		t.Errorf("Delegate[1] path = %q", p.DelegatesTo[1].Path)
	}
}

func TestLoadPluginDir_NoMetadata(t *testing.T) {
	dir := t.TempDir()
	writeTestPlugin(t, dir, "minimal")

	p, err := LoadPluginDir(dir)
	if err != nil {
		t.Fatalf("LoadPluginDir failed: %v", err)
	}
	if p.Name != "minimal" {
		t.Errorf("Name = %q, want %q", p.Name, "minimal")
	}
	if p.DefaultVendor != "" {
		t.Errorf("DefaultVendor = %q, want empty", p.DefaultVendor)
	}
}

func TestLoadPluginDir_InvalidName(t *testing.T) {
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
		pluginDir := filepath.Join(dir, ".claude-plugin")
		if err := os.MkdirAll(pluginDir, 0o755); err != nil {
			t.Fatal(err)
		}
		pj := fmt.Sprintf(`{"name":%q,"version":"0.1.0"}`, name)
		if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(pj), 0o644); err != nil {
			t.Fatal(err)
		}

		_, err := LoadPluginDir(dir)
		if err == nil {
			t.Errorf("expected error for invalid name %q", name)
		}
	}
}

func TestLoadPluginDir_ValidNames(t *testing.T) {
	validNames := []string{
		"david",
		"team-dev",
		"my_persona",
		"v2.0",
		"CamelCase",
		"a",
	}

	for _, name := range validNames {
		dir := t.TempDir()
		writeTestPlugin(t, dir, name)

		p, err := LoadPluginDir(dir)
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

	personasDir := filepath.Join(dir, ".ynh", "personas")
	writeTestPlugin(t, filepath.Join(personasDir, "alpha"), "alpha")
	writeTestPlugin(t, filepath.Join(personasDir, "beta"), "beta")

	// Empty dir (no manifest)
	if err := os.MkdirAll(filepath.Join(personasDir, "no-manifest"), 0o755); err != nil {
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
		t.Error("List should not include dir without plugin.json")
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

func TestLoadPluginDir_WithProvenance(t *testing.T) {
	dir := t.TempDir()
	writeTestPlugin(t, dir, "prov")
	meta := `{
		"ynh": {
			"default_vendor": "claude",
			"installed_from": {
				"source_type": "git",
				"source": "github.com/example/repo",
				"path": "personas/prov",
				"installed_at": "2026-03-22T10:30:00Z"
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := LoadPluginDir(dir)
	if err != nil {
		t.Fatalf("LoadPluginDir failed: %v", err)
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
	if p.InstalledFrom.Path != "personas/prov" {
		t.Errorf("Path = %q, want %q", p.InstalledFrom.Path, "personas/prov")
	}
	if p.InstalledFrom.InstalledAt != "2026-03-22T10:30:00Z" {
		t.Errorf("InstalledAt = %q", p.InstalledFrom.InstalledAt)
	}
}

func TestLoadPluginDir_WithoutProvenance(t *testing.T) {
	dir := t.TempDir()
	writeTestPlugin(t, dir, "noprov")
	meta := `{"ynh": {"default_vendor": "claude"}}`
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := LoadPluginDir(dir)
	if err != nil {
		t.Fatalf("LoadPluginDir failed: %v", err)
	}
	if p.InstalledFrom != nil {
		t.Errorf("InstalledFrom should be nil, got %+v", p.InstalledFrom)
	}
}

func TestLoadPluginDir_RegistryProvenance(t *testing.T) {
	dir := t.TempDir()
	writeTestPlugin(t, dir, "regprov")
	meta := `{
		"ynh": {
			"default_vendor": "claude",
			"installed_from": {
				"source_type": "registry",
				"source": "github.com/example/repo",
				"registry_name": "my-registry",
				"installed_at": "2026-03-22T10:30:00Z"
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := LoadPluginDir(dir)
	if err != nil {
		t.Fatalf("LoadPluginDir failed: %v", err)
	}
	if p.InstalledFrom == nil {
		t.Fatal("InstalledFrom is nil")
	}
	if p.InstalledFrom.RegistryName != "my-registry" {
		t.Errorf("RegistryName = %q, want %q", p.InstalledFrom.RegistryName, "my-registry")
	}
}

func TestScanArtifacts(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("YNH_HOME", "")

	personaDir := filepath.Join(dir, ".ynh", "personas", "arttest")
	writeTestPlugin(t, personaDir, "arttest")

	// Create skills (directories with SKILL.md)
	for _, skill := range []string{"greet", "review"} {
		skillDir := filepath.Join(personaDir, "skills", skill)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# "+skill), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Create agents, rules, commands (.md files)
	for _, artType := range []string{"agents", "rules", "commands"} {
		artDir := filepath.Join(personaDir, artType)
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

	personaDir := filepath.Join(dir, ".ynh", "personas", "empty")
	writeTestPlugin(t, personaDir, "empty")

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

	personaDir := filepath.Join(dir, ".ynh", "personas", "nosk")
	writeTestPlugin(t, personaDir, "nosk")

	// Create a directory in skills/ but without SKILL.md — should not be counted
	badSkill := filepath.Join(personaDir, "skills", "bad")
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

// writeTestPlugin creates a minimal .claude-plugin/plugin.json in dir.
func writeTestPlugin(t *testing.T, dir, name string) {
	t.Helper()
	pluginDir := filepath.Join(dir, ".claude-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pj := fmt.Sprintf(`{"name":%q,"version":"0.1.0"}`, name)
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(pj), 0o644); err != nil {
		t.Fatal(err)
	}
}
