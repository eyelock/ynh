package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// resolveCommandProgram reports a fingerprint of the program a sensor's
// command would actually execute from cwd, or "" when nothing resolvable was
// found.
//
// It asks the shell to word-split the command exactly as it will at run time,
// then resolves the first word with `command -v`. A relative result is made
// absolute against cwd, and the file is hashed rather than compared by path:
// two trees can hold the same path with different contents, which is the
// case a path comparison misses.
//
// The command's own expansions run here, including any substitution. That is
// not new exposure: the same string is about to be handed to `sh -c` anyway.
func resolveCommandProgram(command, cwd, harnessDir string) string {
	// `set --` splits the command the way the shell would, so "$1" is the
	// program however it was quoted or expanded.
	script := `set -- ` + command + `
p=$(command -v -- "$1" 2>/dev/null) || exit 0
[ -n "$p" ] || exit 0
printf '%s' "$p"`

	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "YNH_HARNESS_DIR="+harnessDir)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		// Resolvable but unreadable (a directory, a permissions problem).
		// The path itself is still a usable fingerprint.
		return "path:" + path
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])[:16]
}
