package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eyelock/ynh/internal/agentplugin"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/marketplace"
	"github.com/eyelock/ynh/internal/migration"
	"github.com/eyelock/ynh/internal/pathutil"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/resolver"
	"github.com/eyelock/ynh/internal/workspace"
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

	// An Agent Plugin (https://agent-plugins.org) is a different package
	// format that shares the plugin.json filename. Its root is what the
	// $schema identifier says it is, and the check that applies is the
	// specification's, not ynh's.
	if agentplugin.IsPluginRoot(root) && !plugin.IsPluginDir(root) {
		return validateAgentPlugin(root)
	}

	// Directory: find harness roots and validate them
	harnesses, agentPlugins := findHarnessRoots(root)
	// Same .yndignore as lint and fmt, so the three walk the same set.
	var ignoreErr error
	if harnesses, ignoreErr = filterYndIgnored(root, harnesses); ignoreErr != nil {
		return ignoreErr
	}
	if agentPlugins, ignoreErr = filterYndIgnored(root, agentPlugins); ignoreErr != nil {
		return ignoreErr
	}
	if len(harnesses) == 0 && len(agentPlugins) == 0 {
		// Maybe we're inside a harness directory
		if isHarnessRoot(root) {
			return validateHarness(root)
		}
		// Check for legacy format
		if isLegacyHarnessRoot(root) {
			fmt.Println("Legacy format detected. Migrate .claude-plugin/plugin.json and metadata.json to .agents/harness/plugin.json.")
			return fmt.Errorf("validation failed")
		}
		fmt.Println("No harness directories found.")
		fmt.Println("A harness requires .agents/harness/plugin.json.")
		return nil
	}

	hasError := false
	for _, p := range harnesses {
		if err := validateHarness(p); err != nil {
			hasError = true
		}
	}
	for _, p := range agentPlugins {
		if err := validateAgentPlugin(p); err != nil {
			hasError = true
		}
	}

	// Also validate a marketplace.json in the root's manifest directory, if present.
	if err := validateRootMarketplace(root); err != nil {
		hasError = true
	}

	if hasError {
		return fmt.Errorf("validation failed")
	}
	return nil
}

// validateRootMarketplace validates the registry marketplace.json in dir's
// manifest directory if it exists.
func validateRootMarketplace(dir string) error {
	if legacy := migration.LegacyRegistry(dir); legacy != nil {
		fmt.Printf("%s: %v\n", dir, legacy)
		return fmt.Errorf("validation failed")
	}
	if !plugin.IsRegistryDir(dir) {
		return nil
	}
	path := plugin.MarketplacePath(dir)
	issues := lintRegistryMarketplace(path)
	for _, issue := range issues {
		fmt.Printf("%s: %s\n", issue.File, issue.Message)
	}
	if len(issues) > 0 {
		return fmt.Errorf("validation failed")
	}
	fmt.Printf("%s: valid\n", path)
	return nil
}

func validateFile(path string) error {
	base := filepath.Base(path)
	var issues []lintIssue

	switch {
	case base == plugin.PluginFile:
		issues = lintPluginJSON(path)
	case base == "marketplace.json":
		// Registry index (inside a manifest directory) validates against the
		// schema. Build config (anywhere else) validates via LoadConfig
		// programmatic checks.
		if plugin.InManifestDir(path) {
			issues = lintRegistryMarketplace(path)
		} else {
			issues = lintMarketplaceConfig(path)
		}
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

// isHarnessRoot reports whether dir is a harness, including one whose only
// manifest is the legacy .harness.json or whose manifest directory is the
// legacy .ynh-plugin/: validateHarness reports that as an issue rather than
// skipping the tree as manifest-less. Read only: validate never converts a
// tree (#406).
func isHarnessRoot(dir string) bool {
	return plugin.IsPluginDir(dir) ||
		migration.LegacyHarnessManifest(dir) != nil ||
		migration.LegacyManifestDir(dir) != nil
}

// harnessManifestPath returns the path to dir's plugin.json, or "" if it has
// none.
func harnessManifestPath(dir string) string {
	if plugin.IsPluginDir(dir) {
		return plugin.PluginPath(dir)
	}
	return ""
}

func isLegacyHarnessRoot(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	return err == nil
}

// findHarnessRoots walks root for ynh harnesses and Agent Plugins. Either
// kind of root ends the descent: a package's contents are its own to
// validate, and a skill inside it is not a second harness.
func findHarnessRoots(root string) (harnesses, agentPlugins []string) {
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if d.Name() == plugin.AgentsDir || d.Name() == plugin.LegacyPluginDir {
			return filepath.SkipDir
		}
		if isHarnessRoot(path) {
			harnesses = append(harnesses, path)
			return filepath.SkipDir
		}
		if agentplugin.IsPluginRoot(path) {
			agentPlugins = append(agentPlugins, path)
			return filepath.SkipDir
		}
		return nil
	})
	return harnesses, agentPlugins
}

// validateAgentPlugin runs the Agent Plugins conformance check over dir and
// prints its findings in the same shape as validateHarness. Every finding
// fails validation, fatal or not: a client would only report the non-fatal
// ones, but an author about to publish wants both fixed.
func validateAgentPlugin(dir string) error {
	rel, _ := filepath.Rel(".", dir)
	if rel == "" {
		rel = dir
	}
	issues := agentplugin.Validate(dir)
	if len(issues) > 0 {
		fmt.Printf("%s: INVALID (Agent Plugin)\n", rel)
		for _, issue := range issues {
			fmt.Printf("  - %s\n", issue)
		}
		return fmt.Errorf("agent plugin %q has %d issue(s)", rel, len(issues))
	}
	fmt.Printf("%s: valid (Agent Plugin %s)\n", rel, agentplugin.Version)
	return nil
}

// lintPluginJSON validates a file named plugin.json against whichever
// schema its $schema names: the Agent Plugins manifest schema, or ynh's.
func lintPluginJSON(path string) []lintIssue {
	data, err := os.ReadFile(path)
	if err != nil {
		return []lintIssue{{File: path, Message: fmt.Sprintf("cannot read: %v", err)}}
	}
	if agentplugin.IsManifest(data) {
		return lintAgentPluginManifest(path, data)
	}
	return lintHarnessJSON(path)
}

func lintAgentPluginManifest(path string, data []byte) []lintIssue {
	_, diags, err := agentplugin.ParseManifest(data)
	var issues []lintIssue
	for _, d := range diags {
		issues = append(issues, lintIssue{File: path, Message: d.Message})
	}
	if err != nil {
		issues = append(issues, lintIssue{File: path, Message: err.Error()})
	}
	return issues
}

func validateHarness(dir string) error {
	rel, _ := filepath.Rel(".", dir)
	if rel == "" {
		rel = dir
	}

	var issues []string
	// notes are checks that could not run. They do not fail validation, but a
	// check that was skipped must never read as one that passed.
	var notes []string

	// Check for legacy format
	if isLegacyHarnessRoot(dir) && !isHarnessRoot(dir) {
		issues = append(issues, "legacy format detected: migrate .claude-plugin/plugin.json and metadata.json to .agents/harness/plugin.json")
	}
	if legacy := migration.LegacyManifestDir(dir); legacy != nil {
		issues = append(issues, legacy.Error())
	}

	manifestPath := harnessManifestPath(dir)
	const manifestLabel = ".agents/harness/plugin.json"
	var data []byte
	var err error
	if legacy := migration.LegacyHarnessManifest(dir); legacy != nil {
		issues = append(issues, legacy.Error())
	} else if manifestPath == "" {
		issues = append(issues, "missing .agents/harness/plugin.json")
	} else {
		data, err = os.ReadFile(manifestPath)
		if err != nil {
			issues = append(issues, fmt.Sprintf("missing %s", manifestLabel))
		}
	}
	if err == nil && data != nil {
		// Schema validation covers $schema URL, required fields, types, enums, patterns.
		for _, si := range lintHarnessJSON(manifestPath) {
			issues = append(issues, si.Message)
		}
		var hj map[string]any
		if err := json.Unmarshal(data, &hj); err != nil {
			issues = append(issues, fmt.Sprintf("invalid %s: %v", manifestLabel, err))
		} else {
			// Cross-field checks the schema cannot express.
			issues = append(issues, validateHarnessHooks(hj)...)
			issues = append(issues, validateHarnessMCPServers(hj)...)
			issues = append(issues, validateHarnessProfiles(hj)...)
			issues = append(issues, validateHarnessFocus(hj)...)
			issues = append(issues, validateHarnessSensors(hj)...)
			issues = append(issues, validateHarnessIncludes(hj)...)
			wsIssues, wsNotes := validateHarnessWorkspace(dir, hj)
			issues = append(issues, wsIssues...)
			notes = append(notes, wsNotes...)
			issues = append(issues, validateSensorDetails(dir, data)...)
			soIssues, soNotes := validateIncludeSensorOverrides(dir, hj, data)
			issues = append(issues, soIssues...)
			notes = append(notes, soNotes...)
			issues = append(issues, validateMCPEnvDeclarations(data)...)
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

	// Hooks a targeted vendor cannot carry are warnings: the manifest is valid.
	var warnings []string
	var hjw map[string]any
	if data != nil && json.Unmarshal(data, &hjw) == nil {
		warnings = hookVendorWarnings(hjw)
	}

	checked := describeHarnessChecks(dir, manifestPath, data)
	for _, n := range notes {
		checked = append(checked, "not checked: "+n)
	}
	if len(issues) > 0 {
		fmt.Printf("%s: INVALID\n", rel)
		for _, issue := range issues {
			fmt.Printf("  - %s\n", issue)
		}
		printWarnings(warnings)
		printChecked(checked)
		return fmt.Errorf("harness %q has %d issue(s)", rel, len(issues))
	}

	fmt.Printf("%s: valid\n", rel)
	printWarnings(warnings)
	printChecked(checked)
	return nil
}

// printWarnings lists findings that do not fail validation.
func printWarnings(warnings []string) {
	for _, w := range warnings {
		fmt.Printf("  warning: %s\n", w)
	}
}

// printChecked lists what validation looked at, so "valid" is never a claim the reader has to
// take on trust.
func printChecked(lines []string) {
	if len(lines) == 0 {
		return
	}
	fmt.Println("  checked:")
	for _, l := range lines {
		fmt.Printf("    %s\n", l)
	}
}

// describeHarnessChecks names each part of the harness validateHarness checks, with what it
// found there. Parts the harness does not have are listed as "none" so the reader sees they
// were looked at.
func describeHarnessChecks(dir, manifestPath string, data []byte) []string {
	var out []string
	row := func(label, value string) { out = append(out, fmt.Sprintf("%-12s %s", label, value)) }
	if manifestPath == "" || data == nil {
		row("manifest", "missing")
		return out
	}
	var hj map[string]any
	if err := json.Unmarshal(data, &hj); err != nil {
		row("manifest", manifestPath+" (not valid JSON)")
		return out
	}
	schema, _ := hj["$schema"].(string)
	if schema == "" {
		schema = "no $schema"
	}
	row("manifest", fmt.Sprintf("%s against %s", manifestPath, schema))

	var incs []string
	if list, ok := hj["includes"].([]any); ok {
		for _, raw := range list {
			m, _ := raw.(map[string]any)
			src, _ := m["git"].(string)
			if src == "" {
				src, _ = m["local"].(string)
			}
			detail := src
			if path, _ := m["path"].(string); path != "" {
				detail += " path " + path
			}
			if picks, ok := m["pick"].([]any); ok && len(picks) > 0 {
				var ps []string
				for _, p := range picks {
					if s, ok := p.(string); ok {
						ps = append(ps, s)
					}
				}
				detail += " pick " + strings.Join(ps, ", ")
			}
			incs = append(incs, detail)
		}
	}
	row("includes", listOrNone(incs))

	var servers []string
	if ms, ok := hj["mcp_servers"].(map[string]any); ok {
		for _, name := range checkKeys(ms) {
			m, _ := ms[name].(map[string]any)
			cmd, _ := m["command"].(string)
			if args, ok := m["args"].([]any); ok {
				for _, a := range args {
					if s, ok := a.(string); ok {
						cmd += " " + s
					}
				}
			}
			if url, _ := m["url"].(string); url != "" {
				cmd = url
			}
			servers = append(servers, fmt.Sprintf("%s runs `%s`", name, strings.TrimSpace(cmd)))
		}
	}
	row("mcp_servers", listOrNone(servers))

	var hooks []string
	if hs, ok := hj["hooks"].(map[string]any); ok {
		for _, event := range checkKeys(hs) {
			entries, _ := hs[event].([]any)
			for _, e := range entries {
				m, _ := e.(map[string]any)
				c, _ := m["command"].(string)
				hooks = append(hooks, fmt.Sprintf("%s runs `%s`", event, c))
			}
		}
	}
	row("hooks", listOrNone(hooks))

	for _, key := range []string{"profiles", "focuses", "sensors", "delegates_to"} {
		var names []string
		switch v := hj[key].(type) {
		case map[string]any:
			names = checkKeys(v)
		case []any:
			for _, x := range v {
				if m, ok := x.(map[string]any); ok {
					if s, _ := m["git"].(string); s != "" {
						names = append(names, s)
					}
				}
			}
		}
		row(key, listOrNone(names))
	}

	for _, sub := range []string{"skills", "agents", "rules", "commands"} {
		var names []string
		if entries, err := os.ReadDir(filepath.Join(dir, sub)); err == nil {
			for _, e := range entries {
				n := e.Name()
				if sub != "skills" {
					if e.IsDir() || !strings.HasSuffix(n, ".md") {
						continue
					}
					n = strings.TrimSuffix(n, ".md")
				} else if !e.IsDir() {
					continue
				}
				names = append(names, n)
			}
		}
		row(sub, listOrNone(names))
	}

	var instr []string
	for _, f := range []string{"instructions.md", "AGENTS.md"} {
		if fileExists(filepath.Join(dir, f)) {
			instr = append(instr, f)
		}
	}
	row("instructions", listOrNone(instr))
	return out
}

func listOrNone(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	// One item per line, aligned under the first, so long lists stay readable.
	return strings.Join(items, "\n"+strings.Repeat(" ", 17))
}

func checkKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// vendorHookEventNames maps known vendor-native hook event names to their
// canonical equivalent. plugin.json declares hooks with canonical names; a
// vendor-native name here is a common copy-paste mistake from a settings file,
// so it earns a targeted hint rather than a generic "unknown event".
var vendorHookEventNames = map[string]string{
	"PreToolUse":           "before_tool",
	"PostToolUse":          "after_tool",
	"UserPromptSubmit":     "before_prompt",
	"Stop":                 "on_stop",
	"SessionStart":         "on_session_start",
	"beforeShellExecution": "before_tool",
	"afterFileEdit":        "after_tool",
	"beforeSubmitPrompt":   "before_prompt",
	"stop":                 "on_stop",
	"sessionStart":         "on_session_start",
	"SessionEnd":           "on_session_end",
	"sessionEnd":           "on_session_end",
	"preToolUse":           "before_tool",
	"postToolUse":          "after_tool",
}

// hookEventMessage returns the validation message for an invalid hook event,
// upgrading to a targeted correction when the event is a known vendor name.
func hookEventMessage(event string) string {
	if canonical, ok := vendorHookEventNames[event]; ok {
		return fmt.Sprintf("event %q is a vendor-native name — plugin.json uses canonical events; use %q (ynh translates it per vendor)", event, canonical)
	}
	return fmt.Sprintf("unknown event %q (valid: before_tool, after_tool, before_prompt, on_stop, on_session_start, on_session_end)", event)
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
			issues = append(issues, "hooks: "+hookEventMessage(event))
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
			if t, present := entry["timeout"]; present {
				n, isNum := t.(float64)
				if !isNum || n != float64(int(n)) || n < 1 || n > float64(plugin.MaxHookTimeout) {
					issues = append(issues, fmt.Sprintf("hooks.%s[%d]: timeout must be a whole number of seconds from 1 to %d", event, i, plugin.MaxHookTimeout))
				}
			}
		}
	}

	return issues
}

// validateHarnessWorkspace checks the workspace request: its shape, and each
// entry against the hard refusals ynh applies at launch (internal/workspace),
// which no allow-list or trust can override: the filesystem root, the home
// directory, and any directory that contains the harness. Those are errors.
//
// A folder that does not exist here is a note, not an error. The request
// names sibling checkouts, and a CI job validating one harness in isolation
// does not have them; ynh refuses a missing folder at launch regardless. The
// project directory is not known to validate, so "contains the project" is a
// launch-time refusal only.
//
// Whether the harness may be granted anything depends on where it is
// installed from, which validate cannot see: a harness installed from a remote
// source is never granted a folder.
func validateHarnessWorkspace(dir string, hj map[string]any) (issues, notes []string) {
	raw, ok := hj["workspace"]
	if !ok {
		return nil, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return []string{"'workspace' must be an array of strings"}, nil
	}
	home, _ := os.UserHomeDir()
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	for i, item := range list {
		entry, ok := item.(string)
		if !ok || strings.TrimSpace(entry) == "" {
			issues = append(issues, fmt.Sprintf("workspace[%d] must be a non-empty string", i))
			continue
		}
		why := workspace.Refusal(workspace.Expand(entry, abs, home), abs, "", home)
		switch why {
		case "":
		case "does not exist":
			notes = append(notes, fmt.Sprintf("workspace %q does not exist here (ynh refuses a missing folder at launch)", entry))
		default:
			issues = append(issues, fmt.Sprintf("workspace %q is never granted: %s", entry, why))
		}
	}
	return issues, notes
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
		issues = append(issues, validateProfileSensorOverrides(hj, name, profileMap)...)
	}

	return issues
}

// validateProfileSensorOverrides checks a profile's sensor_overrides as far as
// the manifest alone allows. The shape (tolerance, no ratchet) is always
// decidable. Whether a named sensor exists is decidable only when nothing can
// add sensors to the harness: an include may declare the sensor, and resolving
// it needs the include cache, which validation does not read. `ynh check
// --profile` and `ynh agent run --profile` refuse the rest at run time.
// The harness's own convergence verifiers are always known.
func validateProfileSensorOverrides(hj map[string]any, profile string, profileMap map[string]any) []string {
	raw, ok := profileMap["sensor_overrides"]
	if !ok {
		return nil
	}
	overrides, ok := raw.(map[string]any)
	if !ok {
		return []string{fmt.Sprintf("profile %q: 'sensor_overrides' must be an object", profile)}
	}
	own, _ := hj["sensors"].(map[string]any)
	baseIncludes, _ := hj["includes"].([]any)
	profileIncludes, _ := profileMap["includes"].([]any)
	canGainSensors := len(baseIncludes) > 0 || len(profileIncludes) > 0

	names := make([]string, 0, len(overrides))
	for n := range overrides {
		names = append(names, n)
	}
	sort.Strings(names)

	var issues []string
	for _, name := range names {
		prefix := fmt.Sprintf("profile %q: sensor_overrides.%s:", profile, name)
		ov, ok := overrides[name].(map[string]any)
		if !ok {
			issues = append(issues, prefix+" must be an object")
			continue
		}
		if _, has := ov["ratchet"]; has {
			issues = append(issues, prefix+" "+plugin.ProfileRatchetRefusal)
		}
		tol, _ := ov["tolerance"].(string)
		if tol == "" {
			if _, has := ov["ratchet"]; !has {
				issues = append(issues, prefix+" tolerance is required")
			}
		} else if !plugin.ValidSensorTolerances[tol] {
			issues = append(issues, fmt.Sprintf("%s tolerance %q must be one of blocking, advisory, report", prefix, tol))
		}
		sensor, declared := own[name].(map[string]any)
		switch {
		case declared:
			if role, _ := sensor["role"].(string); role == "convergence-verifier" {
				issues = append(issues, prefix+" a convergence verifier decides when a run is done, so a profile may not change its tolerance")
			}
		case !canGainSensors:
			issues = append(issues, prefix+" no sensor of that name is declared, and the harness has no includes that could bring one")
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

// validateHarnessFocus validates focus entries inside plugin.json.
func validateHarnessFocus(hj map[string]any) []string {
	var issues []string

	focus, ok := hj["focuses"]
	if !ok {
		return issues
	}
	focusMap, ok := focus.(map[string]any)
	if !ok {
		issues = append(issues, "'focuses' must be an object")
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

// validateHarnessSensors validates the sensors section inside plugin.json.
// Schema covers structural rules; this layer enforces cross-field rules
// (focus references resolve, inline-focus profile references resolve)
// and emits sensor-name-prefixed errors.
func validateHarnessSensors(hj map[string]any) []string {
	var issues []string

	sensors, ok := hj["sensors"]
	if !ok {
		return issues
	}
	sensorsMap, ok := sensors.(map[string]any)
	if !ok {
		issues = append(issues, "'sensors' must be an object")
		return issues
	}

	profileNames := map[string]bool{}
	if profiles, ok := hj["profiles"].(map[string]any); ok {
		for name := range profiles {
			profileNames[name] = true
		}
	}
	focusNames := map[string]bool{}
	if focuses, ok := hj["focuses"].(map[string]any); ok {
		for name := range focuses {
			focusNames[name] = true
		}
	}

	for name, raw := range sensorsMap {
		prefix := fmt.Sprintf("sensor %q:", name)
		entry, ok := raw.(map[string]any)
		if !ok {
			issues = append(issues, fmt.Sprintf("%s must be an object", prefix))
			continue
		}
		if cat, ok := entry["category"].(string); ok && cat != "" && !plugin.ValidSensorCategories[cat] {
			issues = append(issues, fmt.Sprintf("%s category %q must be one of maintainability, architecture, behaviour", prefix, cat))
		}
		if role, ok := entry["role"].(string); ok && role != "" && !plugin.ValidSensorRoles[role] {
			issues = append(issues, fmt.Sprintf("%s role %q must be one of regular, convergence-verifier, stuck-recovery", prefix, role))
		}
		if tol, ok := entry["tolerance"].(string); ok && tol != "" && !plugin.ValidSensorTolerances[tol] {
			issues = append(issues, fmt.Sprintf("%s tolerance %q must be one of blocking, advisory, report", prefix, tol))
		}
		if out, ok := entry["output"].(map[string]any); ok {
			if format, _ := out["format"].(string); format == "" {
				issues = append(issues, fmt.Sprintf("%s output.format must not be empty", prefix))
			}
		} else {
			issues = append(issues, fmt.Sprintf("%s output is required", prefix))
		}
		src, ok := entry["source"].(map[string]any)
		if !ok {
			issues = append(issues, fmt.Sprintf("%s source is required", prefix))
			continue
		}
		count := 0
		_, hasFiles := src["files"]
		if hasFiles {
			count++
		}
		if _, has := src["command"]; has {
			count++
		}
		_, hasStatus := src["github_status"]
		_, hasCheck := src["github_check"]
		if hasStatus {
			count++
		}
		if hasCheck {
			count++
		}
		focus, hasFocus := src["focus"]
		if hasFocus {
			count++
			switch fv := focus.(type) {
			case string:
				if fv == "" {
					issues = append(issues, fmt.Sprintf("%s source.focus reference must not be empty", prefix))
				} else if !focusNames[fv] {
					issues = append(issues, fmt.Sprintf("%s source.focus references undefined focus %q", prefix, fv))
				}
			case map[string]any:
				if prompt, _ := fv["prompt"].(string); prompt == "" {
					issues = append(issues, fmt.Sprintf("%s source.focus.prompt must not be empty", prefix))
				}
				if profile, _ := fv["profile"].(string); profile != "" && !profileNames[profile] {
					issues = append(issues, fmt.Sprintf("%s source.focus references unknown profile %q", prefix, profile))
				}
			}
		}
		// Calibration asks one question: does this sensor still trip on a
		// fixture built to trip it? Only a command sensor answers it, because
		// only a command sensor's verdict is an exit code about the tree it
		// was pointed at.
		//
		// This rule was once held only by a validator nothing
		// called, so nothing enforced it here. The result was not
		// inert: --calibrate ran such a sensor anyway and reported "the sensor
		// passed a fixture built to trip it — it is no longer observing",
		// failing the run with a false accusation about a healthy sensor.
		if _, hasRef := entry["reference"]; hasRef {
			switch {
			case hasStatus || hasCheck:
				issues = append(issues, fmt.Sprintf(
					"%s a github sensor cannot declare a reference: calibration runs from the fixture directory, "+
						"so it would ask about that directory's repository and commit rather than the one under test", prefix))
			case hasFiles:
				issues = append(issues, fmt.Sprintf(
					"%s a files sensor cannot declare a reference: its verdict is whether the artifact is fresh, "+
						"not whether it trips on a fixture, so calibration would report a healthy sensor as no longer observing", prefix))
			case hasFocus:
				issues = append(issues, fmt.Sprintf(
					"%s a focus sensor cannot declare a reference: no verdict about an agent's output is derivable "+
						"from a fixture, so calibration would report a healthy sensor as no longer observing", prefix))
			}
		}
		if count != 1 {
			issues = append(issues, fmt.Sprintf("%s source must have exactly one of files, command, focus, github_status, github_check", prefix))
		} else if role, _ := entry["role"].(string); role == "convergence-verifier" {
			// The files half of this rule was held by a validator nothing
			// called, so `ynd validate` accepted a verifier that
			// could never end a run (#447).
			kind := ""
			switch {
			case hasFiles:
				kind = "files"
			case hasFocus:
				kind = "focus"
			}
			if why := plugin.ConvergenceVerifierRefusal(kind); why != "" {
				issues = append(issues, prefix+" "+why)
			}
		}
	}

	return issues
}

// lintHarnessJSON validates a plugin.json file against plugin.schema.json.
func lintHarnessJSON(path string) []lintIssue {
	data, err := os.ReadFile(path)
	if err != nil {
		return []lintIssue{{File: path, Message: fmt.Sprintf("cannot read: %v", err)}}
	}
	issues := schemaIssues(path, data, compiledPluginSchema)
	return append(issues, lintDeclaredReads(path)...)
}

// lintRegistryMarketplace validates a registry marketplace.json index
// against marketplace.schema.json.
func lintRegistryMarketplace(path string) []lintIssue {
	data, err := os.ReadFile(path)
	if err != nil {
		return []lintIssue{{File: path, Message: fmt.Sprintf("cannot read: %v", err)}}
	}
	return schemaIssues(path, data, compiledMarketplaceSchema)
}

// lintMarketplaceConfig validates a marketplace.json build config via programmatic checks.
func lintMarketplaceConfig(path string) []lintIssue {
	if _, err := marketplace.LoadConfig(path); err != nil {
		return []lintIssue{{File: path, Message: err.Error()}}
	}
	return nil
}

// validateMCPEnvDeclarations catches an MCP ${VAR} reference the harness never
// declared in env_passthrough — a defect that used to surface only at
// assembly, and worst under automation, where an unattended `ynh agent run`
// does its setup and dies on a config error CI could have caught.
//
// It decodes to the typed manifest and calls plugin.UndeclaredMCPEnvRefs, the
// same rule ExpandMCPEnv enforces, rather than re-walking the raw map. Two
// implementations of one rule is how they drift.
//
// The check is deliberately silent when a harness declares no env_passthrough
// at all: such a harness may be authored purely for distribution, where
// `ynd export` leaves ${VAR} literal for the consumer's own CLI and strips
// env_passthrough from the artifact entirely. Erroring there would redden a
// harness that works today, and demand an allowlist that never reaches the
// thing it ships. A harness that declares an allowlist and misses an entry is
// the realistic mistake.
func validateMCPEnvDeclarations(data []byte) []string {
	var hj plugin.HarnessJSON
	if err := json.Unmarshal(data, &hj); err != nil {
		return nil // malformed JSON is already reported by the caller
	}
	issues := plugin.UndeclaredMCPEnvRefs(hj.MCPServers, hj.EnvPassthrough)
	for _, prof := range hj.Profiles {
		allowed := hj.EnvPassthrough
		if prof.EnvPassthrough != nil {
			allowed = prof.EnvPassthrough
		}
		// A profile's servers are pointers; flatten so one rule covers both.
		flat := make(map[string]plugin.MCPServer, len(prof.MCPServers))
		for n, srv := range prof.MCPServers {
			if srv != nil {
				flat[n] = *srv
			}
		}
		issues = append(issues, plugin.UndeclaredMCPEnvRefs(flat, allowed)...)
	}
	return issues
}

// validateHarnessIncludes applies the traversal rule the resolver enforces at
// assembly time.
//
// A `local` include may not point above the harness directory. The resolver
// refuses one; the validator did not — so a manifest passed `ynd validate` and
// then failed at `ynd compose` and `ynd preview`:
//
//	$ ynd validate app
//	app: valid
//	$ ynd compose app
//	Error: resolving includes: local include: path "../shared" must not traverse
//	       above its base directory
//
// `ynd validate` is what the harness tells authors to run and what
// `make check-artifacts` gates on, so a rule it does not know moves the error
// from authoring time to run time — the failure mode validate exists to
// prevent.
//
// It reuses pathutil.CheckSubpath rather than restating the rule. Two copies of
// a traversal check drift, and the resolver's copy is the one actually guarding
// the filesystem; this one must agree with it by construction, not by care.
//
// `path` is checked for both include forms, because the resolver applies the
// same rule to it whether the source is git or local.
func validateHarnessIncludes(hj map[string]any) []string {
	var issues []string

	raw, ok := hj["includes"]
	if !ok {
		return issues
	}
	list, ok := raw.([]any)
	if !ok {
		issues = append(issues, "'includes' must be an array")
		return issues
	}

	for i, entry := range list {
		obj, ok := entry.(map[string]any)
		if !ok {
			issues = append(issues, fmt.Sprintf("includes[%d] must be an object", i))
			continue
		}
		if local, ok := obj["local"].(string); ok && local != "" {
			if err := pathutil.CheckSubpath(local); err != nil {
				issues = append(issues, fmt.Sprintf("includes[%d].local: %v", i, err))
			}
		}
		if sub, ok := obj["path"].(string); ok && sub != "" {
			if err := pathutil.CheckSubpath(sub); err != nil {
				issues = append(issues, fmt.Sprintf("includes[%d].path: %v", i, err))
			}
		}
	}
	return issues
}

// validateIncludeSensorOverrides checks the harness's top-level
// sensor_overrides, which tune the sensors its includes bring. The runtime
// (resolver.MergeIncludedSensors) refuses these at `ynh check` time; this
// reports them at authoring time, with the runtime's wording.
//
// Always decidable from the manifest: the shape, the tolerance and ratchet
// values, and an override naming one of this harness's own sensors.
//
// Whether a name is declared by some include needs the includes read. Local
// includes and git includes already in the cache are read; nothing is
// fetched. If any include that could declare a sensor cannot be read, an
// unmatched name is not an error, because the missing include may declare it.
// It comes back as a note instead, naming the include, so the check is never
// silently passed. Includes with "sensors": false contribute nothing, as at
// run time.
func validateIncludeSensorOverrides(dir string, hj map[string]any, data []byte) (issues, notes []string) {
	raw, ok := hj["sensor_overrides"]
	if !ok {
		return nil, nil
	}
	overrides, ok := raw.(map[string]any)
	if !ok {
		return []string{"'sensor_overrides' must be an object"}, nil
	}
	names := make([]string, 0, len(overrides))
	for n := range overrides {
		names = append(names, n)
	}
	sort.Strings(names)

	own, _ := hj["sensors"].(map[string]any)

	// Shape and values first; these need no includes.
	var candidates []string // names still to look for in the includes
	for _, name := range names {
		prefix := fmt.Sprintf("sensor_overrides.%s:", name)
		ov, ok := overrides[name].(map[string]any)
		if !ok {
			issues = append(issues, prefix+" must be an object")
			continue
		}
		if v, has := ov["tolerance"]; has {
			tol, isStr := v.(string)
			if !isStr || !plugin.ValidSensorTolerances[tol] {
				issues = append(issues, fmt.Sprintf("%s tolerance %v must be one of blocking, advisory, report", prefix, quoteIfString(v)))
			}
		}
		if v, has := ov["ratchet"]; has {
			rat, isStr := v.(string)
			if !isStr || !plugin.ValidSensorRatchets[rat] {
				issues = append(issues, fmt.Sprintf("%s ratchet %v must be one of fingerprint, count", prefix, quoteIfString(v)))
			}
		}
		for k := range ov {
			if k != "tolerance" && k != "ratchet" {
				issues = append(issues, fmt.Sprintf("%s %q cannot be overridden: only tolerance and ratchet can, anything else is a fork", prefix, k))
			}
		}
		if _, isOwn := own[name]; isOwn {
			issues = append(issues, fmt.Sprintf(
				"%s sensor %q is declared by this harness, so edit it directly rather than overriding it", prefix, name))
			continue
		}
		candidates = append(candidates, name)
	}
	if len(candidates) == 0 {
		return issues, nil
	}

	// Which includes could declare a sensor, and what do they declare?
	var meta plugin.HarnessJSON
	var includes []plugin.IncludeMeta
	if err := json.Unmarshal(data, &meta); err == nil {
		includes = meta.Includes
	}
	declared := map[string]bool{}
	var unread []string
	for _, im := range includes {
		inc := harness.Include{
			GitSource: harness.GitSource{Git: im.Git, Local: im.Local, Ref: im.Ref, Path: im.Path},
			Sensors:   im.Sensors,
		}
		if !inc.SensorsEnabled() {
			continue
		}
		label := im.Git
		if inc.IsLocal() {
			label = im.Local
		}
		sensors, err := resolver.ReadIncludeSensors(dir, inc)
		if err != nil {
			unread = append(unread, fmt.Sprintf("include %s could not be read (%v)", label, err))
			continue
		}
		for n := range sensors {
			declared[n] = true
		}
	}

	for _, name := range candidates {
		if declared[name] {
			continue
		}
		if len(unread) == 0 {
			issues = append(issues, fmt.Sprintf(
				"sensor_overrides.%s: override names sensor %q, which no include or this harness declares", name, name))
			continue
		}
		notes = append(notes, fmt.Sprintf(
			"sensor_overrides.%s is declared by no include that could be read; %s", name, strings.Join(unread, "; ")))
	}
	return issues, notes
}

func quoteIfString(v any) any {
	if s, ok := v.(string); ok {
		return fmt.Sprintf("%q", s)
	}
	return fmt.Sprintf("%v", v)
}

// validateSensorDetails holds the sensor rules neither the schema nor
// validateHarnessSensors express, found by decoding the manifest to its typed
// form. They are the ones a validator in internal/plugin held and nothing
// called (#367), since removed: a set is published to be included by other harnesses, so a
// defect here fails in every consumer's `ynh check`, not just the author's.
//
//   - ratchet "count" needs a command source: only a command produces
//     countable findings;
//   - an output.match that does not compile would silently select nothing,
//     turning the sensor's whole output into accepted debt;
//   - a reference fixture must be a relative path inside the harness, and must
//     exist: a calibration that cannot find its fixture cannot prove the sensor
//     still observes, and an included sensor's fixture travels with it.
func validateSensorDetails(dir string, data []byte) []string {
	var hj plugin.HarnessJSON
	if err := json.Unmarshal(data, &hj); err != nil {
		return nil // malformed JSON is already reported by the caller
	}
	names := make([]string, 0, len(hj.Sensors))
	for n := range hj.Sensors {
		names = append(names, n)
	}
	sort.Strings(names)

	var issues []string
	for _, name := range names {
		s := hj.Sensors[name]
		prefix := fmt.Sprintf("sensor %q:", name)
		if s.Ratchet == "count" && s.Source.Kind() != "" && s.Source.Kind() != "command" {
			issues = append(issues, fmt.Sprintf(
				"%s ratchet count requires a command source: only a command sensor produces countable findings", prefix))
		}
		if _, err := s.OutputMatcher(); err != nil {
			issues = append(issues, fmt.Sprintf("%s %v", prefix, err))
		}
		if s.Reference == nil || s.Reference.Path == "" {
			continue
		}
		ref := s.Reference.Path
		if err := pathutil.CheckSubpath(ref); err != nil || filepath.IsAbs(ref) {
			issues = append(issues, fmt.Sprintf(
				"%s reference.path %q must be a relative path inside the harness", prefix, ref))
			continue
		}
		if info, err := os.Stat(filepath.Join(dir, ref)); err != nil || !info.IsDir() {
			issues = append(issues, fmt.Sprintf(
				"%s reference.path %q is not a directory in the harness: calibration has no fixture to run against", prefix, ref))
		}
	}
	return issues
}
