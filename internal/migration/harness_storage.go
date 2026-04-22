package migration

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/namespace"
	"github.com/eyelock/ynh/internal/plugin"
)

// HarnessStorageMigrator moves flat ~/.ynh/harnesses/<name>/ to the namespaced
// layout ~/.ynh/harnesses/<org>--<repo>/<name>/.
//
// Applies only when:
//   - The directory's parent is HarnessesDir() directly (flat layout)
//   - .ynh-plugin/plugin.json exists (HarnessFormatMigrator must have run first)
//
// If installed.json is missing or has no source, the harness is left in place
// under a synthetic "local/unknown" namespace and a warning is printed.
//
// Uses write-to-temp-then-rename for atomicity. If interrupted, Applies returns
// true on retry and the migration re-runs cleanly (idempotent).
type HarnessStorageMigrator struct{}

func (HarnessStorageMigrator) Description() string {
	return "harness storage: flat → namespaced layout"
}

func (HarnessStorageMigrator) Applies(dir string) bool {
	if filepath.Dir(dir) != config.HarnessesDir() {
		return false
	}
	return plugin.IsPluginDir(dir)
}

func (HarnessStorageMigrator) Run(dir string) error {
	name := filepath.Base(dir)

	ns := "local/unknown"
	ins, err := plugin.LoadInstalledJSON(dir)
	if err == nil && ins.Source != "" {
		ns = namespace.DeriveFromURL(ins.Source)
	} else {
		fmt.Fprintf(os.Stderr, "warning: harness %q: no provenance found, placing under local/unknown namespace\n", name)
		fmt.Fprintf(os.Stderr, "  To fix: ynh install %s@org/repo\n", name)
	}

	fsName := namespace.ToFSName(ns)
	nsDir := filepath.Join(config.HarnessesDir(), fsName)
	if err := os.MkdirAll(nsDir, 0o755); err != nil {
		return fmt.Errorf("creating namespace dir %s: %w", nsDir, err)
	}

	destDir := filepath.Join(nsDir, name)

	// Update namespace in installed.json before moving
	if ins != nil {
		ins.Namespace = ns
		if err := plugin.SaveInstalledJSON(dir, ins); err != nil {
			return fmt.Errorf("updating installed.json: %w", err)
		}
	}

	// Attempt atomic rename first
	if err := os.Rename(dir, destDir); err != nil {
		// Cross-device move: copy then delete
		if err2 := copyDir(dir, destDir); err2 != nil {
			return fmt.Errorf("copying harness to %s: %w", destDir, err2)
		}
		if err2 := os.RemoveAll(dir); err2 != nil {
			return fmt.Errorf("removing old harness dir %s: %w", dir, err2)
		}
	}

	return nil
}

// copyDir recursively copies src to dst.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}

	return os.Rename(tmp, dst)
}
