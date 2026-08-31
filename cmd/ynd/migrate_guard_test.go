package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// refuseToMigrate is a pure predicate. These tests pass it dangerous paths
// deliberately, which is only safe because it decides and never deletes, per
// .claude/rules/destructive-operations.md. Nothing here calls cmdMigrate.
func TestRefuseToMigrate_HardRefusals(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	cases := []struct {
		name, path string
		explicit   bool
		wantSubstr string
	}{
		{"filesystem root", string(filepath.Separator), true, "filesystem root"},
		{"home directory", home, true, "home directory"},
		{"home directory even with an explicit target", home, true, "home directory"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := refuseToMigrate(tc.path, tc.explicit)
			if got == "" {
				t.Fatalf("refuseToMigrate(%q) allowed it", tc.path)
			}
			if !strings.Contains(got, tc.wantSubstr) {
				t.Errorf("reason %q does not mention %q", got, tc.wantSubstr)
			}
		})
	}
}

// A git working copy is refused only when the target was defaulted. Pointing
// this at a repository on purpose is legitimate; defaulting into one because
// of the directory you happen to be in is how this repo's own fixtures were
// deleted (#349).
func TestRefuseToMigrate_GitWorkingCopyOnlyWhenDefaulted(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	if got := refuseToMigrate(dir, false); got == "" {
		t.Error("a defaulted target inside a git working copy must be refused")
	} else if !strings.Contains(got, "git working copy") {
		t.Errorf("reason %q does not mention the git working copy", got)
	}

	if got := refuseToMigrate(dir, true); got != "" {
		t.Errorf("an explicit target must be allowed, got refusal: %q", got)
	}
}

// An ordinary directory is allowed either way, or the command is unusable.
func TestRefuseToMigrate_AllowsAnOrdinaryDirectory(t *testing.T) {
	dir := t.TempDir()
	for _, explicit := range []bool{true, false} {
		if got := refuseToMigrate(dir, explicit); got != "" {
			t.Errorf("explicit=%v: refused an ordinary directory: %q", explicit, got)
		}
	}
}

// The walk must not descend into directories whose contents are not ours.
func TestSkipDuringMigrate(t *testing.T) {
	for _, name := range []string{".git", "node_modules", "vendor"} {
		if !skipDuringMigrate[name] {
			t.Errorf("%s must be skipped; a registry.json inside it belongs to someone else", name)
		}
	}
	for _, name := range []string{"skills", "agents", "harnesses", "src"} {
		if skipDuringMigrate[name] {
			t.Errorf("%s must not be skipped", name)
		}
	}
}
