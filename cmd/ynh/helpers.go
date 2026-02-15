package main

import (
	"os"
	"strings"

	"github.com/eyelock/ynh/internal/assembler"
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

// copyTree recursively copies a directory, skipping .git.
func copyTree(src, dst string) error {
	return assembler.CopyDir(src, dst)
}
