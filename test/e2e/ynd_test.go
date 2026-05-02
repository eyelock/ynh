//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestYnd_Lint_OK_AndIssues asserts `ynd lint` is silent on a clean
// harness and reports issues on a malformed one. ynd is the developer
// tool surface; this is the smoke layer to catch regressions in the
// validation pipeline.
func TestYnd_Lint_OK_AndIssues(t *testing.T) {
	t.Run("clean harness lints clean", func(t *testing.T) {
		harness := newSyntheticSkillHarness(t, "lint-clean")
		_, errOut, err := runYnd(t, "lint", "--harness", harness)
		if err != nil {
			t.Fatalf("lint of clean harness should succeed, got err=%v\nstderr:\n%s", err, errOut)
		}
	})

	t.Run("missing required field is flagged", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "lint-bad")
		if err := os.MkdirAll(filepath.Join(dir, ".ynh-plugin"), 0o755); err != nil {
			t.Fatal(err)
		}
		// version field missing.
		bad := `{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"lint-bad"}`
		if err := os.WriteFile(filepath.Join(dir, ".ynh-plugin", "plugin.json"), []byte(bad), 0o644); err != nil {
			t.Fatal(err)
		}
		out, errOut, err := runYnd(t, "lint", "--harness", dir)
		if err == nil {
			t.Fatalf("lint of harness missing 'version' should fail, got success\nstdout:\n%s", out)
		}
		combined := out + errOut
		if !strings.Contains(combined, "version") {
			t.Errorf("expected lint output to mention 'version', got:\n%s", combined)
		}
	})
}

// TestYnd_Validate_OK_AndError asserts `ynd validate` prints a valid line
// for a good harness and fails non-zero on a broken plugin.json.
func TestYnd_Validate_OK_AndError(t *testing.T) {
	t.Run("clean harness validates", func(t *testing.T) {
		harness := newSyntheticSkillHarness(t, "validate-clean")
		out, _ := mustRunYnd(t, "validate", "--harness", harness)
		if !strings.Contains(out, "valid") {
			t.Errorf("expected 'valid' in output, got:\n%s", out)
		}
	})

	t.Run("malformed plugin.json fails", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "validate-bad")
		if err := os.MkdirAll(filepath.Join(dir, ".ynh-plugin"), 0o755); err != nil {
			t.Fatal(err)
		}
		// Garbage JSON.
		if err := os.WriteFile(filepath.Join(dir, ".ynh-plugin", "plugin.json"),
			[]byte(`{ this is not json }`), 0o644); err != nil {
			t.Fatal(err)
		}
		_, _, err := runYnd(t, "validate", "--harness", dir)
		if err == nil {
			t.Fatalf("expected validation to fail on garbage JSON, got success")
		}
	})
}

// TestYnd_Fmt_NormalizesMarkdown asserts `ynd fmt` rewrites markdown files
// in a harness to a canonical form (e.g., trailing newline). Pure file IO,
// no LLM, no stdin.
func TestYnd_Fmt_NormalizesMarkdown(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "fmt-target")
	if err := os.MkdirAll(filepath.Join(dir, ".ynh-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	plugin := `{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"fmt-target","version":"0.1.0"}`
	if err := os.WriteFile(filepath.Join(dir, ".ynh-plugin", "plugin.json"), []byte(plugin), 0o644); err != nil {
		t.Fatal(err)
	}
	// Markdown file with no trailing newline — fmt should add one.
	mdPath := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(mdPath, []byte("# heading\n\ntext without trailing newline"), 0o644); err != nil {
		t.Fatal(err)
	}

	mustRunYnd(t, "fmt", "--harness", dir)

	body, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("reading formatted file: %v", err)
	}
	if !strings.HasSuffix(string(body), "\n") {
		t.Errorf("fmt should ensure trailing newline, got:\n%q", body)
	}
}

// TestYnd_Preview_RendersAssembled asserts `ynd preview` writes the
// assembled vendor layout to an output directory. Locks the developer-side
// "see what would be assembled" workflow.
func TestYnd_Preview_RendersAssembled(t *testing.T) {
	harness := newSyntheticSkillHarness(t, "preview-target")

	outDir := filepath.Join(t.TempDir(), "preview-out")
	mustRunYnd(t, "preview", "--harness", harness, "-v", "claude", "-o", outDir)

	// Claude assembled output: .claude/skills/<name>/SKILL.md should appear.
	skillFile := filepath.Join(outDir, ".claude", "skills", "hello", "SKILL.md")
	if _, err := os.Stat(skillFile); err != nil {
		t.Errorf("expected preview to produce %s, got err=%v", skillFile, err)
	}
	manifest := filepath.Join(outDir, ".claude-plugin", "plugin.json")
	if _, err := os.Stat(manifest); err != nil {
		t.Errorf("expected preview to produce %s, got err=%v", manifest, err)
	}
}
