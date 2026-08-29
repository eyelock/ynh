package docs_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Tutorials are identified by their slug, never by a number.
//
// Numbers used to do two incompatible jobs at once: identity (a stable handle
// for a page and its steps, e.g. "T21.4") and reading order (where the page
// sits in the sidebar). Order changes whenever a tutorial is inserted or
// regrouped; identity must not. Encoding both in one integer meant every
// reorder renamed files and silently invalidated every reference that had
// captured the old value — which is how forty-eight stale "Tutorial N" labels
// and thirty dead anchors reached the published site unnoticed.
//
// The rule now: identity is the slug, order lives only in docs/_sidebar.md,
// and deep links use heading anchors. These tests hold that line.

var (
	numberedFile   = regexp.MustCompile(`^\d+[-_]`)
	tutorialNumber = regexp.MustCompile(`Tutorial \d+|\bT\d+\.\d+`)
	sidebarEntry   = regexp.MustCompile(`^\s+\* \[\d+\. (.+?)\]\(tutorial/(.+?)\.md\)`)
	readmeEntry    = regexp.MustCompile(`^\| \[(.+?)\]\(tutorial/(.+?)\.md\) \|`)
	nextEntry      = regexp.MustCompile(`(?m)^## Next\n\n\[([^\]]+)\]\(tutorial/([a-z0-9-]+)\.md\)`)
	firstHeading   = regexp.MustCompile(`(?m)^# (.+?)\s*$`)
)

// tutorialSlugs lists the tutorial pages, excluding the two index documents.
func tutorialSlugs(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "tutorial"))
	if err != nil {
		t.Fatalf("read tutorial dir: %v", err)
	}
	var slugs []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".md") {
			continue
		}
		if name == "README.md" || name == "manual-test-plan.md" {
			continue
		}
		slugs = append(slugs, strings.TrimSuffix(name, ".md"))
	}
	return slugs
}

// TestTutorialFilenamesAreSlugs fails on a numeric filename prefix. A number in
// the filename is order leaking back into identity, and it renames on reorder.
func TestTutorialFilenamesAreSlugs(t *testing.T) {
	root := docsRoot(t)
	if root == "" {
		t.Skip("docs/ not reachable from package CWD")
	}
	slugs := tutorialSlugs(t, root)
	if len(slugs) == 0 {
		t.Fatal("no tutorials found — the checker is looking in the wrong place")
	}
	for _, s := range slugs {
		if numberedFile.MatchString(s) {
			t.Errorf("tutorial %q is numbered: identity is the slug, order lives in docs/_sidebar.md", s)
		}
	}
}

// TestNoTutorialNumbersInProse fails on "Tutorial 12" or "T12.3" anywhere in
// docs/. Every such reference was a number captured at write time that nothing
// updated on reorder. Link to the page or its heading anchor instead.
func TestNoTutorialNumbersInProse(t *testing.T) {
	root := docsRoot(t)
	if root == "" {
		t.Skip("docs/ not reachable from package CWD")
	}
	var scanned int
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
		for _, line := range strings.Split(string(data), "\n") {
			if m := tutorialNumber.FindString(line); m != "" {
				t.Errorf("%s: %q — tutorials are referenced by slug, not number.\n"+
					"Link the page (tutorial/agent-loop.md) or a heading anchor "+
					"(tutorial/agent-loop.md#budgets) instead.", rel, m)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk docs: %v", err)
	}
	if scanned == 0 {
		t.Fatal("no markdown files scanned — the checker is looking in the wrong place")
	}
}

// TestTutorialIndexesAgree checks that _sidebar.md and tutorial/README.md list
// exactly the tutorials that exist, in the same order. The sidebar is the one
// place order lives; the README mirrors it. Without this a new tutorial can be
// added and appear in neither — which is how include-editing stayed orphaned.
func TestTutorialIndexesAgree(t *testing.T) {
	root := docsRoot(t)
	if root == "" {
		t.Skip("docs/ not reachable from package CWD")
	}

	sidebar := collect(t, filepath.Join(root, "_sidebar.md"), sidebarEntry)
	readme := collect(t, filepath.Join(root, "tutorial", "README.md"), readmeEntry)

	onDisk := map[string]bool{}
	for _, s := range tutorialSlugs(t, root) {
		onDisk[s] = true
	}

	for _, s := range sidebar {
		if !onDisk[s] {
			t.Errorf("_sidebar.md lists tutorial/%s.md, which does not exist", s)
		}
		delete(onDisk, s)
	}
	for s := range onDisk {
		t.Errorf("tutorial/%s.md exists but is listed in no sidebar section — "+
			"add it to docs/_sidebar.md, which is where reading order lives", s)
	}

	if len(sidebar) != len(readme) {
		t.Fatalf("_sidebar.md lists %d tutorials, tutorial/README.md lists %d — they must agree",
			len(sidebar), len(readme))
	}
	for i := range sidebar {
		if sidebar[i] != readme[i] {
			t.Errorf("order differs at position %d: _sidebar.md has %q, tutorial/README.md has %q",
				i+1, sidebar[i], readme[i])
		}
	}
}

// collect returns the tutorial slugs matched by re, in file order.
func collect(t *testing.T, path string, re *regexp.Regexp) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		if m := re.FindStringSubmatch(line); m != nil {
			out = append(out, m[2])
		}
	}
	if len(out) == 0 {
		t.Fatalf("%s: matched no tutorial entries — the pattern and the file have diverged", path)
	}
	return out
}

// TestNextLinksFollowSidebar checks that each tutorial's "## Next" link points
// at its successor in docs/_sidebar.md, and that the last tutorial has none.
//
// The Next chain was a fourth competing ordering. It followed the original file
// numbers, so it skipped include-editing entirely and dead-ended at sensors,
// leaving the gate, the loop and shadow mode reachable only from the sidebar. A
// reader working through the series in order simply stopped early.
//
// Reading order has one home now. This test is what keeps it that way: move a
// tutorial in the sidebar without repointing the Next links and CI says so.
func TestNextLinksFollowSidebar(t *testing.T) {
	root := docsRoot(t)
	if root == "" {
		t.Skip("docs/ not reachable from package CWD")
	}

	order := collect(t, filepath.Join(root, "_sidebar.md"), sidebarEntry)
	for i, slug := range order {
		path := filepath.Join(root, "tutorial", slug+".md")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		m := nextEntry.FindStringSubmatch(string(data))

		if i == len(order)-1 {
			if m != nil {
				t.Errorf("tutorial/%s.md is last in _sidebar.md but links onward to %s", slug, m[2])
			}
			continue
		}
		want := order[i+1]
		if m == nil {
			t.Errorf("tutorial/%s.md has no \"## Next\" link; _sidebar.md puts %s after it", slug, want)
			continue
		}
		if m[2] != want {
			t.Errorf("tutorial/%s.md links on to %s, but _sidebar.md puts %s after it", slug, m[2], want)
			continue
		}
		// The label must be the target's own title. A hand-written label is
		// how "Tutorial 4: Hooks" survived on a page pointing at file 10.
		if title := tutorialTitle(t, root, want); m[1] != title {
			t.Errorf("tutorial/%s.md labels its Next link %q, but tutorial/%s.md is titled %q",
				slug, m[1], want, title)
		}
	}
}

// tutorialTitle returns a tutorial's H1 text.
func tutorialTitle(t *testing.T, root, slug string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "tutorial", slug+".md"))
	if err != nil {
		t.Fatalf("read tutorial/%s.md: %v", slug, err)
	}
	m := firstHeading.FindSubmatch(data)
	if m == nil {
		t.Fatalf("tutorial/%s.md has no H1", slug)
	}
	return string(m[1])
}
