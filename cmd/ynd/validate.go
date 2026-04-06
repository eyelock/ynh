package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func cmdValidate(args []string) error {
	root := "."
	if len(args) > 0 {
		root = args[0]
	}

	info, err := os.Stat(root)
	if err != nil {
		return err
	}

	// Single file: validate just that file
	if !info.IsDir() {
		return validateFile(root)
	}

	// Directory: find harness roots and validate them
	harnesses := findHarnessRoots(root)
	if len(harnesses) == 0 {
		// Maybe we're inside a harness directory
		if isHarnessRoot(root) {
			return validateHarness(root)
		}
		fmt.Println("No harness directories found.")
		fmt.Println("A harness requires .claude-plugin/plugin.json")
		return nil
	}

	hasError := false
	for _, p := range harnesses {
		if err := validateHarness(p); err != nil {
			hasError = true
		}
	}

	if hasError {
		return fmt.Errorf("validation failed")
	}
	return nil
}

func validateFile(path string) error {
	base := filepath.Base(path)
	var issues []lintIssue

	switch {
	case base == "plugin.json":
		issues = lintPluginJSON(path)
	case base == "metadata.json":
		issues = lintMetadataJSON(path)
	case strings.HasSuffix(path, ".md"):
		issues = lintMarkdown(path)
	default:
		return fmt.Errorf("don't know how to validate %q", path)
	}

	for _, issue := range issues {
		if issue.Line > 0 {
			fmt.Printf("%s:%d: %s\n", issue.File, issue.Line, issue.Message)
		} else {
			fmt.Printf("%s: %s\n", issue.File, issue.Message)
		}
	}

	if len(issues) > 0 {
		return fmt.Errorf("validation failed")
	}
	fmt.Printf("%s: valid\n", path)
	return nil
}

func isHarnessRoot(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	return err == nil
}

func findHarnessRoots(root string) []string {
	// Check if root itself is a harness
	if isHarnessRoot(root) {
		return []string{root}
	}

	// Check immediate children
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}

	var roots []string
	for _, entry := range entries {
		if entry.IsDir() {
			child := filepath.Join(root, entry.Name())
			if isHarnessRoot(child) {
				roots = append(roots, child)
			}
		}
	}

	return roots
}

func validateHarness(dir string) error {
	rel, _ := filepath.Rel(".", dir)
	if rel == "" {
		rel = dir
	}

	var issues []string

	// Check plugin.json exists and is valid
	pluginPath := filepath.Join(dir, ".claude-plugin", "plugin.json")
	data, err := os.ReadFile(pluginPath)
	if err != nil {
		issues = append(issues, "missing .claude-plugin/plugin.json")
	} else {
		var pj map[string]any
		if err := json.Unmarshal(data, &pj); err != nil {
			issues = append(issues, fmt.Sprintf("invalid plugin.json: %v", err))
		} else {
			if _, ok := pj["name"]; !ok {
				issues = append(issues, "plugin.json missing 'name'")
			}
			if _, ok := pj["version"]; !ok {
				issues = append(issues, "plugin.json missing 'version'")
			}
		}
	}

	// Check for conflicting instructions files
	hasInstructions := fileExists(filepath.Join(dir, "instructions.md"))
	hasAgentsMD := fileExists(filepath.Join(dir, "AGENTS.md"))
	if hasInstructions && hasAgentsMD {
		// Both exist — check if they have different content
		instrData, _ := os.ReadFile(filepath.Join(dir, "instructions.md"))
		agentsData, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
		if string(instrData) != string(agentsData) {
			issues = append(issues, "both instructions.md and AGENTS.md exist with different content — remove one or make them identical")
		}
	}

	// Check metadata.json if present
	metaPath := filepath.Join(dir, "metadata.json")
	if data, err := os.ReadFile(metaPath); err == nil {
		var meta map[string]any
		if err := json.Unmarshal(data, &meta); err != nil {
			issues = append(issues, fmt.Sprintf("invalid metadata.json: %v", err))
		}
	}

	// Validate skills
	skillsDir := filepath.Join(dir, "skills")
	if entries, err := os.ReadDir(skillsDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			skillFile := filepath.Join(skillsDir, entry.Name(), "SKILL.md")
			if _, err := os.Stat(skillFile); os.IsNotExist(err) {
				issues = append(issues, fmt.Sprintf("skills/%s/ missing SKILL.md", entry.Name()))
				continue
			}
			// Validate skill frontmatter
			content, err := os.ReadFile(skillFile)
			if err != nil {
				continue
			}
			fm := parseFrontmatter(string(content))
			if fm == nil {
				issues = append(issues, fmt.Sprintf("skills/%s/SKILL.md missing frontmatter", entry.Name()))
			} else {
				if fm["name"] == "" {
					issues = append(issues, fmt.Sprintf("skills/%s/SKILL.md missing 'name' field", entry.Name()))
				} else if fm["name"] != entry.Name() {
					issues = append(issues, fmt.Sprintf("skills/%s/SKILL.md name %q does not match directory", entry.Name(), fm["name"]))
				}
				if fm["description"] == "" {
					issues = append(issues, fmt.Sprintf("skills/%s/SKILL.md missing 'description' field", entry.Name()))
				}
			}
		}
	}

	// Validate agents
	agentsDir := filepath.Join(dir, "agents")
	if entries, err := os.ReadDir(agentsDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			agentFile := filepath.Join(agentsDir, entry.Name())
			content, err := os.ReadFile(agentFile)
			if err != nil {
				continue
			}
			agentName := strings.TrimSuffix(entry.Name(), ".md")
			fm := parseFrontmatter(string(content))
			if fm == nil {
				issues = append(issues, fmt.Sprintf("agents/%s missing frontmatter", entry.Name()))
			} else {
				if fm["name"] == "" {
					issues = append(issues, fmt.Sprintf("agents/%s missing 'name' field", entry.Name()))
				} else if fm["name"] != agentName {
					issues = append(issues, fmt.Sprintf("agents/%s name %q does not match filename", entry.Name(), fm["name"]))
				}
				if fm["description"] == "" {
					issues = append(issues, fmt.Sprintf("agents/%s missing 'description' field", entry.Name()))
				}
				if fm["tools"] == "" {
					issues = append(issues, fmt.Sprintf("agents/%s missing 'tools' field", entry.Name()))
				}
			}
		}
	}

	// Check for unexpected non-markdown files in artifact dirs
	for _, dirName := range []string{"agents", "rules", "commands"} {
		artDir := filepath.Join(dir, dirName)
		if entries, err := os.ReadDir(artDir); err == nil {
			for _, entry := range entries {
				if !entry.IsDir() && !strings.HasSuffix(entry.Name(), ".md") {
					issues = append(issues, fmt.Sprintf("%s/%s: unexpected non-markdown file", dirName, entry.Name()))
				}
			}
		}
	}

	if len(issues) > 0 {
		fmt.Printf("%s: INVALID\n", rel)
		for _, issue := range issues {
			fmt.Printf("  - %s\n", issue)
		}
		return fmt.Errorf("harness %q has %d issue(s)", rel, len(issues))
	}

	fmt.Printf("%s: valid\n", rel)
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
