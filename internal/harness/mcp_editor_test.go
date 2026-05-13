package harness

import (
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

func ptr[T any](v T) *T { return &v }

// ---- AddMCP (harness-level) -----------------------------------------

func TestAddMCP_Command(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")

	opts := MCPAddOptions{
		Command: "gh-mcp",
		Args:    []string{"serve"},
		Env:     map[string]string{"K": "V"},
	}
	if err := AddMCP(dir, "github", opts); err != nil {
		t.Fatalf("AddMCP: %v", err)
	}
	hj, _ := plugin.LoadPluginJSON(dir)
	srv := hj.MCPServers["github"]
	if srv.Command != "gh-mcp" || len(srv.Args) != 1 || srv.Args[0] != "serve" || srv.Env["K"] != "V" {
		t.Errorf("unexpected server: %+v", srv)
	}
}

func TestAddMCP_URL(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddMCP(dir, "remote", MCPAddOptions{URL: "https://x", Headers: map[string]string{"H": "1"}}); err != nil {
		t.Fatalf("AddMCP: %v", err)
	}
	hj, _ := plugin.LoadPluginJSON(dir)
	if hj.MCPServers["remote"].URL != "https://x" {
		t.Errorf("URL not persisted: %+v", hj.MCPServers["remote"])
	}
}

func TestAddMCP_EmptyName(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	err := AddMCP(dir, "", MCPAddOptions{Command: "x"})
	if err == nil || !strings.Contains(err.Error(), "name") {
		t.Errorf("want name error, got %v", err)
	}
}

func TestAddMCP_NeitherCommandNorURL(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	err := AddMCP(dir, "s", MCPAddOptions{})
	if err == nil || !strings.Contains(err.Error(), "command") {
		t.Errorf("want missing command/url, got %v", err)
	}
}

func TestAddMCP_BothCommandAndURL(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	err := AddMCP(dir, "s", MCPAddOptions{Command: "x", URL: "y"})
	if err == nil || !strings.Contains(err.Error(), "both") {
		t.Errorf("want both error, got %v", err)
	}
}

func TestAddMCP_Duplicate(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddMCP(dir, "s", MCPAddOptions{Command: "x"}); err != nil {
		t.Fatal(err)
	}
	err := AddMCP(dir, "s", MCPAddOptions{Command: "y"})
	if err == nil || !strings.Contains(err.Error(), "already present") {
		t.Errorf("want duplicate error, got %v", err)
	}
}

// ---- RemoveMCP ------------------------------------------------------

func TestRemoveMCP_Removes(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddMCP(dir, "s", MCPAddOptions{Command: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := RemoveMCP(dir, "s"); err != nil {
		t.Fatalf("RemoveMCP: %v", err)
	}
	hj, _ := plugin.LoadPluginJSON(dir)
	if len(hj.MCPServers) != 0 {
		t.Errorf("expected mcp_servers cleared, got %v", hj.MCPServers)
	}
}

func TestRemoveMCP_NotFound(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	err := RemoveMCP(dir, "ghost")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("want not-found, got %v", err)
	}
}

// ---- UpdateMCP ------------------------------------------------------

func TestUpdateMCP_Command(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddMCP(dir, "s", MCPAddOptions{Command: "old"}); err != nil {
		t.Fatal(err)
	}
	if err := UpdateMCP(dir, "s", MCPUpdateOptions{Command: ptr("new")}); err != nil {
		t.Fatalf("UpdateMCP: %v", err)
	}
	hj, _ := plugin.LoadPluginJSON(dir)
	if hj.MCPServers["s"].Command != "new" {
		t.Errorf("command = %q, want new", hj.MCPServers["s"].Command)
	}
}

func TestUpdateMCP_ClearArgs(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddMCP(dir, "s", MCPAddOptions{Command: "x", Args: []string{"a", "b"}}); err != nil {
		t.Fatal(err)
	}
	if err := UpdateMCP(dir, "s", MCPUpdateOptions{SetArgs: true, Args: nil}); err != nil {
		t.Fatalf("UpdateMCP: %v", err)
	}
	hj, _ := plugin.LoadPluginJSON(dir)
	if len(hj.MCPServers["s"].Args) != 0 {
		t.Errorf("expected args cleared, got %v", hj.MCPServers["s"].Args)
	}
}

func TestUpdateMCP_NoFlags(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddMCP(dir, "s", MCPAddOptions{Command: "x"}); err != nil {
		t.Fatal(err)
	}
	err := UpdateMCP(dir, "s", MCPUpdateOptions{})
	if err == nil || !strings.Contains(err.Error(), "at least one flag") {
		t.Errorf("want no-flags error, got %v", err)
	}
}

func TestUpdateMCP_NotFound(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	err := UpdateMCP(dir, "ghost", MCPUpdateOptions{Command: ptr("x")})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("want not-found, got %v", err)
	}
}

func TestUpdateMCP_LeavesNeitherCommandNorURL(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddMCP(dir, "s", MCPAddOptions{Command: "x"}); err != nil {
		t.Fatal(err)
	}
	err := UpdateMCP(dir, "s", MCPUpdateOptions{Command: ptr("")})
	if err == nil || !strings.Contains(err.Error(), "either command or url") {
		t.Errorf("want command-or-url error, got %v", err)
	}
}

func TestUpdateMCP_SetsBothCommandAndURL(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddMCP(dir, "s", MCPAddOptions{Command: "x"}); err != nil {
		t.Fatal(err)
	}
	err := UpdateMCP(dir, "s", MCPUpdateOptions{URL: ptr("https://y")})
	if err == nil || !strings.Contains(err.Error(), "cannot have both") {
		t.Errorf("want both-set error, got %v", err)
	}
}

// ---- AddProfileMCP / RemoveProfileMCP / UpdateProfileMCP -----------

func TestAddProfileMCP_Command(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddProfile(dir, "ci"); err != nil {
		t.Fatal(err)
	}
	if err := AddProfileMCP(dir, "ci", "s", ProfileMCPAddOptions{Command: "cmd"}); err != nil {
		t.Fatalf("AddProfileMCP: %v", err)
	}
	hj, _ := plugin.LoadPluginJSON(dir)
	p := hj.Profiles["ci"]
	if p.MCPServers["s"] == nil || p.MCPServers["s"].Command != "cmd" {
		t.Errorf("unexpected: %+v", p.MCPServers)
	}
}

func TestAddProfileMCP_Null(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddProfile(dir, "ci"); err != nil {
		t.Fatal(err)
	}
	if err := AddProfileMCP(dir, "ci", "s", ProfileMCPAddOptions{Null: true}); err != nil {
		t.Fatalf("AddProfileMCP null: %v", err)
	}
	hj, _ := plugin.LoadPluginJSON(dir)
	p := hj.Profiles["ci"]
	srv, ok := p.MCPServers["s"]
	if !ok {
		t.Fatal("null entry not present")
	}
	if srv != nil {
		t.Errorf("expected nil entry (null), got %+v", srv)
	}
}

func TestAddProfileMCP_NullRejectsCommand(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddProfile(dir, "ci"); err != nil {
		t.Fatal(err)
	}
	err := AddProfileMCP(dir, "ci", "s", ProfileMCPAddOptions{Null: true, Command: "x"})
	if err == nil || !strings.Contains(err.Error(), "--null") {
		t.Errorf("want null+command error, got %v", err)
	}
}

func TestAddProfileMCP_RequiresOne(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddProfile(dir, "ci"); err != nil {
		t.Fatal(err)
	}
	err := AddProfileMCP(dir, "ci", "s", ProfileMCPAddOptions{})
	if err == nil || !strings.Contains(err.Error(), "command") {
		t.Errorf("want missing-one error, got %v", err)
	}
}

func TestAddProfileMCP_BothCommandAndURL(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddProfile(dir, "ci"); err != nil {
		t.Fatal(err)
	}
	err := AddProfileMCP(dir, "ci", "s", ProfileMCPAddOptions{Command: "x", URL: "y"})
	if err == nil || !strings.Contains(err.Error(), "both") {
		t.Errorf("want both error, got %v", err)
	}
}

func TestAddProfileMCP_EmptyName(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddProfile(dir, "ci"); err != nil {
		t.Fatal(err)
	}
	err := AddProfileMCP(dir, "ci", "", ProfileMCPAddOptions{Command: "x"})
	if err == nil || !strings.Contains(err.Error(), "name") {
		t.Errorf("want name error, got %v", err)
	}
}

func TestAddProfileMCP_UnknownProfile(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	err := AddProfileMCP(dir, "ghost", "s", ProfileMCPAddOptions{Command: "x"})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("want not-found, got %v", err)
	}
}

func TestAddProfileMCP_Duplicate(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddProfile(dir, "ci"); err != nil {
		t.Fatal(err)
	}
	if err := AddProfileMCP(dir, "ci", "s", ProfileMCPAddOptions{Command: "x"}); err != nil {
		t.Fatal(err)
	}
	err := AddProfileMCP(dir, "ci", "s", ProfileMCPAddOptions{Command: "y"})
	if err == nil || !strings.Contains(err.Error(), "already present") {
		t.Errorf("want duplicate, got %v", err)
	}
}

func TestRemoveProfileMCP_Removes(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddProfile(dir, "ci"); err != nil {
		t.Fatal(err)
	}
	if err := AddProfileMCP(dir, "ci", "s", ProfileMCPAddOptions{Command: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := RemoveProfileMCP(dir, "ci", "s"); err != nil {
		t.Fatalf("RemoveProfileMCP: %v", err)
	}
	hj, _ := plugin.LoadPluginJSON(dir)
	p := hj.Profiles["ci"]
	if len(p.MCPServers) != 0 {
		t.Errorf("expected cleared, got %v", p.MCPServers)
	}
}

func TestRemoveProfileMCP_UnknownProfile(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	err := RemoveProfileMCP(dir, "ghost", "s")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("want not-found, got %v", err)
	}
}

func TestRemoveProfileMCP_NotFound(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddProfile(dir, "ci"); err != nil {
		t.Fatal(err)
	}
	err := RemoveProfileMCP(dir, "ci", "ghost")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("want not-found, got %v", err)
	}
}

func TestUpdateProfileMCP_Command(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddProfile(dir, "ci"); err != nil {
		t.Fatal(err)
	}
	if err := AddProfileMCP(dir, "ci", "s", ProfileMCPAddOptions{Command: "old"}); err != nil {
		t.Fatal(err)
	}
	if err := UpdateProfileMCP(dir, "ci", "s", MCPUpdateOptions{Command: ptr("new")}); err != nil {
		t.Fatalf("UpdateProfileMCP: %v", err)
	}
	hj, _ := plugin.LoadPluginJSON(dir)
	p := hj.Profiles["ci"]
	if p.MCPServers["s"].Command != "new" {
		t.Errorf("command = %q, want new", p.MCPServers["s"].Command)
	}
}

func TestUpdateProfileMCP_NoFlags(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddProfile(dir, "ci"); err != nil {
		t.Fatal(err)
	}
	if err := AddProfileMCP(dir, "ci", "s", ProfileMCPAddOptions{Command: "x"}); err != nil {
		t.Fatal(err)
	}
	err := UpdateProfileMCP(dir, "ci", "s", MCPUpdateOptions{})
	if err == nil || !strings.Contains(err.Error(), "at least one flag") {
		t.Errorf("want no-flags, got %v", err)
	}
}

func TestUpdateProfileMCP_UnknownProfile(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	err := UpdateProfileMCP(dir, "ghost", "s", MCPUpdateOptions{Command: ptr("x")})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("want not-found, got %v", err)
	}
}

func TestUpdateProfileMCP_NotFound(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddProfile(dir, "ci"); err != nil {
		t.Fatal(err)
	}
	err := UpdateProfileMCP(dir, "ci", "ghost", MCPUpdateOptions{Command: ptr("x")})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("want not-found, got %v", err)
	}
}

func TestUpdateProfileMCP_NullEntryRejected(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddProfile(dir, "ci"); err != nil {
		t.Fatal(err)
	}
	if err := AddProfileMCP(dir, "ci", "s", ProfileMCPAddOptions{Null: true}); err != nil {
		t.Fatal(err)
	}
	err := UpdateProfileMCP(dir, "ci", "s", MCPUpdateOptions{Command: ptr("x")})
	if err == nil || !strings.Contains(err.Error(), "null entry") {
		t.Errorf("want null-entry error, got %v", err)
	}
}
