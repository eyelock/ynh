package assembler

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eyelock/ynh/internal/resolver"
)

// source makes a content directory holding the named files.
func source(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestComposeInstructions(t *testing.T) {
	root := source(t, map[string]string{"instructions.md": "Root rules.\n"})
	a := source(t, map[string]string{"instructions.md": "A rules.\n"})
	b := source(t, map[string]string{"AGENTS.md": "B rules.\n"})
	none := source(t, map[string]string{"skills/x/SKILL.md": "x"})
	rootless := source(t, nil)

	tests := []struct {
		name    string
		content []resolver.ResolvedContent
		want    string
	}{
		{
			name: "root then includes in include order, each headed",
			content: []resolver.ResolvedContent{
				{BasePath: a, Label: "org/a"},
				{BasePath: b, Label: "org/b/sub"},
				{BasePath: root},
			},
			want: "Root rules.\n\n## Instructions from org/a\n\nA rules.\n\n## Instructions from org/b/sub\n\nB rules.\n",
		},
		{
			name: "the same include reached twice appears once",
			content: []resolver.ResolvedContent{
				{BasePath: a, Label: "org/a"},
				{BasePath: a, Label: "org/a"},
				{BasePath: root},
			},
			want: "Root rules.\n\n## Instructions from org/a\n\nA rules.\n",
		},
		{
			name: "instructions false leaves an include out",
			content: []resolver.ResolvedContent{
				{BasePath: a, Label: "org/a", NoInstructions: true},
				{BasePath: b, Label: "org/b"},
				{BasePath: root},
			},
			want: "Root rules.\n\n## Instructions from org/b\n\nB rules.\n",
		},
		{
			name: "root without instructions leaves only the include sections",
			content: []resolver.ResolvedContent{
				{BasePath: a, Label: "org/a"},
				{BasePath: none, Label: "org/none"},
				{BasePath: rootless},
			},
			want: "## Instructions from org/a\n\nA rules.\n",
		},
		{
			name:    "a lone root is returned unchanged",
			content: []resolver.ResolvedContent{{BasePath: root}},
			want:    "Root rules.\n",
		},
		{
			name:    "nothing anywhere gives nothing",
			content: []resolver.ResolvedContent{{BasePath: none, Label: "org/none"}, {BasePath: rootless}},
			want:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(ComposeInstructions(tt.content)); got != tt.want {
				t.Errorf("got:\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}

func TestAssembleComposesInstructions(t *testing.T) {
	root := source(t, map[string]string{"instructions.md": "Root rules.\n"})
	a := source(t, map[string]string{"instructions.md": "A rules.\n"})
	b := source(t, map[string]string{"instructions.md": "B rules.\n"})

	workDir, err := Assemble(&mockAdapter{}, []resolver.ResolvedContent{
		{BasePath: a, Label: "org/a"},
		{BasePath: b, Label: "org/b"},
		{BasePath: root},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer Cleanup(workDir)

	data, err := os.ReadFile(filepath.Join(workDir, "MOCK.md"))
	if err != nil {
		t.Fatal(err)
	}
	want := "Root rules.\n\n## Instructions from org/a\n\nA rules.\n\n## Instructions from org/b\n\nB rules.\n"
	if string(data) != want {
		t.Errorf("got %q, want %q", data, want)
	}
}

// A skill's evals/ directory is authoring-time material: assembly leaves it
// out and keeps the skill's other files, for a whole include and for a pick.
func TestAssembleSkipsSkillEvals(t *testing.T) {
	files := map[string]string{
		"skills/s/SKILL.md":                 "skill",
		"skills/s/references/guide.md":      "guide",
		"skills/s/evals/evals.json":         "{}",
		"skills/s/references/evals/kept.md": "deeper evals dirs are kept",
	}
	tests := []struct {
		name  string
		paths []string
	}{
		{"whole include", nil},
		{"picked skill", []string{"skills/s"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := source(t, files)
			workDir, err := Assemble(&mockAdapter{}, []resolver.ResolvedContent{{BasePath: repo, Paths: tt.paths}})
			if err != nil {
				t.Fatal(err)
			}
			defer Cleanup(workDir)
			skill := filepath.Join(workDir, ".mock", "skills", "s")
			for _, kept := range []string{"SKILL.md", "references/guide.md", "references/evals/kept.md"} {
				if _, err := os.Stat(filepath.Join(skill, kept)); err != nil {
					t.Errorf("%s should be kept: %v", kept, err)
				}
			}
			if _, err := os.Stat(filepath.Join(skill, "evals")); !os.IsNotExist(err) {
				t.Errorf("skill evals/ should be skipped, stat err = %v", err)
			}
		})
	}
}
