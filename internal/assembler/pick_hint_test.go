package assembler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A missing pick whose directory exists elsewhere in the include names where it
// is and suggests the --path that would fix it (#587).
func TestCopyPicked_MissingPickSuggestsWhereItIs(t *testing.T) {
	tests := []struct {
		name     string
		make     []string // directories to create under the include
		picked   string
		wantAll  []string
		wantNone []string
	}{
		{
			name:   "same-named directory under a parent directory",
			make:   []string{"integrations/skills/ynm-memory"},
			picked: "skills/ynm-memory",
			wantAll: []string{
				"pick path not found: skills/ynm-memory",
				`"ynm-memory" exists at "integrations/skills/ynm-memory"`,
				"ynh include update <harness> <url> --path integrations",
			},
		},
		{
			name:   "nested parent gives the whole prefix",
			make:   []string{"a/b/skills/x"},
			picked: "skills/x",
			wantAll: []string{
				"--path a/b",
			},
		},
		{
			name:   "same name but not the same layout names the place only",
			make:   []string{"misc/ynm-memory"},
			picked: "skills/ynm-memory",
			wantAll: []string{
				`exists at "misc/ynm-memory"`,
				"relative to the include's path",
			},
			wantNone: []string{"--path"},
		},
		{
			name:     "nothing of that name gives no hint",
			make:     []string{"skills/other"},
			picked:   "skills/ynm-memory",
			wantNone: []string{"exists at", "--path"},
		},
		{
			name:     ".git contents are not suggested",
			make:     []string{".git/skills/ynm-memory"},
			picked:   "skills/ynm-memory",
			wantNone: []string{"exists at", "--path"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			repoBase := filepath.Join(root, "repo")
			targetBase := filepath.Join(root, "out")
			for _, d := range append(tt.make, "") {
				if err := os.MkdirAll(filepath.Join(repoBase, filepath.FromSlash(d)), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.MkdirAll(targetBase, 0o755); err != nil {
				t.Fatal(err)
			}
			err := CopyPicked(repoBase, tt.picked, targetBase,
				map[string]string{"skills": "skills"}, nil)
			if err == nil {
				t.Fatal("expected an error for a missing pick")
			}
			for _, w := range tt.wantAll {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error missing %q:\n%v", w, err)
				}
			}
			for _, w := range tt.wantNone {
				if strings.Contains(err.Error(), w) {
					t.Errorf("error should not contain %q:\n%v", w, err)
				}
			}
		})
	}
}
