package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/migration"
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

// loadOrSynthesizeHarness loads a harness from a directory. The migration
// chain runs first to convert any legacy format transparently. If no manifest
// exists but AGENTS.md or instructions.md does, a minimal plugin.json is
// synthesized so the install flow works unchanged.
func loadOrSynthesizeHarness(dir string) (*harness.Harness, error) {
	if _, err := migration.FormatChain().Run(dir); err != nil {
		return nil, fmt.Errorf("migrating harness format: %w", err)
	}

	if plugin.IsPluginDir(dir) {
		return harness.LoadDir(dir)
	}

	// No manifest: try to synthesize from AGENTS.md or instructions.md
	if assembler.FindInstructionsFile(dir) == "" {
		return nil, fmt.Errorf("directory %q has no harness manifest or AGENTS.md", dir)
	}

	name := filepath.Base(dir)
	hj := &plugin.HarnessJSON{
		Name:    name,
		Version: "0.0.0",
	}
	if err := plugin.SavePluginJSON(dir, hj); err != nil {
		return nil, fmt.Errorf("writing synthesized plugin.json: %w", err)
	}

	return harness.LoadDir(dir)
}
