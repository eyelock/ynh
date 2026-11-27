package main

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// referenceRows returns the first word after "ynd " of every row in the
// "## ynd Commands" table of docs/reference.md, so a test can compare the
// reference with the dispatch switch.
func referenceRows(t *testing.T) map[string]bool {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed; cannot locate docs/reference.md")
	}
	path := filepath.Join(filepath.Dir(thisFile), "..", "..", "docs", "reference.md")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading docs/reference.md: %v", err)
	}
	doc := string(src)
	start := strings.Index(doc, "\n## ynd Commands")
	if start < 0 {
		t.Fatal("docs/reference.md has no \"## ynd Commands\" section")
	}
	section := doc[start+1:]
	if next := strings.Index(section[3:], "\n## "); next >= 0 {
		section = section[:next+3]
	}
	row := regexp.MustCompile("(?m)^\\| `ynd ([a-z][a-z-]*)")
	rows := map[string]bool{}
	for _, m := range row.FindAllStringSubmatch(section, -1) {
		rows[m[1]] = true
	}
	if len(rows) == 0 {
		t.Fatal("found no `ynd <command>` rows in docs/reference.md; the parser is wrong, not the docs")
	}
	return rows
}

// Every command in the dispatch switch must have a row in docs/reference.md.
// This is the check that stops the reference drifting the way it did when
// several commands were added without a row (#917).
func TestEveryDispatchedCommandHasAReferenceRow(t *testing.T) {
	rows := referenceRows(t)
	for _, name := range dispatchedCommands(t) {
		if !rows[name] {
			t.Errorf("command %q is dispatched but has no row in the \"## ynd Commands\" table of docs/reference.md", name)
		}
	}
}

// And no row may describe a command that is not dispatched: a stale row is a
// promise about a command that no longer exists.
func TestEveryReferenceRowIsADispatchedCommand(t *testing.T) {
	dispatched := map[string]bool{}
	for _, name := range dispatchedCommands(t) {
		dispatched[name] = true
	}
	for name := range referenceRows(t) {
		if !dispatched[name] {
			t.Errorf("docs/reference.md has a row for `ynd %s`, which is not in the dispatch switch", name)
		}
	}
}
