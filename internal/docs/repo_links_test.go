package docs_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Two renderers, two slug rules, and the right one depends on where the file
// is read rather than on what it says.
//
// Everything under docs/ is served by docsify, which replaces each run of
// non-alphanumerics with one hyphen. Everything else — README.md,
// .github/CONTRIBUTING.md, .claude/** — is read on GitHub, which deletes
// punctuation and maps each space to a hyphen, so "A — B" keeps the double
// hyphen the deleted em dash leaves behind.
//
// Applying the wrong rule to a file "fixes" a correct link into a broken one.
// TestDocsAnchorsResolve owns docs/; this owns the rest, and adds the two
// checks neither had: relative file links outside docs/, and the rule that a
// docs/ page may not reach outside the docs root by relative path.

var (
	mdInline = regexp.MustCompile(`\[[^\]]*\]\(\s*<?([^)>\s]*?)>?\s*(?:"[^"]*")?\)`)
	mdRefDef = regexp.MustCompile(`(?m)^\s*\[[^\]]+\]:\s*(\S+)`)
)

// githubSlug reproduces GitHub's heading-to-anchor conversion: lowercase, drop
// everything that is not alphanumeric, space, hyphen or underscore, then map
// spaces to hyphens. Runs are not collapsed and the result is not trimmed,
// which is exactly where it parts company with docsify.
func githubSlug(heading string) string {
	s := strings.ToLower(strings.TrimSpace(htmlTagRE.ReplaceAllString(heading, "")))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	return b.String()
}

// TestGitHubSlugDiffersFromDocsify pins the difference the two renderers have,
// so neither slugifier can be "tidied" into the other.
func TestGitHubSlugDiffersFromDocsify(t *testing.T) {
	cases := []struct{ heading, gh, ds string }{
		{"Design Stance — Declarative-First, Vendor-Neutral",
			"design-stance--declarative-first-vendor-neutral",
			"design-stance-declarative-first-vendor-neutral"},
		{"Versioning & Identifiers", "versioning--identifiers", "versioning-identifiers"},
		{"on_stop Output Semantics (Claude)", "on_stop-output-semantics-claude", "on-stop-output-semantics-claude"},
	}
	for _, c := range cases {
		if got := githubSlug(c.heading); got != c.gh {
			t.Errorf("githubSlug(%q) = %q, want %q", c.heading, got, c.gh)
		}
		if got := slugify(c.heading); got != c.ds {
			t.Errorf("slugify(%q) = %q, want %q", c.heading, got, c.ds)
		}
	}
}

// TestRepoMarkdownLinksResolve checks every relative link in markdown outside
// docs/, using GitHub's resolution (relative to the file) and GitHub's slugs.
//
// Three links in .github/CONTRIBUTING.md pointed at docs/hooks.md and its
// neighbours, which from .github/ resolves to .github/docs/hooks.md. The same
// file used ../docs/ correctly two paragraphs earlier.
func TestRepoMarkdownLinksResolve(t *testing.T) {
	root := repoRoot(t)
	if root == "" {
		t.Skip("repo root not reachable from package CWD")
	}

	files := repoMarkdown(t, root)
	if len(files) == 0 {
		t.Fatal("no markdown found outside docs/ — the checker is looking in the wrong place")
	}

	// Index every markdown file in the repo, docs/ included, using GitHub's
	// rule — because a link *from* a GitHub-rendered file is followed on
	// GitHub even when it points into docs/. docs/-to-docs/ links are the
	// other case, and TestDocsAnchorsResolve owns them with docsify's rule.
	anchors := map[string]map[string]bool{}
	indexed := append(append([]string{}, files...), docsMarkdown(t, root)...)
	for _, rel := range indexed {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		anchors[rel] = headingsWith(string(data), githubSlug)
	}

	var checked int
	for _, rel := range files {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		body := string(data)
		targets := append(mdInline.FindAllStringSubmatch(body, -1),
			mdRefDef.FindAllStringSubmatch(body, -1)...)
		for _, m := range targets {
			target := m[1]
			if target == "" || strings.HasPrefix(target, "http://") ||
				strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			path, frag, _ := strings.Cut(target, "#")
			resolved := rel
			if path != "" {
				resolved = filepath.Clean(filepath.Join(filepath.Dir(rel), path))
				if _, err := os.Stat(filepath.Join(root, resolved)); err != nil {
					t.Errorf("broken link: %s -> %s\n"+
						"Outside docs/, links resolve relative to the file, as GitHub renders them. "+
						"From .github/, a docs page is ../docs/<name>.md.", rel, target)
					continue
				}
			}
			if frag == "" {
				continue
			}
			known, ok := anchors[resolved]
			if !ok {
				continue // not a markdown file we indexed
			}
			checked++
			if !known[frag] {
				t.Errorf("dead anchor: %s -> %s\n"+
					"This file is read on GitHub, which deletes punctuation and maps each space to a "+
					"hyphen — so \"A — B\" is \"a--b\", keeping the double hyphen. Do not apply "+
					"docsify's rule here.", rel, target)
			}
		}
	}
	t.Logf("checked %d anchor links across %d files outside docs/", checked, len(files))
}

// TestDocsDoNotEscapeTheDocsRoot forbids a relative link from docs/ to a file
// outside it.
//
// docs/marketplace.md linked ../.github/CONTRIBUTING.md#versioning--identifiers,
// which is right when the page is read on GitHub and wrong when it is served by
// docsify, because the two disagree about the fragment. There is no single
// correct relative form, so the link has to be absolute — which is what
// _sidebar.md already does for the same file.
func TestDocsDoNotEscapeTheDocsRoot(t *testing.T) {
	docs := docsRoot(t)
	if docs == "" {
		t.Skip("docs/ not reachable from package CWD")
	}
	err := filepath.Walk(docs, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".md") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, _ := filepath.Rel(docs, path)
		for _, m := range mdInline.FindAllStringSubmatch(string(data), -1) {
			target := m[1]
			if strings.HasPrefix(target, "http") || !strings.HasPrefix(target, "../") {
				continue
			}
			t.Errorf("%s links outside the docs root: %s\n"+
				"docsify and GitHub slugify headings differently, so no relative form is correct "+
				"in both. Use the absolute https://github.com/... URL, as docs/_sidebar.md does.",
				rel, target)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk docs: %v", err)
	}
}

// headingsWith indexes a file's anchors using the supplied slugifier, skipping
// fenced code so a shell comment is not mistaken for a heading.
func headingsWith(data string, slug func(string) string) map[string]bool {
	out := map[string]bool{}
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
		s := slug(line[level+1:])
		if s == "" {
			continue
		}
		if n := seen[s]; n > 0 {
			out[s+"-"+string(rune('0'+n))] = true
		} else {
			out[s] = true
		}
		seen[s]++
	}
	return out
}

// repoRoot returns the module root, which is the parent of docs/.
func repoRoot(t *testing.T) string {
	t.Helper()
	d := docsRoot(t)
	if d == "" {
		return ""
	}
	return filepath.Dir(d)
}

// repoMarkdown lists tracked-looking markdown outside docs/, skipping the
// directories that are not ours to police.
func repoMarkdown(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "docs", "node_modules", "bin", "vendor", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".md") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		out = append(out, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}
	return out
}

// docsMarkdown lists markdown under docs/, relative to the repo root. Links
// from outside docs/ land on these files as GitHub renders them.
func docsMarkdown(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(filepath.Join(root, "docs"), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == "schema" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(info.Name(), ".md") {
			rel, _ := filepath.Rel(root, path)
			out = append(out, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk docs: %v", err)
	}
	return out
}
