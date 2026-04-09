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

func TestLoadOrSynthesizeHarness_HarnessFormat(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".harness.json"),
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

	// Verify .harness.json was synthesized
	if _, err := os.Stat(filepath.Join(dir, ".harness.json")); err != nil {
		t.Error("synthesized .harness.json should exist")
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
