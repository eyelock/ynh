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

// TestDocsLinksResolve checks every relative link in docs/ against the
// directory of the file containing it, which is how both readers resolve them.
//
// docs/index.html sets `relativePath: true`, so docsify resolves a relative
// link from the containing file exactly as GitHub does when the same Markdown
// is browsed in the repository. Before that, docsify resolved from the docs
// root instead, and the two readers disagreed: "tutorial/sensors.md" worked on
// the site and 404d on GitHub, which is how 287 broken links accumulated
// unnoticed across 23 files.
//
// docs/_sidebar.md is the exception. It renders on every page, so a relative
// link there would resolve against whichever page is open. Its links are
// root-absolute ("/sensors.md"), which docsify resolves from the docs root
// regardless of relativePath.
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
			// A relative link to anything but a Markdown page 404s on
			// the site whatever the file system says: docsify turns it
			// into a route and looks for "<target>.md". Three schema
			// links did exactly that while resolving fine on GitHub,
			// so existence on disk is not the question to ask here.
			if !strings.HasSuffix(target, ".md") {
				broken = append(broken, rel+" -> "+target+
					" (relative link to a non-Markdown file; docsify routes it and 404s"+
					" — use the absolute https://github.com/... URL)")
				continue
			}

			// The sidebar renders on every page, so its links are
			// root-absolute and resolve from the docs root.
			base := filepath.Dir(path)
			if info.Name() == "_sidebar.md" {
				if !strings.HasPrefix(target, "/") {
					broken = append(broken, rel+" -> "+target+" (sidebar links must be root-absolute)")
					continue
				}
				base = root
			}
			if _, statErr := os.Stat(filepath.Join(base, strings.TrimPrefix(target, "/"))); statErr != nil {
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
			"docs/index.html sets relativePath: true, so links resolve from the file "+
			"that contains them, matching GitHub. Write it as a sibling or with ../ — "+
			"e.g. check.md or ../sensors.md from docs/tutorial/, not tutorial/check.md. "+
			"docs/_sidebar.md is the exception: its links are root-absolute, e.g. /sensors.md.", b)
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
