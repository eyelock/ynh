package vendor

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestPlanSymlinks_EmptyArtifactDirs(t *testing.T) {
	staging := t.TempDir()
	project := t.TempDir()

	entries, err := PlanSymlinks(staging, project, ".claude", map[string]string{})
	if err != nil {
		t.Fatalf("PlanSymlinks: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected no entries, got %d", len(entries))
	}
}

func TestPlanSymlinks_MissingStagingArtifactDir(t *testing.T) {
	staging := t.TempDir()
	project := t.TempDir()

	entries, err := PlanSymlinks(staging, project, ".claude", map[string]string{"skills": "skills"})
	if err != nil {
		t.Fatalf("PlanSymlinks: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("missing staging artifact dir should produce no entries, got %d", len(entries))
	}
}

func TestPlanSymlinks_PlansFilesAndSubdirs(t *testing.T) {
	staging := t.TempDir()
	project := t.TempDir()
	configDir := ".claude"

	// staging/.claude/skills/{review,format}
	skillsDir := filepath.Join(staging, configDir, "skills")
	if err := os.MkdirAll(filepath.Join(skillsDir, "review"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(skillsDir, "format"), 0o755); err != nil {
		t.Fatal(err)
	}
	// staging/.claude/agents/reviewer.md
	agentsDir := filepath.Join(staging, configDir, "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsDir, "reviewer.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	artifactDirs := map[string]string{"skills": "skills", "agents": "agents"}
	entries, err := PlanSymlinks(staging, project, configDir, artifactDirs)
	if err != nil {
		t.Fatalf("PlanSymlinks: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries (review, format, reviewer.md), got %d: %+v", len(entries), entries)
	}

	// Sort for deterministic assertions
	sort.Slice(entries, func(i, j int) bool { return entries[i].Link < entries[j].Link })

	for _, e := range entries {
		if !filepathHasPrefix(e.Target, staging) {
			t.Errorf("target should live under staging: %q", e.Target)
		}
		if !filepathHasPrefix(e.Link, project) {
			t.Errorf("link should live under project: %q", e.Link)
		}
	}
}

func filepathHasPrefix(p, prefix string) bool {
	abs, _ := filepath.Abs(p)
	pa, _ := filepath.Abs(prefix)
	rel, err := filepath.Rel(pa, abs)
	if err != nil {
		return false
	}
	return len(rel) > 0 && rel != ".." && !filepathHasDotDot(rel)
}

func filepathHasDotDot(rel string) bool {
	if len(rel) >= 3 && rel[:3] == "../" {
		return true
	}
	return false
}
