package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFormatMarkdownContent_TrailingWhitespace(t *testing.T) {
	input := "hello   \nworld  \n"
	want := "hello\nworld\n"

	got := formatMarkdownContent(input)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatMarkdownContent_MultipleBlankLines(t *testing.T) {
	input := "hello\n\n\n\nworld\n"
	want := "hello\n\nworld\n"

	got := formatMarkdownContent(input)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatMarkdownContent_NoTrailingNewline(t *testing.T) {
	input := "hello"
	want := "hello\n"

	got := formatMarkdownContent(input)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatMarkdownContent_MultipleTrailingNewlines(t *testing.T) {
	input := "hello\n\n\n"
	want := "hello\n"

	got := formatMarkdownContent(input)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatMarkdownContent_PreservesFrontmatter(t *testing.T) {
	input := "---\nname: test\n---\nhello   \n"
	want := "---\nname: test\n---\nhello\n"

	got := formatMarkdownContent(input)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestFormatMarkdownContent_AlreadyFormatted(t *testing.T) {
	input := "# Hello\n\nThis is clean.\n"

	got := formatMarkdownContent(input)
	if got != input {
		t.Errorf("expected no changes, got %q", got)
	}
}

func TestFormatMarkdown_WritesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	if err := os.WriteFile(path, []byte("hello   \n"), 0o644); err != nil {
		t.Fatal(err)
	}

	modified, err := formatMarkdown(path)
	if err != nil {
		t.Fatalf("formatMarkdown failed: %v", err)
	}
	if !modified {
		t.Error("expected file to be modified")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello\n" {
		t.Errorf("file content = %q, want %q", string(data), "hello\n")
	}
}

func TestFormatMarkdown_NoChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	if err := os.WriteFile(path, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	modified, err := formatMarkdown(path)
	if err != nil {
		t.Fatalf("formatMarkdown failed: %v", err)
	}
	if modified {
		t.Error("expected no modification")
	}
}

func TestSplitFrontmatter_WithFrontmatter(t *testing.T) {
	input := "---\nname: test\n---\nBody here.\n"
	fm, body := splitFrontmatter(input)

	if fm != "---\nname: test\n---\n" {
		t.Errorf("frontmatter = %q", fm)
	}
	if body != "Body here.\n" {
		t.Errorf("body = %q", body)
	}
}

func TestSplitFrontmatter_NoFrontmatter(t *testing.T) {
	input := "# Just a heading\n\nBody here.\n"
	fm, body := splitFrontmatter(input)

	if fm != "" {
		t.Errorf("frontmatter = %q, want empty", fm)
	}
	if body != input {
		t.Errorf("body = %q, want original", body)
	}
}

func TestSplitFrontmatter_UnclosedFrontmatter(t *testing.T) {
	input := "---\nname: test\nno closing\n"
	fm, body := splitFrontmatter(input)

	if fm != "" {
		t.Errorf("frontmatter = %q, want empty (unclosed)", fm)
	}
	if body != input {
		t.Errorf("body = %q, want original", body)
	}
}

func TestSplitFrontmatter_FrontmatterOnly(t *testing.T) {
	input := "---\nname: test\n---\n"
	fm, body := splitFrontmatter(input)

	if fm != "---\nname: test\n---\n" {
		t.Errorf("frontmatter = %q", fm)
	}
	if body != "" {
		t.Errorf("body = %q, want empty", body)
	}
}

func TestSplitFrontmatter_NoTrailingNewline(t *testing.T) {
	input := "---\nname: test\n---"
	fm, body := splitFrontmatter(input)

	if fm != "---\nname: test\n---" {
		t.Errorf("frontmatter = %q", fm)
	}
	if body != "" {
		t.Errorf("body = %q, want empty", body)
	}
}

func TestCmdFmt_NoFiles(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	err := cmdFmt(nil)
	if err != nil {
		t.Errorf("expected no error for empty dir, got %v", err)
	}
}

func TestCmdFmt_NoChanges(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := os.WriteFile(filepath.Join(dir, "clean.md"), []byte("# Clean\n\nAlready formatted.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := cmdFmt(nil)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestCmdFmt_WithChanges(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := os.WriteFile(filepath.Join(dir, "dirty.md"), []byte("hello   \n\n\n\nworld\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := cmdFmt(nil)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "dirty.md"))
	if string(data) != "hello\n\nworld\n" {
		t.Errorf("file content = %q, want %q", string(data), "hello\n\nworld\n")
	}
}

func TestCmdFmt_SingleFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.md")
	if err := os.WriteFile(path, []byte("hello   \n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := cmdFmt([]string{path})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "hello\n" {
		t.Errorf("file content = %q, want %q", string(data), "hello\n")
	}
}

func TestFormatMarkdown_ReadError(t *testing.T) {
	_, err := formatMarkdown("/nonexistent/path/file.md")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestCmdFmt_FormatError(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// Create a file then make it read-only to trigger write error
	path := filepath.Join(dir, "readonly.md")
	if err := os.WriteFile(path, []byte("dirty   \n"), 0o444); err != nil {
		t.Fatal(err)
	}

	// The file is read-only but we can still read it. formatMarkdown will
	// try to write it and fail. cmdFmt prints the error and continues.
	err := cmdFmt(nil)
	// cmdFmt doesn't return an error for individual file write failures
	_ = err
}

func TestSplitFrontmatter_CutExceedsLength(t *testing.T) {
	// Edge case where frontmatter closing --- is at the very end with no trailing content
	input := "---\nk: v\n---"
	fm, body := splitFrontmatter(input)
	if fm == "" {
		t.Error("expected frontmatter to be found")
	}
	if body != "" {
		t.Errorf("body = %q, want empty", body)
	}
}
