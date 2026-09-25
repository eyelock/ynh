package agentplugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func skill(name, desc string) string {
	return "---\nname: " + name + "\ndescription: " + desc + "\n---\n\nDo the thing.\n"
}

func TestDiscoverSkills(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "skills/deploy/SKILL.md", skill("deploy", "Deploy things. Use when deploying."))
	write(t, dir, "skills/deploy/scripts/rollback.sh", "#!/bin/sh\n")
	write(t, dir, "skills/deploy/nested/SKILL.md", skill("nested", "Must not be discovered (§7.1)."))
	write(t, dir, "skills/quoted/SKILL.md", "---\nname: \"quoted\"\ndescription: 'Single quoted.'\nlicense: MIT\n---\nbody\n")
	write(t, dir, "skills/crlf/SKILL.md", "---\r\nname: crlf\r\ndescription: Windows line endings.\r\n---\r\nbody\r\n")
	write(t, dir, "skills/no-md/README.md", "not a skill")
	write(t, dir, "skills/mismatch/SKILL.md", skill("other-name", "Name does not match directory."))
	write(t, dir, "skills/Bad_Name/SKILL.md", skill("Bad_Name", "Breaks the naming rule."))
	write(t, dir, "skills/no-desc/SKILL.md", "---\nname: no-desc\n---\nbody\n")
	write(t, dir, "skills/no-fm/SKILL.md", "Just prose.\n")
	write(t, dir, "skills/long-desc/SKILL.md", skill("long-desc", strings.Repeat("x", 1025)))
	write(t, dir, "skills/stray-file.md", "a file directly under skills/ is ignored")
	if err := os.Mkdir(filepath.Join(dir, "skills", "md-is-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "skills", "md-is-dir", "SKILL.md"), 0o755); err != nil {
		t.Fatal(err)
	}

	skills, diags := DiscoverSkills(dir)
	var got []string
	for _, s := range skills {
		got = append(got, s.Name)
	}
	if want := "crlf deploy quoted"; strings.Join(got, " ") != want {
		t.Errorf("skills = %v, want %s", got, want)
	}
	if skills[2].Dir != "skills/quoted" || skills[2].Description != "Single quoted." {
		t.Errorf("quoted = %+v", skills[2])
	}

	wantSkipped := []string{"skills/Bad_Name", "skills/long-desc", "skills/md-is-dir", "skills/mismatch", "skills/no-desc", "skills/no-fm", "skills/no-md"}
	var skipped []string
	for _, d := range diags {
		if !strings.HasPrefix(d.Message, "skipped: ") {
			t.Errorf("diagnostic %v is not a skip report", d)
		}
		skipped = append(skipped, strings.TrimSuffix(d.Path, "/SKILL.md"))
	}
	if strings.Join(skipped, " ") != strings.Join(wantSkipped, " ") {
		t.Errorf("skipped = %v\nwant    %v", skipped, wantSkipped)
	}
}

func TestDiscoverSkills_ComponentBoundaries(t *testing.T) {
	t.Run("absent skills dir is valid absence", func(t *testing.T) {
		if s, d := DiscoverSkills(t.TempDir()); s != nil || d != nil {
			t.Errorf("got %v %v", s, d)
		}
	})
	t.Run("skills is a file: component disabled, not an error", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "skills", "oops")
		s, d := DiscoverSkills(dir)
		if len(s) != 0 || len(d) != 1 || !strings.Contains(d[0].Message, "not a directory") {
			t.Errorf("got %v %v", s, d)
		}
	})
	t.Run("SKILL.md symlinked outside the root is skipped", func(t *testing.T) {
		outside := t.TempDir()
		write(t, outside, "SKILL.md", skill("esc", "Escapes."))
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "skills", "esc"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(outside, "SKILL.md"), filepath.Join(dir, "skills", "esc", "SKILL.md")); err != nil {
			t.Skip("symlinks unavailable:", err)
		}
		s, d := DiscoverSkills(dir)
		if len(s) != 0 || len(d) != 1 || !strings.Contains(d[0].Message, "outside the plugin root") {
			t.Errorf("got %v %v", s, d)
		}
	})
}

func TestValidSkillName(t *testing.T) {
	for name, want := range map[string]bool{
		"pdf-processing": true, "a": true, "a1-b2": true,
		"PDF-Processing": false, "-pdf": false, "pdf-": false, "pdf--processing": false, "": false,
		"under_score": false, strings.Repeat("a", 64): true, strings.Repeat("a", 65): false,
	} {
		if got := validSkillName(name); got != want {
			t.Errorf("%q: got %v want %v", name, got, want)
		}
	}
}

func TestFrontmatter_Edges(t *testing.T) {
	for name, tc := range map[string]struct {
		in   string
		want map[string]string // nil means "no frontmatter"
	}{
		"no opening":         {"name: x\n", nil},
		"unclosed":           {"---\nname: x\n", nil},
		"closed at eof":      {"---\nname: x\n---", map[string]string{"name": "x"}},
		"empty block":        {"---\n---\nbody", map[string]string{}},
		"bom and comments":   {"\ufeff---\n# c\nname: x\n  nested: y\n---\n", map[string]string{"name": "x"}},
		"colon in value":     {"---\ndescription: Use when: always\n---\n", map[string]string{"description": "Use when: always"}},
		"line without colon": {"---\njunk\nname: x\n---\n", map[string]string{"name": "x"}},
	} {
		t.Run(name, func(t *testing.T) {
			got := frontmatter(tc.in)
			if (got == nil) != (tc.want == nil) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("%s = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}
