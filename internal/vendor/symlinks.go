package vendor

import (
	"fmt"
	"os"
	"path/filepath"
)

// PlanSymlinks returns the symlink entries that would be created by Install,
// without modifying the filesystem. Used to preview before prompting the user.
func PlanSymlinks(stagingDir, projectDir, configDir string, artifactDirs map[string]string) ([]SymlinkEntry, error) {
	var entries []SymlinkEntry

	stagingConfig := filepath.Join(stagingDir, configDir)
	projectConfig := filepath.Join(projectDir, configDir)

	for _, artifactDir := range artifactDirs {
		srcDir := filepath.Join(stagingConfig, artifactDir)
		items, err := os.ReadDir(srcDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("reading %s: %w", srcDir, err)
		}

		for _, item := range items {
			target := filepath.Join(srcDir, item.Name())
			link := filepath.Join(projectConfig, artifactDir, item.Name())
			entries = append(entries, SymlinkEntry{Target: target, Link: link})
		}
	}

	return entries, nil
}

// installSymlinks creates symlinks from projectDir/<configDir>/<artifact>/<name>
// to stagingDir/<configDir>/<artifact>/<name> for each artifact in the staging dir.
//
// Safety: never overwrites existing non-symlink files (user's own config).
// Existing symlinks are replaced (idempotent reinstall).
func installSymlinks(stagingDir, projectDir, configDir string, artifactDirs map[string]string) ([]SymlinkEntry, error) {
	var entries []SymlinkEntry

	stagingConfig := filepath.Join(stagingDir, configDir)
	projectConfig := filepath.Join(projectDir, configDir)

	for _, artifactDir := range artifactDirs {
		srcDir := filepath.Join(stagingConfig, artifactDir)
		items, err := os.ReadDir(srcDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("reading %s: %w", srcDir, err)
		}

		for _, item := range items {
			target := filepath.Join(srcDir, item.Name())
			link := filepath.Join(projectConfig, artifactDir, item.Name())

			entry, err := createSymlink(target, link)
			if err != nil {
				return entries, err
			}
			if entry != nil {
				entries = append(entries, *entry)
			}
		}
	}

	return entries, nil
}

// createSymlink creates a single symlink from target to link.
// Returns nil entry (no error) if the path already has a non-symlink file.
func createSymlink(target, link string) (*SymlinkEntry, error) {
	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return nil, err
	}

	info, err := os.Lstat(link)
	if err == nil {
		// Something exists at this path.
		if info.Mode()&os.ModeSymlink != 0 {
			// Existing symlink - replace it (idempotent).
			if err := os.Remove(link); err != nil {
				return nil, fmt.Errorf("replacing symlink %s: %w", link, err)
			}
		} else {
			// Real file/dir — skip to avoid overwriting user's config.
			// Caller receives nil entry (no symlink created).
			return nil, nil
		}
	}

	if err := os.Symlink(target, link); err != nil {
		return nil, fmt.Errorf("creating symlink %s -> %s: %w", link, target, err)
	}

	return &SymlinkEntry{Target: target, Link: link}, nil
}

// cleanSymlinks removes symlinks that were created by ynh.
// Only removes entries that are still symlinks pointing to the expected target.
func cleanSymlinks(entries []SymlinkEntry) error {
	for _, entry := range entries {
		info, err := os.Lstat(entry.Link)
		if err != nil {
			// Already gone - skip.
			continue
		}

		if info.Mode()&os.ModeSymlink == 0 {
			// No longer a symlink (user replaced it) — skip.
			continue
		}

		// Verify it still points to our target.
		actual, err := os.Readlink(entry.Link)
		if err != nil {
			continue
		}
		if actual != entry.Target {
			// Points elsewhere — skip.
			continue
		}

		if err := os.Remove(entry.Link); err != nil {
			return fmt.Errorf("removing symlink %s: %w", entry.Link, err)
		}
	}
	return nil
}
