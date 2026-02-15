package assembler

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/eyelock/ynh/internal/persona"
	"github.com/eyelock/ynh/internal/resolver"
	"github.com/eyelock/ynh/internal/vendor"
)

// makeGitRepo creates a temp dir with content and initializes it as a git repo.
func makeGitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()

	for path, content := range files {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %s (%v)", args, out, err)
		}
	}
	runGit("init")
	runGit("config", "user.email", "test@test.com")
	runGit("config", "user.name", "Test")
	runGit("add", ".")
	runGit("commit", "-m", "init")

	return dir
}

// resolveAndAssemble is a test helper that resolves and assembles a persona for Claude.
func resolveAndAssemble(t *testing.T, p *persona.Persona, extraContent ...resolver.ResolvedContent) string {
	t.Helper()

	content, err := resolver.Resolve(p)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	content = append(content, extraContent...)

	adapter, _ := vendor.Get("claude")
	workDir, err := Assemble(adapter, content)
	if err != nil {
		t.Fatalf("Assemble failed: %v", err)
	}

	t.Cleanup(func() { Cleanup(workDir) })
	return filepath.Join(workDir, ".claude")
}

// --- Subpath: pick a single skill from a subpath ---
func TestSubpath_PickSingleSkill(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	repo := makeGitRepo(t, map[string]string{
		"config/skills/lint/SKILL.md":   "lint skill",
		"config/skills/format/SKILL.md": "format skill",
		"config/agents/helper.md":       "helper agent",
		"other/unrelated.txt":           "noise",
	})

	p := &persona.Persona{
		Name: "test",
		Includes: []persona.Include{
			{GitSource: persona.GitSource{Git: repo, Path: "config"}, Pick: []string{"skills/lint"}},
		},
	}

	claudeDir := resolveAndAssemble(t, p)

	assertFileExists(t, filepath.Join(claudeDir, "skills", "lint", "SKILL.md"))
	assertFileContains(t, filepath.Join(claudeDir, "skills", "lint", "SKILL.md"), "lint skill")
	assertFileNotExists(t, filepath.Join(claudeDir, "skills", "format"))
	assertFileNotExists(t, filepath.Join(claudeDir, "agents", "helper.md"))
}

// --- Subpath: pick multiple artifact types from a subpath ---
func TestSubpath_PickMixedArtifactTypes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	repo := makeGitRepo(t, map[string]string{
		"ai/skills/deploy/SKILL.md": "deploy",
		"ai/agents/ops.md":          "ops agent",
		"ai/rules/safety.md":        "safety rule",
		"ai/commands/release.md":    "release cmd",
	})

	p := &persona.Persona{
		Name: "test",
		Includes: []persona.Include{
			{
				GitSource: persona.GitSource{Git: repo, Path: "ai"},
				Pick: []string{
					"skills/deploy",
					"agents/ops.md",
					"rules/safety.md",
					"commands/release.md",
				},
			},
		},
	}

	claudeDir := resolveAndAssemble(t, p)

	assertFileExists(t, filepath.Join(claudeDir, "skills", "deploy", "SKILL.md"))
	assertFileExists(t, filepath.Join(claudeDir, "agents", "ops.md"))
	assertFileExists(t, filepath.Join(claudeDir, "rules", "safety.md"))
	assertFileExists(t, filepath.Join(claudeDir, "commands", "release.md"))
}

// --- Subpath: no pick includes all artifact dirs from subpath ---
func TestSubpath_NoPickIncludesAllArtifacts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	repo := makeGitRepo(t, map[string]string{
		"deep/nested/skills/a/SKILL.md": "skill a",
		"deep/nested/agents/b.md":       "agent b",
		"deep/nested/rules/c.md":        "rule c",
		"deep/nested/commands/d.md":     "command d",
		"deep/nested/readme.txt":        "not an artifact",
	})

	p := &persona.Persona{
		Name: "test",
		Includes: []persona.Include{
			{GitSource: persona.GitSource{Git: repo, Path: "deep/nested"}},
		},
	}

	claudeDir := resolveAndAssemble(t, p)

	assertFileExists(t, filepath.Join(claudeDir, "skills", "a", "SKILL.md"))
	assertFileExists(t, filepath.Join(claudeDir, "agents", "b.md"))
	assertFileExists(t, filepath.Join(claudeDir, "rules", "c.md"))
	assertFileExists(t, filepath.Join(claudeDir, "commands", "d.md"))
	// readme.txt is not in a recognized artifact dir, should not appear
}

// --- Same repo, two different subpaths ---
func TestSubpath_SameRepoMultipleSubpaths(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	repo := makeGitRepo(t, map[string]string{
		"team-a/skills/frontend/SKILL.md": "frontend skill",
		"team-a/rules/react.md":           "react rules",
		"team-b/skills/backend/SKILL.md":  "backend skill",
		"team-b/rules/go.md":              "go rules",
	})

	p := &persona.Persona{
		Name: "test",
		Includes: []persona.Include{
			{GitSource: persona.GitSource{Git: repo, Path: "team-a"}, Pick: []string{"skills/frontend"}},
			{GitSource: persona.GitSource{Git: repo, Path: "team-b"}, Pick: []string{"skills/backend", "rules/go.md"}},
		},
	}

	claudeDir := resolveAndAssemble(t, p)

	assertFileExists(t, filepath.Join(claudeDir, "skills", "frontend", "SKILL.md"))
	assertFileExists(t, filepath.Join(claudeDir, "skills", "backend", "SKILL.md"))
	assertFileExists(t, filepath.Join(claudeDir, "rules", "go.md"))
	assertFileNotExists(t, filepath.Join(claudeDir, "rules", "react.md"))
}

// --- Mix: one include with subpath, one without ---
func TestSubpath_MixedWithAndWithoutPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// Standalone skills repo (no subpath needed)
	standaloneRepo := makeGitRepo(t, map[string]string{
		"skills/commit/SKILL.md": "commit skill",
		"rules/test-first.md":    "test first",
	})

	// Monorepo (needs subpath)
	monorepo := makeGitRepo(t, map[string]string{
		"packages/ai/skills/deploy/SKILL.md": "deploy skill",
		"packages/ai/agents/ops.md":          "ops agent",
		"packages/webapp/index.ts":           "webapp code",
	})

	p := &persona.Persona{
		Name: "test",
		Includes: []persona.Include{
			{GitSource: persona.GitSource{Git: standaloneRepo}, Pick: []string{"skills/commit"}},
			{GitSource: persona.GitSource{Git: monorepo, Path: "packages/ai"}, Pick: []string{"skills/deploy"}},
		},
	}

	claudeDir := resolveAndAssemble(t, p)

	assertFileExists(t, filepath.Join(claudeDir, "skills", "commit", "SKILL.md"))
	assertFileExists(t, filepath.Join(claudeDir, "skills", "deploy", "SKILL.md"))
	assertFileNotExists(t, filepath.Join(claudeDir, "agents", "ops.md"))
}

// --- Subpath with embedded persona content ---
func TestSubpath_PlusEmbeddedContent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	repo := makeGitRepo(t, map[string]string{
		"shared/skills/review/SKILL.md": "review skill",
	})

	// Embedded persona content (local directory, not a git repo)
	embeddedDir := t.TempDir()
	for _, sub := range []string{"rules", "agents"} {
		if err := os.MkdirAll(filepath.Join(embeddedDir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range map[string]string{
		"rules/personal.md":   "my personal rule",
		"agents/assistant.md": "my assistant",
	} {
		if err := os.WriteFile(filepath.Join(embeddedDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	p := &persona.Persona{
		Name: "test",
		Includes: []persona.Include{
			{GitSource: persona.GitSource{Git: repo, Path: "shared"}, Pick: []string{"skills/review"}},
		},
	}

	embedded := resolver.ResolvedContent{BasePath: embeddedDir}
	claudeDir := resolveAndAssemble(t, p, embedded)

	// External via subpath
	assertFileExists(t, filepath.Join(claudeDir, "skills", "review", "SKILL.md"))
	// Embedded
	assertFileExists(t, filepath.Join(claudeDir, "rules", "personal.md"))
	assertFileExists(t, filepath.Join(claudeDir, "agents", "assistant.md"))
}

// --- Subpath pointing to nonexistent directory ---
func TestSubpath_NonexistentPathErrors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	repo := makeGitRepo(t, map[string]string{
		"real/skills/a/SKILL.md": "skill a",
	})

	p := &persona.Persona{
		Name: "test",
		Includes: []persona.Include{
			{GitSource: persona.GitSource{Git: repo, Path: "does-not-exist"}},
		},
	}

	_, err := resolver.Resolve(p)
	if err == nil {
		t.Fatal("expected error for nonexistent subpath")
	}
}

// --- Root path (no subpath) full repo include ---
func TestSubpath_RootNoPathFullInclude(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	repo := makeGitRepo(t, map[string]string{
		"skills/a/SKILL.md": "skill a",
		"skills/b/SKILL.md": "skill b",
		"agents/c.md":       "agent c",
		"rules/d.md":        "rule d",
		"commands/e.md":     "command e",
	})

	p := &persona.Persona{
		Name: "test",
		Includes: []persona.Include{
			{GitSource: persona.GitSource{Git: repo}}, // No path, no pick - everything
		},
	}

	claudeDir := resolveAndAssemble(t, p)

	assertFileExists(t, filepath.Join(claudeDir, "skills", "a", "SKILL.md"))
	assertFileExists(t, filepath.Join(claudeDir, "skills", "b", "SKILL.md"))
	assertFileExists(t, filepath.Join(claudeDir, "agents", "c.md"))
	assertFileExists(t, filepath.Join(claudeDir, "rules", "d.md"))
	assertFileExists(t, filepath.Join(claudeDir, "commands", "e.md"))
}

// --- Deeply nested subpath ---
func TestSubpath_DeeplyNested(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	repo := makeGitRepo(t, map[string]string{
		"org/division/team/ai-config/skills/special/SKILL.md": "deeply nested",
		"org/division/team/ai-config/rules/deep-rule.md":      "deep rule",
		"org/division/other-team/code.ts":                     "unrelated",
	})

	p := &persona.Persona{
		Name: "test",
		Includes: []persona.Include{
			{GitSource: persona.GitSource{Git: repo, Path: "org/division/team/ai-config"}},
		},
	}

	claudeDir := resolveAndAssemble(t, p)

	assertFileExists(t, filepath.Join(claudeDir, "skills", "special", "SKILL.md"))
	assertFileContains(t, filepath.Join(claudeDir, "skills", "special", "SKILL.md"), "deeply nested")
	assertFileExists(t, filepath.Join(claudeDir, "rules", "deep-rule.md"))
	assertFileNotExists(t, filepath.Join(claudeDir, "other-team"))
}

// --- Multiple repos, subpaths, and picks combined ---
func TestSubpath_ComplexComposition(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	repoA := makeGitRepo(t, map[string]string{
		"skills/commit/SKILL.md": "commit from A",
		"skills/tdd/SKILL.md":    "tdd from A",
		"skills/deploy/SKILL.md": "deploy from A",
		"agents/reviewer.md":     "reviewer from A",
		"rules/testing.md":       "testing from A",
	})

	repoB := makeGitRepo(t, map[string]string{
		"platform/ai/skills/k8s/SKILL.md":  "k8s from B",
		"platform/ai/agents/infra.md":      "infra from B",
		"platform/ai/rules/security.md":    "security from B",
		"platform/ai/commands/rollback.md": "rollback from B",
		"platform/webapp/src/app.tsx":      "webapp from B",
	})

	repoC := makeGitRepo(t, map[string]string{
		"alpha/skills/design/SKILL.md": "design from C",
		"beta/skills/mobile/SKILL.md":  "mobile from C",
	})

	p := &persona.Persona{
		Name: "mega",
		Includes: []persona.Include{
			// Repo A: cherry-pick two skills and one agent
			{GitSource: persona.GitSource{Git: repoA}, Pick: []string{"skills/commit", "skills/tdd", "agents/reviewer.md"}},
			// Repo B: monorepo subpath, pick specific items
			{GitSource: persona.GitSource{Git: repoB, Path: "platform/ai"}, Pick: []string{"skills/k8s", "commands/rollback.md"}},
			// Repo B again: different picks from same subpath
			{GitSource: persona.GitSource{Git: repoB, Path: "platform/ai"}, Pick: []string{"rules/security.md"}},
			// Repo C: two different subpaths
			{GitSource: persona.GitSource{Git: repoC, Path: "alpha"}, Pick: []string{"skills/design"}},
			{GitSource: persona.GitSource{Git: repoC, Path: "beta"}, Pick: []string{"skills/mobile"}},
		},
	}

	claudeDir := resolveAndAssemble(t, p)

	// From repo A
	assertFileExists(t, filepath.Join(claudeDir, "skills", "commit", "SKILL.md"))
	assertFileContains(t, filepath.Join(claudeDir, "skills", "commit", "SKILL.md"), "commit from A")
	assertFileExists(t, filepath.Join(claudeDir, "skills", "tdd", "SKILL.md"))
	assertFileExists(t, filepath.Join(claudeDir, "agents", "reviewer.md"))
	assertFileNotExists(t, filepath.Join(claudeDir, "skills", "deploy"))    // not picked
	assertFileNotExists(t, filepath.Join(claudeDir, "rules", "testing.md")) // not picked

	// From repo B via subpath
	assertFileExists(t, filepath.Join(claudeDir, "skills", "k8s", "SKILL.md"))
	assertFileContains(t, filepath.Join(claudeDir, "skills", "k8s", "SKILL.md"), "k8s from B")
	assertFileExists(t, filepath.Join(claudeDir, "commands", "rollback.md"))
	assertFileExists(t, filepath.Join(claudeDir, "rules", "security.md"))
	assertFileNotExists(t, filepath.Join(claudeDir, "agents", "infra.md")) // not picked

	// From repo C via two different subpaths
	assertFileExists(t, filepath.Join(claudeDir, "skills", "design", "SKILL.md"))
	assertFileContains(t, filepath.Join(claudeDir, "skills", "design", "SKILL.md"), "design from C")
	assertFileExists(t, filepath.Join(claudeDir, "skills", "mobile", "SKILL.md"))
	assertFileContains(t, filepath.Join(claudeDir, "skills", "mobile", "SKILL.md"), "mobile from C")
}
