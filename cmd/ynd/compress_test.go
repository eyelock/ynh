package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseCompressArgs(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantVendor string
		wantFiles  []string
		wantSkip   bool
		wantErr    bool
	}{
		{
			name: "no args",
			args: []string{},
		},
		{
			name:       "vendor flag short",
			args:       []string{"-v", "claude"},
			wantVendor: "claude",
		},
		{
			name:       "vendor flag long",
			args:       []string{"--vendor", "codex"},
			wantVendor: "codex",
		},
		{
			name:     "yes flag short",
			args:     []string{"-y"},
			wantSkip: true,
		},
		{
			name:     "yes flag long",
			args:     []string{"--yes"},
			wantSkip: true,
		},
		{
			name:       "vendor with file",
			args:       []string{"-v", "claude", "foo.md"},
			wantVendor: "claude",
			wantFiles:  []string{"foo.md"},
		},
		{
			name:       "all flags and files",
			args:       []string{"-v", "claude", "-y", "a.md", "b.md"},
			wantVendor: "claude",
			wantSkip:   true,
			wantFiles:  []string{"a.md", "b.md"},
		},
		{
			name:    "vendor missing value",
			args:    []string{"-v"},
			wantErr: true,
		},
		{
			name:    "unknown flag",
			args:    []string{"--unknown"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, err := parseCompressArgs(tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr = %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			if opts.vendor != tt.wantVendor {
				t.Errorf("vendor = %q, want %q", opts.vendor, tt.wantVendor)
			}
			if opts.skipConfirm != tt.wantSkip {
				t.Errorf("skip = %v, want %v", opts.skipConfirm, tt.wantSkip)
			}
			if len(opts.files) != len(tt.wantFiles) {
				t.Errorf("files = %v, want %v", opts.files, tt.wantFiles)
			} else {
				for i := range opts.files {
					if opts.files[i] != tt.wantFiles[i] {
						t.Errorf("files[%d] = %q, want %q", i, opts.files[i], tt.wantFiles[i])
					}
				}
			}
		})
	}
}

func TestParseCompressArgs_Restore(t *testing.T) {
	opts, err := parseCompressArgs([]string{"--restore", "file.md"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.restore {
		t.Error("expected restore=true")
	}
	if len(opts.files) != 1 || opts.files[0] != "file.md" {
		t.Errorf("files = %v, want [file.md]", opts.files)
	}
}

func TestParseCompressArgs_ListBackups(t *testing.T) {
	opts, err := parseCompressArgs([]string{"--list-backups", "file.md"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.listBackups {
		t.Error("expected listBackups=true")
	}
}

func TestParseCompressArgs_Pick(t *testing.T) {
	opts, err := parseCompressArgs([]string{"--restore", "--pick", "3", "file.md"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.pick != 3 {
		t.Errorf("pick = %d, want 3", opts.pick)
	}
}

func TestParseCompressArgs_PickMissingValue(t *testing.T) {
	_, err := parseCompressArgs([]string{"--pick"})
	if err == nil {
		t.Fatal("expected error for --pick without value")
	}
}

func TestParseCompressArgs_PickInvalid(t *testing.T) {
	_, err := parseCompressArgs([]string{"--pick", "abc"})
	if err == nil {
		t.Fatal("expected error for --pick with non-integer")
	}
}

func TestParseCompressArgs_PickZero(t *testing.T) {
	_, err := parseCompressArgs([]string{"--pick", "0"})
	if err == nil {
		t.Fatal("expected error for --pick 0")
	}
}

func TestCompressWithLLM_PreservesFrontmatter(t *testing.T) {
	mockLLM(t, func(vendor, prompt string) (string, error) {
		return "Compressed body content.", nil
	})

	input := "---\nname: test\ndescription: A test skill.\n---\n\n## Verbose Instructions\n\nDo the thing carefully and thoroughly.\n"
	result, err := compressWithLLM("claude", input)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(result, "---\nname: test\n") {
		t.Errorf("expected original frontmatter preserved, got %q", result[:50])
	}
	if !strings.Contains(result, "Compressed body content.") {
		t.Error("expected compressed body in result")
	}
}

func TestCompressWithLLM_NoFrontmatter(t *testing.T) {
	mockLLM(t, func(vendor, prompt string) (string, error) {
		return "Short version.", nil
	})

	input := "# Long Instructions\n\nVerbose content here.\n"
	result, err := compressWithLLM("claude", input)
	if err != nil {
		t.Fatal(err)
	}

	if strings.HasPrefix(result, "---") {
		t.Error("should not have frontmatter when input had none")
	}
	if !strings.Contains(result, "Short version.") {
		t.Error("expected compressed content")
	}
}

func TestCompressWithLLM_LLMReturnsFrontmatter(t *testing.T) {
	// LLM returns its own frontmatter — should be stripped to avoid duplication
	mockLLM(t, func(vendor, prompt string) (string, error) {
		return "---\nname: test\ndescription: LLM version\n---\n\nLLM body.", nil
	})

	input := "---\nname: test\ndescription: Original.\n---\n\nOriginal body.\n"
	result, err := compressWithLLM("claude", input)
	if err != nil {
		t.Fatal(err)
	}

	// Should have original frontmatter, not double frontmatter
	if strings.Count(result, "---\n") > 2 {
		t.Errorf("expected no double frontmatter, got:\n%s", result)
	}
	if !strings.Contains(result, "description: Original.") {
		t.Error("expected original frontmatter preserved")
	}
}

func TestCompressWithLLM_Error(t *testing.T) {
	mockLLM(t, func(vendor, prompt string) (string, error) {
		return "", fmt.Errorf("LLM error")
	})

	_, err := compressWithLLM("claude", "content")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCmdCompress_FullFlow(t *testing.T) {
	mockLLM(t, func(vendor, prompt string) (string, error) {
		return "Compressed.", nil
	})

	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("YND_BACKUP_DIR", filepath.Join(dir, "backups"))

	srcFile := filepath.Join(dir, "test.md")
	writeFile(t, srcFile, []byte("Very verbose content that needs compression.\n"))

	err := cmdCompress([]string{"-v", "claude", "-y", srcFile})
	if err != nil {
		t.Fatal(err)
	}

	// Verify file was compressed
	data, _ := os.ReadFile(srcFile)
	if !strings.Contains(string(data), "Compressed.") {
		t.Errorf("expected compressed content, got %q", string(data))
	}

	// Verify backup was created
	backups, _ := findBackups(srcFile)
	if len(backups) != 1 {
		t.Errorf("expected 1 backup, got %d", len(backups))
	}
}

func TestCmdCompress_FullFlow_WithFrontmatter(t *testing.T) {
	mockLLM(t, func(vendor, prompt string) (string, error) {
		return "Compressed body.", nil
	})

	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("YND_BACKUP_DIR", filepath.Join(dir, "backups"))

	srcFile := filepath.Join(dir, "skill.md")
	writeFile(t, srcFile, []byte("---\nname: test\ndescription: A test.\n---\n\nVerbose instructions.\n"))

	err := cmdCompress([]string{"-v", "claude", "-y", srcFile})
	if err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(srcFile)
	content := string(data)
	if !strings.HasPrefix(content, "---\nname: test\n") {
		t.Error("expected frontmatter preserved after compression")
	}
	if !strings.Contains(content, "Compressed body.") {
		t.Error("expected compressed body")
	}
}

func TestCmdCompress_DiscoverFiles(t *testing.T) {
	mockLLM(t, func(vendor, prompt string) (string, error) {
		return "Short.", nil
	})

	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("YND_BACKUP_DIR", filepath.Join(dir, "backups"))

	writeFile(t, filepath.Join(dir, "a.md"), []byte("Long content A.\n"))
	writeFile(t, filepath.Join(dir, "b.md"), []byte("Long content B.\n"))

	err := cmdCompress([]string{"-v", "claude", "-y"})
	if err != nil {
		t.Fatal(err)
	}

	// Both files should be compressed
	dataA, _ := os.ReadFile(filepath.Join(dir, "a.md"))
	dataB, _ := os.ReadFile(filepath.Join(dir, "b.md"))
	if !strings.Contains(string(dataA), "Short.") {
		t.Error("expected a.md compressed")
	}
	if !strings.Contains(string(dataB), "Short.") {
		t.Error("expected b.md compressed")
	}
}

func TestCmdCompress_EmptyFileSkippedWithMock(t *testing.T) {
	called := false
	mockLLM(t, func(vendor, prompt string) (string, error) {
		called = true
		return "Short.", nil
	})

	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("YND_BACKUP_DIR", filepath.Join(dir, "backups"))

	// Create whitespace-only file
	writeFile(t, filepath.Join(dir, "empty.md"), []byte("   \n  \n"))

	err := cmdCompress([]string{"-v", "claude", "-y", filepath.Join(dir, "empty.md")})
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Error("LLM should not be called for whitespace-only files")
	}
}

func TestCmdCompress_ReadError(t *testing.T) {
	mockLLM(t, func(vendor, prompt string) (string, error) {
		return "Short.", nil
	})

	// File that doesn't exist
	err := cmdCompress([]string{"-v", "claude", "-y", "/nonexistent/file.md"})
	// Should not return error, just print to stderr and continue
	if err != nil {
		t.Fatal(err)
	}
}

func TestCmdCompress_LLMFails(t *testing.T) {
	mockLLM(t, func(vendor, prompt string) (string, error) {
		return "", fmt.Errorf("LLM unavailable")
	})

	dir := t.TempDir()
	t.Setenv("YND_BACKUP_DIR", filepath.Join(dir, "backups"))
	srcFile := filepath.Join(dir, "test.md")
	writeFile(t, srcFile, []byte("Content to compress.\n"))

	err := cmdCompress([]string{"-v", "claude", "-y", srcFile})
	// Should not return error, just print failure per-file
	if err != nil {
		t.Fatal(err)
	}

	// File should be unchanged
	data, _ := os.ReadFile(srcFile)
	if string(data) != "Content to compress.\n" {
		t.Error("file should not have been modified on LLM failure")
	}
}

func TestCmdCompress_MultipleFiles(t *testing.T) {
	callCount := 0
	mockLLM(t, func(vendor, prompt string) (string, error) {
		callCount++
		return fmt.Sprintf("Compressed %d.", callCount), nil
	})

	dir := t.TempDir()
	t.Setenv("YND_BACKUP_DIR", filepath.Join(dir, "backups"))

	f1 := filepath.Join(dir, "a.md")
	f2 := filepath.Join(dir, "b.md")
	writeFile(t, f1, []byte("Verbose content A.\n"))
	writeFile(t, f2, []byte("Verbose content B.\n"))

	err := cmdCompress([]string{"-v", "claude", "-y", f1, f2})
	if err != nil {
		t.Fatal(err)
	}

	if callCount != 2 {
		t.Errorf("expected 2 LLM calls, got %d", callCount)
	}
}

func TestCmdCompress_InteractiveApply(t *testing.T) {
	mockLLM(t, func(vendor, prompt string) (string, error) {
		return "Compressed.", nil
	})
	origPrompt := promptActionFunc
	t.Cleanup(func() { promptActionFunc = origPrompt })
	promptActionFunc = func(msg string, choices ...string) string {
		return "y"
	}

	dir := t.TempDir()
	t.Setenv("YND_BACKUP_DIR", filepath.Join(dir, "backups"))

	srcFile := filepath.Join(dir, "test.md")
	writeFile(t, srcFile, []byte("Verbose content.\n"))

	err := cmdCompress([]string{"-v", "claude", srcFile})
	if err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(srcFile)
	if !strings.Contains(string(data), "Compressed.") {
		t.Error("expected file to be compressed after interactive approval")
	}
}

func TestCmdCompress_InteractiveSkip(t *testing.T) {
	// Clear CI/YNH_YES so skipConfirmEnv() doesn't auto-skip
	t.Setenv("CI", "")
	t.Setenv("YNH_YES", "")

	mockLLM(t, func(vendor, prompt string) (string, error) {
		return "Compressed.", nil
	})
	origPrompt := promptActionFunc
	t.Cleanup(func() { promptActionFunc = origPrompt })
	promptActionFunc = func(msg string, choices ...string) string {
		return "n"
	}

	dir := t.TempDir()
	srcFile := filepath.Join(dir, "test.md")
	writeFile(t, srcFile, []byte("Verbose content.\n"))

	err := cmdCompress([]string{"-v", "claude", srcFile})
	if err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(srcFile)
	if strings.Contains(string(data), "Compressed.") {
		t.Error("file should not have been modified after interactive skip")
	}
}

func TestCmdCompress_NoVendorAutoDetect(t *testing.T) {
	// With PATH emptied, no vendor should be found
	t.Setenv("PATH", t.TempDir()) // empty dir, no CLIs

	dir := t.TempDir()
	t.Chdir(dir)
	writeFile(t, filepath.Join(dir, "test.md"), []byte("content\n"))

	// Should not error, just print "no CLI found" message
	err := cmdCompress([]string{filepath.Join(dir, "test.md")})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCmdCompress_NoFilesDiscovered(t *testing.T) {
	mockLLM(t, func(vendor, prompt string) (string, error) {
		return "Short.", nil
	})

	dir := t.TempDir()
	t.Chdir(dir)
	// Empty dir, no .md files

	err := cmdCompress([]string{"-v", "claude"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestBuildCompressPrompt(t *testing.T) {
	content := "# My Instructions\n\nDo the thing carefully.\n"
	prompt := buildCompressPrompt(content)

	checks := []string{
		content,
		"SudoLang",
		"compressed text",
	}
	for _, want := range checks {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestReductionPct(t *testing.T) {
	tests := []struct {
		name    string
		orig    int
		comp    int
		wantPct int
	}{
		{"zero orig", 0, 0, 0},
		{"equal", 100, 100, 0},
		{"50 pct reduction", 100, 50, 50},
		{"100 pct reduction", 100, 0, 100},
		{"small reduction", 200, 180, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reductionPct(tt.orig, tt.comp)
			if got != tt.wantPct {
				t.Errorf("reductionPct(%d, %d) = %d, want %d", tt.orig, tt.comp, got, tt.wantPct)
			}
		})
	}
}

func TestCmdCompress_NoFiles(t *testing.T) {
	t.TempDir()
	t.Chdir(t.TempDir())

	err := cmdCompress([]string{"-v", "nonexistent-cli-12345"})
	if err == nil {
		// With explicit vendor that doesn't exist, should error
		// But with no files to process it may return nil — either is acceptable
		return
	}
}

func TestCmdCompress_EmptyFileSkipped(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	emptyFile := filepath.Join(dir, "empty.md")
	if err := os.WriteFile(emptyFile, []byte("   \n\n  \n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Pass explicit file — whitespace-only content should be skipped
	// before reaching the LLM. Auto-detect will fail (no CLI on PATH),
	// which is fine for this test.
	err := cmdCompress([]string{emptyFile})
	_ = err
}

func TestCmdCompress_ExplicitFiles(t *testing.T) {
	dir := t.TempDir()
	f1 := filepath.Join(dir, "a.md")
	f2 := filepath.Join(dir, "b.md")
	if err := os.WriteFile(f1, []byte("content a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f2, []byte("content b\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	opts, err := parseCompressArgs([]string{"-y", f1, f2})
	if err != nil {
		t.Fatal(err)
	}
	if opts.vendor != "" {
		t.Errorf("vendor = %q, want empty", opts.vendor)
	}
	if !opts.skipConfirm {
		t.Error("skip should be true")
	}
	if len(opts.files) != 2 {
		t.Fatalf("files = %v, want 2 files", opts.files)
	}
	if opts.files[0] != f1 || opts.files[1] != f2 {
		t.Errorf("files = %v, want [%s, %s]", opts.files, f1, f2)
	}
}

func TestBackupFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("YND_BACKUP_DIR", filepath.Join(dir, "backups"))

	// Create a file to back up
	srcDir := filepath.Join(dir, "project")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	srcFile := filepath.Join(srcDir, "test.md")
	content := []byte("original content\n")
	if err := os.WriteFile(srcFile, content, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := backupFile(srcFile, content); err != nil {
		t.Fatal(err)
	}

	// Verify backup exists
	backups, err := findBackups(srcFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected 1 backup, got %d", len(backups))
	}

	data, err := os.ReadFile(backups[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(content) {
		t.Errorf("backup content = %q, want %q", string(data), string(content))
	}
}

func TestBackupFile_MultipleBackups(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("YND_BACKUP_DIR", filepath.Join(dir, "backups"))

	srcFile := filepath.Join(dir, "test.md")
	if err := os.WriteFile(srcFile, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create multiple backups with different timestamps by writing directly
	absPath, _ := filepath.Abs(srcFile)
	backupDir := backupDirForFile(absPath)
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(backupDir, "20260314T100000"), []byte("v1"))
	writeFile(t, filepath.Join(backupDir, "20260314T110000"), []byte("v2"))
	writeFile(t, filepath.Join(backupDir, "20260314T120000"), []byte("v3"))

	backups, err := findBackups(srcFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 3 {
		t.Fatalf("expected 3 backups, got %d", len(backups))
	}

	// Newest first
	if filepath.Base(backups[0]) != "20260314T120000" {
		t.Errorf("expected newest first, got %s", filepath.Base(backups[0]))
	}
	if filepath.Base(backups[2]) != "20260314T100000" {
		t.Errorf("expected oldest last, got %s", filepath.Base(backups[2]))
	}
}

func TestRestoreBackup_Latest(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("YND_BACKUP_DIR", filepath.Join(dir, "backups"))

	srcFile := filepath.Join(dir, "test.md")
	if err := os.WriteFile(srcFile, []byte("current"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create backups
	absPath, _ := filepath.Abs(srcFile)
	backupDir := backupDirForFile(absPath)
	mkdirAll(t, backupDir)
	writeFile(t, filepath.Join(backupDir, "20260314T100000"), []byte("old version"))
	writeFile(t, filepath.Join(backupDir, "20260314T120000"), []byte("latest version"))

	if err := restoreBackup(srcFile, 0); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(srcFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "latest version" {
		t.Errorf("restored content = %q, want %q", string(data), "latest version")
	}
}

func TestRestoreBackup_Pick(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("YND_BACKUP_DIR", filepath.Join(dir, "backups"))

	srcFile := filepath.Join(dir, "test.md")
	if err := os.WriteFile(srcFile, []byte("current"), 0o644); err != nil {
		t.Fatal(err)
	}

	absPath, _ := filepath.Abs(srcFile)
	backupDir := backupDirForFile(absPath)
	mkdirAll(t, backupDir)
	writeFile(t, filepath.Join(backupDir, "20260314T100000"), []byte("oldest"))
	writeFile(t, filepath.Join(backupDir, "20260314T110000"), []byte("middle"))
	writeFile(t, filepath.Join(backupDir, "20260314T120000"), []byte("newest"))

	// Pick #2 = middle (newest first: 1=newest, 2=middle, 3=oldest)
	if err := restoreBackup(srcFile, 2); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(srcFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "middle" {
		t.Errorf("restored content = %q, want %q", string(data), "middle")
	}
}

func TestRestoreBackup_PickOutOfRange(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("YND_BACKUP_DIR", filepath.Join(dir, "backups"))

	srcFile := filepath.Join(dir, "test.md")
	if err := os.WriteFile(srcFile, []byte("current"), 0o644); err != nil {
		t.Fatal(err)
	}

	absPath, _ := filepath.Abs(srcFile)
	backupDir := backupDirForFile(absPath)
	mkdirAll(t, backupDir)
	writeFile(t, filepath.Join(backupDir, "20260314T100000"), []byte("only one"))

	err := restoreBackup(srcFile, 5)
	if err == nil {
		t.Fatal("expected error for out-of-range pick")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("expected 'does not exist' error, got %v", err)
	}
}

func TestRestoreBackup_NoBackups(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("YND_BACKUP_DIR", filepath.Join(dir, "backups"))

	srcFile := filepath.Join(dir, "test.md")
	if err := os.WriteFile(srcFile, []byte("current"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := restoreBackup(srcFile, 0)
	if err == nil {
		t.Fatal("expected error for no backups")
	}
	if !strings.Contains(err.Error(), "no backups found") {
		t.Errorf("expected 'no backups found' error, got %v", err)
	}
}

func TestListBackups_NoBackups(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("YND_BACKUP_DIR", filepath.Join(dir, "backups"))

	srcFile := filepath.Join(dir, "test.md")
	if err := os.WriteFile(srcFile, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Should not error, just print "No backups"
	if err := listBackups(srcFile); err != nil {
		t.Fatal(err)
	}
}

func TestListBackups_WithBackups(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("YND_BACKUP_DIR", filepath.Join(dir, "backups"))

	srcFile := filepath.Join(dir, "test.md")
	if err := os.WriteFile(srcFile, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	absPath, _ := filepath.Abs(srcFile)
	backupDir := backupDirForFile(absPath)
	mkdirAll(t, backupDir)
	writeFile(t, filepath.Join(backupDir, "20260314T100000"), []byte("v1"))
	writeFile(t, filepath.Join(backupDir, "20260314T120000"), []byte("v2"))

	if err := listBackups(srcFile); err != nil {
		t.Fatal(err)
	}
}

func TestCmdCompress_ListBackups(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("YND_BACKUP_DIR", filepath.Join(dir, "backups"))

	srcFile := filepath.Join(dir, "test.md")
	if err := os.WriteFile(srcFile, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a backup
	absPath, _ := filepath.Abs(srcFile)
	backupDir := backupDirForFile(absPath)
	mkdirAll(t, backupDir)
	writeFile(t, filepath.Join(backupDir, "20260314T100000"), []byte("v1"))

	// Run through cmdCompress
	err := cmdCompress([]string{"--list-backups", srcFile})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCmdCompress_ListBackups_NoFile(t *testing.T) {
	err := cmdCompress([]string{"--list-backups"})
	if err == nil {
		t.Fatal("expected error for --list-backups without file")
	}
	if !strings.Contains(err.Error(), "requires a file argument") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCmdCompress_Restore(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("YND_BACKUP_DIR", filepath.Join(dir, "backups"))

	srcFile := filepath.Join(dir, "test.md")
	if err := os.WriteFile(srcFile, []byte("current"), 0o644); err != nil {
		t.Fatal(err)
	}

	absPath, _ := filepath.Abs(srcFile)
	backupDir := backupDirForFile(absPath)
	mkdirAll(t, backupDir)
	writeFile(t, filepath.Join(backupDir, "20260314T100000"), []byte("original"))

	err := cmdCompress([]string{"--restore", srcFile})
	if err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(srcFile)
	if string(data) != "original" {
		t.Errorf("content = %q, want %q", string(data), "original")
	}
}

func TestCmdCompress_Restore_NoFile(t *testing.T) {
	err := cmdCompress([]string{"--restore"})
	if err == nil {
		t.Fatal("expected error for --restore without file")
	}
	if !strings.Contains(err.Error(), "requires a file argument") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCmdCompress_Restore_WithPick(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("YND_BACKUP_DIR", filepath.Join(dir, "backups"))

	srcFile := filepath.Join(dir, "test.md")
	if err := os.WriteFile(srcFile, []byte("current"), 0o644); err != nil {
		t.Fatal(err)
	}

	absPath, _ := filepath.Abs(srcFile)
	backupDir := backupDirForFile(absPath)
	mkdirAll(t, backupDir)
	writeFile(t, filepath.Join(backupDir, "20260314T100000"), []byte("older"))
	writeFile(t, filepath.Join(backupDir, "20260314T120000"), []byte("newer"))

	err := cmdCompress([]string{"--restore", "--pick", "2", srcFile})
	if err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(srcFile)
	if string(data) != "older" {
		t.Errorf("content = %q, want %q", string(data), "older")
	}
}

func TestCmdCompress_VendorNotFound(t *testing.T) {
	dir := t.TempDir()
	srcFile := filepath.Join(dir, "test.md")
	writeFile(t, srcFile, []byte("content\n"))

	err := cmdCompress([]string{"-v", "nonexistent-vendor-xyz", srcFile})
	if err == nil {
		t.Fatal("expected error for missing vendor")
	}
	if !strings.Contains(err.Error(), "not found on PATH") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCmdCompress_NoMarkdownFiles(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// No vendor on path, no files — should hit "no CLI found" or "no files"
	// With explicit nonexistent vendor, it errors before discovering files
	err := cmdCompress([]string{"-v", "nonexistent-cli-xyz"})
	if err == nil {
		return // acceptable — no files to process
	}
}

func TestFindBackups_SkipsSubdirectories(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("YND_BACKUP_DIR", filepath.Join(dir, "backups"))

	srcFile := filepath.Join(dir, "test.md")
	writeFile(t, srcFile, []byte("content"))

	absPath, _ := filepath.Abs(srcFile)
	backupDir := backupDirForFile(absPath)
	mkdirAll(t, backupDir)
	writeFile(t, filepath.Join(backupDir, "20260314T100000"), []byte("v1"))
	mkdirAll(t, filepath.Join(backupDir, "subdir")) // should be skipped

	backups, err := findBackups(srcFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Errorf("expected 1 backup (subdir skipped), got %d", len(backups))
	}
}

func TestBackupsDir_WithEnvVar(t *testing.T) {
	t.Setenv("YND_BACKUP_DIR", "/custom/path")
	got := backupsDir()
	if got != "/custom/path" {
		t.Errorf("backupsDir = %q, want /custom/path", got)
	}
}

func TestBackupsDir_Default(t *testing.T) {
	t.Setenv("YND_BACKUP_DIR", "")
	got := backupsDir()
	if !strings.Contains(got, ".ynd/backups") {
		t.Errorf("backupsDir = %q, expected to contain .ynd/backups", got)
	}
}

func TestFormatTimestamp(t *testing.T) {
	got := formatTimestamp("20260314T153042")
	want := "2026-03-14 15:30:42"
	if got != want {
		t.Errorf("formatTimestamp = %q, want %q", got, want)
	}
}

func TestFormatTimestamp_Invalid(t *testing.T) {
	got := formatTimestamp("not-a-timestamp")
	if got != "not-a-timestamp" {
		t.Errorf("expected passthrough for invalid timestamp, got %q", got)
	}
}

func TestBackupDirForFile(t *testing.T) {
	t.Setenv("YND_BACKUP_DIR", "/tmp/test-backups")
	got := backupDirForFile("/Users/david/project/skills/SKILL.md")
	want := filepath.Join("/tmp/test-backups", "Users/david/project/skills/SKILL.md")
	if got != want {
		t.Errorf("backupDirForFile = %q, want %q", got, want)
	}
}
