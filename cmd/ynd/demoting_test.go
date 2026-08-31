package main

import (
	"strings"
	"testing"
)

func demotingMsgs(issues []lintIssue) string {
	var b strings.Builder
	for _, i := range issues {
		b.WriteString(i.Message)
		b.WriteString("\n")
	}
	return b.String()
}

// The reproduction from #327: lint passed, validate passed, assembly preserved
// the fields, and the vendor demoted the skill.
func TestDemoting_CatchesAllThreeFields(t *testing.T) {
	content := "---\nname: demo\ndescription: A demo skill.\n" +
		"compatibility: Designed for Claude Code\nlicense: Apache-2.0\nmetadata:\n  author: someone\n---\n\n# Demo\n"

	issues := lintDemotingFrontmatter("SKILL.md", content)
	if len(issues) != 3 {
		t.Fatalf("expected one finding per field, got %d:\n%s", len(issues), demotingMsgs(issues))
	}
	got := demotingMsgs(issues)
	for _, want := range []string{"compatibility", "license", "metadata"} {
		if !strings.Contains(got, want) {
			t.Errorf("no finding for %q:\n%s", want, got)
		}
	}
	// The consequence is the point. A finding that names the field without
	// saying what happens reads as pedantry and gets ignored.
	if !strings.Contains(got, "never fires") {
		t.Errorf("finding should state the consequence:\n%s", got)
	}
}

// parseFrontmatter flattens the block and ignores indentation. Reusing it
// would report a nested key as a top-level one.
func TestDemoting_IgnoresNestedKeys(t *testing.T) {
	content := "---\nname: demo\ndescription: d\nmetadata:\n  license: MIT\n  compatibility: whatever\n---\n"

	issues := lintDemotingFrontmatter("SKILL.md", content)
	if len(issues) != 1 {
		t.Fatalf("only the top-level metadata key should be reported, got %d:\n%s",
			len(issues), demotingMsgs(issues))
	}
	if !strings.Contains(issues[0].Message, "metadata") {
		t.Errorf("expected the metadata finding, got: %s", issues[0].Message)
	}
}

func TestDemoting_SafeFrontmatterIsSilent(t *testing.T) {
	// What `ynd create skill` scaffolds, plus the Claude extensions that are
	// documented as safe.
	content := "---\nname: demo\ndescription: d\nallowed-tools: Read Grep\nmodel: opus\n---\n"
	if issues := lintDemotingFrontmatter("SKILL.md", content); len(issues) != 0 {
		t.Errorf("safe frontmatter must be silent, got:\n%s", demotingMsgs(issues))
	}
}

func TestDemoting_ReportsTheRightLine(t *testing.T) {
	content := "---\nname: demo\ndescription: d\ncompatibility: x\n---\n"
	issues := lintDemotingFrontmatter("SKILL.md", content)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Line != 4 {
		t.Errorf("compatibility is on line 4, finding says %d", issues[0].Line)
	}
}

func TestDemoting_NoFrontmatterIsSilent(t *testing.T) {
	for _, c := range []string{"# Just a heading\n", "---\nname: x\n", ""} {
		if issues := lintDemotingFrontmatter("SKILL.md", c); len(issues) != 0 {
			t.Errorf("content %q should yield nothing, got:\n%s", c, demotingMsgs(issues))
		}
	}
}

// A comment line beginning with # must not be mistaken for a key.
func TestDemoting_IgnoresComments(t *testing.T) {
	content := "---\nname: demo\ndescription: d\n# license: MIT\n---\n"
	if issues := lintDemotingFrontmatter("SKILL.md", content); len(issues) != 0 {
		t.Errorf("a commented-out field is not present, got:\n%s", demotingMsgs(issues))
	}
}

// The check must be reachable through the real lint path, not merely callable.
func TestDemoting_WiredIntoSkillLint(t *testing.T) {
	content := "---\nname: demo\ndescription: d\ncompatibility: x\n---\n"
	got := demotingMsgs(lintSkillFrontmatter("skills/demo/SKILL.md", content))
	if !strings.Contains(got, "compatibility") {
		t.Errorf("lintSkillFrontmatter should carry the demotion check:\n%s", got)
	}
}
