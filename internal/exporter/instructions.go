package exporter

import (
	"path/filepath"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/resolver"
)

// GenerateInstructions writes instruction files from the harness's instructions.
//
// Always writes outputDir/AGENTS.md (the universal format).
// If vendorInstructionsFile is non-empty and differs from "AGENTS.md",
// also writes outputDir/<vendorInstructionsFile> (same content).
func GenerateInstructions(instructionsPath string, outputDir string, vendorInstructionsFile string) error {
	// Always write AGENTS.md
	if err := assembler.CopyFile(instructionsPath, filepath.Join(outputDir, "AGENTS.md")); err != nil {
		return err
	}

	// Write vendor-specific file if different from AGENTS.md
	if vendorInstructionsFile != "" && vendorInstructionsFile != "AGENTS.md" {
		if err := assembler.CopyFile(instructionsPath, filepath.Join(outputDir, vendorInstructionsFile)); err != nil {
			return err
		}
	}

	return nil
}

// DiscoverInstructions finds the last instructions file across all resolved content.
// Checks instructions.md first, then AGENTS.md as fallback per source.
// Later sources override earlier ones (harness's own instructions win).
// Returns empty string if none found.
func DiscoverInstructions(content []resolver.ResolvedContent) string {
	var found string
	for _, rc := range content {
		candidate := assembler.FindInstructionsFile(rc.BasePath)
		if candidate != "" {
			found = candidate
		}
	}
	return found
}
