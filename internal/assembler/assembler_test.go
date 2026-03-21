package assembler

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eyelock/ynh/internal/resolver"
	"github.com/eyelock/ynh/internal/vendor"
)

type mockAdapter struct{}

func (m *mockAdapter) Name() string             { return "mock" }
func (m *mockAdapter) CLIName() string          { return "mock" }
func (m *mockAdapter) ConfigDir() string        { return ".mock" }
func (m *mockAdapter) InstructionsFile() string { return "MOCK.md" }
func (m *mockAdapter) ArtifactDirs() map[string]string {
	return map[string]string{
		"skills":   "skills",
		"agents":   "agents",
		"rules":    "rules",
		"commands": "commands",
	}
}
func (m *mockAdapter) NeedsSymlinks() bool { return false }
func (m *mockAdapter) Install(stagingDir string, projectDir string) ([]vendor.SymlinkEntry, error) {
	return nil, nil
}
func (m *mockAdapter) Clean(entries []vendor.SymlinkEntry) error                     { return nil }
func (m *mockAdapter) LaunchInteractive(configPath string, extraArgs []string) error { return nil }
func (m *mockAdapter) LaunchNonInteractive(configPath string, prompt string, extraArgs []string) error {
	return nil
}

func TestAssembleWithPick(t *testing.T) {
	// Create a fake repo with skills and agents
	repoDir := t.TempDir()
	for _, dir := range []string{"skills/commit", "agents"} {
		if err := os.MkdirAll(filepath.Join(repoDir, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range map[string]string{
		"skills/commit/SKILL.md": "commit skill",
		"agents/reviewer.md":     "reviewer agent",
	} {
		if err := os.WriteFile(filepath.Join(repoDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	content := []resolver.ResolvedContent{
		{
			BasePath: repoDir,
			Paths:    []string{"skills/commit", "agents/reviewer.md"},
		},
	}

	adapter := &mockAdapter{}
	workDir, err := Assemble(adapter, content)
	if err != nil {
		t.Fatalf("Assemble failed: %v", err)
	}
	defer Cleanup(workDir)

	// Verify skill was copied
	skillPath := filepath.Join(workDir, ".mock", "skills", "commit", "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("skill not found: %v", err)
	}
	if string(data) != "commit skill" {
		t.Errorf("skill content = %q", string(data))
	}

	// Verify agent was copied
	agentPath := filepath.Join(workDir, ".mock", "agents", "reviewer.md")
	data, err = os.ReadFile(agentPath)
	if err != nil {
		t.Fatalf("agent not found: %v", err)
	}
	if string(data) != "reviewer agent" {
		t.Errorf("agent content = %q", string(data))
	}
}

func TestAssembleAllArtifacts(t *testing.T) {
	repoDir := t.TempDir()
	for _, dir := range []string{"skills/tdd", "rules"} {
		if err := os.MkdirAll(filepath.Join(repoDir, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range map[string]string{
		"skills/tdd/SKILL.md": "tdd",
		"rules/be-nice.md":    "be nice",
	} {
		if err := os.WriteFile(filepath.Join(repoDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	content := []resolver.ResolvedContent{
		{BasePath: repoDir}, // No pick = include all
	}

	adapter := &mockAdapter{}
	workDir, err := Assemble(adapter, content)
	if err != nil {
		t.Fatalf("Assemble failed: %v", err)
	}
	defer Cleanup(workDir)

	// Both should be present
	if _, err := os.Stat(filepath.Join(workDir, ".mock", "skills", "tdd", "SKILL.md")); err != nil {
		t.Error("tdd skill not found")
	}
	if _, err := os.Stat(filepath.Join(workDir, ".mock", "rules", "be-nice.md")); err != nil {
		t.Error("be-nice rule not found")
	}
}

func TestAssembleTo_CleansAndPopulates(t *testing.T) {
	repoDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoDir, "skills", "hello"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "skills", "hello", "SKILL.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	content := []resolver.ResolvedContent{
		{BasePath: repoDir},
	}

	adapter := &mockAdapter{}
	destDir := filepath.Join(t.TempDir(), "run", "test")

	// First assembly
	if err := AssembleTo(destDir, adapter, content); err != nil {
		t.Fatalf("AssembleTo failed: %v", err)
	}

	skillPath := filepath.Join(destDir, ".mock", "skills", "hello", "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		t.Fatal("skill not found after first assembly")
	}

	// Add a stale file that should be cleaned on second run
	staleFile := filepath.Join(destDir, "stale.txt")
	if err := os.WriteFile(staleFile, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Second assembly should clean and repopulate
	if err := AssembleTo(destDir, adapter, content); err != nil {
		t.Fatalf("AssembleTo (second) failed: %v", err)
	}

	// Skill should still exist
	if _, err := os.Stat(skillPath); err != nil {
		t.Fatal("skill not found after second assembly")
	}

	// Stale file should be gone
	if _, err := os.Stat(staleFile); !os.IsNotExist(err) {
		t.Error("stale file should have been cleaned")
	}
}

func TestCopyFilePreservesPermissions(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	// Create an executable script
	scriptPath := filepath.Join(srcDir, "run.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/bash\necho hi"), 0o755); err != nil {
		t.Fatal(err)
	}

	dstPath := filepath.Join(dstDir, "run.sh")
	if err := CopyFile(scriptPath, dstPath); err != nil {
		t.Fatalf("CopyFile failed: %v", err)
	}

	info, err := os.Stat(dstPath)
	if err != nil {
		t.Fatal(err)
	}

	// Check that execute bit is preserved
	if info.Mode()&0o111 == 0 {
		t.Errorf("execute permission not preserved: got %o", info.Mode())
	}
}

func TestAssembleInstructionsFile(t *testing.T) {
	// Create a repo with instructions.md
	repoDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoDir, "skills", "hello"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "skills", "hello", "SKILL.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "instructions.md"), []byte("You are a helpful assistant."), 0o644); err != nil {
		t.Fatal(err)
	}

	content := []resolver.ResolvedContent{
		{BasePath: repoDir},
	}

	adapter := &mockAdapter{}
	workDir, err := Assemble(adapter, content)
	if err != nil {
		t.Fatalf("Assemble failed: %v", err)
	}
	defer Cleanup(workDir)

	// instructions.md should be copied as MOCK.md at the project root
	instructionsPath := filepath.Join(workDir, "MOCK.md")
	data, err := os.ReadFile(instructionsPath)
	if err != nil {
		t.Fatalf("instructions file not created: %v", err)
	}
	if string(data) != "You are a helpful assistant." {
		t.Errorf("instructions content = %q", string(data))
	}
}

func TestAssembleInstructionsFile_LastSourceWins(t *testing.T) {
	// Create two content sources with instructions.md
	repo1 := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo1, "instructions.md"), []byte("from repo1"), 0o644); err != nil {
		t.Fatal(err)
	}

	repo2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo2, "instructions.md"), []byte("from repo2"), 0o644); err != nil {
		t.Fatal(err)
	}

	content := []resolver.ResolvedContent{
		{BasePath: repo1},
		{BasePath: repo2},
	}

	adapter := &mockAdapter{}
	workDir, err := Assemble(adapter, content)
	if err != nil {
		t.Fatalf("Assemble failed: %v", err)
	}
	defer Cleanup(workDir)

	// Last source should win
	data, err := os.ReadFile(filepath.Join(workDir, "MOCK.md"))
	if err != nil {
		t.Fatalf("instructions file not created: %v", err)
	}
	if string(data) != "from repo2" {
		t.Errorf("expected last source to win, got %q", string(data))
	}
}

func TestAssembleInstructionsFile_NoInstructions(t *testing.T) {
	// Create a repo without instructions.md
	repoDir := t.TempDir()

	content := []resolver.ResolvedContent{
		{BasePath: repoDir},
	}

	adapter := &mockAdapter{}
	workDir, err := Assemble(adapter, content)
	if err != nil {
		t.Fatalf("Assemble failed: %v", err)
	}
	defer Cleanup(workDir)

	// No instructions file should be created
	if _, err := os.Stat(filepath.Join(workDir, "MOCK.md")); !os.IsNotExist(err) {
		t.Error("instructions file should not exist when no instructions.md in source")
	}
}

// Ensure the vendor package is imported for side effects
var _ vendor.Adapter = &mockAdapter{}
