package docs_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// anchorLinkRE matches a Markdown link carrying a fragment: the target may be
// empty (a same-page link), so both "](#frag)" and "](path.md#frag)" match.
var anchorLinkRE = regexp.MustCompile(`\]\(([^)#\s]*)#([^)\s]+)\)`)

var htmlTagRE = regexp.MustCompile(`<[^>]+>`)

// slugify reproduces docsify@4's heading-to-anchor conversion: strip HTML,
// lowercase, then collapse every run of non-alphanumeric characters to a
// single hyphen. docs/index.html loads stock docsify with no slugify override,
// so this is the rule the published site actually applies.
//
// It is deliberately not GitHub's rule, which deletes punctuation instead of
// replacing it — "eyelock/assistants" is "eyelock-assistants" here and
// "eyelockassistants" on GitHub. Hand-written anchors in this repo had drifted
// toward the GitHub form and 404'd on the site while looking right in a
// local preview.
func slugify(heading string) string {
	s := strings.ToLower(strings.TrimSpace(htmlTagRE.ReplaceAllString(heading, "")))
	var b strings.Builder
	pendingDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9',
			r >= 0x4e00 && r <= 0x9fa5, r >= 0x0400 && r <= 0x04ff:
			if pendingDash && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingDash = false
			b.WriteRune(r)
		default:
			pendingDash = true
		}
	}
	return b.String()
}

// headingAnchors returns every anchor a file publishes, matching docsify's
// duplicate handling (the second "## Setup" becomes "setup-1").
//
// Lines inside fenced code blocks are skipped: a shell comment such as
// "# Expected: ..." is not a heading, and counting it would shift the
// duplicate suffixes of every real heading after it.
func headingAnchors(data string) map[string]bool {
	anchors := map[string]bool{}
	seen := map[string]int{}
	inFence := false
	for _, line := range strings.Split(data, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence || !strings.HasPrefix(line, "#") {
			continue
		}
		level := len(line) - len(strings.TrimLeft(line, "#"))
		if level < 1 || level > 6 || len(line) <= level || line[level] != ' ' {
			continue
		}
		slug := slugify(line[level+1:])
		if slug == "" {
			continue
		}
		n := seen[slug]
		seen[slug]++
		if n > 0 {
			slug = fmt.Sprintf("%s-%d", slug, n)
		}
		anchors[slug] = true
	}
	return anchors
}

// TestDocsAnchorsResolve checks every link fragment against the headings of the
// file it points at.
//
// TestDocsLinksResolve validates only the path, so a link to a real file with a
// dead fragment passed it. Thirty of those had accumulated in the manual test
// plan, pointing at heading text that had been rewritten underneath them.
func TestDocsAnchorsResolve(t *testing.T) {
	root := docsRoot(t)
	if root == "" {
		t.Skip("docs/ not reachable from package CWD")
	}

	anchors := map[string]map[string]bool{}
	var files []string
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
		rel, _ := filepath.Rel(root, path)
		anchors[rel] = headingAnchors(string(data))
		files = append(files, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("walk docs: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no markdown files found — the checker is looking in the wrong place")
	}

	var checked int
	for _, rel := range files {
		data, readErr := os.ReadFile(filepath.Join(root, rel))
		if readErr != nil {
			t.Fatalf("read %s: %v", rel, readErr)
		}
		for _, m := range anchorLinkRE.FindAllStringSubmatch(string(data), -1) {
			target, frag := m[1], m[2]
			if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
				continue
			}
			if target == "" {
				target = rel // same-page link
			}
			known, ok := anchors[filepath.Clean(target)]
			if !ok {
				continue // path breakage is TestDocsLinksResolve's to report
			}
			checked++
			if !known[frag] {
				t.Errorf("dead anchor: %s -> %s#%s\n"+
					"No heading in %s slugifies to %q. docsify collapses each run of "+
					"non-alphanumeric characters to one hyphen, so \"Baseline — inheriting a repo\" "+
					"is \"baseline-inheriting-a-repo\".", rel, target, frag, target, frag)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no anchor links checked — the matcher and the docs have diverged")
	}
	t.Logf("checked %d anchor links across %d files", checked, len(files))
}
