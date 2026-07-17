package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMCPJSON_Missing(t *testing.T) {
	dir := t.TempDir()
	servers, err := LoadMCPJSON(dir)
	if err != nil {
		t.Fatalf("LoadMCPJSON: %v", err)
	}
	if servers != nil {
		t.Errorf("servers = %v, want nil", servers)
	}
}

func TestLoadMCPJSON_Valid(t *testing.T) {
	dir := t.TempDir()
	data := `{
  "mcpServers": {
    "rekordbox-database": {
      "command": "uv",
      "args": ["run", "rekordbox-mcp"],
      "cwd": "${MEDIA_MGMT_REKORDBOX_MCP_PATH}"
    }
  }
}`
	if err := os.WriteFile(filepath.Join(dir, MCPJSONFile), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	servers, err := LoadMCPJSON(dir)
	if err != nil {
		t.Fatalf("LoadMCPJSON: %v", err)
	}
	srv, ok := servers["rekordbox-database"]
	if !ok {
		t.Fatalf("servers = %v, want rekordbox-database entry", servers)
	}
	if srv.Command != "uv" || srv.Cwd != "${MEDIA_MGMT_REKORDBOX_MCP_PATH}" {
		t.Errorf("server = %+v, want command=uv cwd=${MEDIA_MGMT_REKORDBOX_MCP_PATH}", srv)
	}
	if len(srv.Args) != 2 || srv.Args[0] != "run" || srv.Args[1] != "rekordbox-mcp" {
		t.Errorf("args = %v, want [run rekordbox-mcp]", srv.Args)
	}
}

func TestLoadMCPJSON_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, MCPJSONFile), []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadMCPJSON(dir); err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}
