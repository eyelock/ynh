package vendor

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/eyelock/ynh/internal/plugin"
)

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s mode = %o, want %o", path, got, want)
	}
}

// Every vendor's generated MCP files hold expanded secrets, so each file is
// 0600 and every directory above it, up to the base, is 0700.
func TestWriteSecretFiles_AllVendors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes")
	}
	servers := map[string]plugin.MCPServer{
		"local":  {Command: "srv", Env: map[string]string{"TOKEN": "s3cret"}},
		"remote": {Type: "http", URL: "https://example.com/mcp", Headers: map[string]string{"Authorization": "Bearer s3cret"}},
	}
	for _, name := range []string{"claude", "codex", "copilot", "cursor", "opencode"} {
		t.Run(name, func(t *testing.T) {
			a, err := Get(name)
			if err != nil {
				t.Fatal(err)
			}
			files, err := a.GenerateMCPConfig(servers)
			if err != nil {
				t.Fatal(err)
			}
			if len(files) == 0 {
				t.Fatal("vendor wrote no MCP file")
			}
			base := filepath.Join(t.TempDir(), "run")
			// Pre-existing loose modes must be tightened, not kept.
			for rel := range files {
				dir := filepath.Dir(filepath.Join(base, rel))
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(base, rel), []byte("old"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := WriteSecretFiles(base, files); err != nil {
				t.Fatal(err)
			}
			assertMode(t, base, 0o700)
			for rel := range files {
				abs := filepath.Join(base, rel)
				assertMode(t, abs, 0o600)
				for d := filepath.Dir(abs); d != base; d = filepath.Dir(d) {
					assertMode(t, d, 0o700)
				}
			}
		})
	}
}

func TestProjectCopilotMCPConfig_Mode0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes")
	}
	configPath, projectDir := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(configPath, ".copilot"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configPath, ".copilot", ".mcp.json"), []byte(`{"mcpServers":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// An existing 0644 file from an older ynh is tightened.
	target := filepath.Join(projectDir, copilotProjectMCPRelPath)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := projectCopilotMCPConfig(configPath, projectDir); err != nil {
		t.Fatal(err)
	}
	assertMode(t, target, 0o600)
	// The project's own .github directory keeps its mode.
	assertMode(t, filepath.Dir(target), 0o755)
}
