package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// HarnessJSON represents the .harness.json manifest — single source of truth.
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
	Focuses       map[string]Focus       `json:"focus,omitempty"`
	Sensors       map[string]Sensor      `json:"sensors,omitempty"`
	InstalledFrom *ProvenanceMeta        `json:"installed_from,omitempty"`
}

// Sensor declares an observation surface — a feedforward signal a loop
// driver consumes between agent turns. ynh declares; the loop driver runs.
type Sensor struct {
	Category string       `json:"category,omitempty"`
	Source   SensorSource `json:"source"`
	Output   SensorOutput `json:"output"`
}

// SensorSource is a strict one-of: files, command, or focus. Exactly one
// must be set. Discriminated by structure, not labels.
type SensorSource struct {
	Files   []string  `json:"files,omitempty"`
	Command string    `json:"command,omitempty"`
	Focus   *FocusRef `json:"focus,omitempty"`
}

// Kind reports which source variant is set. Returns "" if none or
// (impossibly, for a validated sensor) more than one.
func (s SensorSource) Kind() string {
	count := 0
	kind := ""
	if s.Files != nil {
		count++
		kind = "files"
	}
	if s.Command != "" {
		count++
		kind = "command"
	}
	if s.Focus != nil {
		count++
		kind = "focus"
	}
	if count != 1 {
		return ""
	}
	return kind
}

// UnmarshalJSON enforces that exactly one of files, command, focus is set.
func (s *SensorSource) UnmarshalJSON(data []byte) error {
	type alias SensorSource
	var raw alias
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return err
	}
	*s = SensorSource(raw)
	if s.Kind() == "" {
		return fmt.Errorf("source must have exactly one of files, command, focus")
	}
	return nil
}

// FocusRef is either a string (referring to a named top-level focus) or
// an inline focus object.
type FocusRef struct {
	Name   string       // set when source.focus is a string reference
	Inline *InlineFocus // set when source.focus is an inline object
}

// InlineFocus is a focus declared inline inside a sensor's source.
// Inline focuses are not exposed via --focus or YNH_FOCUS — they live
// only as the sensor's source.
type InlineFocus struct {
	Profile string `json:"profile,omitempty"`
	Prompt  string `json:"prompt"`
}

// UnmarshalJSON accepts either a string ref or an inline focus object.
func (f *FocusRef) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		if s == "" {
			return fmt.Errorf("focus reference must not be empty")
		}
		f.Name = s
		return nil
	}
	var inline InlineFocus
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&inline); err != nil {
		return err
	}
	if inline.Prompt == "" {
		return fmt.Errorf("inline focus must have a non-empty prompt")
	}
	f.Inline = &inline
	return nil
}

// MarshalJSON emits the variant that is set.
func (f FocusRef) MarshalJSON() ([]byte, error) {
	if f.Inline != nil {
		return json.Marshal(f.Inline)
	}
	return json.Marshal(f.Name)
}

// SensorOutput declares where a sensor's result lives and what shape it
// has. format is a freeform pass-through identifier; ynh maintains no
// vocabulary.
type SensorOutput struct {
	Format  string `json:"format"`
	Channel string `json:"channel,omitempty"`
	Path    string `json:"path,omitempty"`
}

// ValidSensorCategories lists the Fowler buckets a sensor's category may use.
var ValidSensorCategories = map[string]bool{
	"maintainability": true,
	"architecture":    true,
	"behaviour":       true,
}

// ValidateSensors checks each sensor's source/output/category and resolves
// focus references against the supplied profile and focus name sets.
// profileNames and focusNames may be nil if no cross-reference data is
// available (callers should pass both to enable full validation).
func ValidateSensors(sensors map[string]Sensor, profileNames, focusNames map[string]bool) []string {
	var issues []string
	for name, s := range sensors {
		if name == "" {
			issues = append(issues, "sensor name must not be empty")
			continue
		}
		prefix := fmt.Sprintf("sensor %q:", name)
		if s.Category != "" && !ValidSensorCategories[s.Category] {
			issues = append(issues, fmt.Sprintf("%s category %q must be one of maintainability, architecture, behaviour", prefix, s.Category))
		}
		if s.Source.Kind() == "" {
			issues = append(issues, fmt.Sprintf("%s source must have exactly one of files, command, focus", prefix))
		} else {
			switch s.Source.Kind() {
			case "files":
				if len(s.Source.Files) == 0 {
					issues = append(issues, fmt.Sprintf("%s source.files must be a non-empty array", prefix))
				}
				for i, p := range s.Source.Files {
					if p == "" {
						issues = append(issues, fmt.Sprintf("%s source.files[%d] must be non-empty", prefix, i))
					}
				}
			case "command":
				if s.Source.Command == "" {
					issues = append(issues, fmt.Sprintf("%s source.command must be a non-empty string", prefix))
				}
			case "focus":
				if s.Source.Focus.Inline != nil {
					if s.Source.Focus.Inline.Prompt == "" {
						issues = append(issues, fmt.Sprintf("%s source.focus.prompt must not be empty", prefix))
					}
					if p := s.Source.Focus.Inline.Profile; p != "" && profileNames != nil && !profileNames[p] {
						issues = append(issues, fmt.Sprintf("%s source.focus references unknown profile %q", prefix, p))
					}
				} else if s.Source.Focus.Name != "" && focusNames != nil && !focusNames[s.Source.Focus.Name] {
					issues = append(issues, fmt.Sprintf("%s source.focus references undefined focus %q", prefix, s.Source.Focus.Name))
				}
			}
		}
		if s.Output.Format == "" {
			issues = append(issues, fmt.Sprintf("%s output.format must not be empty", prefix))
		}
	}
	return issues
}

// Focus is a named combination of profile + prompt for repeatable AI execution.
type Focus struct {
	Profile string `json:"profile,omitempty"`
	Prompt  string `json:"prompt"`
}

// Profile is a named configuration variant. When selected, its fields
// are merged with top-level values:
//   - `hooks`: per-event replace. If the profile declares an event,
//     it replaces the default; absent events are inherited.
//   - `mcp_servers`: deep merge (profile keys win on collision;
//     absent keys inherited; nil pointer removes an inherited server).
//   - `includes`: appended to the harness's base includes. Profile-only
//     includes let a single harness carry multiple artifact sets and
//     swap them based on the active profile.
type Profile struct {
	Hooks      map[string][]HookEntry `json:"hooks,omitempty"`
	MCPServers map[string]*MCPServer  `json:"mcp_servers,omitempty"`
	Includes   []IncludeMeta          `json:"includes,omitempty"`
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

// ValidateProfiles validates hooks and mcp_servers within each profile.
// Nil MCPServer entries (JSON null) are skipped — they signal removal of
// an inherited server during profile merge.
func ValidateProfiles(profiles map[string]Profile) []string {
	var issues []string
	for name, profile := range profiles {
		for _, issue := range ValidateHooks(profile.Hooks) {
			issues = append(issues, fmt.Sprintf("profile %q: %s", name, issue))
		}
		// Filter out nil entries (null removals) before validating
		servers := make(map[string]MCPServer)
		for k, v := range profile.MCPServers {
			if v != nil {
				servers[k] = *v
			}
		}
		for _, issue := range ValidateMCPServers(servers) {
			issues = append(issues, fmt.Sprintf("profile %q: %s", name, issue))
		}
	}
	return issues
}

// ValidateFocus checks that each focus entry has a non-empty prompt.
func ValidateFocus(focuses map[string]Focus) []string {
	var issues []string
	for name, f := range focuses {
		if f.Prompt == "" {
			issues = append(issues, fmt.Sprintf("focus.%s: prompt must not be empty", name))
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

// IncludeMeta is the JSON representation of an include source. Exactly one
// of `git` (remote) or `local` (path-based) must be set. For both forms
// `path` scopes into a subdirectory of the source and `pick` filters paths.
// `ref` is Git-only.
type IncludeMeta struct {
	Git   string   `json:"git,omitempty"`
	Local string   `json:"local,omitempty"`
	Ref   string   `json:"ref,omitempty"`
	Path  string   `json:"path,omitempty"`
	Pick  []string `json:"pick,omitempty"`
}

// DelegateMeta is the JSON representation of a delegate reference.
type DelegateMeta struct {
	Git  string `json:"git"`
	Ref  string `json:"ref,omitempty"`
	Path string `json:"path,omitempty"`
}

// HarnessFile is the manifest filename used in harness directories.
const HarnessFile = ".harness.json"

// PluginDir is the manifest directory for the 0.2+ format.
const PluginDir = ".ynh-plugin"

// PluginFile is the manifest filename inside PluginDir.
const PluginFile = "plugin.json"

// InstalledFile holds install-time provenance inside PluginDir.
// Authors never write this file — ynh install writes it at install time.
const InstalledFile = "installed.json"

// InstalledJSON records where a harness was installed from.
// It lives at .ynh-plugin/installed.json, separate from the author-controlled plugin.json.
type InstalledJSON struct {
	SourceType   string               `json:"source_type"`
	Source       string               `json:"source"`
	Ref          string               `json:"ref,omitempty"`
	SHA          string               `json:"sha,omitempty"`
	Path         string               `json:"path,omitempty"`
	Namespace    string               `json:"namespace,omitempty"`
	RegistryName string               `json:"registry_name,omitempty"`
	InstalledAt  string               `json:"installed_at"`
	ForkedFrom   *ForkedFromJSON      `json:"forked_from,omitempty"`
	Resolved     []ResolvedSourceJSON `json:"resolved,omitempty"`
}

// ResolvedSourceJSON records the resolved commit SHA for an include or
// delegate at install/update time. Identity is the (Git, Ref, Path) triple
// matching the manifest entry. Floating-ref entries (Ref == "" or branch
// name) get their SHA filled here so consumers can compare against
// ls-remote without re-resolving.
type ResolvedSourceJSON struct {
	Git  string `json:"git"`
	Ref  string `json:"ref,omitempty"`
	Path string `json:"path,omitempty"`
	SHA  string `json:"sha"`
}

// ForkedFromJSON records the upstream a local harness was forked from.
// Populated by `ynh fork`; absent on installs that were not forked.
type ForkedFromJSON struct {
	SourceType   string `json:"source_type"`
	Source       string `json:"source"`
	Ref          string `json:"ref,omitempty"`
	SHA          string `json:"sha,omitempty"`
	Path         string `json:"path,omitempty"`
	RegistryName string `json:"registry_name,omitempty"`
	Version      string `json:"version,omitempty"`
}

// IsHarnessDir returns true if the directory contains a .harness.json manifest.
func IsHarnessDir(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, HarnessFile))
	return err == nil
}

// IsPluginDir returns true if the directory contains a .ynh-plugin/plugin.json manifest.
func IsPluginDir(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, PluginDir, PluginFile))
	return err == nil
}

// LoadPluginJSON reads and parses .ynh-plugin/plugin.json from dir.
// Unknown fields are rejected. The migration chain must run before this
// so callers can assume the new format exists.
func LoadPluginJSON(dir string) (*HarnessJSON, error) {
	data, err := os.ReadFile(filepath.Join(dir, PluginDir, PluginFile))
	if err != nil {
		return nil, fmt.Errorf("reading plugin.json: %w", err)
	}

	var hj HarnessJSON
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&hj); err != nil {
		return nil, fmt.Errorf("invalid plugin.json: %w", err)
	}

	if hj.Name == "" {
		return nil, fmt.Errorf("plugin.json missing required field: name")
	}

	return &hj, nil
}

// SavePluginJSON writes hj to .ynh-plugin/plugin.json in dir.
// InstalledFrom is stripped — provenance belongs in installed.json.
func SavePluginJSON(dir string, hj *HarnessJSON) error {
	if err := os.MkdirAll(filepath.Join(dir, PluginDir), 0o755); err != nil {
		return fmt.Errorf("creating .ynh-plugin dir: %w", err)
	}

	clean := *hj
	clean.InstalledFrom = nil

	data, err := json.MarshalIndent(clean, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling plugin.json: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(filepath.Join(dir, PluginDir, PluginFile), data, 0o644); err != nil {
		return fmt.Errorf("writing plugin.json: %w", err)
	}

	return nil
}

// LoadInstalledJSON reads .ynh-plugin/installed.json from dir.
func LoadInstalledJSON(dir string) (*InstalledJSON, error) {
	data, err := os.ReadFile(filepath.Join(dir, PluginDir, InstalledFile))
	if err != nil {
		return nil, fmt.Errorf("reading installed.json: %w", err)
	}

	var ins InstalledJSON
	if err := json.Unmarshal(data, &ins); err != nil {
		return nil, fmt.Errorf("invalid installed.json: %w", err)
	}

	return &ins, nil
}

// SaveInstalledJSON writes ins to .ynh-plugin/installed.json in dir.
func SaveInstalledJSON(dir string, ins *InstalledJSON) error {
	if err := os.MkdirAll(filepath.Join(dir, PluginDir), 0o755); err != nil {
		return fmt.Errorf("creating .ynh-plugin dir: %w", err)
	}

	data, err := json.MarshalIndent(ins, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling installed.json: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(filepath.Join(dir, PluginDir, InstalledFile), data, 0o644); err != nil {
		return fmt.Errorf("writing installed.json: %w", err)
	}

	return nil
}

// IsLegacyPluginDir returns true if the directory contains a legacy .claude-plugin/plugin.json.
func IsLegacyPluginDir(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	return err == nil
}

// LoadHarnessJSON reads and parses harness.json from dir.
// Unknown fields are rejected via DisallowUnknownFields.
func LoadHarnessJSON(dir string) (*HarnessJSON, error) {
	data, err := os.ReadFile(filepath.Join(dir, HarnessFile))
	if err != nil {
		return nil, fmt.Errorf("reading .harness.json: %w", err)
	}

	var hj HarnessJSON
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&hj); err != nil {
		return nil, fmt.Errorf("invalid .harness.json: %w", err)
	}

	if hj.Name == "" {
		return nil, fmt.Errorf(".harness.json missing required field: name")
	}

	return &hj, nil
}

// LoadHarnessFile reads and parses a .harness.json from a file path directly.
// Unlike LoadHarnessJSON, the name field is not required (for inline config).
func LoadHarnessFile(path string) (*HarnessJSON, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var hj HarnessJSON
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&hj); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", path, err)
	}

	return &hj, nil
}

// SaveHarnessJSON writes a HarnessJSON manifest to dir/.harness.json.
func SaveHarnessJSON(dir string, hj *HarnessJSON) error {
	data, err := json.MarshalIndent(hj, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling .harness.json: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(filepath.Join(dir, HarnessFile), data, 0o644); err != nil {
		return fmt.Errorf("writing .harness.json: %w", err)
	}

	return nil
}
