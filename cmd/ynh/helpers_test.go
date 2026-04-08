package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsLocalPath(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"absolute path", "/tmp/some-path", true},
		{"relative path", "./some-path", true},
		{"existing dir", dir, true},
		{"git URL", "github.com/user/repo", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isLocalPath(tt.input); got != tt.want {
				t.Errorf("isLocalPath(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
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

func TestLoadOrSynthesizeHarness_HarnessFormat(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "harness.json"),
		[]byte(`{"name":"test","version":"0.1.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	h, err := loadOrSynthesizeHarness(dir)
	if err != nil {
		t.Fatalf("loadOrSynthesizeHarness failed: %v", err)
	}
	if h.Name != "test" {
		t.Errorf("Name = %q, want %q", h.Name, "test")
	}
}

func TestLoadOrSynthesizeHarness_BareAgentsMD(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my-project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"),
		[]byte("# My Project\n\nInstructions here.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	h, err := loadOrSynthesizeHarness(dir)
	if err != nil {
		t.Fatalf("loadOrSynthesizeHarness failed: %v", err)
	}
	if h.Name != "my-project" {
		t.Errorf("Name = %q, want %q", h.Name, "my-project")
	}

	// Verify harness.json was synthesized
	if _, err := os.Stat(filepath.Join(dir, "harness.json")); err != nil {
		t.Error("synthesized harness.json should exist")
	}
}

func TestLoadOrSynthesizeHarness_BareInstructionsMD(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bare-instr")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "instructions.md"),
		[]byte("instructions"), 0o644); err != nil {
		t.Fatal(err)
	}

	h, err := loadOrSynthesizeHarness(dir)
	if err != nil {
		t.Fatalf("loadOrSynthesizeHarness failed: %v", err)
	}
	if h.Name != "bare-instr" {
		t.Errorf("Name = %q, want %q", h.Name, "bare-instr")
	}
}

func TestLoadOrSynthesizeHarness_NoInstructions(t *testing.T) {
	dir := t.TempDir()
	_, err := loadOrSynthesizeHarness(dir)
	if err == nil {
		t.Fatal("expected error for directory with no plugin.json or instructions")
	}
}
