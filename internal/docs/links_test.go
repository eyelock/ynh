package docs_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// linkRE matches Markdown links that are not absolute URLs or bare anchors.
var linkRE = regexp.MustCompile(`\]\((?:\./)?([^)#\s]+)`)

// TestDocsLinksResolve checks every relative link in docs/ against the docs
// root, which is how docsify resolves them.
//
// docs/index.html does not set `relativePath`, so docsify resolves relative
// links from the docs root regardless of which file they appear in. A link
// written as a sibling path ("19-sensors.md") or with a parent prefix
// ("../hooks.md") therefore 404s on the published site while looking correct
// in a local editor and in GitHub's Markdown preview — which is why fifteen of
// them accumulated unnoticed.
func TestDocsLinksResolve(t *testing.T) {
	root := docsRoot(t)
	if root == "" {
		t.Skip("docs/ not reachable from package CWD")
	}

	// A checker that finds no files passes vacuously, which is how the first
	// version of this test silently checked nothing.
	var scanned int
	var broken []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == "schema" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".md") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		scanned++
		rel, _ := filepath.Rel(root, path)
		for _, m := range linkRE.FindAllStringSubmatch(string(data), -1) {
			target := m[1]
			if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") ||
				strings.HasPrefix(target, "mailto:") {
				continue
			}
			if _, statErr := os.Stat(filepath.Join(root, target)); statErr != nil {
				broken = append(broken, rel+" -> "+target)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk docs: %v", err)
	}

	if scanned == 0 {
		t.Fatalf("no markdown files found under %s — the checker is looking in the wrong place", root)
	}
	for _, b := range broken {
		t.Errorf("broken link: %s\n"+
			"docsify resolves relative links from the docs root, not from the file. "+
			"Write it as it would be reached from docs/ — e.g. tutorial/sensors.md, not 19-sensors.md or ../sensors.md.", b)
	}
}

// docsRoot resolves the repository's docs directory by anchoring on go.mod.
//
// Searching upward for any directory named "docs" finds this package's own
// directory first — internal/docs — and the test then walks an empty tree and
// passes having checked nothing. Anchoring on the module root is what makes it
// find the right one.
func docsRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	for dir := wd; dir != "/"; dir = filepath.Dir(dir) {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr != nil {
			continue
		}
		candidate := filepath.Join(dir, "docs")
		if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
			return candidate
		}
	}
	return ""
}
