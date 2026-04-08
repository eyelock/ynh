package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
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
			fmt.Fprintln(os.Stderr, "No supported LLM CLI found (checked: claude, codex).")
			fmt.Fprintln(os.Stderr, "Inspect requires an LLM to analyze your codebase.")
			fmt.Fprintln(os.Stderr)
			fmt.Fprintln(os.Stderr, "Install one of:")
			fmt.Fprintln(os.Stderr, "  claude  → https://docs.anthropic.com/claude-code")
			fmt.Fprintln(os.Stderr, "  codex   → https://openai.com/codex")
			fmt.Fprintln(os.Stderr)
			fmt.Fprintln(os.Stderr, "Or specify one explicitly: ynd inspect -v claude")
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
