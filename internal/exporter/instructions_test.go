package exporter

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
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
	src := []byte("# Rules\n")
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
	src := []byte("distinctive body\n")
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

// Merged export writes one tree for several vendors. Files they share are
// deduplicated; files unique to one vendor must all survive.
func TestWriteMergedSystemPrompt_UnionsVendorFiles(t *testing.T) {
	src := []byte("shared\n")
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
	src := []byte("x\n")
	out := t.TempDir()

	if err := WriteMergedSystemPrompt(src, out, []string{"claude", "nosuchvendor"}); err != nil {
		t.Fatalf("an unknown vendor must not fail the export: %v", err)
	}
	if got := lsRel(t, out); len(got) == 0 {
		t.Error("the known vendor's files should still have been written")
	}
}

func TestWriteMergedSystemPrompt_NoVendorsWritesNothing(t *testing.T) {
	src := []byte("x\n")
	out := t.TempDir()

	if err := WriteMergedSystemPrompt(src, out, nil); err != nil {
		t.Fatalf("WriteMergedSystemPrompt: %v", err)
	}
	if got := lsRel(t, out); len(got) != 0 {
		t.Errorf("no vendors should write no files, got %v", got)
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
