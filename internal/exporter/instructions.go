package exporter

import (
	"os"
	"path/filepath"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/resolver"
)

// GenerateInstructions writes instruction files from the persona's instructions.md.
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

// DiscoverInstructions finds the last instructions.md across all resolved content.
// Later sources override earlier ones (persona's own instructions.md wins).
// Returns empty string if none found.
func DiscoverInstructions(content []resolver.ResolvedContent) string {
	var found string
	for _, rc := range content {
		candidate := filepath.Join(rc.BasePath, "instructions.md")
		if _, err := os.Stat(candidate); err == nil {
			found = candidate
		}
	}
	return found
}
