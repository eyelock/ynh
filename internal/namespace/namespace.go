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

// CanonicalID returns the path-shaped, host-prefixed canonical id for an
// installed harness given its source URL and name. The canonical id is the
// single identity form across CLI, install records, and ls JSON output.
//
// Examples:
//
//	("https://github.com/eyelock/assistants",   "planner") → "github.com/eyelock/assistants/planner"
//	("git@github.com:eyelock/assistants.git",   "planner") → "github.com/eyelock/assistants/planner"
//	("https://gitlab.com/myorg/tools",          "linter")  → "gitlab.com/myorg/tools/linter"
//	("",                                        "planner") → "local/planner"
//	("/Users/david/harnesses",                  "planner") → "local/planner"
//
// Unlike DeriveFromURL the host segment is preserved — that is the whole
// point of the canonical id, since it lets the source URL be re-derived from
// the id without a separate stored field.
func CanonicalID(sourceURL, name string) string {
	if name == "" {
		return ""
	}
	host, ns := splitHostNamespace(sourceURL)
	if host == "" {
		// Local path or unrecognised URL — no remote source to derive from.
		return "local/" + name
	}
	return host + "/" + ns + "/" + name
}

// splitHostNamespace returns ("github.com", "eyelock/assistants") for a
// recognised remote URL, or ("", "") for a local path / empty / unparseable.
func splitHostNamespace(sourceURL string) (host, ns string) {
	if sourceURL == "" {
		return "", ""
	}
	if strings.HasPrefix(sourceURL, "/") || strings.HasPrefix(sourceURL, ".") || strings.HasPrefix(sourceURL, "~") {
		return "", ""
	}

	cleaned := strings.TrimSuffix(sourceURL, ".git")

	// SSH: git@host:org/repo
	if strings.HasPrefix(cleaned, "git@") {
		rest := strings.TrimPrefix(cleaned, "git@")
		idx := strings.Index(rest, ":")
		if idx <= 0 {
			return "", ""
		}
		host = rest[:idx]
		path := rest[idx+1:]
		parts := splitNonEmpty(path, "/")
		if len(parts) < 2 {
			return "", ""
		}
		return host, parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}

	cleaned = strings.TrimPrefix(cleaned, "https://")
	cleaned = strings.TrimPrefix(cleaned, "http://")

	parts := splitNonEmpty(cleaned, "/")
	// Need at least host + org + repo to be a real remote URL.
	if len(parts) < 3 || !strings.Contains(parts[0], ".") {
		return "", ""
	}
	return parts[0], parts[len(parts)-2] + "/" + parts[len(parts)-1]
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
