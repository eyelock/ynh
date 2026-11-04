package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsClaudePluginDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".claude-plugin", "plugin.json"), []byte(`{"name":"test"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if !IsClaudePluginDir(dir) {
		t.Error("expected IsClaudePluginDir to return true")
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
		t.Fatal("expected error for missing .harness.json")
	}
}

func TestLoadHarnessJSON_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".harness.json"), []byte(`{invalid`), 0o644); err != nil {
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
	if !strings.Contains(err.Error(), "invalid .harness.json") {
		t.Errorf("error should mention invalid .harness.json, got: %v", err)
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

func TestLoadHarnessJSON_WithFocus(t *testing.T) {
	dir := t.TempDir()
	writeHarnessJSON(t, dir, `{
		"name": "test",
		"version": "0.1.0",
		"focuses": {
			"review": {"profile": "ci", "prompt": "Review staged changes"},
			"docs": {"prompt": "Generate API docs"}
		}
	}`)
	hj, err := LoadHarnessJSON(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(hj.Focuses) != 2 {
		t.Fatalf("Focuses = %d, want 2", len(hj.Focuses))
	}
	review := hj.Focuses["review"]
	if review.Profile != "ci" || review.Prompt != "Review staged changes" {
		t.Errorf("review = %+v", review)
	}
	docs := hj.Focuses["docs"]
	if docs.Profile != "" || docs.Prompt != "Generate API docs" {
		t.Errorf("docs = %+v", docs)
	}
}

func TestLoadPluginJSON_TestdataRoundTrip(t *testing.T) {
	// Every testdata harness is in the current format, so reading one never
	// needs ynd migrate and running a command against it never changes the
	// checkout (#406).
	entries, err := filepath.Glob("../../testdata/*/.agents/harness/plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		// Not a skip. These fixtures are checked in, and zero matches means
		// they have been deleted, which is the failure this test exists to
		// catch (#350).
		t.Fatal("no testdata fixtures found under testdata/*/.agents/harness/plugin.json; " +
			"they are committed, so their absence is a defect rather than a reason to skip")
	}
	for _, path := range entries {
		dir := filepath.Dir(filepath.Dir(filepath.Dir(path)))
		if _, err := LoadPluginJSON(dir); err != nil {
			t.Errorf("LoadPluginJSON(%s) failed: %v", dir, err)
		}
	}
	legacy, err := filepath.Glob("../../testdata/*/" + HarnessFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(legacy) > 0 {
		t.Errorf("legacy fixtures %v: build legacy trees under t.TempDir() in the test that needs one", legacy)
	}
}

func writeHarnessJSON(t *testing.T, dir string, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".harness.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The rule ExpandMCPEnv enforces at assembly, minus the part that needs a live
// environment — so it can run before assembly, where the failure is cheap.
func TestUndeclaredMCPEnvRefs(t *testing.T) {
	srv := func(headers, env map[string]string) map[string]MCPServer {
		return map[string]MCPServer{"s": {URL: "https://x", Headers: headers, Env: env}}
	}
	cases := []struct {
		name    string
		servers map[string]MCPServer
		allowed []string
		want    int
	}{
		{
			name:    "declared and missing one — the realistic author slip",
			servers: srv(map[string]string{"Authorization": "Bearer ${MISSING}"}, nil),
			allowed: []string{"KNOWN"},
			want:    1,
		},
		{
			name:    "declared and complete",
			servers: srv(map[string]string{"Authorization": "Bearer ${TOKEN}"}, nil),
			allowed: []string{"TOKEN"},
			want:    0,
		},
		{
			// export leaves ${VAR} literal and strips env_passthrough from the
			// artifact, so a distribution-only harness works today. Flagging
			// it would redden something that is not broken.
			name:    "no allowlist at all — may be distribution-only",
			servers: srv(map[string]string{"Authorization": "Bearer ${DOCS_API_KEY}"}, nil),
			allowed: nil,
			want:    0,
		},
		{
			name:    "env field is checked too, not just headers",
			servers: srv(nil, map[string]string{"API": "${NOPE}"}),
			allowed: []string{"KNOWN"},
			want:    1,
		},
		{
			name:    "several references, several issues",
			servers: srv(map[string]string{"A": "${ONE}", "B": "${TWO}"}, map[string]string{"C": "${THREE}"}),
			allowed: []string{"KNOWN"},
			want:    3,
		},
		{
			name:    "no servers",
			servers: nil,
			allowed: []string{"KNOWN"},
			want:    0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := UndeclaredMCPEnvRefs(c.servers, c.allowed)
			if len(got) != c.want {
				t.Errorf("got %d issues %v, want %d", len(got), got, c.want)
			}
		})
	}
}

// Declaration only. An unset variable is legitimately a run-time condition —
// a developer without the credential still needs `ynd validate` to pass.
func TestUndeclaredMCPEnvRefs_DoesNotRequireTheVariableToBeSet(t *testing.T) {
	// Deliberately not set: the point is that validation does not need it.
	servers := map[string]MCPServer{"s": {URL: "https://x",
		Headers: map[string]string{"Authorization": "Bearer ${DECLARED_BUT_UNSET}"}}}
	if got := UndeclaredMCPEnvRefs(servers, []string{"DECLARED_BUT_UNSET"}); len(got) != 0 {
		t.Errorf("a declared but unset variable must not fail validation, got %v", got)
	}
}

func TestEffectiveRatchet(t *testing.T) {
	if got := (Sensor{}).EffectiveRatchet(); got != "fingerprint" {
		t.Errorf("default = %q, want fingerprint — changing the default would affect every existing harness", got)
	}
	if got := (Sensor{Ratchet: "count"}).EffectiveRatchet(); got != "count" {
		t.Errorf("got %q", got)
	}
}

func TestHarnessJSON_NullMCPServerRoundTrips(t *testing.T) {
	in := `{"name":"h","version":"1","mcp_servers":{"gone":null,"mine":{"command":"c"}}}`
	var hj HarnessJSON
	if err := json.Unmarshal([]byte(in), &hj); err != nil {
		t.Fatal(err)
	}
	if len(hj.MCPRemovals) != 1 || hj.MCPRemovals[0] != "gone" || len(hj.MCPServers) != 1 {
		t.Fatalf("decoded %+v", hj)
	}
	out, err := json.Marshal(hj)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(out, &raw); err != nil {
		t.Fatal(err)
	}
	servers, _ := raw["mcp_servers"].(map[string]any)
	if v, ok := servers["gone"]; !ok || v != nil {
		t.Errorf("removal not written back as null: %s", out)
	}
	if _, ok := servers["mine"]; !ok {
		t.Errorf("server lost: %s", out)
	}
}

func TestHarnessJSON_UnknownFieldStillRejected(t *testing.T) {
	var hj HarnessJSON
	if err := json.Unmarshal([]byte(`{"name":"h","bogus":1}`), &hj); err == nil {
		t.Fatal("unknown field must be rejected")
	}
}

func TestHarnessJSON_MarshalWithoutRemovalsKeepsFieldOrder(t *testing.T) {
	out, err := json.Marshal(HarnessJSON{Name: "h", Version: "1", MCPServers: map[string]MCPServer{"a": {Command: "c"}}})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"name":"h","version":"1","mcp_servers":{"a":{"command":"c"}}}`; string(out) != want {
		t.Errorf("got %s, want %s", out, want)
	}
}
