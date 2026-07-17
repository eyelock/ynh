package harness

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDir_MCPJSONFallback_UsedWhenManifestEmpty(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")

	data := `{"mcpServers":{"rekordbox-database":{"command":"uv","args":["run","rekordbox-mcp"],"cwd":"${PATH_VAR}"}}}`
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	h, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	srv, ok := h.MCPServers["rekordbox-database"]
	if !ok {
		t.Fatalf("MCPServers = %v, want rekordbox-database entry ingested from .mcp.json", h.MCPServers)
	}
	if srv.Command != "uv" || srv.Cwd != "${PATH_VAR}" {
		t.Errorf("server = %+v, want command=uv cwd=${PATH_VAR}", srv)
	}
}

func TestLoadDir_MCPJSONFallback_ManifestTakesPrecedence(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")
	if err := AddMCP(dir, "manifest-server", MCPAddOptions{Command: "from-manifest"}); err != nil {
		t.Fatalf("AddMCP: %v", err)
	}

	data := `{"mcpServers":{"json-server":{"command":"from-json"}}}`
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	h, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if _, ok := h.MCPServers["json-server"]; ok {
		t.Errorf("MCPServers = %v, want .mcp.json ignored when manifest already declares mcp_servers", h.MCPServers)
	}
	if h.MCPServers["manifest-server"].Command != "from-manifest" {
		t.Errorf("MCPServers = %v, want manifest-server from manifest", h.MCPServers)
	}
}

func TestLoadDir_MCPJSONFallback_AbsentWhenNoFile(t *testing.T) {
	dir := t.TempDir()
	writeTestHarness(t, dir, "h")

	h, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if len(h.MCPServers) != 0 {
		t.Errorf("MCPServers = %v, want empty", h.MCPServers)
	}
}
