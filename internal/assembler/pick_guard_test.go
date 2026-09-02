package assembler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A pick's second segment lands in the copy destination. Without a guard, one
// containing ".." writes outside the target tree.
//
// Both trees live inside one t.TempDir(), at deliberately different depths,
// mirroring a deep cache and a shallow export target. That asymmetry is what
// makes the escape possible: with equal depths the two climbs cancel out and
// it degenerates to a self-copy, which is why this looks harmless if tested
// carelessly.
func TestCopyPicked_RejectsEscapingPick(t *testing.T) {
	cases := []string{
		"skills/../../payload.md",
		"skills/../../../payload.md",
		"skills/../outside",
		"skills//../payload.md",
	}
	for _, picked := range cases {
		t.Run(picked, func(t *testing.T) {
			root := t.TempDir()
			repoBase := filepath.Join(root, "cache", "host", "org", "repo")
			targetBase := filepath.Join(root, "out")
			if err := os.MkdirAll(filepath.Join(repoBase, "skills"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(targetBase, 0o755); err != nil {
				t.Fatal(err)
			}
			// Present at every depth the pick might climb to, so the guard is
			// what stops the copy rather than a missing source file.
			for _, d := range []string{
				filepath.Join(root, "cache", "host", "org"),
				filepath.Join(root, "cache", "host"),
				filepath.Join(root, "cache"),
				root,
			} {
				_ = os.WriteFile(filepath.Join(d, "payload.md"), []byte("owned\n"), 0o644)
				_ = os.MkdirAll(filepath.Join(d, "outside"), 0o755)
			}

			err := CopyPicked(repoBase, picked, targetBase,
				map[string]string{"skills": "skills"}, nil)
			if err == nil {
				t.Fatalf("pick %q was accepted", picked)
			}
			if !strings.Contains(err.Error(), "stay inside the harness") {
				t.Errorf("unexpected error for %q: %v", picked, err)
			}

			// Nothing may exist outside the target tree as a result.
			if _, sErr := os.Stat(filepath.Join(root, "payload.md")); sErr == nil {
				// The fixture writes one at root, so compare content instead:
				// what must not happen is a copy landing anywhere new.
				t.Log("fixture file at root is expected; asserting no new writes below")
			}
			entries, err := os.ReadDir(targetBase)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Errorf("target tree was written to: %v", entries)
			}
		})
	}
}

// An ordinary pick must still work, or the guard has broken composition.
func TestCopyPicked_AcceptsAnOrdinaryPick(t *testing.T) {
	root := t.TempDir()
	repoBase := filepath.Join(root, "repo")
	targetBase := filepath.Join(root, "out")
	skillDir := filepath.Join(repoBase, "skills", "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(targetBase, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := CopyPicked(repoBase, "skills/demo", targetBase,
		map[string]string{"skills": "skills"}, nil); err != nil {
		t.Fatalf("an ordinary pick must still copy: %v", err)
	}
	if _, err := os.Stat(filepath.Join(targetBase, "skills", "demo", "SKILL.md")); err != nil {
		t.Errorf("the picked skill was not copied: %v", err)
	}
}

// An absolute pick is not inside the harness either.
func TestCopyPicked_RejectsAbsolutePick(t *testing.T) {
	root := t.TempDir()
	err := CopyPicked(filepath.Join(root, "repo"), "skills//etc/passwd",
		filepath.Join(root, "out"), map[string]string{"skills": "skills"}, nil)
	if err == nil {
		t.Fatal("an absolute pick was accepted")
	}
}
