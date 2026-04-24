// Package pathutil provides path safety helpers.
package pathutil

import (
	"fmt"
	"path/filepath"
	"strings"
)

// CheckSubpath returns an error if p is not a safe relative subdirectory path.
// Safe means: not absolute, and no ".." component that would escape the base
// directory after cleaning.
func CheckSubpath(p string) error {
	if filepath.IsAbs(p) {
		return fmt.Errorf("path %q must be relative, not absolute", p)
	}
	clean := filepath.Clean(p)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path %q must not traverse above its base directory", p)
	}
	return nil
}
