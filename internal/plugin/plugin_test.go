package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsPluginDir_True(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".claude-plugin", "plugin.json"), []byte(`{"name":"test"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if !IsPluginDir(dir) {
		t.Error("expected IsPluginDir to return true")
	}
}

func TestIsPluginDir_False(t *testing.T) {
	dir := t.TempDir()
	if IsPluginDir(dir) {
		t.Error("expected IsPluginDir to return false for empty dir")
	}
}

func TestLoadPluginJSON_Valid(t *testing.T) {
	dir := t.TempDir()
	writePluginJSON(t, dir, PluginJSON{
		Name:    "test-harness",
		Version: "1.0.0",
	})

	pj, err := LoadPluginJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	if pj.Name != "test-harness" {
		t.Errorf("Name = %q, want %q", pj.Name, "test-harness")
	}
	if pj.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", pj.Version, "1.0.0")
	}
}

func TestLoadPluginJSON_MissingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadPluginJSON(dir)
	if err == nil {
		t.Fatal("expected error for missing plugin.json")
	}
}

func TestLoadPluginJSON_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".claude-plugin", "plugin.json"), []byte(`{invalid`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadPluginJSON(dir)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestLoadPluginJSON_MissingName(t *testing.T) {
	dir := t.TempDir()
	writePluginJSON(t, dir, PluginJSON{Version: "1.0.0"})

	_, err := LoadPluginJSON(dir)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestLoadMetadataJSON_WithYNH(t *testing.T) {
	dir := t.TempDir()
	writeMetadataJSON(t, dir, MetadataJSON{
		YNH: &YNHMetadata{
			DefaultVendor: "claude",
			Includes: []IncludeMeta{
				{Git: "github.com/example/repo", Pick: []string{"skills/hello"}},
			},
			DelegatesTo: []DelegateMeta{
				{Git: "github.com/example/team"},
			},
		},
	})

	meta, err := LoadMetadataJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.YNH == nil {
		t.Fatal("YNH is nil")
	}
	if meta.YNH.DefaultVendor != "claude" {
		t.Errorf("DefaultVendor = %q, want %q", meta.YNH.DefaultVendor, "claude")
	}
	if len(meta.YNH.Includes) != 1 {
		t.Fatalf("Includes = %d, want 1", len(meta.YNH.Includes))
	}
	if len(meta.YNH.DelegatesTo) != 1 {
		t.Fatalf("DelegatesTo = %d, want 1", len(meta.YNH.DelegatesTo))
	}
}

func TestLoadMetadataJSON_FileNotExists(t *testing.T) {
	dir := t.TempDir()
	meta, err := LoadMetadataJSON(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta != nil {
		t.Error("expected nil for missing metadata.json")
	}
}

func TestLoadMetadataJSON_NoYNHKey(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(`{"other_tool": {}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	meta, err := LoadMetadataJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.YNH != nil {
		t.Error("expected YNH to be nil")
	}
}

func TestLoadMetadataJSON_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(`{bad`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadMetadataJSON(dir)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestSaveMetadataJSON_NewFile(t *testing.T) {
	dir := t.TempDir()
	ynh := &YNHMetadata{
		DefaultVendor: "claude",
		InstalledFrom: &ProvenanceMeta{
			SourceType:  "git",
			Source:      "github.com/example/repo",
			Path:        "personas/test",
			InstalledAt: "2026-03-22T10:30:00Z",
		},
	}

	if err := SaveMetadataJSON(dir, ynh); err != nil {
		t.Fatal(err)
	}

	// Read back and verify
	meta, err := LoadMetadataJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.YNH == nil {
		t.Fatal("YNH is nil after save")
	}
	if meta.YNH.DefaultVendor != "claude" {
		t.Errorf("DefaultVendor = %q, want %q", meta.YNH.DefaultVendor, "claude")
	}
	if meta.YNH.InstalledFrom == nil {
		t.Fatal("InstalledFrom is nil after save")
	}
	if meta.YNH.InstalledFrom.SourceType != "git" {
		t.Errorf("SourceType = %q, want %q", meta.YNH.InstalledFrom.SourceType, "git")
	}
	if meta.YNH.InstalledFrom.Source != "github.com/example/repo" {
		t.Errorf("Source = %q, want %q", meta.YNH.InstalledFrom.Source, "github.com/example/repo")
	}
	if meta.YNH.InstalledFrom.Path != "personas/test" {
		t.Errorf("Path = %q, want %q", meta.YNH.InstalledFrom.Path, "personas/test")
	}
}

func TestSaveMetadataJSON_PreservesNonYNHKeys(t *testing.T) {
	dir := t.TempDir()

	// Write a file with a non-ynh key
	initial := []byte(`{"other_tool": {"setting": true}, "ynh": {"default_vendor": "codex"}}`)
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), initial, 0o644); err != nil {
		t.Fatal(err)
	}

	// Save new ynh metadata (overwrites ynh, preserves other_tool)
	ynh := &YNHMetadata{
		DefaultVendor: "claude",
		InstalledFrom: &ProvenanceMeta{
			SourceType:  "local",
			Source:      "./my-harness",
			InstalledAt: "2026-03-22T10:30:00Z",
		},
	}
	if err := SaveMetadataJSON(dir, ynh); err != nil {
		t.Fatal(err)
	}

	// Read raw JSON and check other_tool is preserved
	data, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	otherTool, ok := raw["other_tool"]
	if !ok {
		t.Fatal("other_tool key was lost during save")
	}
	otherMap, ok := otherTool.(map[string]any)
	if !ok {
		t.Fatal("other_tool is not an object")
	}
	if otherMap["setting"] != true {
		t.Errorf("other_tool.setting = %v, want true", otherMap["setting"])
	}

	// Also verify ynh was updated
	meta, err := LoadMetadataJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.YNH.DefaultVendor != "claude" {
		t.Errorf("DefaultVendor = %q, want %q", meta.YNH.DefaultVendor, "claude")
	}
}

func TestSaveMetadataJSON_RoundTrip(t *testing.T) {
	dir := t.TempDir()

	// Write full metadata with includes and provenance
	ynh := &YNHMetadata{
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

	if err := SaveMetadataJSON(dir, ynh); err != nil {
		t.Fatal(err)
	}

	meta, err := LoadMetadataJSON(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(meta.YNH.Includes) != 1 {
		t.Fatalf("Includes = %d, want 1", len(meta.YNH.Includes))
	}
	if meta.YNH.Includes[0].Path != "skills/dev" {
		t.Errorf("Include.Path = %q, want %q", meta.YNH.Includes[0].Path, "skills/dev")
	}
	if len(meta.YNH.DelegatesTo) != 1 {
		t.Fatalf("DelegatesTo = %d, want 1", len(meta.YNH.DelegatesTo))
	}
	if meta.YNH.InstalledFrom == nil {
		t.Fatal("InstalledFrom is nil after round-trip")
	}
	if meta.YNH.InstalledFrom.RegistryName != "my-registry" {
		t.Errorf("RegistryName = %q, want %q", meta.YNH.InstalledFrom.RegistryName, "my-registry")
	}
}

func TestLoadMetadataJSON_WithHooks(t *testing.T) {
	dir := t.TempDir()
	metaJSON := `{
		"ynh": {
			"default_vendor": "claude",
			"hooks": {
				"before_tool": [
					{"matcher": "Bash", "command": "echo before bash"},
					{"command": "echo before all"}
				],
				"on_stop": [
					{"command": "echo done"}
				]
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(metaJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	meta, err := LoadMetadataJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.YNH == nil {
		t.Fatal("YNH is nil")
	}
	if len(meta.YNH.Hooks) != 2 {
		t.Fatalf("Hooks events = %d, want 2", len(meta.YNH.Hooks))
	}
	beforeTool := meta.YNH.Hooks["before_tool"]
	if len(beforeTool) != 2 {
		t.Fatalf("before_tool entries = %d, want 2", len(beforeTool))
	}
	if beforeTool[0].Matcher != "Bash" {
		t.Errorf("Matcher = %q, want %q", beforeTool[0].Matcher, "Bash")
	}
	if beforeTool[0].Command != "echo before bash" {
		t.Errorf("Command = %q, want %q", beforeTool[0].Command, "echo before bash")
	}
	if beforeTool[1].Matcher != "" {
		t.Errorf("second entry Matcher = %q, want empty", beforeTool[1].Matcher)
	}
	onStop := meta.YNH.Hooks["on_stop"]
	if len(onStop) != 1 {
		t.Fatalf("on_stop entries = %d, want 1", len(onStop))
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

func TestLoadMetadataJSON_WithMCPServers(t *testing.T) {
	dir := t.TempDir()
	metaJSON := `{
		"ynh": {
			"default_vendor": "claude",
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
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(metaJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	meta, err := LoadMetadataJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	if meta.YNH == nil {
		t.Fatal("YNH is nil")
	}
	if len(meta.YNH.MCPServers) != 2 {
		t.Fatalf("MCPServers = %d, want 2", len(meta.YNH.MCPServers))
	}

	gh := meta.YNH.MCPServers["github"]
	if gh.Command != "npx" {
		t.Errorf("github.Command = %q, want %q", gh.Command, "npx")
	}
	if len(gh.Args) != 2 || gh.Args[0] != "-y" {
		t.Errorf("github.Args = %v, want [-y @modelcontextprotocol/server-github]", gh.Args)
	}
	if gh.Env["GITHUB_TOKEN"] != "${GITHUB_TOKEN}" {
		t.Errorf("github.Env = %v", gh.Env)
	}

	api := meta.YNH.MCPServers["api"]
	if api.URL != "https://api.example.com/mcp" {
		t.Errorf("api.URL = %q", api.URL)
	}
	if api.Headers["Authorization"] != "Bearer ${API_KEY}" {
		t.Errorf("api.Headers = %v", api.Headers)
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

func writePluginJSON(t *testing.T, dir string, pj PluginJSON) {
	t.Helper()
	pluginDir := filepath.Join(dir, ".claude-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(pj)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeMetadataJSON(t *testing.T, dir string, meta MetadataJSON) {
	t.Helper()
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}
