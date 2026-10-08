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

func TestValidateHooks_Valid(t *testing.T) {
	hooks := map[string][]HookEntry{
		"before_tool":      {{Matcher: "Bash", Command: "echo hi"}},
		"on_stop":          {{Command: "echo bye"}},
		"on_session_start": {{Matcher: "startup|resume", Command: "echo start"}},
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

func TestValidateFocus_Valid(t *testing.T) {
	focuses := map[string]Focus{
		"review":   {Profile: "ci", Prompt: "Review staged changes"},
		"security": {Prompt: "Audit for OWASP Top 10"},
	}
	issues := ValidateFocus(focuses)
	if len(issues) != 0 {
		t.Errorf("expected no issues, got %v", issues)
	}
}

func TestValidateFocus_MissingPrompt(t *testing.T) {
	focuses := map[string]Focus{
		"review": {Profile: "ci", Prompt: ""},
	}
	issues := ValidateFocus(focuses)
	if len(issues) == 0 {
		t.Fatal("expected issues for focus with empty prompt")
	}
	found := false
	for _, issue := range issues {
		if strings.Contains(issue, "focus.review") && strings.Contains(issue, "prompt") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected focus.review prompt error, got %v", issues)
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

// A convergence verifier decides a run is finished, so it must be able to
// produce a verdict. A files sensor cannot: it would end the run because a
// path exists, with contents never read, and the path sits inside the agent's
// own write path. A focus sensor cannot either: `ynh sensors run` reports it
// deferred, never pass, so the run would go to its turn cap (#447).
func TestValidate_ConvergenceVerifierRejectsFilesSource(t *testing.T) {
	cases := []struct {
		name    string
		sensor  Sensor
		wantErr bool
	}{
		{
			name: "files source as convergence verifier is refused",
			sensor: Sensor{
				Role:   "convergence-verifier",
				Source: SensorSource{Files: []string{"reports/done.txt"}},
				Output: SensorOutput{Format: "text"},
			},
			wantErr: true,
		},
		{
			name: "command source as convergence verifier is fine",
			sensor: Sensor{
				Role:   "convergence-verifier",
				Source: SensorSource{Command: "make verify"},
				Output: SensorOutput{Format: "text"},
			},
		},
		{
			name: "a files sensor with no special role stays legal",
			sensor: Sensor{
				Source: SensorSource{Files: []string{"reports/*.json"}},
				Output: SensorOutput{Format: "json"},
			},
		},
		{
			name: "focus source as convergence verifier is refused",
			sensor: Sensor{
				Role:   "convergence-verifier",
				Source: SensorSource{Focus: &FocusRef{Name: "reviewer"}},
				Output: SensorOutput{Format: "text"},
			},
			wantErr: true,
		},
		{
			name: "github_check source as convergence verifier is fine",
			sensor: Sensor{
				Role:   "convergence-verifier",
				Source: SensorSource{GitHubCheck: &GitHubCheckSource{Name: "build"}},
				Output: SensorOutput{Format: "text"},
			},
		},
		{
			name: "a focus sensor with no special role stays legal",
			sensor: Sensor{
				Source: SensorSource{Focus: &FocusRef{Name: "reviewer"}},
				Output: SensorOutput{Format: "text"},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			issues := ValidateSensors(map[string]Sensor{"s": c.sensor}, nil, map[string]bool{"reviewer": true})
			found := false
			for _, i := range issues {
				if strings.Contains(i, "requires a command source") {
					found = true
				}
			}
			if found != c.wantErr {
				t.Errorf("rejected=%v want=%v; issues: %v", found, c.wantErr, issues)
			}
		})
	}
}

// A reference an agent can edit calibrates nothing, and only a command sensor
// produces a verdict to calibrate against.
func TestValidate_SensorReference(t *testing.T) {
	cmdSrc := SensorSource{Command: "make lint"}
	out := SensorOutput{Format: "text"}
	cases := []struct {
		name   string
		sensor Sensor
		want   string // substring the issue must contain; "" means no issue
	}{
		{"valid fail reference", Sensor{Source: cmdSrc, Output: out,
			Reference: &SensorReference{Path: "testdata/calibration/lint", Expect: "fail"}}, ""},
		{"valid pass reference", Sensor{Source: cmdSrc, Output: out,
			Reference: &SensorReference{Path: "testdata/clean", Expect: "pass"}}, ""},
		{"empty path", Sensor{Source: cmdSrc, Output: out,
			Reference: &SensorReference{Expect: "fail"}}, "reference.path must be non-empty"},
		{"absolute path escapes the harness", Sensor{Source: cmdSrc, Output: out,
			Reference: &SensorReference{Path: "/etc", Expect: "fail"}}, "relative path inside the harness"},
		{"traversal escapes the harness", Sensor{Source: cmdSrc, Output: out,
			Reference: &SensorReference{Path: "../../elsewhere", Expect: "fail"}}, "relative path inside the harness"},
		{"unknown expectation", Sensor{Source: cmdSrc, Output: out,
			Reference: &SensorReference{Path: "testdata/x", Expect: "maybe"}}, "must be one of fail, pass"},
		{"files sensor cannot be calibrated", Sensor{Source: SensorSource{Files: []string{"*.json"}}, Output: out,
			Reference: &SensorReference{Path: "testdata/x", Expect: "fail"}}, "requires a command source"},
		{"no reference is legal", Sensor{Source: cmdSrc, Output: out}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			issues := ValidateSensors(map[string]Sensor{"s": c.sensor}, nil, nil)
			found := ""
			for _, i := range issues {
				if c.want != "" && strings.Contains(i, c.want) {
					found = i
				}
			}
			if c.want == "" {
				for _, i := range issues {
					if strings.Contains(i, "reference") {
						t.Errorf("unexpected reference issue: %s", i)
					}
				}
				return
			}
			if found == "" {
				t.Errorf("expected an issue containing %q, got %v", c.want, issues)
			}
		})
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

func TestValidate_SensorRatchet(t *testing.T) {
	cmdSrc := SensorSource{Command: "grep -rn nolint ."}
	out := SensorOutput{Format: "text"}
	cases := []struct {
		name   string
		sensor Sensor
		want   string // substring the issue must contain; "" means none
	}{
		{"count on a command sensor", Sensor{Source: cmdSrc, Output: out, Ratchet: "count"}, ""},
		{"fingerprint is explicit and fine", Sensor{Source: cmdSrc, Output: out, Ratchet: "fingerprint"}, ""},
		{"absent defaults to fingerprint", Sensor{Source: cmdSrc, Output: out}, ""},
		{"unknown mode", Sensor{Source: cmdSrc, Output: out, Ratchet: "vibes"},
			"must be one of fingerprint, count"},
		{"count on a files sensor has nothing countable",
			Sensor{Source: SensorSource{Files: []string{"*.go"}}, Output: out, Ratchet: "count"},
			"requires a command source"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			issues := ValidateSensors(map[string]Sensor{"s": c.sensor}, nil, nil)
			hit := false
			for _, i := range issues {
				if c.want != "" && strings.Contains(i, c.want) {
					hit = true
				}
				if c.want == "" && strings.Contains(i, "ratchet") {
					t.Errorf("unexpected ratchet issue: %s", i)
				}
			}
			if c.want != "" && !hit {
				t.Errorf("expected an issue containing %q, got %v", c.want, issues)
			}
		})
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
