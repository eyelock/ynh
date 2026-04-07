package assembler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/resolver"
	"github.com/eyelock/ynh/internal/vendor"
)

// AssembleDelegates generates agent files for each delegate harness
// in the assembled config directory. Each delegate becomes a vendor-native
// agent that the parent harness can invoke.
func AssembleDelegates(workDir string, adapter vendor.Adapter, delegates []harness.Delegate) error {
	if len(delegates) == 0 {
		return nil
	}

	agentsDir, ok := adapter.ArtifactDirs()["agents"]
	if !ok {
		return nil
	}

	configDir := filepath.Join(workDir, adapter.ConfigDir())
	agentsPath := filepath.Join(configDir, agentsDir)
	if err := os.MkdirAll(agentsPath, 0o755); err != nil {
		return err
	}

	for _, del := range delegates {
		basePath, _, err := resolver.ResolveGitSourceFromCache(del.GitSource)
		if err != nil {
			return fmt.Errorf("delegate: %w", err)
		}

		delHarness, err := harness.LoadDir(basePath)
		if err != nil {
			return fmt.Errorf("loading delegate harness %s: %w", del.Git, err)
		}

		agentContent := BuildDelegateAgent(delHarness, basePath)
		agentFile := filepath.Join(agentsPath, delHarness.Name+".md")
		if err := os.WriteFile(agentFile, []byte(agentContent), 0o644); err != nil {
			return fmt.Errorf("writing delegate agent %s: %w", delHarness.Name, err)
		}
	}

	return nil
}

// BuildDelegateAgent generates a markdown agent file for a delegate harness.
// It includes the delegate's instructions, rules, and available skills.
func BuildDelegateAgent(p *harness.Harness, basePath string) string {
	var b strings.Builder

	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", p.Name)
	if p.Description != "" {
		fmt.Fprintf(&b, "description: %s\n", p.Description)
	} else {
		fmt.Fprintf(&b, "description: Delegate harness %q.\n", p.Name)
	}
	b.WriteString("---\n\n")

	fmt.Fprintf(&b, "You are the **%s** harness, invoked as a delegate.\n\n", p.Name)

	// Include harness instructions (instructions.md / CLAUDE.md)
	instructions := readInstructionsFrom(basePath)
	if instructions != "" {
		b.WriteString("## Instructions\n\n")
		b.WriteString(instructions)
		b.WriteString("\n\n")
	}

	// Inline rules as context
	rules := readRulesFrom(basePath)
	if len(rules) > 0 {
		b.WriteString("## Rules\n\n")
		for name, content := range rules {
			fmt.Fprintf(&b, "### %s\n\n%s\n\n", name, content)
		}
	}

	// List available skills
	skills := listSkillsFrom(basePath)
	if len(skills) > 0 {
		b.WriteString("## Available Skills\n\n")
		for _, skill := range skills {
			fmt.Fprintf(&b, "- %s\n", skill)
		}
		b.WriteString("\n")
	}

	return b.String()
}

// readInstructionsFrom reads the harness's instructions file.
// Checks instructions.md first, then AGENTS.md, then CLAUDE.md as fallback.
func readInstructionsFrom(basePath string) string {
	for _, name := range []string{"instructions.md", "AGENTS.md", "CLAUDE.md"} {
		data, err := os.ReadFile(filepath.Join(basePath, name))
		if err == nil {
			return strings.TrimSpace(string(data))
		}
	}
	return ""
}

// readRulesFrom reads all rule markdown files from basePath/rules/.
func readRulesFrom(basePath string) map[string]string {
	rulesDir := filepath.Join(basePath, "rules")
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		return nil
	}

	rules := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(rulesDir, entry.Name()))
		if err != nil {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".md")
		rules[name] = strings.TrimSpace(string(data))
	}
	return rules
}

// listSkillsFrom discovers skill directories under basePath/skills/.
func listSkillsFrom(basePath string) []string {
	skillsDir := filepath.Join(basePath, "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return nil
	}

	var skills []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillMD := filepath.Join(skillsDir, entry.Name(), "SKILL.md")
		if _, err := os.Stat(skillMD); err == nil {
			skills = append(skills, entry.Name())
		}
	}
	return skills
}
