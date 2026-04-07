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
