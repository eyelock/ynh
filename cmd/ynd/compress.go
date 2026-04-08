package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func cmdCompress(args []string) error {
	opts, err := parseCompressArgs(args)
	if err != nil {
		return err
	}

	// Handle --list-backups
	if opts.listBackups {
		if len(opts.files) == 0 {
			return fmt.Errorf("--list-backups requires a file argument")
		}
		var errs []string
		for _, f := range opts.files {
			if err := listBackups(f); err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", f, err)
				errs = append(errs, f)
			}
		}
		if len(errs) > 0 {
			return fmt.Errorf("failed to list backups for %d file(s)", len(errs))
		}
		return nil
	}

	// Handle --restore
	if opts.restore {
		if len(opts.files) == 0 {
			return fmt.Errorf("--restore requires a file argument")
		}
		var errs []string
		for _, f := range opts.files {
			if err := restoreBackup(f, opts.pick); err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", f, err)
				errs = append(errs, f)
			}
		}
		if len(errs) > 0 {
			return fmt.Errorf("failed to restore %d file(s)", len(errs))
		}
		return nil
	}

	// Env var fallback for skip-confirm
	if !opts.skipConfirm {
		opts.skipConfirm = skipConfirmEnv()
	}

	// Auto-detect vendor if not specified
	if opts.vendor == "" {
		opts.vendor = resolveVendorEnv()
	}
	if opts.vendor == "" {
		opts.vendor = detectVendorCLI()
		if opts.vendor == "" {
			fmt.Fprintln(os.Stderr, "No supported LLM CLI found (checked: claude, codex).")
			fmt.Fprintln(os.Stderr, "Compression requires an LLM to perform semantic optimization.")
			fmt.Fprintln(os.Stderr)
			fmt.Fprintln(os.Stderr, "Install one of:")
			fmt.Fprintln(os.Stderr, "  claude  → https://docs.anthropic.com/claude-code")
			fmt.Fprintln(os.Stderr, "  codex   → https://openai.com/codex")
			fmt.Fprintln(os.Stderr)
			fmt.Fprintln(os.Stderr, "Or specify one explicitly: ynd compress -v claude")
			return nil
		}
	} else {
		if _, err := lookPathFunc(opts.vendor); err != nil {
			return fmt.Errorf("vendor CLI %q not found on PATH", opts.vendor)
		}
	}

	// Discover files if none specified
	if len(opts.files) == 0 {
		discovered, err := discoverFiles(".", []string{".md"})
		if err != nil {
			return err
		}
		opts.files = discovered
	}

	if len(opts.files) == 0 {
		fmt.Println("No markdown files found.")
		return nil
	}

	totalOriginal := 0
	totalCompressed := 0
	compressed := 0

	for _, f := range opts.files {
		original, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", f, err)
			continue
		}

		if len(strings.TrimSpace(string(original))) == 0 {
			continue
		}

		fmt.Printf("Compressing %s...\n", f)

		result, err := compressWithLLM(opts.vendor, string(original))
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %s: compression failed: %v\n", f, err)
			continue
		}

		origLen := len(original)
		compLen := len(result)
		reduction := reductionPct(origLen, compLen)

		if !opts.skipConfirm {
			fmt.Printf("\n--- Original (%d chars) ---\n%s\n", origLen, string(original))
			fmt.Printf("--- Compressed (%d chars, %d%% reduction) ---\n%s\n", compLen, reduction, result)

			action := promptAction("Apply? [y/N] ", "y", "n")
			if action != "y" {
				fmt.Println("Skipped.")
				continue
			}
		}

		// Back up the original before overwriting
		if backupErr := backupFile(f, original); backupErr != nil {
			fmt.Fprintf(os.Stderr, "  %s: backup failed: %v\n", f, backupErr)
			continue
		}

		if err := os.WriteFile(f, []byte(result), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "  %s: write error: %v\n", f, err)
			continue
		}

		totalOriginal += origLen
		totalCompressed += compLen
		compressed++

		if opts.skipConfirm {
			fmt.Printf("  %d → %d chars (%d%% reduction)\n", origLen, compLen, reduction)
		} else {
			fmt.Println("Applied.")
		}
	}

	if compressed > 0 {
		totalReduction := reductionPct(totalOriginal, totalCompressed)
		fmt.Printf("\n%d file(s) compressed, avg %d%% reduction.\n", compressed, totalReduction)
	}

	return nil
}

// compressOpts holds parsed compress command arguments.
type compressOpts struct {
	vendor      string
	files       []string
	skipConfirm bool
	restore     bool
	listBackups bool
	pick        int // 0 means latest
}

func parseCompressArgs(args []string) (compressOpts, error) {
	var opts compressOpts
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-v", "--vendor":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("-v requires a vendor name")
			}
			opts.vendor = args[i+1]
			i++
		case "-h", "--help":
			return opts, errHelp
		case "-y", "--yes":
			opts.skipConfirm = true
		case "--restore":
			opts.restore = true
		case "--list-backups":
			opts.listBackups = true
		case "--pick":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--pick requires a number")
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil || n < 1 {
				return opts, fmt.Errorf("--pick requires a positive integer, got %q", args[i+1])
			}
			opts.pick = n
			i++
		default:
			if strings.HasPrefix(args[i], "-") {
				return opts, fmt.Errorf("unknown flag: %s", args[i])
			}
			opts.files = append(opts.files, args[i])
		}
	}
	return opts, nil
}

// backupsDir returns the base directory for compress backups.
// Defaults to ~/.ynd/backups. Override with YND_BACKUP_DIR for testing.
func backupsDir() string {
	if dir := os.Getenv("YND_BACKUP_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".ynd", "backups")
}

// backupPath returns the backup directory for a given file's absolute path.
// e.g. /tmp/project/skills/SKILL.md → ~/.ynd/backups/tmp/project/skills/SKILL.md/
func backupDirForFile(absPath string) string {
	// Strip volume/root for clean nesting
	clean := filepath.Clean(absPath)
	// Remove leading separator to make it relative under backupsDir
	rel := strings.TrimPrefix(clean, string(filepath.Separator))
	return filepath.Join(backupsDir(), rel)
}

// backupFile saves a copy of content to ~/.ynd/backups/<abs-path>/<timestamp>.
func backupFile(path string, content []byte) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	dir := backupDirForFile(absPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	ts := time.Now().Format("20060102T150405.000000000")
	backupPath := filepath.Join(dir, ts)
	return os.WriteFile(backupPath, content, 0o644)
}

// findBackups returns backup file paths for the given file, sorted newest first.
func findBackups(path string) ([]string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	dir := backupDirForFile(absPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var backups []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		backups = append(backups, filepath.Join(dir, e.Name()))
	}

	// Sort newest first (timestamps sort lexicographically)
	sort.Sort(sort.Reverse(sort.StringSlice(backups)))
	return backups, nil
}

// listBackups prints the backup history for a file.
func listBackups(path string) error {
	backups, err := findBackups(path)
	if err != nil {
		return err
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	if len(backups) == 0 {
		fmt.Printf("No backups for %s\n", absPath)
		return nil
	}

	backupDir := backupDirForFile(absPath)
	fmt.Printf("Backups for %s:\n", absPath)
	fmt.Printf("  dir: %s\n", backupDir)
	for i, b := range backups {
		ts := filepath.Base(b)
		label := ""
		if i == 0 {
			label = " (latest)"
		}
		fmt.Printf("  %d. %s%s\n", i+1, formatTimestamp(ts), label)
	}
	return nil
}

// restoreBackup restores a file from backup. pick=0 means latest, pick=N uses the Nth entry.
func restoreBackup(path string, pick int) error {
	backups, err := findBackups(path)
	if err != nil {
		return err
	}

	if len(backups) == 0 {
		return fmt.Errorf("no backups found for %s", path)
	}

	idx := 0
	if pick > 0 {
		if pick > len(backups) {
			return fmt.Errorf("backup #%d does not exist (only %d available)", pick, len(backups))
		}
		idx = pick - 1
	}

	content, err := os.ReadFile(backups[idx])
	if err != nil {
		return err
	}

	if err := os.WriteFile(path, content, 0o644); err != nil {
		return err
	}

	ts := filepath.Base(backups[idx])
	fmt.Printf("Restored %s from backup %s\n", path, formatTimestamp(ts))
	return nil
}

// formatTimestamp converts backup filenames to human-readable timestamps.
// Handles both "20260314T153042" and "20260314T153042.123456789" formats.
func formatTimestamp(ts string) string {
	for _, layout := range []string{"20060102T150405.000000000", "20060102T150405"} {
		if t, err := time.Parse(layout, ts); err == nil {
			return t.Format("2006-01-02 15:04:05")
		}
	}
	return ts
}

func buildCompressPrompt(content string) string {
	return fmt.Sprintf(`Compress the following prompt/instruction text using SudoLang-style techniques:

- Remove filler words and redundant phrasing
- Use concise, information-dense language
- Maintain ALL semantic meaning and instructions
- Preserve code blocks and technical specifications verbatim
- Use shorthand notation where meaning is clearly preserved
- Combine related instructions into compact forms

Output ONLY the compressed text with no explanation, preamble, or commentary.

---
%s`, content)
}

func reductionPct(origLen, compLen int) int {
	if origLen <= 0 {
		return 0
	}
	return 100 - (compLen*100)/origLen
}

func compressWithLLM(vendor, content string) (string, error) {
	// Strip frontmatter before sending to LLM — reattach after.
	// This prevents the LLM from mangling or dropping YAML frontmatter.
	fm, body := splitFrontmatter(content)

	prompt := buildCompressPrompt(body)

	result, err := queryLLM(vendor, prompt)
	if err != nil {
		return "", err
	}

	result = strings.TrimSpace(result)

	// If the LLM returned its own frontmatter, strip it so we don't double up
	if fm != "" {
		_, resultBody := splitFrontmatter(result)
		result = strings.TrimLeft(resultBody, "\n")
	}

	// Reassemble: original frontmatter + compressed body
	if fm != "" {
		result = fm + "\n" + result
	}

	// Ensure trailing newline
	if !strings.HasSuffix(result, "\n") {
		result += "\n"
	}
	return result, nil
}
