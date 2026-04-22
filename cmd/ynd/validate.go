package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/marketplace"
	"github.com/eyelock/ynh/internal/plugin"
)

func cmdValidate(args []string) error {
	var root string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--harness":
			if i+1 >= len(args) {
				return fmt.Errorf("--harness requires a value")
			}
			i++
			root = args[i]
		case "-h", "--help":
			return errHelp
		default:
			if strings.HasPrefix(args[i], "-") {
				return fmt.Errorf("unknown flag: %s", args[i])
			}
			if root == "" {
				root = args[i]
			}
		}
	}

	// Resolve source: --harness flag > YNH_HARNESS > positional > CWD
	if root == "" {
		root = resolveHarnessEnv()
	}
	if root == "" {
		root = "."
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
		// Check for legacy format
		if isLegacyHarnessRoot(root) {
			fmt.Println("Legacy format detected. Migrate .claude-plugin/plugin.json and metadata.json to .ynh-plugin/plugin.json.")
			return fmt.Errorf("validation failed")
		}
		fmt.Println("No harness directories found.")
		fmt.Println("A harness requires .ynh-plugin/plugin.json (or legacy .harness.json).")
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
	case base == plugin.PluginFile || base == plugin.HarnessFile:
		issues = lintHarnessJSON(path)
	case base == "marketplace.json":
		issues = lintMarketplaceConfig(path)
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
	if plugin.IsPluginDir(dir) {
		return true
	}
	_, err := os.Stat(filepath.Join(dir, plugin.HarnessFile))
	return err == nil
}

// harnessManifestPath returns the path to the manifest file for dir.
// Prefers .ynh-plugin/plugin.json; falls back to .harness.json. Empty if neither.
func harnessManifestPath(dir string) string {
	pluginPath := filepath.Join(dir, plugin.PluginDir, plugin.PluginFile)
	if _, err := os.Stat(pluginPath); err == nil {
		return pluginPath
	}
	legacyPath := filepath.Join(dir, plugin.HarnessFile)
	if _, err := os.Stat(legacyPath); err == nil {
		return legacyPath
	}
	return ""
}

func isLegacyHarnessRoot(dir string) bool {
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

	// Check for legacy format
	if isLegacyHarnessRoot(dir) && !isHarnessRoot(dir) {
		issues = append(issues, "legacy format detected: migrate .claude-plugin/plugin.json and metadata.json to .ynh-plugin/plugin.json")
	}

	// Check manifest exists (plugin.json or .harness.json) and is valid
	manifestPath := harnessManifestPath(dir)
	manifestLabel := "manifest"
	if manifestPath == filepath.Join(dir, plugin.PluginDir, plugin.PluginFile) {
		manifestLabel = ".ynh-plugin/plugin.json"
	} else if manifestPath == filepath.Join(dir, plugin.HarnessFile) {
		manifestLabel = ".harness.json"
	}
	var data []byte
	var err error
	if manifestPath == "" {
		issues = append(issues, "missing manifest (.ynh-plugin/plugin.json or .harness.json)")
	} else {
		data, err = os.ReadFile(manifestPath)
		if err != nil {
			issues = append(issues, fmt.Sprintf("missing %s", manifestLabel))
		}
	}
	if err == nil && data != nil {
		var hj map[string]any
		if err := json.Unmarshal(data, &hj); err != nil {
			issues = append(issues, fmt.Sprintf("invalid %s: %v", manifestLabel, err))
		} else {
			if _, ok := hj["name"]; !ok {
				issues = append(issues, fmt.Sprintf("%s missing 'name'", manifestLabel))
			}
			if _, ok := hj["version"]; !ok {
				issues = append(issues, fmt.Sprintf("%s missing 'version'", manifestLabel))
			}
			// Validate hooks
			issues = append(issues, validateHarnessHooks(hj)...)
			// Validate MCP servers
			issues = append(issues, validateHarnessMCPServers(hj)...)
			// Validate profiles
			issues = append(issues, validateHarnessProfiles(hj)...)
			// Validate focus entries
			issues = append(issues, validateHarnessFocus(hj)...)
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

// validateHarnessHooks validates the hooks section inside harness.json.
func validateHarnessHooks(hj map[string]any) []string {
	var issues []string

	hooks, ok := hj["hooks"]
	if !ok {
		return issues
	}
	hooksMap, ok := hooks.(map[string]any)
	if !ok {
		issues = append(issues, "'hooks' must be an object")
		return issues
	}

	for event, entries := range hooksMap {
		if !plugin.ValidHookEvents[event] {
			issues = append(issues, fmt.Sprintf("hooks: unknown event %q (valid: before_tool, after_tool, before_prompt, on_stop)", event))
		}
		arr, ok := entries.([]any)
		if !ok {
			issues = append(issues, fmt.Sprintf("hooks.%s must be an array", event))
			continue
		}
		for i, item := range arr {
			entry, ok := item.(map[string]any)
			if !ok {
				issues = append(issues, fmt.Sprintf("hooks.%s[%d] must be an object", event, i))
				continue
			}
			cmd, _ := entry["command"].(string)
			if cmd == "" {
				issues = append(issues, fmt.Sprintf("hooks.%s[%d]: command must not be empty", event, i))
			}
		}
	}

	return issues
}

// validateHarnessMCPServers validates the mcp_servers section inside harness.json.
func validateHarnessMCPServers(hj map[string]any) []string {
	var issues []string

	servers, ok := hj["mcp_servers"]
	if !ok {
		return issues
	}
	serversMap, ok := servers.(map[string]any)
	if !ok {
		issues = append(issues, "'mcp_servers' must be an object")
		return issues
	}

	for name, entry := range serversMap {
		serverMap, ok := entry.(map[string]any)
		if !ok {
			issues = append(issues, fmt.Sprintf("mcp_servers.%s must be an object", name))
			continue
		}
		cmd, _ := serverMap["command"].(string)
		url, _ := serverMap["url"].(string)
		hasCommand := cmd != ""
		hasURL := url != ""
		if !hasCommand && !hasURL {
			issues = append(issues, fmt.Sprintf("mcp_servers.%s: must have either command or url", name))
		}
		if hasCommand && hasURL {
			issues = append(issues, fmt.Sprintf("mcp_servers.%s: must have command or url, not both", name))
		}
	}

	return issues
}

// validateHarnessProfiles validates hooks and mcp_servers within each profile.
func validateHarnessProfiles(hj map[string]any) []string {
	var issues []string

	profiles, ok := hj["profiles"]
	if !ok {
		return issues
	}
	profilesMap, ok := profiles.(map[string]any)
	if !ok {
		issues = append(issues, "'profiles' must be an object")
		return issues
	}

	for name, profile := range profilesMap {
		profileMap, ok := profile.(map[string]any)
		if !ok {
			issues = append(issues, fmt.Sprintf("profile %q must be an object", name))
			continue
		}
		// Validate hooks within profile
		for _, issue := range validateHarnessHooks(profileMap) {
			issues = append(issues, fmt.Sprintf("profile %q: %s", name, issue))
		}
		// Validate MCP servers within profile (skip null entries — they signal removal)
		for _, issue := range validateProfileMCPServers(profileMap) {
			issues = append(issues, fmt.Sprintf("profile %q: %s", name, issue))
		}
	}

	return issues
}

// validateProfileMCPServers validates mcp_servers within a profile, skipping
// null entries (which signal removal of inherited servers during merge).
func validateProfileMCPServers(profile map[string]any) []string {
	var issues []string

	servers, ok := profile["mcp_servers"]
	if !ok {
		return issues
	}
	serversMap, ok := servers.(map[string]any)
	if !ok {
		issues = append(issues, "'mcp_servers' must be an object")
		return issues
	}

	for name, entry := range serversMap {
		if entry == nil {
			continue // null = remove inherited server
		}
		serverMap, ok := entry.(map[string]any)
		if !ok {
			issues = append(issues, fmt.Sprintf("mcp_servers.%s must be an object or null", name))
			continue
		}
		cmd, _ := serverMap["command"].(string)
		url, _ := serverMap["url"].(string)
		hasCommand := cmd != ""
		hasURL := url != ""
		if !hasCommand && !hasURL {
			issues = append(issues, fmt.Sprintf("mcp_servers.%s: must have either command or url", name))
		}
		if hasCommand && hasURL {
			issues = append(issues, fmt.Sprintf("mcp_servers.%s: must have command or url, not both", name))
		}
	}

	return issues
}

// validateHarnessFocus validates focus entries inside .harness.json.
func validateHarnessFocus(hj map[string]any) []string {
	var issues []string

	focus, ok := hj["focus"]
	if !ok {
		return issues
	}
	focusMap, ok := focus.(map[string]any)
	if !ok {
		issues = append(issues, "'focus' must be an object")
		return issues
	}

	// Collect profile names for cross-field validation
	profileNames := map[string]bool{}
	if profiles, ok := hj["profiles"].(map[string]any); ok {
		for name := range profiles {
			profileNames[name] = true
		}
	}

	for name, entry := range focusMap {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			issues = append(issues, fmt.Sprintf("focus.%s must be an object", name))
			continue
		}
		prompt, _ := entryMap["prompt"].(string)
		if prompt == "" {
			issues = append(issues, fmt.Sprintf("focus.%s: prompt must not be empty", name))
		}
		profile, _ := entryMap["profile"].(string)
		if profile != "" && !profileNames[profile] {
			issues = append(issues, fmt.Sprintf("focus.%s: references unknown profile %q", name, profile))
		}
	}

	return issues
}

// lintMarketplaceConfig validates a marketplace.json build config.
func lintMarketplaceConfig(path string) []lintIssue {
	if _, err := marketplace.LoadConfig(path); err != nil {
		return []lintIssue{{File: path, Message: err.Error()}}
	}
	return nil
}

// lintHarnessJSON validates harness.json structure for the lint command.
func lintHarnessJSON(path string) []lintIssue {
	var issues []lintIssue

	data, err := os.ReadFile(path)
	if err != nil {
		return []lintIssue{{File: path, Message: fmt.Sprintf("cannot read: %v", err)}}
	}

	var hj map[string]any
	if err := json.Unmarshal(data, &hj); err != nil {
		return []lintIssue{{File: path, Message: fmt.Sprintf("invalid JSON: %v", err)}}
	}

	if _, ok := hj["name"]; !ok {
		issues = append(issues, lintIssue{File: path, Message: "missing 'name' field"})
	}
	if _, ok := hj["version"]; !ok {
		issues = append(issues, lintIssue{File: path, Message: "missing 'version' field"})
	}

	return issues
}
