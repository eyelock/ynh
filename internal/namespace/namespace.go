package namespace

import (
	"fmt"
	"strings"
)

// ParseQualified splits "name@org/repo" into name and namespace.
// If no "@" is present, name is the full ref and namespace is empty.
func ParseQualified(ref string) (name, ns string, err error) {
	idx := strings.LastIndex(ref, "@")
	if idx < 0 {
		return ref, "", nil
	}
	name = ref[:idx]
	ns = ref[idx+1:]
	if name == "" {
		return "", "", fmt.Errorf("qualified ref %q: name must not be empty", ref)
	}
	if ns == "" {
		return "", "", fmt.Errorf("qualified ref %q: namespace must not be empty", ref)
	}
	return name, ns, nil
}

// DeriveFromURL derives "org/repo" from a Git URL or local path.
// Uses the same URL parsing as internal/resolver.repoDirName but without
// the hash suffix — namespaces must be ref-agnostic and stable across machines.
//
// Examples:
//
//	https://github.com/eyelock/assistants     → eyelock/assistants
//	git@github.com:eyelock/assistants.git     → eyelock/assistants
//	github.com/eyelock/assistants             → eyelock/assistants
//	/Users/david/harnesses                    → david/harnesses
func DeriveFromURL(url string) string {
	if strings.HasPrefix(url, "/") || strings.HasPrefix(url, ".") {
		return deriveFromLocalPath(url)
	}

	cleaned := strings.TrimSuffix(url, ".git")

	// SSH: git@host:org/repo
	if strings.HasPrefix(cleaned, "git@") {
		if idx := strings.Index(cleaned, ":"); idx > 0 {
			cleaned = cleaned[idx+1:]
		}
	}

	cleaned = strings.TrimPrefix(cleaned, "https://")
	cleaned = strings.TrimPrefix(cleaned, "http://")

	parts := splitNonEmpty(cleaned, "/")
	switch len(parts) {
	case 0:
		return "local/unknown"
	case 1:
		return "local/" + parts[0]
	default:
		// Drop the host segment if it looks like a hostname (contains a dot)
		if strings.Contains(parts[0], ".") && len(parts) >= 3 {
			parts = parts[1:]
		}
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
}

func deriveFromLocalPath(path string) string {
	cleaned := strings.TrimLeft(path, "./")
	parts := splitNonEmpty(cleaned, "/")
	switch len(parts) {
	case 0:
		return "local/unknown"
	case 1:
		return "local/" + parts[0]
	default:
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
}

// ToFSName converts "org/repo" to "org--repo" for use as a directory name.
func ToFSName(ns string) string {
	return strings.ReplaceAll(ns, "/", "--")
}

// FromFSName converts "org--repo" back to "org/repo".
func FromFSName(fsName string) string {
	return strings.ReplaceAll(fsName, "--", "/")
}

func splitNonEmpty(s, sep string) []string {
	var out []string
	for _, p := range strings.Split(s, sep) {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
