package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func cmdInspect(args []string) error {
	vendor, skipConfirm, outputDir, err := parseInspectArgs(args)
	if err != nil {
		return err
	}

	// Env var fallback for skip-confirm
	if !skipConfirm {
		skipConfirm = skipConfirmEnv()
	}

	// Auto-detect vendor CLI
	if vendor == "" {
		vendor = resolveVendorEnv()
	}
	if vendor == "" {
		vendor = detectVendorCLI()
		if vendor == "" {
			fmt.Println("No supported LLM CLI found (checked: claude, codex).")
			fmt.Println("Inspect requires an LLM to analyze your codebase.")
			fmt.Println()
			fmt.Println("Install one of:")
			fmt.Println("  claude  → https://docs.anthropic.com/claude-code")
			fmt.Println("  codex   → https://openai.com/codex")
			fmt.Println()
			fmt.Println("Or specify one explicitly: ynd inspect -v claude")
			return nil
		}
	} else {
		if _, err := lookPathFunc(vendor); err != nil {
			return fmt.Errorf("vendor CLI %q not found on PATH", vendor)
		}
	}

	fmt.Printf("Using %s for analysis.\n\n", vendor)

	root := "."

	// Determine where to write artifacts
	if outputDir == "" {
		outputDir = filepath.Join(root, "."+vendor)
	}

	// Scan project signals
	signals := scanSignals(root)
	if len(signals) == 0 {
		fmt.Println("No recognizable project files found. Are you in a project directory?")
		return nil
	}

	// Discover existing skills and agents (check both root and vendor dirs)
	existingSkills := discoverExistingSkillsAll(root)
	existingAgents := discoverExistingAgentsAll(root)

	// Step 1: Project understanding
	fmt.Println("─── Step 1: Project Understanding ───")
	fmt.Println()

	overview, err := analyzeProject(vendor, root, signals)
	if err != nil {
		return fmt.Errorf("project analysis failed: %w", err)
	}

	fmt.Println(overview)
	fmt.Println()

	if !skipConfirm {
		action := promptAction("Does this look right? [c]ontinue / [r]efine / [q]uit: ", "c", "r", "q")
		switch action {
		case "q":
			fmt.Println("Stopped.")
			return nil
		case "r":
			refinement := promptInput("What should I adjust? ")
			refined, refErr := refineAnalysis(vendor, overview, refinement)
			if refErr != nil {
				fmt.Fprintf(os.Stderr, "Refinement failed: %v\n", refErr)
			} else {
				overview = refined
				fmt.Println()
				fmt.Println(overview)
				fmt.Println()
			}
		}
	}

	// Step 2: Review existing artifacts
	if len(existingSkills) > 0 || len(existingAgents) > 0 {
		fmt.Println()
		fmt.Println("─── Step 2: Review Existing Artifacts ───")
		fmt.Println()

		for _, skill := range existingSkills {
			if reviewErr := reviewExistingArtifact(vendor, root, overview, skill, "skill", skipConfirm); reviewErr != nil {
				fmt.Fprintf(os.Stderr, "  %s: %v\n", skill, reviewErr)
			}
		}

		for _, agent := range existingAgents {
			if reviewErr := reviewExistingArtifact(vendor, root, overview, agent, "agent", skipConfirm); reviewErr != nil {
				fmt.Fprintf(os.Stderr, "  %s: %v\n", agent, reviewErr)
			}
		}
	}

	// Step 3: Propose new skills/agents
	fmt.Println()
	fmt.Println("─── Step 3: New Artifact Proposals ───")
	fmt.Println()

	proposals, err := proposeNewArtifacts(vendor, root, overview, signals, existingSkills, existingAgents)
	if err != nil {
		return fmt.Errorf("proposal generation failed: %w", err)
	}

	if strings.TrimSpace(proposals) == "" || strings.Contains(strings.ToLower(proposals), "no additional") {
		fmt.Println("No new artifacts suggested — your project looks well-covered.")
		return nil
	}

	fmt.Println(proposals)
	fmt.Println()

	if !skipConfirm {
		action := promptAction("Generate these? [y]es all / [w]alk through each / [s]kip: ", "y", "w", "s")
		switch action {
		case "s":
			fmt.Println("Skipped.")
			return nil
		case "y":
			return generateAllProposals(vendor, overview, proposals, root, outputDir)
		case "w":
			return walkthroughProposals(vendor, overview, proposals, root, outputDir)
		}
	} else {
		return generateAllProposals(vendor, overview, proposals, root, outputDir)
	}

	return nil
}

func parseInspectArgs(args []string) (vendor string, skipConfirm bool, outputDir string, err error) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-v", "--vendor":
			if i+1 >= len(args) {
				return "", false, "", fmt.Errorf("-v requires a vendor name")
			}
			vendor = args[i+1]
			i++
		case "-o", "--output-dir":
			if i+1 >= len(args) {
				return "", false, "", fmt.Errorf("-o requires a directory path")
			}
			outputDir = args[i+1]
			i++
		case "-h", "--help":
			return "", false, "", errHelp
		case "-y", "--yes":
			skipConfirm = true
		default:
			if strings.HasPrefix(args[i], "-") {
				return "", false, "", fmt.Errorf("unknown flag: %s", args[i])
			}
			return "", false, "", fmt.Errorf("unexpected argument: %s (inspect does not take file arguments)", args[i])
		}
	}
	return
}

// discoverExistingSkills finds SKILL.md files in skills/ subdirectories.
func discoverExistingSkills(root string) []string {
	skillsDir := filepath.Join(root, "skills")
	if _, err := os.Stat(skillsDir); os.IsNotExist(err) {
		return nil
	}

	var skills []string
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return nil
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillFile := filepath.Join(skillsDir, e.Name(), "SKILL.md")
		if _, err := os.Stat(skillFile); err == nil {
			skills = append(skills, skillFile)
		}
	}
	return skills
}

// discoverExistingAgents finds .md files in the agents/ directory.
func discoverExistingAgents(root string) []string {
	agentsDir := filepath.Join(root, "agents")
	if _, err := os.Stat(agentsDir); os.IsNotExist(err) {
		return nil
	}

	var agents []string
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return nil
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		agents = append(agents, filepath.Join(agentsDir, e.Name()))
	}
	return agents
}

// supportedVendors lists the vendor CLI names whose config dirs we search.
var supportedVendors = []string{"claude", "cursor", "codex"}

// discoverExistingSkillsAll finds SKILL.md files in skills/ at the project root
// and inside each supported vendor config directory (e.g. .claude/skills/).
func discoverExistingSkillsAll(root string) []string {
	dirs := []string{root}
	for _, v := range supportedVendors {
		dirs = append(dirs, filepath.Join(root, "."+v))
	}
	seen := make(map[string]bool)
	var all []string
	for _, dir := range dirs {
		for _, s := range discoverExistingSkills(dir) {
			key := s
			if abs, err := filepath.Abs(s); err == nil {
				key = abs
			}
			if !seen[key] {
				seen[key] = true
				all = append(all, s)
			}
		}
	}
	return all
}

// discoverExistingAgentsAll finds agent .md files in agents/ at the project root
// and inside each supported vendor config directory (e.g. .claude/agents/).
func discoverExistingAgentsAll(root string) []string {
	dirs := []string{root}
	for _, v := range supportedVendors {
		dirs = append(dirs, filepath.Join(root, "."+v))
	}
	seen := make(map[string]bool)
	var all []string
	for _, dir := range dirs {
		for _, a := range discoverExistingAgents(dir) {
			key := a
			if abs, err := filepath.Abs(a); err == nil {
				key = abs
			}
			if !seen[key] {
				seen[key] = true
				all = append(all, a)
			}
		}
	}
	return all
}

// readFileContext reads a file and truncates to maxLen bytes for LLM context.
func readFileContext(path string, maxLen int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	content := string(data)
	if len(content) > maxLen {
		content = content[:maxLen] + "\n... (truncated)"
	}
	return content
}

// buildSignalContext prepares signal file contents for LLM prompts.
func buildSignalContext(root string, signals []signal, maxFiles int) string {
	top := topSignalFiles(signals, maxFiles)
	var sb strings.Builder

	for _, s := range top {
		relPath, err := filepath.Rel(root, s.Path)
		if err != nil {
			relPath = s.Path
		}
		fmt.Fprintf(&sb, "### %s [%s]\n", relPath, s.Category)
		content := readFileContext(s.Path, 2000)
		if content != "" {
			sb.WriteString("```\n")
			sb.WriteString(content)
			sb.WriteString("\n```\n\n")
		}
	}
	return sb.String()
}

// analyzeProject asks the LLM to characterize the project.
func analyzeProject(vendor, root string, signals []signal) (string, error) {
	signalContext := buildSignalContext(root, signals, 15)

	groups := signalsByCategory(signals)
	var categories []string
	for _, cat := range categoryOrder {
		if sigs, ok := groups[cat]; ok {
			var names []string
			for _, s := range sigs {
				rel, err := filepath.Rel(root, s.Path)
				if err != nil {
					rel = s.Path
				}
				names = append(names, rel)
			}
			categories = append(categories, fmt.Sprintf("**%s**: %s", cat, strings.Join(names, ", ")))
		}
	}

	prompt := fmt.Sprintf(`You are inspecting a software project to understand its structure and suggest AI-powered development skills and agents.

Here is a summary of discovered project files by category:

%s

Here are the contents of the most important files:

%s

Provide a concise project characterization (3-8 bullet points):
- Primary language(s) and framework(s)
- Build system and dependency management
- Testing approach (framework, patterns)
- CI/CD setup
- Linting and formatting tools
- Release/deployment approach
- Key architectural patterns
- Notable conventions

Be specific and factual. Only mention what you can confirm from the files shown.`,
		strings.Join(categories, "\n"), signalContext)

	return queryLLM(vendor, prompt)
}

// refineAnalysis adjusts the project analysis based on user feedback.
func refineAnalysis(vendor, currentAnalysis, feedback string) (string, error) {
	prompt := fmt.Sprintf(`You previously characterized a project as follows:

%s

The developer says: %s

Update your characterization to address this feedback. Keep the same concise bullet-point format.
Output ONLY the updated characterization.`, currentAnalysis, feedback)

	return queryLLM(vendor, prompt)
}

// reviewExistingArtifact presents an existing skill/agent and optionally proposes an update.
func reviewExistingArtifact(vendor, root, overview, path, artifactType string, skipConfirm bool) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	relPath, relErr := filepath.Rel(root, path)
	if relErr != nil {
		relPath = path
	}

	fmt.Printf("  %s (%s)\n", relPath, artifactType)

	fm := parseFrontmatter(string(content))
	if fm != nil {
		if name := fm["name"]; name != "" {
			fmt.Printf("    Name: %s\n", name)
		}
		if desc := fm["description"]; desc != "" {
			fmt.Printf("    Description: %s\n", desc)
		}
	}

	if skipConfirm {
		return nil
	}

	action := promptAction("    [s]kip / [u]pdate / [v]iew: ", "s", "u", "v")

	switch action {
	case "s":
		return nil
	case "v":
		fmt.Println()
		fmt.Println(string(content))
		action = promptAction("    [s]kip / [u]pdate: ", "s", "u")
		if action == "s" {
			return nil
		}
	}

	// Ask LLM to propose an update
	signals := scanSignals(root)
	signalContext := buildSignalContext(root, signals, 10)

	toolsNote := ""
	if artifactType == "agent" {
		toolsNote = ", tools"
	}

	prompt := fmt.Sprintf(`You are updating an existing %s for an AI-powered development harness.

Project characterization:
%s

Relevant project files:
%s

Current %s content:
%s

Propose an updated version that:
- Preserves any domain-specific knowledge or learned conventions in the original
- Incorporates insights from the current project state
- Maintains the same format (YAML frontmatter with name, description%s + markdown body)
- Keeps the same name

Output ONLY the complete updated file content, nothing else.`,
		artifactType, overview, signalContext, artifactType, string(content), toolsNote)

	updated, llmErr := queryLLM(vendor, prompt)
	if llmErr != nil {
		return fmt.Errorf("update generation failed: %w", llmErr)
	}
	updated = extractContent(updated)

	fmt.Println()
	fmt.Println("--- Proposed Update ---")
	fmt.Println(updated)
	fmt.Println("--- End ---")

	applyAction := promptAction("    [a]pply / [s]kip: ", "a", "s")
	if applyAction == "a" {
		if writeErr := os.WriteFile(path, []byte(updated), 0o644); writeErr != nil {
			return writeErr
		}
		fmt.Printf("    Updated %s\n", relPath)
	}

	return nil
}

// proposeNewArtifacts asks the LLM what new skills/agents would benefit the project.
func proposeNewArtifacts(vendor, root, overview string, signals []signal, existingSkills, existingAgents []string) (string, error) {
	signalContext := buildSignalContext(root, signals, 10)

	var existing []string
	for _, s := range existingSkills {
		rel, _ := filepath.Rel(root, s)
		existing = append(existing, "skill: "+rel)
	}
	for _, a := range existingAgents {
		rel, _ := filepath.Rel(root, a)
		existing = append(existing, "agent: "+rel)
	}

	existingList := "None"
	if len(existing) > 0 {
		existingList = strings.Join(existing, "\n")
	}

	prompt := fmt.Sprintf(`You are suggesting new AI development skills and agents for a project.

Project characterization:
%s

Relevant project files:
%s

Existing artifacts (do NOT duplicate these):
%s

Based on the project's technology stack and workflows, suggest new skills and agents. Consider:

**Skills** (reusable workflows a developer triggers):
- CI/CD workflows (running tests, deploying, checking pipelines)
- Development workflows (building, formatting, linting)
- Release processes (versioning, changelog, tagging)
- Testing patterns specific to this project's framework
- Contributing guidelines as actionable steps

**Agents** (specialist delegates for complex tasks):
- Code reviewers that know the project's patterns
- Test generators that understand the testing framework
- Documentation maintainers
- Architecture advisors for the specific tech stack

For each suggestion, provide:
1. Type (skill or agent)
2. Proposed name
3. One-line description
4. Why it's useful for THIS specific project

Format as a numbered list. Be specific to this project — don't suggest generic things.
If the project is already well-covered, say "No additional artifacts needed."`, overview, signalContext, existingList)

	return queryLLM(vendor, prompt)
}

// generateAllProposals generates all proposed artifacts at once.
func generateAllProposals(vendor, overview, proposalText, root, outputDir string) error {
	proposals := parseProposals(proposalText)
	if len(proposals) == 0 {
		fmt.Println("Could not parse any proposals from the suggestions.")
		return nil
	}

	generated := 0
	for _, p := range proposals {
		fmt.Printf("  Generating %s: %s\n", p.Type, p.Name)
		content, err := generateSingleArtifact(vendor, overview, p.Line, root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "    Failed: %v\n", err)
			continue
		}

		content = ensureFrontmatter(content, p)
		if err := writeArtifact(content, p, outputDir); err != nil {
			fmt.Fprintf(os.Stderr, "    Write failed: %v\n", err)
			continue
		}
		generated++
	}

	if generated > 0 {
		fmt.Printf("\nGenerated %d artifact(s).\n", generated)
	}
	return nil
}

// walkthroughProposals walks through each proposal one by one.
func walkthroughProposals(vendor, overview, proposalText, root, outputDir string) error {
	proposals := parseProposals(proposalText)
	if len(proposals) == 0 {
		fmt.Println("Could not parse any proposals from the suggestions.")
		return nil
	}

	for _, p := range proposals {
		fmt.Printf("\n  %s\n", p.Line)
		action := promptAction("  [g]enerate / [s]kip / [q]uit: ", "g", "s", "q")

		switch action {
		case "q":
			fmt.Println("Stopped.")
			return nil
		case "s":
			continue
		case "g":
			content, err := generateSingleArtifact(vendor, overview, p.Line, root)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  Generation failed: %v\n", err)
				continue
			}

			content = ensureFrontmatter(content, p)
			fmt.Println()
			fmt.Println(content)
			fmt.Println()

			writeAction := promptAction("  [w]rite / [s]kip: ", "w", "s")
			if writeAction == "w" {
				if writeErr := writeArtifact(content, p, outputDir); writeErr != nil {
					fmt.Fprintf(os.Stderr, "  Write failed: %v\n", writeErr)
				}
			}
		}
	}

	return nil
}

// proposal represents a parsed artifact suggestion from the LLM.
type proposal struct {
	Type string // "skill" or "agent"
	Name string // e.g. "ynh-add-vendor"
	Line string // the full original line
}

// proposalPattern matches lines like:
//  1. **Skill: `ynh-add-vendor`** — description
//  1. **Skill** — `go-dev` — description
var proposalPattern = regexp.MustCompile(`(?i)^\d+\.\s+\*{0,2}(skill|agent)(?:\*{0,2}\s*[—–-]+\s*|[:\s]+)` + "`?" + `([a-zA-Z0-9][a-zA-Z0-9._-]*)` + "`?" + `\*{0,2}`)

// parseProposals extracts typed proposals from the LLM's numbered list.
func parseProposals(text string) []proposal {
	var proposals []proposal
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		m := proposalPattern.FindStringSubmatch(trimmed)
		if m == nil {
			continue
		}
		proposals = append(proposals, proposal{
			Type: strings.ToLower(m[1]),
			Name: m[2],
			Line: trimmed,
		})
	}
	return proposals
}

// generateSingleArtifact asks the LLM to generate one skill or agent file.
func generateSingleArtifact(vendor, overview, proposal, root string) (string, error) {
	signals := scanSignals(root)
	signalContext := buildSignalContext(root, signals, 8)

	prompt := fmt.Sprintf(`Generate a complete file for this proposed artifact:

%s

Project characterization:
%s

Relevant project files for context:
%s

Rules:
- Start the output with exactly --- on the first line (YAML frontmatter opener)
- If it's a skill: frontmatter must have lowercase fields: name, description
- If it's an agent: frontmatter must have lowercase fields: name, description, tools
- Do NOT wrap output in code fences or add any preamble
- Be specific to this project's tech stack and conventions
- Include actionable steps, not vague guidelines
- Reference actual files and patterns from this project

Example skill format:
---
name: my-skill
description: What this skill does.
---

## Instructions
...

Example agent format:
---
name: my-agent
description: What this agent does.
tools: Read, Grep, Glob
---

You are a specialist agent...

Output ONLY the raw file content starting with ---.`, proposal, overview, signalContext)

	return queryLLM(vendor, prompt)
}

// extractContent strips common LLM wrapping (code fences, preamble) to find
// the actual file content starting with YAML frontmatter.
func extractContent(raw string) string {
	// Strip markdown code fences: ```markdown\n...\n``` or ```\n...\n```
	for _, fence := range []string{"```markdown\n", "```md\n", "```yaml\n", "```\n"} {
		if idx := strings.Index(raw, fence); idx >= 0 {
			inner := raw[idx+len(fence):]
			if end := strings.LastIndex(inner, "\n```"); end >= 0 {
				raw = inner[:end]
			} else {
				raw = inner
			}
			break
		}
	}

	// Find frontmatter: look for ---\n followed by a name: field
	// This handles preamble text, explanations, etc.
	for i := 0; i+4 <= len(raw); i++ {
		if (i == 0 || raw[i-1] == '\n') && raw[i:i+4] == "---\n" {
			// Check if what follows looks like frontmatter (key: value lines)
			rest := raw[i+4:]
			if strings.Contains(rest, ":") {
				endIdx := strings.Index(rest, "\n---")
				if endIdx >= 0 {
					raw = raw[i:]
					break
				}
			}
		}
	}

	return strings.TrimSpace(raw) + "\n"
}

// ensureFrontmatter strips LLM wrapping and guarantees the content starts with
// correct frontmatter using the proposal's name and type as the source of truth.
func ensureFrontmatter(raw string, p proposal) string {
	raw = extractContent(raw)

	// Try to find the body after any existing frontmatter
	_, body := splitFrontmatter(raw)
	if body == "" {
		// No frontmatter found — the entire output is the body
		body = raw
	}

	// Strip leading blank lines from body
	body = strings.TrimLeft(body, "\n")

	// Build correct frontmatter from the proposal
	var fm string
	if p.Type == "agent" {
		// Try to salvage a tools line from the LLM output
		tools := "Read, Grep, Glob"
		if existing := parseFrontmatter(raw); existing != nil {
			if t := existing["tools"]; t != "" {
				tools = t
			}
		}
		fm = fmt.Sprintf("---\nname: %s\ndescription: %s\ntools: %s\n---\n", p.Name, extractDescription(p.Line), tools)
	} else {
		fm = fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n", p.Name, extractDescription(p.Line))
	}

	if body != "" {
		return fm + "\n" + body
	}
	return fm
}

// extractDescription pulls the description from a proposal line.
// e.g. "1. **Skill: `name`** — Some description here." -> "Some description here."
func extractDescription(line string) string {
	// Look for em-dash or regular dash separator
	for _, sep := range []string{" — ", " - ", "—"} {
		if idx := strings.Index(line, sep); idx >= 0 {
			desc := strings.TrimSpace(line[idx+len(sep):])
			// Trim trailing period for cleaner frontmatter
			desc = strings.TrimRight(desc, ".")
			if len(desc) > 120 {
				// Truncate to first sentence for frontmatter
				if dot := strings.Index(desc, ". "); dot > 0 {
					desc = desc[:dot]
				} else if len(desc) > 120 {
					desc = desc[:120]
				}
			}
			return desc
		}
	}
	return "Generated by ynd inspect"
}

// writeArtifact writes content to the correct path based on the proposal type and name.
func writeArtifact(content string, p proposal, root string) error {
	var path string
	if p.Type == "agent" {
		dir := filepath.Join(root, "agents")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		path = filepath.Join(dir, p.Name+".md")
	} else {
		dir := filepath.Join(root, "skills", p.Name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		path = filepath.Join(dir, "SKILL.md")
	}

	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists — use update flow instead", path)
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}

	fmt.Printf("  Wrote %s\n", path)
	return nil
}

// promptActionFunc and promptInputFunc are replaceable for testing.
var promptActionFunc = promptActionImpl
var promptInputFunc = promptInputImpl

// promptAction shows a prompt and returns one of the valid choices.
func promptAction(msg string, choices ...string) string {
	return promptActionFunc(msg, choices...)
}

// promptInput shows a prompt and returns free-text input.
func promptInput(msg string) string {
	return promptInputFunc(msg)
}

// promptActionImpl is the real implementation that reads from stdin.
func promptActionImpl(msg string, choices ...string) string {
	reader := bufio.NewReader(os.Stdin)
	choiceSet := make(map[string]bool, len(choices))
	for _, c := range choices {
		choiceSet[c] = true
	}

	for {
		fmt.Print(msg)
		answer, err := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))

		if len(answer) > 0 {
			first := string(answer[0])
			if choiceSet[first] {
				return first
			}
		}

		// Default to first choice on empty input or EOF
		if (answer == "" || err != nil) && len(choices) > 0 {
			return choices[0]
		}

		fmt.Printf("  Please enter one of: %s\n", strings.Join(choices, ", "))
	}
}

// promptInputImpl is the real implementation that reads from stdin.
func promptInputImpl(msg string) string {
	fmt.Print(msg)
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	return strings.TrimSpace(answer)
}
