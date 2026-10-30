package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitRepo(t *testing.T, ignore string) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if ignore != "" {
		writeFile(t, filepath.Join(dir, ".gitignore"), []byte(ignore))
	}
	return dir
}

// Discovery must not wander into files git was told to ignore.
//
// `.gitignore` here excludes `.claude/plans/` so a contributor can keep local
// notes beside the harness. Because `make check` runs `ynd lint .claude`, those
// notes could fail the project's own gate — which happened, over two blank
// lines in a private file, unrelated to the change being made.
func TestDiscover_SkipsGitignoredFiles(t *testing.T) {
	dir := gitRepo(t, "notes/\n")
	writeFile(t, filepath.Join(dir, "kept.md"), []byte("# kept\n"))
	writeFile(t, filepath.Join(dir, "notes", "private.md"), []byte("# private\n"))

	found, err := discoverAll(dir, []string{".md"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range found {
		names = append(names, filepath.Base(f))
	}
	joined := strings.Join(names, " ")
	if !strings.Contains(joined, "kept.md") {
		t.Errorf("tracked file missing from discovery: %v", names)
	}
	if strings.Contains(joined, "private.md") {
		t.Errorf("discovery reached a gitignored file: %v", names)
	}
}

// Naming a file outright is a clear instruction. The filter exists to stop a
// walk wandering into private notes, not to veto a direct request.
func TestDiscover_ExplicitlyNamedIgnoredFileIsHonoured(t *testing.T) {
	dir := gitRepo(t, "notes/\n")
	target := filepath.Join(dir, "notes", "private.md")
	writeFile(t, target, []byte("# private\n"))

	found, err := discoverAll(target, []string{".md"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0] != target {
		t.Errorf("an explicitly named file should be returned, got %v", found)
	}
}

// Outside a work tree there are no rules to consult, and every path stands.
// Failing open is the safe direction: this narrows what the tools touch, so a
// missing git or a plain directory must not stop `ynd lint` working.
func TestDiscover_OutsideAGitRepoNothingIsFiltered(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.md"), []byte("# a\n"))
	writeFile(t, filepath.Join(dir, "sub", "b.md"), []byte("# b\n"))

	found, err := discoverAll(dir, []string{".md"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 {
		t.Errorf("expected both files outside a repo, got %v", found)
	}
}

// Nested .gitignore files and negations are git's to resolve, not ours — the
// reason this asks git rather than matching patterns itself.
func TestDiscover_RespectsNestedIgnoreAndNegation(t *testing.T) {
	dir := gitRepo(t, "*.md\n!keep.md\n")
	writeFile(t, filepath.Join(dir, "keep.md"), []byte("# keep\n"))
	writeFile(t, filepath.Join(dir, "drop.md"), []byte("# drop\n"))

	found, err := discoverAll(dir, []string{".md"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range found {
		names = append(names, filepath.Base(f))
	}
	joined := strings.Join(names, " ")
	if !strings.Contains(joined, "keep.md") {
		t.Errorf("negated pattern should keep the file: %v", names)
	}
	if strings.Contains(joined, "drop.md") {
		t.Errorf("ignored file survived: %v", names)
	}
}

// .yndignore lists tracked paths ynd must not walk, such as eval fixtures that
// are deliberately broken. lint, validate and fmt all honour it (#544).
func TestYndIgnore_Walks(t *testing.T) {
	cases := []struct {
		name    string
		ignore  string
		inRepo  bool
		skipped []string // relative paths that must not be found
		kept    []string
	}{
		{"directory pattern", "evals/fixtures/\n", true,
			[]string{"evals/fixtures/broken/SKILL.md", "evals/fixtures/x.md"},
			[]string{"README.md", "evals/real.md"}},
		{"outside a git repository", "evals/fixtures/\n", false,
			[]string{"evals/fixtures/broken/SKILL.md"},
			[]string{"README.md"}},
		{"glob and negation", "*.bad.md\n!keep.bad.md\n", true,
			[]string{"a.bad.md", "evals/b.bad.md"},
			[]string{"keep.bad.md", "README.md"}},
		{"anchored pattern only matches at the root", "/top.md\n", true,
			[]string{"top.md"},
			[]string{"evals/top.md"}},
		{"comments and blanks", "# deliberately broken\n\nbroken/\n", true,
			[]string{"broken/x.md"},
			[]string{"README.md"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if c.inRepo {
				dir = gitRepo(t, "")
			}
			all := append(append([]string{}, c.skipped...), c.kept...)
			for _, f := range all {
				writeFile(t, filepath.Join(dir, f), []byte("# x\n"))
			}
			writeFile(t, filepath.Join(dir, yndIgnoreFile), []byte(c.ignore))

			found, err := discoverAll(dir, []string{".md"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]bool{}
			for _, f := range found {
				rel, _ := filepath.Rel(dir, f)
				got[filepath.ToSlash(rel)] = true
			}
			for _, f := range c.skipped {
				if got[f] {
					t.Errorf("%s should be ignored, walk found %v", f, got)
				}
			}
			for _, f := range c.kept {
				if !got[f] {
					t.Errorf("%s should be found, walk found %v", f, got)
				}
			}
		})
	}
}

// The three commands agree: validate skips an ignored harness, fmt does not
// rewrite an ignored file, lint does not report it.
func TestYndIgnore_LintValidateFmtAgree(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good")
	bad := filepath.Join(dir, "evals", "fixtures", "broken")
	writeFile(t, filepath.Join(good, "plugin-marker"), []byte("x"))
	if err := writePluginJSONFile(good, []byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","name":"good","version":"0.1.0"}`)); err != nil {
		t.Fatal(err)
	}
	// Invalid: no name.
	if err := writePluginJSONFile(bad, []byte(`{"$schema":"https://eyelock.github.io/ynh/schema/plugin.schema.json","version":"0.1.0"}`)); err != nil {
		t.Fatal(err)
	}
	messy := filepath.Join(dir, "evals", "fixtures", "messy.md")
	writeFile(t, messy, []byte("# t\n\n\n\nbody\n"))
	writeFile(t, filepath.Join(dir, yndIgnoreFile), []byte("evals/fixtures/\n"))

	if err := cmdValidate([]string{dir}); err != nil {
		t.Errorf("validate walked an ignored harness: %v", err)
	}
	if err := cmdFmt([]string{dir}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(messy)
	if string(data) != "# t\n\n\n\nbody\n" {
		t.Errorf("fmt rewrote an ignored file: %q", data)
	}
	files, err := discoverAll(dir, []string{".md", ".sh"}, []string{"plugin.json"})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.Contains(f, "fixtures") {
			t.Errorf("lint discovery found ignored file %s", f)
		}
	}
}

// A path named outright is honoured, as with .gitignore.
func TestYndIgnore_ExplicitFileHonoured(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "broken.md")
	writeFile(t, f, []byte("# x\n"))
	writeFile(t, filepath.Join(dir, yndIgnoreFile), []byte("broken.md\n"))
	found, err := discoverFiles(f, []string{".md"})
	if err != nil || len(found) != 1 {
		t.Fatalf("explicit file dropped: %v %v", found, err)
	}
}
