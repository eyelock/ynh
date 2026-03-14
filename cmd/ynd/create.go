package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

var validName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

func cmdCreate(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: ynd create <type> <name>\n\nTypes: skill, agent, persona, rule, command")
	}

	kind := args[0]
	name := args[1]

	if !validName.MatchString(name) {
		return fmt.Errorf("invalid name %q: must start with a letter or digit and contain only alphanumeric, hyphens, underscores, or dots", name)
	}

	switch kind {
	case "skill":
		return createSkill(name)
	case "agent":
		return createAgent(name)
	case "persona":
		return createPersona(name)
	case "rule":
		return createRule(name)
	case "command":
		return createCommand(name)
	default:
		return fmt.Errorf("unknown type %q: must be skill, agent, persona, rule, or command", kind)
	}
}

func createSkill(name string) error {
	dir := filepath.Join("skills", name)
	path := filepath.Join(dir, "SKILL.md")

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", path)
	}

	content := fmt.Sprintf(`---
name: %s
description: Describe what this skill does and when it should be used.
---

# %s

## Instructions

- Step-by-step instructions for the AI agent
- Be specific about expected behavior
- Include examples where helpful
`, name, name)

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}

	fmt.Printf("Created %s\n", path)
	return nil
}

func createAgent(name string) error {
	dir := "agents"
	path := filepath.Join(dir, name+".md")

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", path)
	}

	content := fmt.Sprintf(`---
name: %s
description: Describe this agent's purpose and when to delegate to it.
tools: Read, Grep, Glob
---

You are a specialist agent. When delegated to:

- Define your core responsibilities
- Specify what you analyze or produce
- Note any constraints or guidelines

Provide actionable output, not just observations.
`, name)

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}

	fmt.Printf("Created %s\n", path)
	return nil
}

func createPersona(name string) error {
	if isPersonaRoot(".") {
		return fmt.Errorf("already inside a persona directory — create personas from outside")
	}
	if _, err := os.Stat(name); err == nil {
		return fmt.Errorf("directory %q already exists", name)
	}

	dirs := []string{
		filepath.Join(name, ".claude-plugin"),
		filepath.Join(name, "skills"),
		filepath.Join(name, "agents"),
		filepath.Join(name, "rules"),
		filepath.Join(name, "commands"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	pluginJSON := map[string]string{
		"name":        name,
		"version":     "0.1.0",
		"description": "",
	}
	data, _ := json.MarshalIndent(pluginJSON, "", "  ")
	if err := os.WriteFile(filepath.Join(name, ".claude-plugin", "plugin.json"), append(data, '\n'), 0o644); err != nil {
		return err
	}

	metadataJSON := map[string]any{
		"ynh": map[string]any{
			"default_vendor": "claude",
		},
	}
	data, _ = json.MarshalIndent(metadataJSON, "", "  ")
	if err := os.WriteFile(filepath.Join(name, "metadata.json"), append(data, '\n'), 0o644); err != nil {
		return err
	}

	instructions := fmt.Sprintf(`# %s

Project-level instructions that apply to every session with this persona.
`, name)
	if err := os.WriteFile(filepath.Join(name, "instructions.md"), []byte(instructions), 0o644); err != nil {
		return err
	}

	fmt.Printf("Created persona %q:\n", name)
	fmt.Printf("  %s/.claude-plugin/plugin.json\n", name)
	fmt.Printf("  %s/metadata.json\n", name)
	fmt.Printf("  %s/instructions.md\n", name)
	fmt.Printf("  %s/skills/\n", name)
	fmt.Printf("  %s/agents/\n", name)
	fmt.Printf("  %s/rules/\n", name)
	fmt.Printf("  %s/commands/\n", name)
	return nil
}

func createRule(name string) error {
	dir := "rules"
	path := filepath.Join(dir, name+".md")

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", path)
	}

	content := "Describe the rule or constraint that should always be followed.\n"

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}

	fmt.Printf("Created %s\n", path)
	return nil
}

func createCommand(name string) error {
	dir := "commands"
	path := filepath.Join(dir, name+".md")

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", path)
	}

	content := fmt.Sprintf(`# %s

Describe what this command does.

`+"```bash\n# Add your command here\n```"+`

If any step fails, fix the issue and re-run.
`, name)

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}

	fmt.Printf("Created %s\n", path)
	return nil
}
