package config

import (
	"fmt"
	"strings"
)

// CheckRemoteSource validates a remote Git URL against the configured allow-list.
// If AllowedRemoteSources is nil (not configured), all remote sources are allowed.
// If AllowedRemoteSources is an empty slice, all remote sources are denied.
// Otherwise, the URL must match at least one pattern in the allow-list.
func (c *Config) CheckRemoteSource(gitURL string) error {
	if c.AllowedRemoteSources == nil {
		return nil
	}

	normalized := normalizeForMatch(gitURL)

	for _, pattern := range c.AllowedRemoteSources {
		if matchGlob(pattern, normalized) {
			return nil
		}
	}

	return fmt.Errorf("remote source %q is not in the allowed sources list\n  configure allowed_remote_sources in %s", gitURL, ConfigPath())
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
