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

// RefKind classifies a user-typed harness reference.
type RefKind int

const (
	// RefInvalid is a ref that cannot be resolved as either a path or a
	// canonical id. Today this is bare names ("planner") and the legacy
	// "name@org/repo" form.
	RefInvalid RefKind = iota
	// RefPath is a filesystem path. Lexically: starts with "./", "../",
	// "/", "~/", or a Windows drive letter ("X:\" / "X:/").
	RefPath
	// RefID is a canonical harness id — slash-bearing, not a path.
	// "<host>/<org>/<repo>/<name>" or "local/<name>" or any user-assigned
	// shape with at least one slash and no leading path marker.
	RefID
)

// Classify decides what shape a user-typed ref takes. The rule is purely
// lexical — no stat call, no heuristic, no fallback. This is the resolver's
// only entry point for ref interpretation; everything else flows from the
// returned kind.
//
//	"./planner"                          → RefPath
//	"../shared/reviewer"                 → RefPath
//	"/abs/planner"                       → RefPath
//	"~/work/planner"                     → RefPath
//	"C:\\Users\\david\\planner"          → RefPath  (Windows)
//	"github.com/eyelock/assistants/x"    → RefID
//	"local/planner"                      → RefID
//	"planner"                            → RefInvalid (bare name)
//	"planner@eyelock/assistants"         → RefInvalid (legacy qualified form)
//	""                                   → RefInvalid
func Classify(ref string) RefKind {
	if ref == "" {
		return RefInvalid
	}
	if isPathRef(ref) {
		return RefPath
	}
	// "@" is reserved for future version pins (e.g.
	// "github.com/eyelock/assistants/planner@v2.1.0"). Today no command
	// accepts it, and in particular the legacy "name@org/repo" form must
	// not slip through the canonical-id branch on its trailing slash.
	if strings.Contains(ref, "@") {
		return RefInvalid
	}
	if strings.Contains(ref, "/") {
		return RefID
	}
	return RefInvalid
}

func isPathRef(ref string) bool {
	if strings.HasPrefix(ref, "./") || strings.HasPrefix(ref, "../") ||
		strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "~/") {
		return true
	}
	// Bare "." and ".." also resolve as filesystem paths.
	if ref == "." || ref == ".." {
		return true
	}
	// Windows drive letter: "X:" followed by "/" or "\".
	if len(ref) >= 3 && ref[1] == ':' && (ref[2] == '/' || ref[2] == '\\') {
		c := ref[0]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
			return true
		}
	}
	return false
}

// SplitID splits a canonical id "<namespace>/<name>" into its namespace and
// name parts. The namespace is everything before the last slash; the name is
// the last segment. Returns ("", "") for refs that don't contain a slash —
// callers should Classify first.
//
//	"github.com/eyelock/assistants/planner" → ("github.com/eyelock/assistants", "planner")
//	"local/planner"                         → ("local", "planner")
//	"planner"                               → ("", "")
func SplitID(id string) (ns, name string) {
	idx := strings.LastIndex(id, "/")
	if idx < 0 {
		return "", ""
	}
	return id[:idx], id[idx+1:]
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

// IDToFSName transliterates a canonical id to a filesystem-safe name by
// replacing every "/" with "--". One direction only — the canonical id is
// the user-facing form; the fs name is how it's stored on disk.
//
//	"github.com/eyelock/assistants/planner" → "github.com--eyelock--assistants--planner"
//	"local/planner"                         → "local--planner"
//
// Users never type the "--" form on the CLI; ynh never accepts it. See
// the IsLegacyFSNameCollision note in canonicalid.go for why "--" can't
// appear in a canonical id segment.
func IDToFSName(id string) string {
	return strings.ReplaceAll(id, "/", "--")
}

// FSNameToID reverses IDToFSName.
func FSNameToID(fsName string) string {
	return strings.ReplaceAll(fsName, "--", "/")
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
