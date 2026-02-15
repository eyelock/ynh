package assembler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/persona"
	"github.com/eyelock/ynh/internal/resolver"
	"github.com/eyelock/ynh/internal/vendor"
)

// AssembleDelegates generates agent files for each delegate persona
// in the assembled config directory. Each delegate becomes a vendor-native
// agent that the parent persona can invoke.
func AssembleDelegates(workDir string, adapter vendor.Adapter, delegates []persona.Delegate) error {
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
		basePath, err := resolver.ResolveGitSource(del.GitSource)
		if err != nil {
			return fmt.Errorf("delegate: %w", err)
		}

		delPersona, err := persona.LoadPluginDir(basePath)
		if err != nil {
			return fmt.Errorf("loading delegate persona %s: %w", del.Git, err)
		}

		agentContent := buildDelegateAgent(delPersona, basePath)
		agentFile := filepath.Join(agentsPath, delPersona.Name+".md")
		if err := os.WriteFile(agentFile, []byte(agentContent), 0o644); err != nil {
			return fmt.Errorf("writing delegate agent %s: %w", delPersona.Name, err)
		}
	}

	return nil
}

// buildDelegateAgent generates a markdown agent file for a delegate persona.
// It includes the delegate's rules as inline context and lists available skills.
func buildDelegateAgent(p *persona.Persona, basePath string) string {
	var b strings.Builder

	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", p.Name)
	fmt.Fprintf(&b, "description: Delegate persona %q. Use when the user requests tasks that %s handles.\n", p.Name, p.Name)
	b.WriteString("---\n\n")

	fmt.Fprintf(&b, "You are the **%s** persona, invoked as a delegate.\n\n", p.Name)

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
