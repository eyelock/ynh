package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

// CheckSource checks an include or delegate source against the allow-list.
// It governs every source, local paths included: ynh run re-resolves includes
// from their sources on every launch, so a local path is read on each run.
//
// If AllowedRemoteSources is nil (not configured), every source is allowed.
// If it is an empty slice, every source is denied. Otherwise the source must
// match at least one pattern.
//
// A Git URL is matched in its host/path form. A local path (absolute, "./" or
// "../" relative, or a file:// URL) is matched as a clean absolute path, a
// relative one resolved against baseDir, the harness directory. Allow-list
// entries for local paths are therefore absolute paths.
func (c *Config) CheckSource(source, baseDir string) error {
	if c.AllowedRemoteSources == nil {
		return nil
	}

	match, local := localSourcePath(source, baseDir)
	if !local {
		match = normalizeForMatch(source)
	}

	for _, pattern := range c.AllowedRemoteSources {
		if local && strings.HasPrefix(pattern, "/") {
			pattern = filepath.Clean(pattern)
		}
		if matchGlob(pattern, match) {
			return nil
		}
	}

	kind := "remote source"
	if local {
		kind = "source"
	}
	return fmt.Errorf("%s %q is not in the allowed sources list (add %q to allowed_remote_sources)", kind, source, match)
}

// localSourcePath reports whether source is a local filesystem path and, if
// so, returns the clean path an allow-list entry is matched against. A
// relative path is joined to baseDir, the directory the resolver reads it
// from. A file:// URL counts only in its local form, file:///abs/path.
func localSourcePath(source, baseDir string) (string, bool) {
	p, fileURL := strings.CutPrefix(source, "file://")
	switch {
	case fileURL && !strings.HasPrefix(p, "/"):
		return "", false
	case !fileURL && !strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "."):
		return "", false
	}
	if !filepath.IsAbs(p) && baseDir != "" {
		p = filepath.Join(baseDir, p)
	}
	return filepath.Clean(p), true
}

// normalizeForMatch strips a Git URL down to a canonical host/path form for matching.
// Examples:
//
//	"git@github.com:user/repo.git"       -> "github.com/user/repo"
//	"https://github.com/user/repo.git"   -> "github.com/user/repo"
//	"github.com/user/repo"               -> "github.com/user/repo"
func normalizeForMatch(gitURL string) string {
	s := gitURL

	// SSH: git@host:path -> host/path
	if strings.HasPrefix(s, "git@") {
		s = strings.TrimPrefix(s, "git@")
		s = strings.Replace(s, ":", "/", 1)
	}

	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimSuffix(s, ".git")

	return s
}

// matchGlob matches a path against a glob pattern.
// Patterns use path segments separated by "/".
//
//   - "*"  matches any single path segment (no "/" crossing)
//   - "**" matches zero or more path segments
//   - everything else is a literal, case-sensitive match
//
// Both pattern and path are split on "/" and matched segment by segment.
func matchGlob(pattern, path string) bool {
	patParts := strings.Split(pattern, "/")
	pathParts := strings.Split(path, "/")
	return matchSegments(patParts, pathParts)
}

func matchSegments(pattern, path []string) bool {
	pi, si := 0, 0

	for pi < len(pattern) && si < len(path) {
		seg := pattern[pi]

		if seg == "**" {
			// ** at end of pattern matches everything remaining
			if pi == len(pattern)-1 {
				return true
			}
			// Try matching ** against zero or more path segments
			for trySkip := si; trySkip <= len(path); trySkip++ {
				if matchSegments(pattern[pi+1:], path[trySkip:]) {
					return true
				}
			}
			return false
		}

		if !matchSegment(seg, path[si]) {
			return false
		}

		pi++
		si++
	}

	// Consume trailing ** patterns (they match zero segments)
	for pi < len(pattern) && pattern[pi] == "**" {
		pi++
	}

	return pi == len(pattern) && si == len(path)
}

// matchSegment matches a single path segment against a pattern segment.
// "*" matches any non-empty segment. Otherwise, literal match.
func matchSegment(pattern, segment string) bool {
	if pattern == "*" {
		return true
	}
	return pattern == segment
}
