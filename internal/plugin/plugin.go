package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// HarnessJSON represents the harness.json manifest — single source of truth.
type HarnessJSON struct {
	Schema        string                 `json:"$schema,omitempty"`
	Name          string                 `json:"name"`
	Version       string                 `json:"version"`
	Description   string                 `json:"description,omitempty"`
	Author        *AuthorInfo            `json:"author,omitempty"`
	Keywords      []string               `json:"keywords,omitempty"`
	DefaultVendor string                 `json:"default_vendor,omitempty"`
	Includes      []IncludeMeta          `json:"includes,omitempty"`
	DelegatesTo   []DelegateMeta         `json:"delegates_to,omitempty"`
	Hooks         map[string][]HookEntry `json:"hooks,omitempty"`
	MCPServers    map[string]MCPServer   `json:"mcp_servers,omitempty"`
	Profiles      map[string]Profile     `json:"profiles,omitempty"`
	InstalledFrom *ProvenanceMeta        `json:"installed_from,omitempty"`
}

// Profile is a named configuration variant. When selected, its fields
// fully replace the corresponding top-level values (no merge).
type Profile struct {
	Hooks      map[string][]HookEntry `json:"hooks,omitempty"`
	MCPServers map[string]MCPServer   `json:"mcp_servers,omitempty"`
}

// AuthorInfo holds harness author information.
type AuthorInfo struct {
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
	URL   string `json:"url,omitempty"`
}

// MCPServer defines an MCP server dependency.
type MCPServer struct {
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// ValidateMCPServers checks that each MCP server has either Command or URL (not both, not neither).
func ValidateMCPServers(servers map[string]MCPServer) []string {
	var issues []string
	for name, server := range servers {
		hasCommand := server.Command != ""
		hasURL := server.URL != ""
		if !hasCommand && !hasURL {
			issues = append(issues, fmt.Sprintf("mcp_servers.%s: must have either command or url", name))
		}
		if hasCommand && hasURL {
			issues = append(issues, fmt.Sprintf("mcp_servers.%s: must have command or url, not both", name))
		}
	}
	return issues
}

// HookEntry defines a single hook action.
type HookEntry struct {
	Matcher string `json:"matcher,omitempty"` // tool name pattern (optional)
	Command string `json:"command"`           // shell command to run
}

// ValidHookEvents lists the canonical hook event names.
var ValidHookEvents = map[string]bool{
	"before_tool":   true,
	"after_tool":    true,
	"before_prompt": true,
	"on_stop":       true,
}

// ValidateHooks checks that hook event names are valid and commands are non-empty.
func ValidateHooks(hooks map[string][]HookEntry) []string {
	var issues []string
	for event, entries := range hooks {
		if !ValidHookEvents[event] {
			issues = append(issues, fmt.Sprintf("unknown hook event %q (valid: before_tool, after_tool, before_prompt, on_stop)", event))
		}
		for i, entry := range entries {
			if entry.Command == "" {
				issues = append(issues, fmt.Sprintf("hooks.%s[%d]: command must not be empty", event, i))
			}
		}
	}
	return issues
}

// ProvenanceMeta records where a harness was installed from.
type ProvenanceMeta struct {
	SourceType   string `json:"source_type"`
	Source       string `json:"source"`
	Path         string `json:"path,omitempty"`
	RegistryName string `json:"registry_name,omitempty"`
	InstalledAt  string `json:"installed_at"`
}

// IncludeMeta is the JSON representation of a Git include.
type IncludeMeta struct {
	Git  string   `json:"git"`
	Ref  string   `json:"ref,omitempty"`
	Path string   `json:"path,omitempty"`
	Pick []string `json:"pick,omitempty"`
}

// DelegateMeta is the JSON representation of a delegate reference.
type DelegateMeta struct {
	Git  string `json:"git"`
	Ref  string `json:"ref,omitempty"`
	Path string `json:"path,omitempty"`
}

// IsHarnessDir returns true if the directory contains a harness.json manifest.
func IsHarnessDir(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "harness.json"))
	return err == nil
}

// IsLegacyPluginDir returns true if the directory contains a legacy .claude-plugin/plugin.json.
func IsLegacyPluginDir(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	return err == nil
}

// LoadHarnessJSON reads and parses harness.json from dir.
func LoadHarnessJSON(dir string) (*HarnessJSON, error) {
	data, err := os.ReadFile(filepath.Join(dir, "harness.json"))
	if err != nil {
		return nil, fmt.Errorf("reading harness.json: %w", err)
	}

	var hj HarnessJSON
	if err := json.Unmarshal(data, &hj); err != nil {
		return nil, fmt.Errorf("parsing harness.json: %w", err)
	}

	if hj.Name == "" {
		return nil, fmt.Errorf("harness.json missing required field: name")
	}

	return &hj, nil
}

// SaveHarnessJSON writes a HarnessJSON manifest to dir/harness.json.
func SaveHarnessJSON(dir string, hj *HarnessJSON) error {
	data, err := json.MarshalIndent(hj, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling harness.json: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(filepath.Join(dir, "harness.json"), data, 0o644); err != nil {
		return fmt.Errorf("writing harness.json: %w", err)
	}

	return nil
}
