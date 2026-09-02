package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eyelock/ynh/internal/harness"
)

// lintDeclaredReads checks the manifest's `reads` map: every path an artifact
// declares it opens must resolve inside something the harness actually ships.
//
// The rule exists because a path that resolves in the authoring repo and a
// path that resolves for a user are indistinguishable from the text. An agent
// reading `docs/`, two skills reading `testdata/` and a focus prompt reading
// `README.md` all shipped, all worked for their author, and all broke on
// install (#249, #256, #276). Each of those paths exists in this repository
// and in none of what ynh assembles.
//
// Scanning bodies instead was measured and abandoned: 541 of 566 backticked
// paths in the artifact tree would have been flagged, because artifacts
// legitimately name paths belonging to the user's project, to the ynh source,
// or to nothing at all. Only the author knows which are instructions.
//
// A manifest declaring nothing is silent, not an error. Adoption is gradual
// by design (#284).
func lintDeclaredReads(manifestPath string) []lintIssue {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil // absence and unreadability are already reported elsewhere
	}
	var m struct {
		Reads map[string][]string `json:"reads"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil // invalid JSON is already reported by the schema check
	}
	if len(m.Reads) == 0 {
		return nil
	}

	// The harness root is the directory holding .ynh-plugin/.
	root := filepath.Dir(filepath.Dir(manifestPath))

	var issues []lintIssue
	for _, artifact := range sortedKeys(m.Reads) {
		if msg := artifactMissing(root, artifact); msg != "" {
			issues = append(issues, lintIssue{File: manifestPath,
				Message: fmt.Sprintf("reads declares %q, but %s", artifact, msg)})
			continue
		}
		for _, read := range m.Reads[artifact] {
			if msg := readUnshipped(root, read); msg != "" {
				issues = append(issues, lintIssue{File: manifestPath,
					Message: fmt.Sprintf("%s reads %q, but %s", artifact, read, msg)})
			}
		}
	}
	return issues
}

// artifactMissing reports why a declared artifact key is not a real artifact.
// A stale key is the drift this map could otherwise accumulate: rename a skill
// and its declaration silently stops covering anything.
func artifactMissing(root, artifact string) string {
	if !shipped(artifact) {
		return "that is not an artifact path (expected skills/, agents/, rules/ or commands/)"
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(artifact))); err != nil {
		return "no such artifact in this harness"
	}
	return ""
}

// readUnshipped reports why a declared read is not something the artifact
// ships with. Existing in the repository is not enough, and is precisely the
// trap: every one of the three defects named a path that was present here.
func readUnshipped(root, read string) string {
	if read == "" {
		return "the path is empty"
	}
	if filepath.IsAbs(read) || strings.HasPrefix(read, "~") {
		return "declared reads are relative to the harness root"
	}
	clean := filepath.ToSlash(filepath.Clean(read))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "it escapes the harness root"
	}
	if !shipped(clean) {
		return "that is not inside a shipping artifact directory " +
			"(skills/, agents/, rules/, commands/), so it will not exist once installed"
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(clean))); err != nil {
		return "no such file in this harness"
	}
	return ""
}

// shipped reports whether a harness-root-relative path lies under an artifact
// root. internal/harness owns the list; deriving it here rather than repeating
// it keeps this in step when a new artifact type is added.
func shipped(p string) bool {
	clean := filepath.ToSlash(filepath.Clean(p))
	for _, dir := range append(append([]string{}, harness.ArtifactTypeDirs...), harness.ArtifactTypeFiles...) {
		if strings.HasPrefix(clean, dir+"/") {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
