package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
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
// has harness.json, it loads normally. If the directory has AGENTS.md (or
// instructions.md) but no harness.json, it synthesizes minimal harness.json
// on disk so that the rest of the install flow works unchanged.
func loadOrSynthesizeHarness(dir string) (*harness.Harness, error) {
	// Try harness.json format first
	if harness.DetectFormat(dir) == "harness" {
		return harness.LoadDir(dir)
	}

	// Legacy format detection
	if harness.DetectFormat(dir) == "legacy" {
		return nil, fmt.Errorf("legacy format detected in %q. Consolidate .claude-plugin/plugin.json and metadata.json into harness.json", dir)
	}

	// Check for bare AGENTS.md or instructions.md
	hasInstructions := assembler.FindInstructionsFile(dir) != ""
	if !hasInstructions {
		return nil, fmt.Errorf("directory %q has no harness.json or AGENTS.md", dir)
	}

	// Synthesize minimal harness.json in the source directory
	name := filepath.Base(dir)
	hj := &plugin.HarnessJSON{
		Name:    name,
		Version: "0.0.0",
	}
	if err := plugin.SaveHarnessJSON(dir, hj); err != nil {
		return nil, fmt.Errorf("writing synthesized harness.json: %w", err)
	}

	return harness.LoadDir(dir)
}
