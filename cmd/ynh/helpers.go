package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/harness"
)

// isLocalPath determines if a source string refers to a local filesystem path.
func isLocalPath(source string) bool {
	if strings.HasPrefix(source, ".") || strings.HasPrefix(source, "/") {
		return true
	}
	if _, err := os.Stat(source); err == nil {
		return true
	}
	return false
}

// copyTree recursively copies a directory, skipping .git.
func copyTree(src, dst string) error {
	return assembler.CopyDir(src, dst)
}

// loadOrSynthesizeHarness loads a harness from a directory. If the directory
// has .claude-plugin/plugin.json, it loads normally. If the directory has
// AGENTS.md (or instructions.md) but no plugin.json, it synthesizes minimal
// plugin metadata from the directory name and creates the structure on disk
// so that the rest of the install flow works unchanged.
func loadOrSynthesizeHarness(dir string) (*harness.Harness, error) {
	// Try standard plugin format first
	if harness.DetectFormat(dir) == "plugin" {
		return harness.LoadPluginDir(dir)
	}

	// Check for bare AGENTS.md or instructions.md
	hasInstructions := assembler.FindInstructionsFile(dir) != ""
	if !hasInstructions {
		return nil, fmt.Errorf("directory %q has no .claude-plugin/plugin.json or AGENTS.md", dir)
	}

	// Synthesize minimal plugin.json in the source directory
	name := filepath.Base(dir)
	pluginDir := filepath.Join(dir, ".claude-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating plugin dir: %w", err)
	}

	pj := map[string]string{
		"name":    name,
		"version": "0.0.0",
	}
	data, _ := json.MarshalIndent(pj, "", "  ")
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), append(data, '\n'), 0o644); err != nil {
		return nil, fmt.Errorf("writing synthesized plugin.json: %w", err)
	}

	return harness.LoadPluginDir(dir)
}
