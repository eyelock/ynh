package main

import (
	"fmt"
	"os"
	"strings"
)

func cmdFmt(args []string) error {
	root := "."
	if len(args) > 0 {
		root = args[0]
	}

	files, err := discoverFiles(root, []string{".md"})
	if err != nil {
		return err
	}

	if len(files) == 0 {
		fmt.Println("No markdown files found.")
		return nil
	}

	changed := 0
	for _, f := range files {
		modified, err := formatMarkdown(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", f, err)
			continue
		}
		if modified {
			changed++
			fmt.Printf("Formatted %s\n", f)
		}
	}

	if changed == 0 {
		fmt.Printf("Checked %d file(s) — all formatted.\n", len(files))
	} else {
		fmt.Printf("Formatted %d of %d file(s).\n", changed, len(files))
	}
	return nil
}

func formatMarkdown(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}

	original := string(data)
	result := formatMarkdownContent(original)

	if result == original {
		return false, nil
	}

	if err := os.WriteFile(path, []byte(result), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

func formatMarkdownContent(content string) string {
	// Split into frontmatter and body
	frontmatter, body := splitFrontmatter(content)

	// Format the body
	lines := strings.Split(body, "\n")

	// Trim trailing whitespace from each line
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}

	// Collapse multiple consecutive blank lines into one
	var collapsed []string
	prevBlank := false
	for _, line := range lines {
		isBlank := strings.TrimSpace(line) == ""
		if isBlank && prevBlank {
			continue
		}
		collapsed = append(collapsed, line)
		prevBlank = isBlank
	}
	lines = collapsed

	body = strings.Join(lines, "\n")

	// Ensure single trailing newline
	body = strings.TrimRight(body, "\n") + "\n"

	if frontmatter != "" {
		return frontmatter + body
	}
	return body
}

// splitFrontmatter separates YAML frontmatter from the markdown body.
// Returns ("", content) if no frontmatter is found.
func splitFrontmatter(content string) (frontmatter, body string) {
	if !strings.HasPrefix(content, "---\n") {
		return "", content
	}

	end := strings.Index(content[4:], "\n---")
	if end < 0 {
		return "", content
	}

	// end is relative to content[4:], so the closing --- starts at 4+end+1
	cut := 4 + end + 1 + 3 // position after "---"

	// Include trailing newline after --- if present
	if cut < len(content) && content[cut] == '\n' {
		cut++
	}

	// Cap to content length
	if cut > len(content) {
		cut = len(content)
	}

	return content[:cut], content[cut:]
}
