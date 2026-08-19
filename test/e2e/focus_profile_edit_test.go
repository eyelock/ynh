//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// extendedManifest carries the manifest fields the new editor commands
// touch — focuses, profiles, top-level hooks, top-level mcp_servers.
// Subset of plugin.HarnessJSON sufficient to assert end-to-end round-trips.
type extendedManifest struct {
	Name       string                       `json:"name"`
	Version    string                       `json:"version"`
	Hooks      map[string][]manifestHook    `json:"hooks,omitempty"`
	MCPServers map[string]manifestMCPServer `json:"mcp_servers,omitempty"`
	Focuses    map[string]manifestFocus     `json:"focuses,omitempty"`
	Profiles   map[string]manifestProfile   `json:"profiles,omitempty"`
}

type manifestHook struct {
	Matcher string `json:"matcher,omitempty"`
	Command string `json:"command"`
}

type manifestMCPServer struct {
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

type manifestFocus struct {
	Profile string `json:"profile,omitempty"`
	Prompt  string `json:"prompt"`
}

type manifestProfile struct {
	Hooks      map[string][]manifestHook     `json:"hooks,omitempty"`
	MCPServers map[string]*manifestMCPServer `json:"mcp_servers,omitempty"`
	Includes   []manifestSourceJSON          `json:"includes,omitempty"`
}

func readExtendedManifest(t *testing.T, harnessDir string) extendedManifest {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(harnessDir, ".ynh-plugin", "plugin.json"))
	if err != nil {
		t.Fatalf("reading plugin.json: %v", err)
	}
	var mf extendedManifest
	if err := json.Unmarshal(body, &mf); err != nil {
		t.Fatalf("parsing plugin.json: %v\n%s", err, body)
	}
	return mf
}

// TestFocus_LocalInstall_RoundTrip exercises the focus editor end-to-end
// against a pointer-form local install:
//   - install creates a pointer; no copy dir
//   - focus add writes to the user's source tree
//   - focus update mutates the same entry
//   - ynh info surfaces the change (read/write symmetry)
//   - focus remove drops it
func TestFocus_LocalInstall_RoundTrip(t *testing.T) {
	s := newSandbox(t)
	clone := cloneAssistantsAtSHA(t)
	sourceDir := filepath.Join(clone, "e2e-fixtures", "minimal")

	s.mustRunYnh(t, "install", sourceDir)

	s.mustRunYnh(t, "focus", "add", "local/minimal", "review", "review this code")
	mf := readExtendedManifest(t, sourceDir)
	if f, ok := mf.Focuses["review"]; !ok || f.Prompt != "review this code" {
		t.Fatalf("expected focus review with prompt, got %+v", mf.Focuses)
	}

	s.mustRunYnh(t, "focus", "update", "local/minimal", "review", "--prompt", "review carefully")
	mf = readExtendedManifest(t, sourceDir)
	if mf.Focuses["review"].Prompt != "review carefully" {
		t.Errorf("expected updated prompt, got %+v", mf.Focuses["review"])
	}

	infoOut, _ := s.mustRunYnh(t, "info", "local/minimal", "--format", "json")
	if !strings.Contains(infoOut, "review carefully") {
		t.Errorf("focus edit not visible to ynh info — read/write symmetry broken.\noutput:\n%s", infoOut)
	}

	s.mustRunYnh(t, "focus", "remove", "local/minimal", "review")
	mf = readExtendedManifest(t, sourceDir)
	if _, ok := mf.Focuses["review"]; ok {
		t.Errorf("focus not removed: %+v", mf.Focuses)
	}
}

// TestProfile_LocalInstall_HookAndMCP exercises profile creation plus the
// nested hook and mcp editors on a pointer-form local install.
func TestProfile_LocalInstall_HookAndMCP(t *testing.T) {
	s := newSandbox(t)
	clone := cloneAssistantsAtSHA(t)
	sourceDir := filepath.Join(clone, "e2e-fixtures", "minimal")

	s.mustRunYnh(t, "install", sourceDir)

	s.mustRunYnh(t, "profile", "add", "local/minimal", "thorough")
	s.mustRunYnh(t, "hook", "add", "local/minimal", "before_tool", "echo before", "--profile", "thorough")
	s.mustRunYnh(t, "mcp", "add", "local/minimal", "github", "--profile", "thorough",
		"--command", "gh", "--arg", "mcp", "--env", "TOK=abc")

	mf := readExtendedManifest(t, sourceDir)
	p, ok := mf.Profiles["thorough"]
	if !ok {
		t.Fatalf("expected profile thorough, got %+v", mf.Profiles)
	}
	if len(p.Hooks["before_tool"]) != 1 || p.Hooks["before_tool"][0].Command != "echo before" {
		t.Errorf("expected hook on profile, got %+v", p.Hooks)
	}
	srv := p.MCPServers["github"]
	if srv == nil || srv.Command != "gh" || srv.Env["TOK"] != "abc" {
		t.Errorf("expected mcp server on profile, got %+v", srv)
	}

	// Profile-level mcp update is a key path TermQ drives.
	s.mustRunYnh(t, "mcp", "update", "local/minimal", "github", "--profile", "thorough",
		"--arg", "--verbose")
	mf = readExtendedManifest(t, sourceDir)
	srv = mf.Profiles["thorough"].MCPServers["github"]
	if srv == nil || len(srv.Args) != 1 || srv.Args[0] != "--verbose" {
		t.Errorf("expected args replaced after update, got %+v", srv)
	}

	s.mustRunYnh(t, "hook", "remove", "local/minimal", "before_tool", "0", "--profile", "thorough")
	s.mustRunYnh(t, "mcp", "remove", "local/minimal", "github", "--profile", "thorough")
	mf = readExtendedManifest(t, sourceDir)
	if len(mf.Profiles["thorough"].Hooks) != 0 || len(mf.Profiles["thorough"].MCPServers) != 0 {
		t.Errorf("profile not cleared, got %+v", mf.Profiles["thorough"])
	}
}

// TestHook_HarnessLevel_LocalInstall exercises top-level hook editing on
// a pointer-form local install. Pins read/write symmetry for the
// harness-level (not profile-nested) hook surface.
func TestHook_HarnessLevel_LocalInstall(t *testing.T) {
	s := newSandbox(t)
	clone := cloneAssistantsAtSHA(t)
	sourceDir := filepath.Join(clone, "e2e-fixtures", "minimal")

	s.mustRunYnh(t, "install", sourceDir)

	s.mustRunYnh(t, "hook", "add", "local/minimal", "before_tool", "echo go", "--matcher", "Write")
	mf := readExtendedManifest(t, sourceDir)
	entries := mf.Hooks["before_tool"]
	if len(entries) != 1 || entries[0].Command != "echo go" || entries[0].Matcher != "Write" {
		t.Fatalf("expected hook on harness, got %+v", entries)
	}

	infoOut, _ := s.mustRunYnh(t, "info", "local/minimal", "--format", "json")
	if !strings.Contains(infoOut, "echo go") {
		t.Errorf("hook edit not visible to ynh info — read/write symmetry broken.\noutput:\n%s", infoOut)
	}

	s.mustRunYnh(t, "hook", "remove", "local/minimal", "before_tool", "0")
	mf = readExtendedManifest(t, sourceDir)
	if _, ok := mf.Hooks["before_tool"]; ok {
		t.Errorf("hook event key should be dropped when empty, got %+v", mf.Hooks)
	}
}

// TestMCP_HarnessLevel_LocalInstall exercises top-level mcp editing with
// the add → update → remove lifecycle.
func TestMCP_HarnessLevel_LocalInstall(t *testing.T) {
	s := newSandbox(t)
	clone := cloneAssistantsAtSHA(t)
	sourceDir := filepath.Join(clone, "e2e-fixtures", "minimal")

	s.mustRunYnh(t, "install", sourceDir)

	s.mustRunYnh(t, "mcp", "add", "local/minimal", "api",
		"--url", "https://example.com", "--header", "Authorization=Bearer xyz")
	mf := readExtendedManifest(t, sourceDir)
	srv, ok := mf.MCPServers["api"]
	if !ok || srv.URL != "https://example.com" || srv.Headers["Authorization"] != "Bearer xyz" {
		t.Fatalf("expected url-based mcp server, got %+v", srv)
	}

	s.mustRunYnh(t, "mcp", "update", "local/minimal", "api",
		"--header", "X-Trace=on")
	mf = readExtendedManifest(t, sourceDir)
	if mf.MCPServers["api"].Headers["X-Trace"] != "on" {
		t.Errorf("expected new header after update, got %+v", mf.MCPServers["api"])
	}

	s.mustRunYnh(t, "mcp", "remove", "local/minimal", "api")
	mf = readExtendedManifest(t, sourceDir)
	if _, ok := mf.MCPServers["api"]; ok {
		t.Errorf("mcp server not removed: %+v", mf.MCPServers)
	}
}

// TestYndCompose_AcceptsCanonicalID exercises the ynd compose
// canonical-id resolver added on this branch. TermQ drives
// `ynd compose <id>` using ids from `ynh ls --format json`; this test
// pins that contract so the resolveSource change can't silently regress.
//
// Also asserts the new map-shaped profiles field is emitted, which is a
// breaking change for any pre-existing consumer relying on the old array
// shape.
func TestYndCompose_AcceptsCanonicalID(t *testing.T) {
	s := newSandbox(t)
	clone := cloneAssistantsAtSHA(t)
	sourceDir := filepath.Join(clone, "e2e-fixtures", "minimal")

	s.mustRunYnh(t, "install", sourceDir)

	// Seed a profile so the compose output's profiles map has content
	// and we can assert on the new shape.
	s.mustRunYnh(t, "profile", "add", "local/minimal", "p1")
	s.mustRunYnh(t, "hook", "add", "local/minimal", "before_tool", "echo from-p1", "--profile", "p1")

	stdout, _, err := runYndInDirEnv(t, "", []string{"YNH_HOME=" + s.home},
		"compose", "local/minimal", "--format", "json")
	if err != nil {
		t.Fatalf("ynd compose with canonical id failed: %v", err)
	}

	var got struct {
		Name     string `json:"name"`
		Profiles map[string]struct {
			Hooks map[string][]struct {
				Command string `json:"command"`
			} `json:"hooks,omitempty"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("unmarshal compose output: %v\n%s", err, stdout)
	}

	if got.Name != "minimal" {
		t.Errorf("compose name = %q, want minimal", got.Name)
	}

	p1, ok := got.Profiles["p1"]
	if !ok {
		t.Fatalf("compose output missing profile p1; profiles=%+v", got.Profiles)
	}
	hooks := p1.Hooks["before_tool"]
	if len(hooks) != 1 || hooks[0].Command != "echo from-p1" {
		t.Errorf("expected profile hook surfaced in compose, got %+v", hooks)
	}
}
