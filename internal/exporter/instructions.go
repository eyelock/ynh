package exporter

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/resolver"
	"github.com/eyelock/ynh/internal/vendor"
)

// SystemPromptGenerator describes the ability to produce vendor-native
// instruction files. Used by WriteSystemPrompt to avoid depending on the
// full vendor.Adapter interface.
type SystemPromptGenerator interface {
	GenerateSystemPrompt(content []byte) map[string][]byte
}

// WriteSystemPrompt reads instructions from instructionsPath and writes
// vendor-native instruction files to outputDir using the adapter's
// GenerateSystemPrompt method.
func WriteSystemPrompt(instructionsPath string, outputDir string, adapter SystemPromptGenerator) error {
	content, err := os.ReadFile(instructionsPath)
	if err != nil {
		return fmt.Errorf("reading instructions: %w", err)
	}

	files := adapter.GenerateSystemPrompt(content)
	for relPath, data := range files {
		absPath := filepath.Join(outputDir, relPath)
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(absPath, data, 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", relPath, err)
		}
	}

	return nil
}

// WriteMergedSystemPrompt writes instruction files for multiple vendors
// into a single output directory. Used by merged export mode.
func WriteMergedSystemPrompt(instructionsPath string, outputDir string, vendors []string) error {
	content, err := os.ReadFile(instructionsPath)
	if err != nil {
		return fmt.Errorf("reading instructions: %w", err)
	}

	// Collect files from all vendor adapters, deduplicating by path.
	// If multiple vendors write the same file (e.g. AGENTS.md), last wins
	// — content is identical so order doesn't matter.
	files := make(map[string][]byte)
	for _, v := range vendors {
		adapter, err := vendor.Get(v)
		if err != nil {
			continue
		}
		for path, data := range adapter.GenerateSystemPrompt(content) {
			files[path] = data
		}
	}

	for relPath, data := range files {
		absPath := filepath.Join(outputDir, relPath)
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(absPath, data, 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", relPath, err)
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
