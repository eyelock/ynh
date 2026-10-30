package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// yndIgnoreFile names the file, at the root of a walked tree, that lists paths
// ynd must not walk into. The syntax is gitignore's.
//
// It exists for tracked paths that should stay out of lint, validate and fmt:
// eval fixtures that are deliberately broken to test a skill that diagnoses
// broken skills. `.gitignore` cannot say that, because the files must stay in
// git. Before this, the only escape was to put them under a directory called
// `vendor/`, which rides the built-in skip by accident.
const yndIgnoreFile = ".yndignore"

// applyIgnores narrows a walk's results by git's ignore rules and then by the
// root's .yndignore, so lint, validate and fmt agree on what they walk.
func applyIgnores(root string, files []string) ([]string, error) {
	return filterYndIgnored(root, filterIgnored(files))
}

// filterYndIgnored removes the paths root's .yndignore lists. With no
// .yndignore there is nothing to do and nothing is run.
//
// Matching is asked of git, as filterIgnored does, and for the same reason:
// negation, directory-only patterns, anchoring and `**` are easy to get subtly
// wrong, and a wrong answer means linting files the author excluded or hiding
// files they did not. It runs
//
//	git --git-dir=<scratch> --work-tree=<root> check-ignore --no-index --stdin -z
//
// with the patterns installed as the scratch repository's info/exclude. The
// scratch repository is an empty one rather than the tree's own. That makes
// the answer the same in a repository, outside one, and in a worktree: the
// patterns are anchored at the walked root, the tree's .gitignore does not
// leak in (it is applied separately by filterIgnored), and neither does the
// user's global excludesfile.
//
// Unlike filterIgnored this fails closed. A missing .yndignore changes
// nothing, but one that exists and cannot be applied is an error: failing
// open would lint the very fixtures it was written to exclude, and report
// them as defects in a project that has none.
func filterYndIgnored(root string, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return paths, nil
	}
	ignoreFile := filepath.Join(root, yndIgnoreFile)
	if info, err := os.Stat(ignoreFile); err != nil || info.IsDir() {
		return paths, nil
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", yndIgnoreFile, err)
	}
	// Resolved so paths and work tree agree on /private/var vs /var.
	if resolved, err := filepath.EvalSymlinks(absRoot); err == nil {
		absRoot = resolved
	}
	absIgnore := filepath.Join(absRoot, yndIgnoreFile)

	rels := make([]string, 0, len(paths))
	relOf := map[string]string{}
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			abs = resolved
		}
		rel, err := filepath.Rel(absRoot, abs)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue // the root itself, or outside it: not ours to filter
		}
		rel = filepath.ToSlash(rel)
		relOf[p] = rel
		rels = append(rels, rel)
	}
	if len(rels) == 0 {
		return paths, nil
	}

	scratch, err := os.MkdirTemp("", "yndignore-")
	if err != nil {
		return nil, fmt.Errorf("applying %s: %w", yndIgnoreFile, err)
	}
	defer func() { _ = os.RemoveAll(scratch) }()
	if out, err := exec.Command("git", "init", "--quiet", scratch).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("applying %s needs git: %v: %s", yndIgnoreFile, err, strings.TrimSpace(string(out)))
	}

	// check-ignore has no --exclude-from, so the patterns go where git reads
	// them for any repository: info/exclude, which anchors at the work tree.
	patterns, err := os.ReadFile(absIgnore)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", yndIgnoreFile, err)
	}
	gitDir := filepath.Join(scratch, ".git")
	if err := os.MkdirAll(filepath.Join(gitDir, "info"), 0o755); err != nil {
		return nil, fmt.Errorf("applying %s: %w", yndIgnoreFile, err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "info", "exclude"), patterns, 0o644); err != nil {
		return nil, fmt.Errorf("applying %s: %w", yndIgnoreFile, err)
	}

	cmd := exec.Command("git",
		"--git-dir="+gitDir, "--work-tree="+absRoot,
		"-c", "core.excludesFile=/dev/null",
		"check-ignore", "--no-index", "--stdin", "-z")
	cmd.Dir = absRoot
	cmd.Stdin = strings.NewReader(strings.Join(rels, "\x00") + "\x00")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// Exit 1 means nothing matched, which is the common case.
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 1 {
			return nil, fmt.Errorf("applying %s: %v: %s", yndIgnoreFile, err, strings.TrimSpace(stderr.String()))
		}
	}
	ignored := map[string]bool{}
	for _, r := range strings.Split(stdout.String(), "\x00") {
		if r != "" {
			ignored[r] = true
		}
	}

	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if rel, ok := relOf[p]; ok && ignored[rel] {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}
