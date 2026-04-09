package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsHarnessDir_True(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "harness.json"), []byte(`{"name":"test","version":"0.1.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if !IsHarnessDir(dir) {
		t.Error("expected IsHarnessDir to return true")
	}
}

func TestIsHarnessDir_False(t *testing.T) {
	dir := t.TempDir()
	if IsHarnessDir(dir) {
		t.Error("expected IsHarnessDir to return false for empty dir")
	}
}

func TestIsLegacyPluginDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".claude-plugin", "plugin.json"), []byte(`{"name":"test"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if !IsLegacyPluginDir(dir) {
		t.Error("expected IsLegacyPluginDir to return true")
	}
}

func TestLoadHarnessJSON_Valid(t *testing.T) {
	dir := t.TempDir()
	writeHarnessJSON(t, dir, `{"name":"test-harness","version":"1.0.0"}`)

	hj, err := LoadHarnessJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	if hj.Name != "test-harness" {
		t.Errorf("Name = %q, want %q", hj.Name, "test-harness")
	}
	if hj.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", hj.Version, "1.0.0")
	}
}

func TestLoadHarnessJSON_FullFields(t *testing.T) {
	dir := t.TempDir()
	writeHarnessJSON(t, dir, `{
		"name": "full",
		"version": "0.1.0",
		"description": "A full harness",
		"author": {"name": "David", "email": "david@example.com", "url": "https://example.com"},
		"keywords": ["go", "typescript"],
		"default_vendor": "claude",
		"includes": [
			{"git": "github.com/example/skills", "ref": "v1.0.0", "pick": ["skills/hello"]}
		],
		"delegates_to": [
			{"git": "github.com/example/team"}
		],
		"hooks": {
			"before_tool": [
				{"matcher": "Bash", "command": "echo before bash"},
				{"command": "echo before all"}
			],
			"on_stop": [
				{"command": "echo done"}
			]
		},
		"mcp_servers": {
			"github": {
				"command": "npx",
				"args": ["-y", "@modelcontextprotocol/server-github"],
				"env": {"GITHUB_TOKEN": "${GITHUB_TOKEN}"}
			},
			"api": {
				"url": "https://api.example.com/mcp",
				"headers": {"Authorization": "Bearer ${API_KEY}"}
			}
		},
		"profiles": {
			"ci": {
				"hooks": {
					"before_tool": [{"command": "echo ci only"}]
				}
			}
		}
	}`)

	hj, err := LoadHarnessJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	if hj.Name != "full" {
		t.Errorf("Name = %q", hj.Name)
	}
	if hj.Author == nil || hj.Author.Email != "david@example.com" {
		t.Errorf("Author = %+v", hj.Author)
	}
	if hj.Author.URL != "https://example.com" {
		t.Errorf("Author.URL = %q", hj.Author.URL)
	}
	if len(hj.Includes) != 1 {
		t.Errorf("Includes = %d, want 1", len(hj.Includes))
	}
	if len(hj.DelegatesTo) != 1 {
		t.Errorf("DelegatesTo = %d, want 1", len(hj.DelegatesTo))
	}
	if len(hj.Hooks) != 2 {
		t.Errorf("Hooks events = %d, want 2", len(hj.Hooks))
	}
	beforeTool := hj.Hooks["before_tool"]
	if len(beforeTool) != 2 {
		t.Fatalf("before_tool entries = %d, want 2", len(beforeTool))
	}
	if beforeTool[0].Matcher != "Bash" {
		t.Errorf("Matcher = %q, want %q", beforeTool[0].Matcher, "Bash")
	}
	if len(hj.MCPServers) != 2 {
		t.Errorf("MCPServers = %d, want 2", len(hj.MCPServers))
	}
	gh := hj.MCPServers["github"]
	if gh.Command != "npx" {
		t.Errorf("github.Command = %q", gh.Command)
	}
	api := hj.MCPServers["api"]
	if api.URL != "https://api.example.com/mcp" {
		t.Errorf("api.URL = %q", api.URL)
	}
	if len(hj.Profiles) != 1 {
		t.Errorf("Profiles = %d, want 1", len(hj.Profiles))
	}
	ci := hj.Profiles["ci"]
	if len(ci.Hooks) != 1 {
		t.Errorf("ci.Hooks events = %d, want 1", len(ci.Hooks))
	}
}

func TestLoadHarnessJSON_MissingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadHarnessJSON(dir)
	if err == nil {
		t.Fatal("expected error for missing harness.json")
	}
}

func TestLoadHarnessJSON_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "harness.json"), []byte(`{invalid`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadHarnessJSON(dir)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestLoadHarnessJSON_MissingName(t *testing.T) {
	dir := t.TempDir()
	writeHarnessJSON(t, dir, `{"version":"1.0.0"}`)

	_, err := LoadHarnessJSON(dir)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestLoadHarnessJSON_UnknownField(t *testing.T) {
	dir := t.TempDir()
	writeHarnessJSON(t, dir, `{"name":"test","version":"1.0.0","badfield":"value"}`)

	_, err := LoadHarnessJSON(dir)
	if err == nil {
		t.Fatal("expected error for unknown field")
	}
	if !strings.Contains(err.Error(), "invalid harness.json") {
		t.Errorf("error should mention invalid harness.json, got: %v", err)
	}
}

func TestLoadHarnessJSON_WithSchema(t *testing.T) {
	dir := t.TempDir()
	writeHarnessJSON(t, dir, `{"$schema":"https://eyelock.github.io/ynh/schema/harness.schema.json","name":"test","version":"1.0.0"}`)

	hj, err := LoadHarnessJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	if hj.Schema != "https://eyelock.github.io/ynh/schema/harness.schema.json" {
		t.Errorf("Schema = %q", hj.Schema)
	}
}

func TestSaveHarnessJSON_RoundTrip(t *testing.T) {
	dir := t.TempDir()

	hj := &HarnessJSON{
		Name:          "round-trip",
		Version:       "1.0.0",
		DefaultVendor: "claude",
		Includes: []IncludeMeta{
			{Git: "github.com/example/repo", Path: "skills/dev", Pick: []string{"review"}},
		},
		DelegatesTo: []DelegateMeta{
			{Git: "github.com/example/team"},
		},
		InstalledFrom: &ProvenanceMeta{
			SourceType:   "registry",
			Source:       "github.com/example/repo",
			RegistryName: "my-registry",
			InstalledAt:  "2026-03-22T10:30:00Z",
		},
	}

	if err := SaveHarnessJSON(dir, hj); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadHarnessJSON(dir)
	if err != nil {
		t.Fatal(err)
	}

	if loaded.Name != "round-trip" {
		t.Errorf("Name = %q", loaded.Name)
	}
	if len(loaded.Includes) != 1 {
		t.Fatalf("Includes = %d, want 1", len(loaded.Includes))
	}
	if loaded.Includes[0].Path != "skills/dev" {
		t.Errorf("Include.Path = %q", loaded.Includes[0].Path)
	}
	if len(loaded.DelegatesTo) != 1 {
		t.Fatalf("DelegatesTo = %d, want 1", len(loaded.DelegatesTo))
	}
	if loaded.InstalledFrom == nil {
		t.Fatal("InstalledFrom is nil after round-trip")
	}
	if loaded.InstalledFrom.RegistryName != "my-registry" {
		t.Errorf("RegistryName = %q", loaded.InstalledFrom.RegistryName)
	}
}

func TestValidateHooks_Valid(t *testing.T) {
	hooks := map[string][]HookEntry{
		"before_tool": {{Matcher: "Bash", Command: "echo hi"}},
		"on_stop":     {{Command: "echo bye"}},
	}
	issues := ValidateHooks(hooks)
	if len(issues) != 0 {
		t.Errorf("expected no issues, got %v", issues)
	}
}

func TestValidateHooks_UnknownEvent(t *testing.T) {
	hooks := map[string][]HookEntry{
		"unknown_event": {{Command: "echo hi"}},
	}
	issues := ValidateHooks(hooks)
	if len(issues) == 0 {
		t.Fatal("expected issues for unknown event")
	}
	found := false
	for _, issue := range issues {
		if strings.Contains(issue, "unknown hook event") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'unknown hook event' in issues, got %v", issues)
	}
}

func TestValidateHooks_EmptyCommand(t *testing.T) {
	hooks := map[string][]HookEntry{
		"before_tool": {{Command: ""}},
	}
	issues := ValidateHooks(hooks)
	if len(issues) == 0 {
		t.Fatal("expected issues for empty command")
	}
	found := false
	for _, issue := range issues {
		if strings.Contains(issue, "command must not be empty") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'command must not be empty' in issues, got %v", issues)
	}
}

func TestValidateMCPServers_CommandOnly(t *testing.T) {
	servers := map[string]MCPServer{
		"test": {Command: "npx", Args: []string{"-y", "server"}},
	}
	issues := ValidateMCPServers(servers)
	if len(issues) != 0 {
		t.Errorf("expected no issues, got %v", issues)
	}
}

func TestValidateMCPServers_URLOnly(t *testing.T) {
	servers := map[string]MCPServer{
		"test": {URL: "https://example.com/mcp"},
	}
	issues := ValidateMCPServers(servers)
	if len(issues) != 0 {
		t.Errorf("expected no issues, got %v", issues)
	}
}

func TestValidateMCPServers_Neither(t *testing.T) {
	servers := map[string]MCPServer{
		"test": {},
	}
	issues := ValidateMCPServers(servers)
	if len(issues) == 0 {
		t.Fatal("expected issues for server with neither command nor url")
	}
	found := false
	for _, issue := range issues {
		if strings.Contains(issue, "must have either command or url") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'must have either command or url' in issues, got %v", issues)
	}
}

func TestValidateMCPServers_Both(t *testing.T) {
	servers := map[string]MCPServer{
		"test": {Command: "npx", URL: "https://example.com"},
	}
	issues := ValidateMCPServers(servers)
	if len(issues) == 0 {
		t.Fatal("expected issues for server with both command and url")
	}
	found := false
	for _, issue := range issues {
		if strings.Contains(issue, "not both") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'not both' in issues, got %v", issues)
	}
}

func TestValidateProfiles_Valid(t *testing.T) {
	profiles := map[string]Profile{
		"ci": {
			Hooks: map[string][]HookEntry{
				"before_tool": {{Command: "echo ci"}},
			},
			MCPServers: map[string]*MCPServer{
				"test": {Command: "npx"},
			},
		},
	}
	issues := ValidateProfiles(profiles)
	if len(issues) != 0 {
		t.Errorf("expected no issues, got %v", issues)
	}
}

func TestValidateProfiles_InvalidHookEvent(t *testing.T) {
	profiles := map[string]Profile{
		"ci": {
			Hooks: map[string][]HookEntry{
				"bad_event": {{Command: "echo hi"}},
			},
		},
	}
	issues := ValidateProfiles(profiles)
	if len(issues) == 0 {
		t.Fatal("expected issues for invalid hook event in profile")
	}
	found := false
	for _, issue := range issues {
		if strings.Contains(issue, `profile "ci"`) && strings.Contains(issue, "unknown hook event") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected profile-prefixed error, got %v", issues)
	}
}

func TestValidateProfiles_MCPServerNoCommand(t *testing.T) {
	profiles := map[string]Profile{
		"audit": {
			MCPServers: map[string]*MCPServer{
				"bad": {},
			},
		},
	}
	issues := ValidateProfiles(profiles)
	if len(issues) == 0 {
		t.Fatal("expected issues for MCP server with no command/url in profile")
	}
}

func TestValidateProfiles_NullEntrySkipped(t *testing.T) {
	profiles := map[string]Profile{
		"ci": {
			MCPServers: map[string]*MCPServer{
				"postgres": nil, // null removal — should not cause validation error
				"github":   {Command: "gh-cmd"},
			},
		},
	}
	issues := ValidateProfiles(profiles)
	if len(issues) != 0 {
		t.Errorf("expected no issues, got %v", issues)
	}
}

func TestLoadHarnessJSON_TestdataRoundTrip(t *testing.T) {
	// Verify all testdata harness.json files parse without error
	entries, err := filepath.Glob("../../testdata/*/harness.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Skip("no testdata harness.json files found")
	}
	for _, path := range entries {
		dir := filepath.Dir(path)
		_, err := LoadHarnessJSON(dir)
		if err != nil {
			t.Errorf("LoadHarnessJSON(%s) failed: %v", dir, err)
		}
	}
}

func writeHarnessJSON(t *testing.T, dir string, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "harness.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
