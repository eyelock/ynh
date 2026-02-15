package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsLocalPath_AbsolutePath(t *testing.T) {
	if !isLocalPath("/tmp/some-path") {
		t.Error("expected /tmp/some-path to be local")
	}
}

func TestIsLocalPath_RelativePath(t *testing.T) {
	if !isLocalPath("./some-path") {
		t.Error("expected ./some-path to be local")
	}
}

func TestIsLocalPath_ExistingDir(t *testing.T) {
	dir := t.TempDir()
	if !isLocalPath(dir) {
		t.Errorf("expected existing dir %q to be local", dir)
	}
}

func TestIsLocalPath_GitURL(t *testing.T) {
	if isLocalPath("github.com/user/repo") {
		t.Error("expected github.com URL to not be local")
	}
}

func TestCopyTree(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	// Create source structure
	for _, dir := range []string{"skills/hello", ".git/objects"} {
		if err := os.MkdirAll(filepath.Join(src, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range map[string]string{
		"skills/hello/SKILL.md": "hello",
		"root.txt":              "root",
		".git/HEAD":             "ref",
	} {
		if err := os.WriteFile(filepath.Join(src, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := copyTree(src, dst); err != nil {
		t.Fatalf("copyTree failed: %v", err)
	}

	// Verify files were copied
	data, err := os.ReadFile(filepath.Join(dst, "skills", "hello", "SKILL.md"))
	if err != nil {
		t.Fatalf("skill not copied: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("skill content = %q, want %q", string(data), "hello")
	}

	data, err = os.ReadFile(filepath.Join(dst, "root.txt"))
	if err != nil {
		t.Fatalf("root.txt not copied: %v", err)
	}
	if string(data) != "root" {
		t.Errorf("root content = %q, want %q", string(data), "root")
	}

	// Verify .git was skipped
	if _, err := os.Stat(filepath.Join(dst, ".git")); !os.IsNotExist(err) {
		t.Error(".git directory should have been skipped")
	}
}

func TestCopyTree_PreservesPermissions(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "out")

	if err := os.MkdirAll(filepath.Join(src, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "scripts", "run.sh"), []byte("#!/bin/bash"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := copyTree(src, dst); err != nil {
		t.Fatalf("copyTree failed: %v", err)
	}

	info, err := os.Stat(filepath.Join(dst, "scripts", "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("execute permission not preserved: got %o", info.Mode())
	}
}

func TestCopyTree_EmptyDir(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	if err := copyTree(src, dst); err != nil {
		t.Fatalf("copyTree on empty dir failed: %v", err)
	}
}
