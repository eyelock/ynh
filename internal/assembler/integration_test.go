package assembler

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/persona"
	"github.com/eyelock/ynh/internal/resolver"
	"github.com/eyelock/ynh/internal/vendor"
)

// testdataDir returns the absolute path to the testdata directory.
func testdataDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata")
}

// initGitRepo initializes a git repo at the given path so the resolver can clone it.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	run(t, dir, "git", "init")
	run(t, dir, "git", "config", "user.email", "test@test.com")
	run(t, dir, "git", "config", "user.name", "Test")
	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-m", "init")
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed in %s: %v\n%s", name, args, dir, err, out)
	}
}

// copyTestdata copies a testdata directory to a temp dir and initializes it as a git repo.
// This lets us use local paths as if they were cloned repos.
func copyTestdata(t *testing.T, name string) string {
	t.Helper()
	src := filepath.Join(testdataDir(t), name)
	dst := filepath.Join(t.TempDir(), name)

	cmd := exec.Command("cp", "-r", src, dst)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("copy %s: %v\n%s", name, err, out)
	}

	initGitRepo(t, dst)
	return dst
}

// TestIntegration_MultiSourceComposition tests the full flow:
// - Pull skills from a standalone skills repo (cherry-picked)
// - Pull from a monorepo subdirectory (using path)
// - Include embedded persona artifacts
// - Assemble into a vendor config directory
func TestIntegration_MultiSourceComposition(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// Set up "remote" repos from testdata
	skillsRepo := copyTestdata(t, "skills-repo")
	monorepo := copyTestdata(t, "monorepo")
	composedDir := copyTestdata(t, "composed-persona")

	// Build a persona that composes from all sources
	p := &persona.Persona{
		Name:          "composed",
		DefaultVendor: "claude",
		Includes: []persona.Include{
			{
				GitSource: persona.GitSource{Git: skillsRepo},
				Pick:      []string{"skills/commit", "skills/tdd", "agents/architecture-advisor.md"},
			},
			{
				GitSource: persona.GitSource{Git: monorepo, Path: "packages/ai-config"},
				Pick:      []string{"skills/deploy", "agents/ops-specialist.md"},
			},
			{
				GitSource: persona.GitSource{Git: monorepo, Path: "packages/ai-config"},
				Pick:      []string{"rules/production-safety.md"},
			},
		},
	}

	// Resolve all includes
	content, err := resolver.Resolve(p, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// Add embedded persona content (simulating what cmdRun does)
	content = append(content, resolver.ResolvedContent{
		BasePath: composedDir,
	})

	// Assemble for Claude
	adapter, _ := vendor.Get("claude")
	workDir, err := Assemble(adapter, content)
	if err != nil {
		t.Fatalf("Assemble failed: %v", err)
	}
	defer Cleanup(workDir)

	claudeDir := filepath.Join(workDir, ".claude")

	// Verify skills from skills-repo were picked
	assertFileExists(t, filepath.Join(claudeDir, "skills", "commit", "SKILL.md"))
	assertFileExists(t, filepath.Join(claudeDir, "skills", "tdd", "SKILL.md"))
	assertFileContains(t, filepath.Join(claudeDir, "skills", "commit", "SKILL.md"), "conventional commit")

	// Verify review skill was NOT included (not in pick list)
	assertFileNotExists(t, filepath.Join(claudeDir, "skills", "review", "SKILL.md"))

	// Verify agent from skills-repo
	assertFileExists(t, filepath.Join(claudeDir, "agents", "architecture-advisor.md"))
	assertFileContains(t, filepath.Join(claudeDir, "agents", "architecture-advisor.md"), "architecture advisor")

	// Verify skill from monorepo subpath
	assertFileExists(t, filepath.Join(claudeDir, "skills", "deploy", "SKILL.md"))
	assertFileContains(t, filepath.Join(claudeDir, "skills", "deploy", "SKILL.md"), "Deploy applications")

	// Verify agent from monorepo subpath
	assertFileExists(t, filepath.Join(claudeDir, "agents", "ops-specialist.md"))

	// Verify rule from monorepo subpath
	assertFileExists(t, filepath.Join(claudeDir, "rules", "production-safety.md"))
	assertFileContains(t, filepath.Join(claudeDir, "rules", "production-safety.md"), "destructive commands")

	// Verify embedded persona artifacts
	assertFileExists(t, filepath.Join(claudeDir, "rules", "my-style.md"))
	assertFileContains(t, filepath.Join(claudeDir, "rules", "my-style.md"), "No fluff")
	assertFileExists(t, filepath.Join(claudeDir, "agents", "personal-assistant.md"))

	// Verify monorepo webapp code was NOT included
	assertFileNotExists(t, filepath.Join(claudeDir, "packages"))
	assertFileNotExists(t, filepath.Join(workDir, "packages"))

	// Verify instructions.md was copied as CLAUDE.md at project root
	assertFileExists(t, filepath.Join(workDir, "CLAUDE.md"))
	assertFileContains(t, filepath.Join(workDir, "CLAUDE.md"), "composed persona for testing")
}

// TestIntegration_MonorepoNoPickIncludesAll tests that omitting pick
// from a monorepo path includes all artifacts from that subdirectory.
func TestIntegration_MonorepoNoPickIncludesAll(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	monorepo := copyTestdata(t, "monorepo")

	p := &persona.Persona{
		Name: "mono-all",
		Includes: []persona.Include{
			{
				GitSource: persona.GitSource{Git: monorepo, Path: "packages/ai-config"},
				// No pick - should include all artifact dirs
			},
		},
	}

	content, err := resolver.Resolve(p, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	adapter, _ := vendor.Get("claude")
	workDir, err := Assemble(adapter, content)
	if err != nil {
		t.Fatalf("Assemble failed: %v", err)
	}
	defer Cleanup(workDir)

	claudeDir := filepath.Join(workDir, ".claude")

	// All artifact types from packages/ai-config should be present
	assertFileExists(t, filepath.Join(claudeDir, "skills", "deploy", "SKILL.md"))
	assertFileExists(t, filepath.Join(claudeDir, "agents", "ops-specialist.md"))
	assertFileExists(t, filepath.Join(claudeDir, "rules", "production-safety.md"))

	// But NOT the webapp code
	assertFileNotExists(t, filepath.Join(claudeDir, "webapp"))
}

// TestIntegration_SkillsRepoFullInclude tests including an entire skills repo
// without cherry-picking.
func TestIntegration_SkillsRepoFullInclude(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	skillsRepo := copyTestdata(t, "skills-repo")

	p := &persona.Persona{
		Name: "full-include",
		Includes: []persona.Include{
			{
				GitSource: persona.GitSource{Git: skillsRepo},
				// No pick - include everything
			},
		},
	}

	content, err := resolver.Resolve(p, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	adapter, _ := vendor.Get("claude")
	workDir, err := Assemble(adapter, content)
	if err != nil {
		t.Fatalf("Assemble failed: %v", err)
	}
	defer Cleanup(workDir)

	claudeDir := filepath.Join(workDir, ".claude")

	// All skills should be present
	assertFileExists(t, filepath.Join(claudeDir, "skills", "commit", "SKILL.md"))
	assertFileExists(t, filepath.Join(claudeDir, "skills", "tdd", "SKILL.md"))
	assertFileExists(t, filepath.Join(claudeDir, "skills", "review", "SKILL.md"))

	// Agent and rule too
	assertFileExists(t, filepath.Join(claudeDir, "agents", "architecture-advisor.md"))
	assertFileExists(t, filepath.Join(claudeDir, "rules", "testing-standards.md"))
}

// TestIntegration_CrossVendorAssembly tests assembling the same content
// for different vendors produces different directory layouts.
func TestIntegration_CrossVendorAssembly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	skillsRepo := copyTestdata(t, "skills-repo")

	p := &persona.Persona{
		Name: "cross-vendor",
		Includes: []persona.Include{
			{
				GitSource: persona.GitSource{Git: skillsRepo},
				Pick:      []string{"skills/commit"},
			},
		},
	}

	content, err := resolver.Resolve(p, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}

	// Assemble for Claude
	claudeAdapter, _ := vendor.Get("claude")
	claudeDir, err := Assemble(claudeAdapter, content)
	if err != nil {
		t.Fatalf("Assemble for claude failed: %v", err)
	}
	defer Cleanup(claudeDir)

	// Assemble for Codex
	codexAdapter, _ := vendor.Get("codex")
	codexDir, err := Assemble(codexAdapter, content)
	if err != nil {
		t.Fatalf("Assemble for codex failed: %v", err)
	}
	defer Cleanup(codexDir)

	// Assemble for Cursor
	cursorAdapter, _ := vendor.Get("cursor")
	cursorDir, err := Assemble(cursorAdapter, content)
	if err != nil {
		t.Fatalf("Assemble for cursor failed: %v", err)
	}
	defer Cleanup(cursorDir)

	// Each should have the skill in their vendor-specific directory
	assertFileExists(t, filepath.Join(claudeDir, ".claude", "skills", "commit", "SKILL.md"))
	assertFileExists(t, filepath.Join(codexDir, ".codex", "skills", "commit", "SKILL.md"))
	assertFileExists(t, filepath.Join(cursorDir, ".cursor", "skills", "commit", "SKILL.md"))

	// Content should be identical across vendors
	claudeContent, _ := os.ReadFile(filepath.Join(claudeDir, ".claude", "skills", "commit", "SKILL.md"))
	codexContent, _ := os.ReadFile(filepath.Join(codexDir, ".codex", "skills", "commit", "SKILL.md"))
	cursorContent, _ := os.ReadFile(filepath.Join(cursorDir, ".cursor", "skills", "commit", "SKILL.md"))

	if string(claudeContent) != string(codexContent) {
		t.Error("claude and codex skill content differ")
	}
	if string(claudeContent) != string(cursorContent) {
		t.Error("claude and cursor skill content differ")
	}
}

// TestIntegration_InstructionsFileCrossVendor tests that instructions.md maps to
// the correct vendor-specific filename across all vendors.
func TestIntegration_InstructionsFileCrossVendor(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "instructions.md"), []byte("project instructions"), 0o644); err != nil {
		t.Fatal(err)
	}

	content := []resolver.ResolvedContent{
		{BasePath: repoDir},
	}

	tests := []struct {
		vendor   string
		expected string
	}{
		{"claude", "CLAUDE.md"},
		{"codex", "codex.md"},
		{"cursor", ".cursorrules"},
	}

	for _, tt := range tests {
		t.Run(tt.vendor, func(t *testing.T) {
			adapter, _ := vendor.Get(tt.vendor)
			workDir, err := Assemble(adapter, content)
			if err != nil {
				t.Fatalf("Assemble for %s failed: %v", tt.vendor, err)
			}
			defer Cleanup(workDir)

			instructionsPath := filepath.Join(workDir, tt.expected)
			data, err := os.ReadFile(instructionsPath)
			if err != nil {
				t.Fatalf("%s instructions file %q not found: %v", tt.vendor, tt.expected, err)
			}
			if string(data) != "project instructions" {
				t.Errorf("instructions content = %q", string(data))
			}
		})
	}
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("expected file to exist: %s", path)
	}
}

func assertFileNotExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Errorf("expected file to NOT exist: %s", path)
	}
}

func assertFileContains(t *testing.T, path string, substr string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("reading %s: %v", path, err)
		return
	}
	if !strings.Contains(string(data), substr) {
		t.Errorf("file %s does not contain %q", path, substr)
	}
}
