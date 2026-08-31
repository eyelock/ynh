package exporter

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/eyelock/ynh/internal/resolver"
)

// fakeGenerator stands in for a vendor adapter. WriteSystemPrompt takes the
// narrow SystemPromptGenerator interface precisely so this is possible.
type fakeGenerator struct{ files map[string][]byte }

func (f fakeGenerator) GenerateSystemPrompt(content []byte) map[string][]byte {
	if f.files != nil {
		return f.files
	}
	return map[string][]byte{"AGENTS.md": content}
}

func writeFile(t *testing.T, path, body string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func lsRel(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() {
			return nil
		}
		rel, rErr := filepath.Rel(root, p)
		if rErr != nil {
			return rErr
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	sort.Strings(out)
	return out
}

func TestWriteSystemPrompt_WritesWhatTheAdapterReturns(t *testing.T) {
	src := writeFile(t, filepath.Join(t.TempDir(), "instructions.md"), "# Rules\n")
	out := t.TempDir()

	gen := fakeGenerator{files: map[string][]byte{
		"AGENTS.md":              []byte("# Rules\n"),
		"CLAUDE.md":              []byte("@AGENTS.md\n"),
		".cursor/rules/main.mdc": []byte("nested\n"),
	}}
	if err := WriteSystemPrompt(src, out, gen); err != nil {
		t.Fatalf("WriteSystemPrompt: %v", err)
	}

	got := lsRel(t, out)
	want := []string{".cursor/rules/main.mdc", "AGENTS.md", "CLAUDE.md"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("wrote %v, want %v", got, want)
	}
	// A nested target must have had its parent directory created.
	body, err := os.ReadFile(filepath.Join(out, ".cursor", "rules", "main.mdc"))
	if err != nil {
		t.Fatalf("nested file not written: %v", err)
	}
	if string(body) != "nested\n" {
		t.Errorf("nested content = %q", body)
	}
}

func TestWriteSystemPrompt_PassesFileContentThrough(t *testing.T) {
	src := writeFile(t, filepath.Join(t.TempDir(), "instructions.md"), "distinctive body\n")
	out := t.TempDir()

	if err := WriteSystemPrompt(src, out, fakeGenerator{}); err != nil {
		t.Fatalf("WriteSystemPrompt: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(out, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "distinctive body\n" {
		t.Errorf("the adapter was handed %q", body)
	}
}

func TestWriteSystemPrompt_MissingInstructionsIsAnError(t *testing.T) {
	err := WriteSystemPrompt(filepath.Join(t.TempDir(), "absent.md"), t.TempDir(), fakeGenerator{})
	if err == nil {
		t.Fatal("expected an error for a missing instructions file")
	}
	if !strings.Contains(err.Error(), "reading instructions") {
		t.Errorf("error should say what it was doing, got: %v", err)
	}
}

// Merged export writes one tree for several vendors. Files they share are
// deduplicated; files unique to one vendor must all survive.
func TestWriteMergedSystemPrompt_UnionsVendorFiles(t *testing.T) {
	src := writeFile(t, filepath.Join(t.TempDir(), "instructions.md"), "shared\n")
	out := t.TempDir()

	if err := WriteMergedSystemPrompt(src, out, []string{"claude", "codex", "cursor"}); err != nil {
		t.Fatalf("WriteMergedSystemPrompt: %v", err)
	}
	got := lsRel(t, out)

	// AGENTS.md is common to all three and must appear once.
	// CLAUDE.md is Claude's alone, .cursorrules is Cursor's alone.
	for _, want := range []string{"AGENTS.md", "CLAUDE.md", ".cursorrules"} {
		if !containsStr(got, want) {
			t.Errorf("merged output is missing %s: %v", want, got)
		}
	}
	n := 0
	for _, g := range got {
		if g == "AGENTS.md" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("AGENTS.md should appear once, appeared %d times", n)
	}
}

// An unknown vendor is skipped rather than failing the export. That is a
// deliberate choice and worth pinning: a typo in a vendor name silently
// contributes nothing rather than stopping the build.
func TestWriteMergedSystemPrompt_SkipsUnknownVendor(t *testing.T) {
	src := writeFile(t, filepath.Join(t.TempDir(), "instructions.md"), "x\n")
	out := t.TempDir()

	if err := WriteMergedSystemPrompt(src, out, []string{"claude", "nosuchvendor"}); err != nil {
		t.Fatalf("an unknown vendor must not fail the export: %v", err)
	}
	if got := lsRel(t, out); len(got) == 0 {
		t.Error("the known vendor's files should still have been written")
	}
}

func TestWriteMergedSystemPrompt_NoVendorsWritesNothing(t *testing.T) {
	src := writeFile(t, filepath.Join(t.TempDir(), "instructions.md"), "x\n")
	out := t.TempDir()

	if err := WriteMergedSystemPrompt(src, out, nil); err != nil {
		t.Fatalf("WriteMergedSystemPrompt: %v", err)
	}
	if got := lsRel(t, out); len(got) != 0 {
		t.Errorf("no vendors should write no files, got %v", got)
	}
}

func TestWriteMergedSystemPrompt_MissingInstructionsIsAnError(t *testing.T) {
	err := WriteMergedSystemPrompt(filepath.Join(t.TempDir(), "gone.md"), t.TempDir(), []string{"claude"})
	if err == nil {
		t.Fatal("expected an error for a missing instructions file")
	}
}

// DiscoverInstructions prefers instructions.md over AGENTS.md within one
// source, and lets a later source override an earlier one, so a harness's own
// instructions beat those of anything it includes.
func TestDiscoverInstructions(t *testing.T) {
	early := t.TempDir()
	late := t.TempDir()
	writeFile(t, filepath.Join(early, "AGENTS.md"), "early\n")
	writeFile(t, filepath.Join(late, "instructions.md"), "late\n")

	got := DiscoverInstructions([]resolver.ResolvedContent{
		{BasePath: early}, {BasePath: late},
	})
	if got != filepath.Join(late, "instructions.md") {
		t.Errorf("later source should win, got %q", got)
	}

	// Reversed, the later source has only AGENTS.md and still wins.
	got = DiscoverInstructions([]resolver.ResolvedContent{
		{BasePath: late}, {BasePath: early},
	})
	if got != filepath.Join(early, "AGENTS.md") {
		t.Errorf("later source should win regardless of filename, got %q", got)
	}
}

func TestDiscoverInstructions_PrefersInstructionsOverAgents(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "AGENTS.md"), "agents\n")
	writeFile(t, filepath.Join(dir, "instructions.md"), "instructions\n")

	if got := DiscoverInstructions([]resolver.ResolvedContent{{BasePath: dir}}); got != filepath.Join(dir, "instructions.md") {
		t.Errorf("instructions.md should win within one source, got %q", got)
	}
}

func TestDiscoverInstructions_NoneFound(t *testing.T) {
	if got := DiscoverInstructions([]resolver.ResolvedContent{{BasePath: t.TempDir()}}); got != "" {
		t.Errorf("expected empty string when nothing is found, got %q", got)
	}
	if got := DiscoverInstructions(nil); got != "" {
		t.Errorf("expected empty string for no content, got %q", got)
	}
}

func containsStr(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}
