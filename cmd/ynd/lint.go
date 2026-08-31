package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/plugin"
)

type lintIssue struct {
	File    string
	Line    int
	Message string
}

func cmdLint(args []string) error {
	// Every positional is a root, not just the first.
	//
	// This used to keep `args[i]` only when `root == ""`, so every later path
	// was parsed, discarded, and never linted — with no warning and exit 0.
	// `ynd lint skills agents` checked 8 files when `skills` alone checked 8
	// and `agents` alone checked 2. The user validated their agents, was told
	// "no issues found", and the agents were never opened. A linter that
	// silently declines to look is worse than one that refuses to run.
	var roots []string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--harness":
			if i+1 >= len(args) {
				return fmt.Errorf("--harness requires a value")
			}
			i++
			roots = append(roots, args[i])
		case "-h", "--help":
			return errHelp
		default:
			if strings.HasPrefix(args[i], "-") {
				return fmt.Errorf("unknown flag: %s", args[i])
			}
			roots = append(roots, args[i])
		}
	}

	// Resolve source: explicit paths > YNH_HARNESS > CWD
	if len(roots) == 0 {
		if env := resolveHarnessEnv(); env != "" {
			roots = []string{env}
		} else {
			roots = []string{"."}
		}
	}

	// Deduplicate while preserving order. Overlapping roots — `ynd lint . skills`
	// — would otherwise report the same finding twice and inflate the counts,
	// making the summary line disagree with the list above it.
	seen := map[string]bool{}
	var files []string
	for _, root := range roots {
		found, err := discoverAll(root,
			[]string{".md", ".sh"},
			[]string{plugin.PluginFile},
		)
		if err != nil {
			return err
		}
		for _, f := range found {
			abs, absErr := filepath.Abs(f)
			if absErr != nil {
				abs = f
			}
			if seen[abs] {
				continue
			}
			seen[abs] = true
			files = append(files, f)
		}
	}

	if len(files) == 0 {
		fmt.Println("No lintable files found.")
		return nil
	}

	var issues []lintIssue

	for _, f := range files {
		switch {
		case strings.HasSuffix(f, ".md"):
			issues = append(issues, lintMarkdown(f)...)
		case strings.HasSuffix(f, ".sh"):
			issues = append(issues, lintShell(f)...)
		case filepath.Base(f) == plugin.PluginFile:
			issues = append(issues, lintHarnessJSONFile(f)...)
			issues = append(issues, lintDeclaredReads(f)...)
		}
	}

	if len(issues) == 0 {
		fmt.Printf("Checked %d file(s) — no issues found.\n", len(files))
		return nil
	}

	for _, issue := range issues {
		if issue.Line > 0 {
			fmt.Printf("%s:%d: %s\n", issue.File, issue.Line, issue.Message)
		} else {
			fmt.Printf("%s: %s\n", issue.File, issue.Message)
		}
	}

	fmt.Printf("\n%d issue(s) in %d file(s).\n", len(issues), len(files))
	return fmt.Errorf("lint failed")
}

func lintMarkdown(path string) []lintIssue {
	data, err := os.ReadFile(path)
	if err != nil {
		return []lintIssue{{File: path, Message: fmt.Sprintf("read error: %v", err)}}
	}

	content := string(data)
	var issues []lintIssue

	// Check trailing newline
	if len(content) > 0 && !strings.HasSuffix(content, "\n") {
		issues = append(issues, lintIssue{File: path, Message: "file does not end with a newline"})
	}

	// Check lines
	lines := strings.Split(content, "\n")
	prevBlank := false
	for i, line := range lines {
		lineNum := i + 1

		// Trailing whitespace
		if strings.TrimRight(line, " \t") != line {
			issues = append(issues, lintIssue{File: path, Line: lineNum, Message: "trailing whitespace"})
		}

		// Multiple consecutive blank lines
		isBlank := strings.TrimSpace(line) == ""
		if isBlank && prevBlank && lineNum < len(lines) {
			issues = append(issues, lintIssue{File: path, Line: lineNum, Message: "multiple consecutive blank lines"})
		}
		prevBlank = isBlank
	}

	// Context-specific checks based on artifact type
	if isSkillFile(path) {
		issues = append(issues, lintSkillFrontmatter(path, content)...)
	} else if isAgentFile(path) {
		issues = append(issues, lintAgentFrontmatter(path, content)...)
	}

	// Check shell code blocks
	issues = append(issues, lintShellBlocks(path, content)...)

	return issues
}

// isSkillFile returns true if path looks like skills/<name>/SKILL.md.
func isSkillFile(path string) bool {
	return filepath.Base(path) == "SKILL.md"
}

// isAgentFile returns true if path is inside an agents/ directory.
func isAgentFile(path string) bool {
	dir := filepath.Dir(path)
	return filepath.Base(dir) == "agents"
}

// expectedFrontmatterName returns the name that should appear in frontmatter
// based on the file's location. For skills, it's the parent directory name.
// For agents, it's the filename without .md extension.
func expectedFrontmatterName(path string) string {
	if isSkillFile(path) {
		return filepath.Base(filepath.Dir(path))
	}
	return strings.TrimSuffix(filepath.Base(path), ".md")
}

// parseFrontmatter extracts key-value pairs from YAML frontmatter.
// Returns nil if no frontmatter is found.
func parseFrontmatter(content string) map[string]string {
	if !strings.HasPrefix(content, "---\n") {
		return nil
	}

	endIdx := strings.Index(content[4:], "\n---")
	if endIdx < 0 {
		return nil
	}

	fm := make(map[string]string)
	for _, line := range strings.Split(content[4:4+endIdx], "\n") {
		if idx := strings.Index(line, ":"); idx > 0 {
			key := strings.ToLower(strings.TrimSpace(line[:idx]))
			val := strings.TrimSpace(line[idx+1:])
			// Strip common YAML quoting
			val = strings.Trim(val, "\"'`")
			fm[key] = val
		}
	}
	return fm
}

func lintSkillFrontmatter(path, content string) []lintIssue {
	var issues []lintIssue

	if !strings.HasPrefix(content, "---\n") {
		issues = append(issues, lintIssue{File: path, Line: 1, Message: "skill file missing YAML frontmatter (expected --- delimiter)"})
		return issues
	}

	endIdx := strings.Index(content[4:], "\n---")
	if endIdx < 0 {
		issues = append(issues, lintIssue{File: path, Line: 1, Message: "unclosed frontmatter (missing closing ---)"})
		return issues
	}

	fm := parseFrontmatter(content)

	name, hasName := fm["name"]
	if !hasName {
		issues = append(issues, lintIssue{File: path, Line: 1, Message: "frontmatter missing required field 'name'"})
	} else if name == "" {
		issues = append(issues, lintIssue{File: path, Line: 1, Message: "frontmatter 'name' is empty"})
	} else {
		expected := expectedFrontmatterName(path)
		if name != expected {
			issues = append(issues, lintIssue{File: path, Line: 1, Message: fmt.Sprintf("frontmatter name %q does not match expected %q", name, expected)})
		}
	}

	desc := fm["description"]
	if desc == "" {
		issues = append(issues, lintIssue{File: path, Line: 1, Message: "frontmatter missing required field 'description'"})
	}

	issues = append(issues, lintDemotingFrontmatter(path, content)...)

	return issues
}

func lintAgentFrontmatter(path, content string) []lintIssue {
	var issues []lintIssue

	if !strings.HasPrefix(content, "---\n") {
		issues = append(issues, lintIssue{File: path, Line: 1, Message: "agent file missing YAML frontmatter (expected --- delimiter)"})
		return issues
	}

	endIdx := strings.Index(content[4:], "\n---")
	if endIdx < 0 {
		issues = append(issues, lintIssue{File: path, Line: 1, Message: "unclosed frontmatter (missing closing ---)"})
		return issues
	}

	fm := parseFrontmatter(content)

	name, hasName := fm["name"]
	if !hasName {
		issues = append(issues, lintIssue{File: path, Line: 1, Message: "frontmatter missing required field 'name'"})
	} else if name == "" {
		issues = append(issues, lintIssue{File: path, Line: 1, Message: "frontmatter 'name' is empty"})
	} else {
		expected := expectedFrontmatterName(path)
		if name != expected {
			issues = append(issues, lintIssue{File: path, Line: 1, Message: fmt.Sprintf("frontmatter name %q does not match expected %q", name, expected)})
		}
	}

	desc := fm["description"]
	if desc == "" {
		issues = append(issues, lintIssue{File: path, Line: 1, Message: "frontmatter missing required field 'description'"})
	}

	tools := fm["tools"]
	if tools == "" {
		issues = append(issues, lintIssue{File: path, Line: 1, Message: "frontmatter missing required field 'tools'"})
	}

	return issues
}

func lintShellBlocks(path, content string) []lintIssue {
	var issues []lintIssue

	lines := strings.Split(content, "\n")
	inBlock := false
	var blockLines []string
	blockStart := 0

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inBlock && (trimmed == "```bash" || trimmed == "```sh") {
			inBlock = true
			blockStart = i + 1
			blockLines = nil
			continue
		}
		if inBlock && trimmed == "```" {
			inBlock = false
			if len(blockLines) > 0 {
				script := shellScriptFromBlock(blockLines)
				if !hasTemplatePlaceholders(script) {
					if err := checkBashSyntax(script); err != nil {
						issues = append(issues, lintIssue{
							File:    path,
							Line:    blockStart + 1,
							Message: fmt.Sprintf("shell syntax error: %s", err),
						})
					}
				}
			}
			continue
		}
		if inBlock {
			blockLines = append(blockLines, line)
		}
	}

	return issues
}

// hasTemplatePlaceholders returns true if the script contains <placeholder>
// patterns, indicating documentation templates that aren't runnable code.
func hasTemplatePlaceholders(script string) bool {
	for i := 0; i < len(script); i++ {
		if script[i] == '<' {
			// end is relative to script[i+1:], so script[i+1:i+1+end] is the
			// content between < and > (exclusive). Safe because end is bounded
			// by IndexByte searching within the same substring.
			end := strings.IndexByte(script[i+1:], '>')
			if end > 0 && end < 40 && !strings.ContainsAny(script[i+1:i+1+end], " \t\n") {
				return true
			}
		}
	}
	return false
}

func checkBashSyntax(script string) error {
	if _, err := exec.LookPath("bash"); err != nil {
		return nil // bash not available, skip check
	}

	cmd := exec.Command("bash", "-n")
	cmd.Stdin = strings.NewReader(script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if msg != "" {
			if idx := strings.Index(msg, "\n"); idx >= 0 {
				msg = msg[:idx]
			}
			return fmt.Errorf("%s", msg)
		}
		return err
	}
	return nil
}

func lintShell(path string) []lintIssue {
	var issues []lintIssue

	data, err := os.ReadFile(path)
	if err != nil {
		return []lintIssue{{File: path, Message: fmt.Sprintf("read error: %v", err)}}
	}

	content := string(data)
	lines := strings.Split(content, "\n")

	// Check shebang
	if len(lines) > 0 && !strings.HasPrefix(lines[0], "#!") {
		issues = append(issues, lintIssue{File: path, Line: 1, Message: "shell script missing shebang line"})
	}

	// Syntax check
	if err := checkBashSyntax(content); err != nil {
		issues = append(issues, lintIssue{File: path, Message: fmt.Sprintf("syntax error: %s", err)})
	}

	return issues
}

func lintHarnessJSONFile(path string) []lintIssue {
	var issues []lintIssue

	data, err := os.ReadFile(path)
	if err != nil {
		return []lintIssue{{File: path, Message: fmt.Sprintf("read error: %v", err)}}
	}

	var hj map[string]any
	if err := json.Unmarshal(data, &hj); err != nil {
		return []lintIssue{{File: path, Message: fmt.Sprintf("invalid JSON: %v", err)}}
	}

	name, ok := hj["name"]
	if !ok {
		issues = append(issues, lintIssue{File: path, Message: "missing required field 'name'"})
	} else if nameStr, ok := name.(string); !ok || nameStr == "" {
		issues = append(issues, lintIssue{File: path, Message: "'name' must be a non-empty string"})
	} else if !validName.MatchString(nameStr) {
		issues = append(issues, lintIssue{File: path, Message: fmt.Sprintf("'name' %q does not match naming convention (alphanumeric, hyphens, underscores, dots)", nameStr)})
	}

	version, ok := hj["version"]
	if !ok {
		issues = append(issues, lintIssue{File: path, Message: "missing required field 'version'"})
	} else if vStr, ok := version.(string); !ok || vStr == "" {
		issues = append(issues, lintIssue{File: path, Message: "'version' must be a non-empty string"})
	}

	if includes, ok := hj["includes"]; ok {
		arr, ok := includes.([]any)
		if !ok {
			issues = append(issues, lintIssue{File: path, Message: "'includes' must be an array"})
		} else {
			for i, item := range arr {
				inc, ok := item.(map[string]any)
				if !ok {
					issues = append(issues, lintIssue{File: path, Message: fmt.Sprintf("includes[%d] must be an object", i)})
					continue
				}
				if _, ok := inc["git"]; !ok {
					issues = append(issues, lintIssue{File: path, Message: fmt.Sprintf("includes[%d] missing required field 'git'", i)})
				}
			}
		}
	}

	if delegates, ok := hj["delegates_to"]; ok {
		arr, ok := delegates.([]any)
		if !ok {
			issues = append(issues, lintIssue{File: path, Message: "'delegates_to' must be an array"})
		} else {
			for i, item := range arr {
				del, ok := item.(map[string]any)
				if !ok {
					issues = append(issues, lintIssue{File: path, Message: fmt.Sprintf("delegates_to[%d] must be an object", i)})
					continue
				}
				if _, ok := del["git"]; !ok {
					issues = append(issues, lintIssue{File: path, Message: fmt.Sprintf("delegates_to[%d] missing required field 'git'", i)})
				}
			}
		}
	}

	// Validate hooks
	hookIssues := validateHarnessHooks(hj)
	for _, msg := range hookIssues {
		issues = append(issues, lintIssue{File: path, Message: msg})
	}

	// Validate MCP servers
	mcpIssues := validateHarnessMCPServers(hj)
	for _, msg := range mcpIssues {
		issues = append(issues, lintIssue{File: path, Message: msg})
	}

	return issues
}

// shellScriptFromBlock extracts the checkable shell from a fenced block.
//
// A block whose lines are prefixed with "$ " is a *transcript*: the prefixed
// lines are commands and everything else is their output. Piping the whole
// thing to `bash -n` lints the output as if it were code, which is how
// `docs/vendors.md` failed on a `ynh vendors` table containing
// "(ollama · qwen3)" — an unbalanced parenthesis in a column of results, not a
// syntax error in anything anyone would run.
//
// A block with no prompt is an ordinary script and is returned whole.
//
// Continuations are followed: a command ending in a backslash keeps consuming
// lines until one does not, so a wrapped invocation is linted as the single
// command it is rather than as two fragments.
func shellScriptFromBlock(lines []string) string {
	const prompt = "$ "
	hasPrompt := false
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), prompt) {
			hasPrompt = true
			break
		}
	}
	if !hasPrompt {
		return strings.Join(lines, "\n")
	}

	var cmds []string
	continuing := false
	for _, l := range lines {
		t := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(t, prompt):
			cmd := strings.TrimPrefix(t, prompt)
			cmds = append(cmds, cmd)
			continuing = strings.HasSuffix(cmd, "\\")
		case continuing:
			cmds = append(cmds, t)
			continuing = strings.HasSuffix(t, "\\")
		default:
			// Output. Not shell, and not ours to check.
		}
	}
	return strings.Join(cmds, "\n")
}
