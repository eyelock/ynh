package assembler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/resolver"
)

// LayoutProvider describes the vendor layout information that the assembler
// needs. Consumers define their own narrow interface rather than depending on
// the full vendor.Adapter.
type LayoutProvider interface {
	// ConfigDir returns the vendor's config directory name (e.g. ".claude").
	ConfigDir() string
	// ArtifactDirs maps artifact types to their directory names within the config dir.
	ArtifactDirs() map[string]string
	// InstructionsFile returns the filename for project-level instructions.
	InstructionsFile() string
}

// Assemble creates a temporary directory with vendor-specific config layout
// populated from resolved Git content.
func Assemble(adapter LayoutProvider, content []resolver.ResolvedContent) (string, error) {
	workDir, err := os.MkdirTemp("", "ynh-*")
	if err != nil {
		return "", fmt.Errorf("creating temp dir: %w", err)
	}

	if err := assembleInto(workDir, adapter, content); err != nil {
		return "", err
	}
	return workDir, nil
}

// AssembleTo populates a specific directory with vendor-specific config layout.
// The directory is cleaned and recreated. Use this for deterministic paths that
// survive process replacement (syscall.Exec).
func AssembleTo(dir string, adapter LayoutProvider, content []resolver.ResolvedContent) error {
	// Clean previous run
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("cleaning run dir: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating run dir: %w", err)
	}

	return assembleInto(dir, adapter, content)
}

func assembleInto(workDir string, adapter LayoutProvider, content []resolver.ResolvedContent) error {
	configDir := filepath.Join(workDir, adapter.ConfigDir())
	artifactDirs := adapter.ArtifactDirs()

	// Create all artifact directories
	for _, dir := range artifactDirs {
		if err := os.MkdirAll(filepath.Join(configDir, dir), 0o755); err != nil {
			return err
		}
	}

	// Copy content into the right places
	for _, rc := range content {
		if len(rc.Paths) == 0 {
			// No pick list - include everything that matches artifact types
			if err := CopyAllArtifacts(rc.BasePath, configDir, artifactDirs); err != nil {
				return err
			}
		} else {
			for _, picked := range rc.Paths {
				if err := CopyPicked(rc.BasePath, picked, configDir, artifactDirs); err != nil {
					return err
				}
			}
		}
	}

	// Copy instructions to vendor-specific project instructions file.
	// Checks instructions.md first, then AGENTS.md as fallback.
	// Later sources override earlier ones (harness's own instructions win).
	instructionsFile := adapter.InstructionsFile()
	if instructionsFile != "" {
		for _, rc := range content {
			src := FindInstructionsFile(rc.BasePath)
			if src != "" {
				dst := filepath.Join(workDir, instructionsFile)
				if err := CopyFile(src, dst); err != nil {
					return fmt.Errorf("copying instructions: %w", err)
				}
			}
		}
	}

	return nil
}

// Cleanup removes an assembled directory.
func Cleanup(workDir string) {
	_ = os.RemoveAll(workDir)
}

// CopyPicked copies a specific path from the repo into the right artifact directory.
// picked is like "skills/commit" or "agents/code-reviewer.md".
// targetBaseDir is where artifact type directories live (e.g., workDir/.claude/ for runtime,
// or pluginRoot/ for export).
func CopyPicked(repoBase string, picked string, targetBaseDir string, artifactDirs map[string]string) error {
	// Determine which artifact type this belongs to
	parts := strings.SplitN(picked, "/", 2)
	if len(parts) < 2 {
		return fmt.Errorf("pick path must be in format 'type/name': %s", picked)
	}

	artifactType := parts[0]
	targetDir, ok := artifactDirs[artifactType]
	if !ok {
		return fmt.Errorf("unknown artifact type %q in pick: %s", artifactType, picked)
	}

	src := filepath.Join(repoBase, picked)
	dst := filepath.Join(targetBaseDir, targetDir, parts[1])

	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("pick path not found: %s (%w)", picked, err)
	}

	if info.IsDir() {
		return CopyDir(src, dst)
	}
	return CopyFile(src, dst)
}

// CopyAllArtifacts scans the repo for known artifact type directories and copies them.
// targetBaseDir is where artifact type directories live (e.g., workDir/.claude/ for runtime,
// or pluginRoot/ for export).
func CopyAllArtifacts(repoBase string, targetBaseDir string, artifactDirs map[string]string) error {
	for artifactType, targetDir := range artifactDirs {
		srcDir := filepath.Join(repoBase, artifactType)
		if _, err := os.Stat(srcDir); os.IsNotExist(err) {
			continue
		}

		entries, err := os.ReadDir(srcDir)
		if err != nil {
			return err
		}

		for _, entry := range entries {
			src := filepath.Join(srcDir, entry.Name())
			dst := filepath.Join(targetBaseDir, targetDir, entry.Name())

			if entry.IsDir() {
				if err := CopyDir(src, dst); err != nil {
					return err
				}
			} else {
				if err := CopyFile(src, dst); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// CopyFile copies a single file from src to dst, creating parent directories as needed.
func CopyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	return os.WriteFile(dst, data, info.Mode())
}

// CopyDir recursively copies src to dst, skipping .git directories.
func CopyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() && info.Name() == ".git" {
			return filepath.SkipDir
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		target := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		return CopyFile(path, target)
	})
}

// FindInstructionsFile returns the path to the instructions file in dir.
// Checks instructions.md first, then AGENTS.md as fallback.
// Returns empty string if neither exists.
func FindInstructionsFile(dir string) string {
	for _, name := range []string{"instructions.md", "AGENTS.md"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}
