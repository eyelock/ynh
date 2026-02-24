package main

import (
	"path/filepath"
	"testing"
)

func TestScanSignals_GoProject(t *testing.T) {
	dir := t.TempDir()

	// Simulate a Go project
	writeFile(t, filepath.Join(dir, "go.mod"), []byte("module example.com/test\n\ngo 1.25\n"))
	writeFile(t, filepath.Join(dir, "Makefile"), []byte("build:\n\tgo build ./...\n"))
	writeFile(t, filepath.Join(dir, "README.md"), []byte("# Test Project\n"))
	writeFile(t, filepath.Join(dir, ".gitignore"), []byte("bin/\n"))

	signals := scanSignals(dir)
	if len(signals) == 0 {
		t.Fatal("expected signals, got none")
	}

	// Should find go.mod, Makefile, README.md, .gitignore
	found := make(map[string]bool)
	for _, s := range signals {
		found[filepath.Base(s.Path)] = true
	}

	for _, want := range []string{"go.mod", "Makefile", "README.md", ".gitignore"} {
		if !found[want] {
			t.Errorf("expected to find %s in signals", want)
		}
	}
}

func TestScanSignals_NodeProject(t *testing.T) {
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "package.json"), []byte(`{"name":"test"}`))
	writeFile(t, filepath.Join(dir, "tsconfig.json"), []byte(`{}`))
	writeFile(t, filepath.Join(dir, "jest.config.js"), []byte("module.exports = {};\n"))

	signals := scanSignals(dir)

	found := make(map[string]bool)
	for _, s := range signals {
		found[filepath.Base(s.Path)] = true
	}

	for _, want := range []string{"package.json", "tsconfig.json", "jest.config.js"} {
		if !found[want] {
			t.Errorf("expected to find %s in signals", want)
		}
	}
}

func TestScanSignals_Empty(t *testing.T) {
	dir := t.TempDir()
	signals := scanSignals(dir)
	if len(signals) != 0 {
		t.Errorf("expected no signals, got %d", len(signals))
	}
}

func TestScanSignals_SortedByPriority(t *testing.T) {
	dir := t.TempDir()

	// Priority 5
	writeFile(t, filepath.Join(dir, ".gitignore"), []byte("bin/\n"))
	// Priority 1
	writeFile(t, filepath.Join(dir, "go.mod"), []byte("module test\n"))
	// Priority 3
	writeFile(t, filepath.Join(dir, ".golangci.yml"), []byte("run:\n"))

	signals := scanSignals(dir)
	if len(signals) < 3 {
		t.Fatalf("expected at least 3 signals, got %d", len(signals))
	}

	// First signal should have lowest priority number
	if signals[0].Priority > signals[1].Priority {
		t.Errorf("signals not sorted by priority: %d > %d", signals[0].Priority, signals[1].Priority)
	}
}

func TestScanSignals_GlobPatterns(t *testing.T) {
	dir := t.TempDir()

	mkdirAll(t, filepath.Join(dir, ".github", "workflows"))
	writeFile(t, filepath.Join(dir, ".github", "workflows", "ci.yml"), []byte("name: CI\n"))

	signals := scanSignals(dir)

	found := false
	for _, s := range signals {
		if filepath.Base(s.Path) == "ci.yml" {
			found = true
			if s.Category != catCI {
				t.Errorf("expected CI/CD category, got %s", s.Category)
			}
		}
	}
	if !found {
		t.Error("expected to find ci.yml via glob pattern")
	}
}

func TestSignalsByCategory(t *testing.T) {
	signals := []signal{
		{catBuild, "go.mod", 1},
		{catBuild, "Makefile", 1},
		{catCI, "ci.yml", 2},
		{catDocs, "README.md", 1},
	}

	grouped := signalsByCategory(signals)

	if len(grouped[catBuild]) != 2 {
		t.Errorf("expected 2 build signals, got %d", len(grouped[catBuild]))
	}
	if len(grouped[catCI]) != 1 {
		t.Errorf("expected 1 CI signal, got %d", len(grouped[catCI]))
	}
	if len(grouped[catDocs]) != 1 {
		t.Errorf("expected 1 docs signal, got %d", len(grouped[catDocs]))
	}
}

func TestTopSignalFiles(t *testing.T) {
	signals := []signal{
		{catBuild, "a", 1},
		{catBuild, "b", 2},
		{catCI, "c", 3},
		{catDocs, "d", 4},
		{catTest, "e", 5},
	}

	top := topSignalFiles(signals, 3)
	if len(top) != 3 {
		t.Errorf("expected 3 signals, got %d", len(top))
	}

	// Under limit returns all
	all := topSignalFiles(signals, 10)
	if len(all) != 5 {
		t.Errorf("expected all 5 signals, got %d", len(all))
	}
}

func TestScanSignals_NoDuplicates(t *testing.T) {
	dir := t.TempDir()

	// go.mod is in rootSignals
	writeFile(t, filepath.Join(dir, "go.mod"), []byte("module test\n"))

	signals := scanSignals(dir)

	count := 0
	for _, s := range signals {
		if filepath.Base(s.Path) == "go.mod" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 go.mod signal, got %d (duplicates)", count)
	}
}
