package vendor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WriteSecretFiles writes generated vendor MCP files under baseDir. Unlike
// an export, a run, an agent session or a preview has already resolved
// ${VAR} in env and headers, so these files hold real secret values. They
// are written mode 0600, as the per-delegate .env.ynh is, and baseDir and
// every directory created beneath it are made 0700. WriteFile and MkdirAll
// keep the mode of what already exists, so each is chmodded explicitly.
func WriteSecretFiles(baseDir string, files map[string][]byte) error {
	if len(files) == 0 {
		return nil
	}
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(baseDir, 0o700); err != nil {
		return err
	}
	for relPath, data := range files {
		abs := filepath.Join(baseDir, relPath)
		dir := filepath.Dir(abs)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("creating %s: %w", filepath.Dir(relPath), err)
		}
		// Tighten each directory from baseDir down to the file.
		rel, err := filepath.Rel(baseDir, dir)
		if err != nil {
			return err
		}
		cur := baseDir
		if rel != "." {
			for _, part := range strings.Split(rel, string(filepath.Separator)) {
				cur = filepath.Join(cur, part)
				if err := os.Chmod(cur, 0o700); err != nil {
					return err
				}
			}
		}
		if err := WriteSecretFile(abs, data); err != nil {
			return fmt.Errorf("writing %s: %w", relPath, err)
		}
	}
	return nil
}

// WriteSecretFile writes one file at mode 0600, tightening one that exists.
func WriteSecretFile(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}
