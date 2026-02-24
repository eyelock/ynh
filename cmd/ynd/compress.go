package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// NOTE: queryLLM and detectVendorCLI live in inspect.go

func cmdCompress(args []string) error {
	vendor, files, skipConfirm, err := parseCompressArgs(args)
	if err != nil {
		return err
	}

	// Auto-detect vendor if not specified
	if vendor == "" {
		vendor = detectVendorCLI()
		if vendor == "" {
			fmt.Println("No supported LLM CLI found (checked: claude, codex).")
			fmt.Println("Compression requires an LLM to perform semantic optimization.")
			fmt.Println()
			fmt.Println("Install one of:")
			fmt.Println("  claude  → https://docs.anthropic.com/claude-code")
			fmt.Println("  codex   → https://openai.com/codex")
			fmt.Println()
			fmt.Println("Or specify one explicitly: ynd compress -v claude")
			return nil
		}
	} else {
		if _, err := exec.LookPath(vendor); err != nil {
			return fmt.Errorf("vendor CLI %q not found on PATH", vendor)
		}
	}

	// Discover files if none specified
	if len(files) == 0 {
		discovered, err := discoverFiles(".", []string{".md"})
		if err != nil {
			return err
		}
		files = discovered
	}

	if len(files) == 0 {
		fmt.Println("No markdown files found.")
		return nil
	}

	totalOriginal := 0
	totalCompressed := 0
	compressed := 0

	for _, f := range files {
		original, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", f, err)
			continue
		}

		if len(strings.TrimSpace(string(original))) == 0 {
			continue
		}

		fmt.Printf("Compressing %s...\n", f)

		result, err := compressWithLLM(vendor, string(original))
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %s: compression failed: %v\n", f, err)
			continue
		}

		origLen := len(original)
		compLen := len(result)
		reduction := 0
		if origLen > 0 {
			reduction = 100 - (compLen*100)/origLen
		}

		if !skipConfirm {
			fmt.Printf("\n--- Original (%d chars) ---\n%s\n", origLen, string(original))
			fmt.Printf("--- Compressed (%d chars, %d%% reduction) ---\n%s\n", compLen, reduction, result)
			fmt.Printf("Apply? [y/N] ")

			reader := bufio.NewReader(os.Stdin)
			answer, _ := reader.ReadString('\n')
			answer = strings.TrimSpace(strings.ToLower(answer))
			if answer != "y" && answer != "yes" {
				fmt.Println("Skipped.")
				continue
			}
		}

		if err := os.WriteFile(f, []byte(result), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "  %s: write error: %v\n", f, err)
			continue
		}

		totalOriginal += origLen
		totalCompressed += compLen
		compressed++

		if skipConfirm {
			fmt.Printf("  %d → %d chars (%d%% reduction)\n", origLen, compLen, reduction)
		} else {
			fmt.Println("Applied.")
		}
	}

	if compressed > 0 {
		totalReduction := 0
		if totalOriginal > 0 {
			totalReduction = 100 - (totalCompressed*100)/totalOriginal
		}
		fmt.Printf("\n%d file(s) compressed, avg %d%% reduction.\n", compressed, totalReduction)
	}

	return nil
}

func parseCompressArgs(args []string) (vendor string, files []string, skipConfirm bool, err error) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-v", "--vendor":
			if i+1 >= len(args) {
				return "", nil, false, fmt.Errorf("-v requires a vendor name")
			}
			vendor = args[i+1]
			i++
		case "-y", "--yes":
			skipConfirm = true
		default:
			if strings.HasPrefix(args[i], "-") {
				return "", nil, false, fmt.Errorf("unknown flag: %s", args[i])
			}
			files = append(files, args[i])
		}
	}
	return
}

func compressWithLLM(vendor, content string) (string, error) {
	prompt := fmt.Sprintf(`Compress the following prompt/instruction text using SudoLang-style techniques:

- Remove filler words and redundant phrasing
- Use concise, information-dense language
- Maintain ALL semantic meaning and instructions
- Preserve code blocks and technical specifications verbatim
- Preserve YAML frontmatter structure (--- delimiters, key: value pairs)
- Use shorthand notation where meaning is clearly preserved
- Combine related instructions into compact forms

Output ONLY the compressed text with no explanation, preamble, or commentary.

---
%s`, content)

	result, err := queryLLM(vendor, prompt)
	if err != nil {
		return "", err
	}

	// Ensure trailing newline
	if !strings.HasSuffix(result, "\n") {
		result += "\n"
	}
	return result, nil
}
