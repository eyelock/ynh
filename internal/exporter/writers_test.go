package exporter

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/resolver"
)

// stubExporter lets each generator be driven independently, including into
// failure, which is how the error paths are reached without a broken vendor.
type stubExporter struct {
	mcp     map[string][]byte
	mcpErr  error
	hooks   map[string][]byte
	hookErr error
}

func (s stubExporter) ArtifactDirs() map[string]string       { return map[string]string{"skills": "skills"} }
func (s stubExporter) ExportArtifactDirs() map[string]string { return nil }
func (s stubExporter) SupportsExportDelegates() bool         { return true }
func (s stubExporter) GenerateSystemPrompt(c []byte) map[string][]byte {
	return map[string][]byte{"AGENTS.md": c}
}
func (s stubExporter) GeneratePluginManifest(*plugin.HarnessJSON, string) (map[string][]byte, error) {
	return nil, nil
}
func (s stubExporter) GenerateHookConfig(map[string][]plugin.HookEntry) (map[string][]byte, error) {
	return s.hooks, s.hookErr
}
func (s stubExporter) GenerateMCPConfig(map[string]plugin.MCPServer) (map[string][]byte, error) {
	return s.mcp, s.mcpErr
}

func TestWriteGeneratedFiles_CreatesNestedParents(t *testing.T) {
	out := t.TempDir()
	err := writeGeneratedFiles(out, map[string][]byte{
		"top.md":                  []byte("a"),
		"a/b/c/deep.md":           []byte("b"),
		".hidden/dir/config.json": []byte("c"),
	})
	if err != nil {
		t.Fatalf("writeGeneratedFiles: %v", err)
	}
	for path, want := range map[string]string{
		"top.md":                  "a",
		"a/b/c/deep.md":           "b",
		".hidden/dir/config.json": "c",
	} {
		got, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(path)))
		if err != nil {
			t.Errorf("%s: %v", path, err)
			continue
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}
}

func TestWriteGeneratedFiles_EmptyWritesNothing(t *testing.T) {
	out := t.TempDir()
	if err := writeGeneratedFiles(out, nil); err != nil {
		t.Fatalf("writeGeneratedFiles: %v", err)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected nothing written, got %v", entries)
	}
}

func TestWriteMCPConfig(t *testing.T) {
	out := t.TempDir()
	s := stubExporter{mcp: map[string][]byte{".claude/.mcp.json": []byte(`{"mcpServers":{}}`)}}
	if err := writeMCPConfig(out, s, nil); err != nil {
		t.Fatalf("writeMCPConfig: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, ".claude", ".mcp.json")); err != nil {
		t.Errorf("MCP config not written: %v", err)
	}
}

// A vendor that cannot generate its MCP config must stop the export, not
// produce a plugin missing the servers its manifest declared.
func TestWriteMCPConfig_GeneratorErrorPropagates(t *testing.T) {
	err := writeMCPConfig(t.TempDir(), stubExporter{mcpErr: errors.New("boom")}, nil)
	if err == nil {
		t.Fatal("a generator failure must not be swallowed")
	}
	if !strings.Contains(err.Error(), "generating MCP config") {
		t.Errorf("error should say what it was doing, got: %v", err)
	}
}

func TestWriteHookConfig(t *testing.T) {
	out := t.TempDir()
	s := stubExporter{hooks: map[string][]byte{"hooks/hooks.json": []byte(`{}`)}}
	if err := writeHookConfig(out, s, nil); err != nil {
		t.Fatalf("writeHookConfig: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "hooks", "hooks.json")); err != nil {
		t.Errorf("hook config not written: %v", err)
	}
}

func TestWriteHookConfig_GeneratorErrorPropagates(t *testing.T) {
	err := writeHookConfig(t.TempDir(), stubExporter{hookErr: errors.New("boom")}, nil)
	if err == nil {
		t.Fatal("a generator failure must not be swallowed")
	}
	if !strings.Contains(err.Error(), "generating hook config") {
		t.Errorf("error should say what it was doing, got: %v", err)
	}
}

// A vendor emitting no hook config is normal, not an error: Copilot does
// exactly this because its hooks never fire.
func TestWriteHookConfig_NoFilesIsFine(t *testing.T) {
	out := t.TempDir()
	if err := writeHookConfig(out, stubExporter{}, nil); err != nil {
		t.Fatalf("a vendor with no hook config must not fail: %v", err)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("nothing should have been written, got %v", entries)
	}
}

// copyContent has two branches: a source with no picks copies everything, a
// source with picks copies only those. The picked branch was uncovered, and
// it is the one that decides what a `pick` in the manifest actually ships.
func TestCopyContent_AllVersusPicked(t *testing.T) {
	src := t.TempDir()
	for _, name := range []string{"alpha", "beta"} {
		dir := filepath.Join(src, "skills", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dirs := map[string]string{"skills": "skills"}

	t.Run("no picks copies everything", func(t *testing.T) {
		out := t.TempDir()
		err := copyContent(out, []resolver.ResolvedContent{{BasePath: src}}, dirs, assembler.ArtifactTransform(nil))
		if err != nil {
			t.Fatalf("copyContent: %v", err)
		}
		for _, name := range []string{"alpha", "beta"} {
			if _, err := os.Stat(filepath.Join(out, "skills", name, "SKILL.md")); err != nil {
				t.Errorf("%s was not copied: %v", name, err)
			}
		}
	})

	t.Run("picks copy only what was picked", func(t *testing.T) {
		out := t.TempDir()
		err := copyContent(out, []resolver.ResolvedContent{
			{BasePath: src, Paths: []string{"skills/alpha"}},
		}, dirs, assembler.ArtifactTransform(nil))
		if err != nil {
			t.Fatalf("copyContent: %v", err)
		}
		if _, err := os.Stat(filepath.Join(out, "skills", "alpha", "SKILL.md")); err != nil {
			t.Errorf("the picked skill was not copied: %v", err)
		}
		if _, err := os.Stat(filepath.Join(out, "skills", "beta", "SKILL.md")); err == nil {
			t.Error("an unpicked skill was copied; pick is not restricting anything")
		}
	})
}
