package exporter

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/eyelock/ynh/internal/plugin"
)

// GenerateClaudeManifest creates .claude-plugin/plugin.json by copying the
// source plugin.json verbatim (it's already in Claude's format).
func GenerateClaudeManifest(src *plugin.PluginJSON, outputDir string) error {
	return writeManifest(src, filepath.Join(outputDir, ".claude-plugin"))
}

// GenerateCursorManifest creates .cursor-plugin/plugin.json by translating
// from Claude's plugin.json format. Same schema, different location.
func GenerateCursorManifest(src *plugin.PluginJSON, outputDir string) error {
	return writeManifest(src, filepath.Join(outputDir, ".cursor-plugin"))
}

func writeManifest(src *plugin.PluginJSON, manifestDir string) error {
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		return fmt.Errorf("creating manifest dir: %w", err)
	}

	data, err := json.MarshalIndent(src, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling plugin.json: %w", err)
	}
	data = append(data, '\n')

	return os.WriteFile(filepath.Join(manifestDir, "plugin.json"), data, 0o644)
}
