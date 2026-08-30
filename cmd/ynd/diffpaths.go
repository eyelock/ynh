package main

import (
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/vendor"
)

// Canonical path segments. Real vendors never use these, so a canonical path
// cannot collide with a literal one.
const (
	canonConfigDir   = "<config>"
	canonManifestDir = "<manifest>"
	canonInstruction = "<instructions>"
)

// canonicalPath rewrites a vendor-specific path into a form comparable across
// vendors.
//
// Three prefixes differ per vendor and all three must be mapped, not just the
// config directory: claude writes `.claude/` + `.claude-plugin/` + `CLAUDE.md`,
// cursor writes `.cursor/` + `.cursor-plugin/` + `.cursorrules`, codex writes
// `.agents/plugins/` + `codex.md`, copilot `.github/plugin/` + `AGENTS.md`.
//
// Without this the two file sets never intersect, so `ynd diff` between any
// two vendors reported every file as "only in" one side and its
// content-comparison branch was unreachable — a diff that could not diff.
func canonicalPath(adapter vendor.Adapter, path string) string {
	path = filepath.ToSlash(path)

	// Manifest dir first: ".claude-plugin" would otherwise be caught by a
	// ".claude" prefix test and mapped to the wrong thing.
	if md := filepath.ToSlash(adapter.MarketplaceManifestDir()); md != "" {
		if rest, ok := trimSegment(path, md); ok {
			return joinCanon(canonManifestDir, rest)
		}
	}
	if cd := filepath.ToSlash(adapter.ConfigDir()); cd != "" {
		if rest, ok := trimSegment(path, cd); ok {
			return joinCanon(canonConfigDir, rest)
		}
	}
	if inst := filepath.ToSlash(adapter.InstructionsFile()); inst != "" && path == inst {
		return canonInstruction
	}
	return path
}

// trimSegment reports whether path sits under prefix, and returns the remainder.
// Matching is on whole segments so ".cursorrules" is not treated as ".cursor/".
func trimSegment(path, prefix string) (string, bool) {
	if path == prefix {
		return "", true
	}
	if strings.HasPrefix(path, prefix+"/") {
		return strings.TrimPrefix(path, prefix+"/"), true
	}
	return "", false
}

func joinCanon(head, rest string) string {
	if rest == "" {
		return head
	}
	return head + "/" + rest
}

// renderedDifferently reports whether two canonically-equal paths differ only
// by the extension a vendor renders in.
//
// Cursor writes rules as `.mdc` where the others use `.md`. That is neither
// "the same file" nor "a file only one vendor has" — it is the same content in
// the form each vendor requires, and reporting it as a difference would bury
// the real ones. It gets its own category so a reader can tell a rendering
// convention from a divergence.
func renderedDifferently(pathA, pathB string) bool {
	if pathA == pathB {
		return false
	}
	extA, extB := filepath.Ext(pathA), filepath.Ext(pathB)
	return strings.TrimSuffix(pathA, extA) == strings.TrimSuffix(pathB, extB)
}

// canonicalKey strips a rendering-only extension so `.md` and `.mdc` land on
// the same key and can be compared at all.
func canonicalKey(path string) string {
	if strings.HasSuffix(path, ".mdc") {
		return strings.TrimSuffix(path, ".mdc") + ".md"
	}
	return path
}
