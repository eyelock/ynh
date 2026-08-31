package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeReadsHarness builds a harness whose shape mirrors a real one: a skill
// bundle with a references file, a flat agent, and the non-artifact
// directories that made the historical defects possible.
func writeReadsHarness(t *testing.T, reads map[string][]string) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{
		".ynh-plugin",
		"skills/demo/references",
		"agents",
		"docs/tutorial",     // exists here, never ships
		"testdata/fixtures", // exists here, never ships
	} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{
		"skills/demo/SKILL.md",
		"skills/demo/references/guide.md",
		"agents/helper.md",
		"docs/tutorial/intro.md",
		"testdata/fixtures/sample.json",
		"README.md",
	} {
		if err := os.WriteFile(filepath.Join(root, f), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := map[string]any{"name": "demo", "version": "1.0.0"}
	if reads != nil {
		m["reads"] = reads
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".ynh-plugin", "plugin.json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func messages(issues []lintIssue) string {
	var b strings.Builder
	for _, i := range issues {
		b.WriteString(i.Message)
		b.WriteString("\n")
	}
	return b.String()
}

// The three defects this check exists for. Each path is present in the
// authoring repo and absent from what ships, which is exactly why reading
// the text could not tell them apart.
func TestDeclaredReads_CatchesTheHistoricalDefects(t *testing.T) {
	cases := []struct {
		name, artifact, read, want string
	}{
		{"agent reading docs (#249)", "agents/helper.md", "docs/tutorial/", "not inside a shipping artifact"},
		{"skill reading testdata (#256)", "skills/demo", "testdata/fixtures/sample.json", "not inside a shipping artifact"},
		{"prompt reading README (#276)", "skills/demo", "README.md", "not inside a shipping artifact"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := writeReadsHarness(t, map[string][]string{tc.artifact: {tc.read}})
			got := messages(lintDeclaredReads(p))
			if !strings.Contains(got, tc.want) {
				t.Errorf("expected an issue containing %q, got:\n%s", tc.want, got)
			}
			// The path really does exist in the tree — the check must not be
			// passing merely because the file is missing.
			if _, err := os.Stat(filepath.Join(filepath.Dir(filepath.Dir(p)), tc.read)); err != nil {
				t.Fatalf("fixture is vacuous: %s does not exist, so the "+
					"check would fire for the wrong reason: %v", tc.read, err)
			}
		})
	}
}

func TestDeclaredReads_AcceptsAShippedPath(t *testing.T) {
	p := writeReadsHarness(t, map[string][]string{
		"skills/demo": {"skills/demo/references/guide.md"},
	})
	if issues := lintDeclaredReads(p); len(issues) != 0 {
		t.Errorf("expected no issues for a path inside the skill bundle, got:\n%s", messages(issues))
	}
}

func TestDeclaredReads_SilentWhenUndeclared(t *testing.T) {
	for _, reads := range []map[string][]string{nil, {}} {
		if issues := lintDeclaredReads(writeReadsHarness(t, reads)); len(issues) != 0 {
			t.Errorf("undeclared must be silent, got:\n%s", messages(issues))
		}
	}
}

func TestDeclaredReads_RejectsBadDeclarations(t *testing.T) {
	cases := []struct {
		name     string
		reads    map[string][]string
		wantPart string
	}{
		{"stale artifact key", map[string][]string{"skills/gone": {"skills/demo/SKILL.md"}}, "no such artifact"},
		{"key is not an artifact", map[string][]string{"docs/x.md": {"skills/demo/SKILL.md"}}, "not an artifact path"},
		{"read missing from bundle", map[string][]string{"skills/demo": {"skills/demo/references/absent.md"}}, "no such file"},
		{"absolute path", map[string][]string{"skills/demo": {"/etc/passwd"}}, "relative to the harness root"},
		{"escapes root", map[string][]string{"skills/demo": {"../outside.md"}}, "escapes the harness root"},
		{"empty path", map[string][]string{"skills/demo": {""}}, "path is empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := messages(lintDeclaredReads(writeReadsHarness(t, tc.reads)))
			if !strings.Contains(got, tc.wantPart) {
				t.Errorf("expected %q, got:\n%s", tc.wantPart, got)
			}
		})
	}
}

// shipped() derives its roots from internal/harness. If a new artifact type is
// added there, this check must follow rather than silently ignoring it.
func TestDeclaredReads_TracksArtifactRoots(t *testing.T) {
	for _, dir := range []string{"skills", "agents", "rules", "commands"} {
		if !shipped(dir + "/thing.md") {
			t.Errorf("%s/ should be a shipping artifact root", dir)
		}
	}
	for _, dir := range []string{"docs", "testdata", "scripts", "internal"} {
		if shipped(dir + "/thing.md") {
			t.Errorf("%s/ must not count as a shipping artifact root", dir)
		}
	}
}
