package assembler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/resolver"
	"github.com/eyelock/ynh/internal/vendor"
)

// TestRulesFoldIntoInstructions: Claude Code, Copilot and Codex do not read the
// rules ynh stages in their config dir, so each rule must land in the
// instructions file the vendor does read. Cursor reads .mdc rules itself and
// OpenCode folds at launch, so neither is folded here.
func TestRulesFoldIntoInstructions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	repo := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(repo, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("instructions.md", "base instructions")
	write("rules/b-second.md", "SECOND-RULE-TOKEN\n")
	write("rules/a-first.md", "---\npaths:\n  - \"**/*.go\"\n---\n\nFIRST-RULE-TOKEN\n")
	content := []resolver.ResolvedContent{{BasePath: repo}}

	for _, v := range []string{"claude", "codex", "copilot"} {
		t.Run(v, func(t *testing.T) {
			adapter, _ := vendor.Get(v)
			dir := t.TempDir()
			if err := AssembleTo(dir, adapter, content); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(dir, adapter.InstructionsFile()))
			if err != nil {
				t.Fatal(err)
			}
			got := string(data)
			for _, want := range []string{"base instructions", "## Rule: a-first", "FIRST-RULE-TOKEN", "## Rule: b-second", "SECOND-RULE-TOKEN"} {
				if !strings.Contains(got, want) {
					t.Errorf("%s instructions missing %q:\n%s", v, want, got)
				}
			}
			if strings.Contains(got, "paths:") || strings.Contains(got, "---") {
				t.Errorf("%s instructions kept a rule's frontmatter:\n%s", v, got)
			}
			if strings.Index(got, "FIRST") > strings.Index(got, "SECOND") {
				t.Errorf("%s rules not sorted by name", v)
			}
		})
	}

	for _, v := range []string{"cursor", "opencode"} {
		t.Run(v+" is not folded", func(t *testing.T) {
			adapter, _ := vendor.Get(v)
			dir := t.TempDir()
			if err := AssembleTo(dir, adapter, content); err != nil {
				t.Fatal(err)
			}
			data, _ := os.ReadFile(filepath.Join(dir, adapter.InstructionsFile()))
			if strings.Contains(string(data), "RULE-TOKEN") {
				t.Errorf("%s instructions hold rules:\n%s", v, data)
			}
		})
	}
}

func TestRulesFoldWithoutInstructions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "rules", "only.md"), []byte("ONLY-RULE-TOKEN"), 0o644); err != nil {
		t.Fatal(err)
	}
	adapter, _ := vendor.Get("claude")
	dir := t.TempDir()
	if err := AssembleTo(dir, adapter, []resolver.ResolvedContent{{BasePath: repo}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil || !strings.Contains(string(data), "## Rule: only\n\nONLY-RULE-TOKEN") {
		t.Fatalf("CLAUDE.md = %q, %v", data, err)
	}
}
