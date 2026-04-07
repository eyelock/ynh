package exporter

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/eyelock/ynh/internal/plugin"
)

// pluginJSON is the Claude Code plugin.json schema — only identity fields.
type pluginJSON struct {
	Name        string             `json:"name"`
	Version     string             `json:"version"`
	Description string             `json:"description,omitempty"`
	Author      *plugin.AuthorInfo `json:"author,omitempty"`
	Keywords    []string           `json:"keywords,omitempty"`
}

// toPluginJSON extracts vendor-compatible fields from a HarnessJSON.
func toPluginJSON(hj *plugin.HarnessJSON) *pluginJSON {
	return &pluginJSON{
		Name:        hj.Name,
		Version:     hj.Version,
		Description: hj.Description,
		Author:      hj.Author,
		Keywords:    hj.Keywords,
	}
}

// GenerateClaudeManifest creates .claude-plugin/plugin.json from harness identity fields.
func GenerateClaudeManifest(hj *plugin.HarnessJSON, outputDir string) error {
	return writeManifest(toPluginJSON(hj), filepath.Join(outputDir, ".claude-plugin"))
}

// GenerateCursorManifest creates .cursor-plugin/plugin.json from harness identity fields.
func GenerateCursorManifest(hj *plugin.HarnessJSON, outputDir string) error {
	return writeManifest(toPluginJSON(hj), filepath.Join(outputDir, ".cursor-plugin"))
}

// codexPluginJSON is the Codex plugin.json schema — identity fields plus path pointers.
// Codex manifests require path pointers (skills, mcpServers) so the plugin system
// knows where to find components within the plugin directory.
type codexPluginJSON struct {
	Name        string             `json:"name"`
	Version     string             `json:"version"`
	Description string             `json:"description,omitempty"`
	Author      *plugin.AuthorInfo `json:"author,omitempty"`
	Keywords    []string           `json:"keywords,omitempty"`
	Skills      string             `json:"skills,omitempty"`
	MCPServers  string             `json:"mcpServers,omitempty"`
}

// GenerateCodexManifest creates .codex-plugin/plugin.json from harness identity fields.
// Includes path pointers for skills and MCP servers when present in the output directory.
func GenerateCodexManifest(hj *plugin.HarnessJSON, outputDir string) error {
	cpj := &codexPluginJSON{
		Name:        hj.Name,
		Version:     hj.Version,
		Description: hj.Description,
		Author:      hj.Author,
		Keywords:    hj.Keywords,
	}

	// Add path pointers for components that exist
	if dirHasContent(filepath.Join(outputDir, "skills")) {
		cpj.Skills = "./skills/"
	}
	if fileExists(filepath.Join(outputDir, ".mcp.json")) {
		cpj.MCPServers = "./.mcp.json"
	}

	manifestDir := filepath.Join(outputDir, ".codex-plugin")
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		return fmt.Errorf("creating manifest dir: %w", err)
	}

	data, err := json.MarshalIndent(cpj, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling plugin.json: %w", err)
	}
	data = append(data, '\n')

	return os.WriteFile(filepath.Join(manifestDir, "plugin.json"), data, 0o644)
}

// dirHasContent returns true if dir exists and contains at least one entry.
func dirHasContent(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}

// fileExists returns true if path exists and is a regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func writeManifest(pj *pluginJSON, manifestDir string) error {
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		return fmt.Errorf("creating manifest dir: %w", err)
	}

	data, err := json.MarshalIndent(pj, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling plugin.json: %w", err)
	}
	data = append(data, '\n')

	return os.WriteFile(filepath.Join(manifestDir, "plugin.json"), data, 0o644)
}
