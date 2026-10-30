package exporter

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/eyelock/ynh/internal/vendor"
)

// SystemPromptGenerator describes the ability to produce vendor-native
// instruction files. Used by WriteSystemPrompt to avoid depending on the
// full vendor.Adapter interface.
type SystemPromptGenerator interface {
	GenerateSystemPrompt(content []byte) map[string][]byte
}

// WriteSystemPrompt writes vendor-native instruction files for the composed
// instructions to outputDir using the adapter's GenerateSystemPrompt method.
func WriteSystemPrompt(content []byte, outputDir string, adapter SystemPromptGenerator) error {
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
func WriteMergedSystemPrompt(content []byte, outputDir string, vendors []string) error {
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
