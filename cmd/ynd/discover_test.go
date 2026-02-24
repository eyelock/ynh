package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverFiles(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "readme.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "script.sh"), []byte("#!/bin/bash"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "code.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}

	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "nested.md"), []byte("nested"), 0o644); err != nil {
		t.Fatal(err)
	}

	// .git should be skipped
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD.md"), []byte("ref"), 0o644); err != nil {
		t.Fatal(err)
	}

	files, err := discoverFiles(dir, []string{".md"})
	if err != nil {
		t.Fatalf("discoverFiles failed: %v", err)
	}

	if len(files) != 2 {
		t.Errorf("found %d files, want 2 (readme.md, nested.md)", len(files))
		for _, f := range files {
			t.Logf("  %s", f)
		}
	}
}

func TestDiscoverFiles_SingleFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.go")
	if err := os.WriteFile(path, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	files, err := discoverFiles(path, []string{".md"})
	if err != nil {
		t.Fatalf("discoverFiles failed: %v", err)
	}

	// Single file always returned regardless of extension filter
	if len(files) != 1 {
		t.Errorf("found %d files, want 1", len(files))
	}
}

func TestDiscoverFiles_SkipsNodeModules(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "readme.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	nmDir := filepath.Join(dir, "node_modules", "pkg")
	if err := os.MkdirAll(nmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nmDir, "index.md"), []byte("skip"), 0o644); err != nil {
		t.Fatal(err)
	}

	files, err := discoverFiles(dir, []string{".md"})
	if err != nil {
		t.Fatalf("discoverFiles failed: %v", err)
	}

	if len(files) != 1 {
		t.Errorf("found %d files, want 1", len(files))
	}
}

func TestDiscoverByName(t *testing.T) {
	dir := t.TempDir()

	pluginDir := filepath.Join(dir, ".claude-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	files, err := discoverByName(dir, []string{"plugin.json", "metadata.json"})
	if err != nil {
		t.Fatalf("discoverByName failed: %v", err)
	}

	if len(files) != 2 {
		t.Errorf("found %d files, want 2", len(files))
	}
}

func TestDiscoverAll(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "readme.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "script.sh"), []byte("#!/bin/bash"), 0o644); err != nil {
		t.Fatal(err)
	}

	pluginDir := filepath.Join(dir, ".claude-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	files, err := discoverAll(dir, []string{".md", ".sh"}, []string{"plugin.json"})
	if err != nil {
		t.Fatalf("discoverAll failed: %v", err)
	}

	if len(files) != 3 {
		t.Errorf("found %d files, want 3", len(files))
		for _, f := range files {
			t.Logf("  %s", f)
		}
	}
}
