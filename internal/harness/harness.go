package harness

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/plugin"
)

// validName matches safe harness names: alphanumeric, hyphens, underscores, dots.
// Must start with a letter or digit. Prevents path traversal and shell injection.
var validName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// GitSource holds the common fields for any Git-backed reference.
type GitSource struct {
	Git  string
	Ref  string
	Path string
}

type Include struct {
	GitSource
	Pick []string
}

type Delegate struct {
	GitSource
}

// Provenance records where a harness was installed from.
type Provenance struct {
	SourceType   string
	Source       string
	Path         string
	RegistryName string
	InstalledAt  string
}

type Harness struct {
	Name          string
	Description   string
	DefaultVendor string
	Includes      []Include
	DelegatesTo   []Delegate
	Hooks         map[string][]plugin.HookEntry
	MCPServers    map[string]plugin.MCPServer
	Profiles      map[string]plugin.Profile
	Focuses       map[string]plugin.Focus
	InstalledFrom *Provenance
}

// DetectFormat returns "harness" if dir contains harness.json,
// "bare" if dir contains AGENTS.md or instructions.md but no harness.json,
// or "" if neither is found. Returns "legacy" if .claude-plugin/plugin.json
// is found without harness.json.
func DetectFormat(dir string) string {
	if plugin.IsHarnessDir(dir) {
		return "harness"
	}
	if plugin.IsLegacyPluginDir(dir) {
		return "legacy"
	}
	return ""
}

// ErrNotFound is returned when a harness is not installed.
var ErrNotFound = errors.New("harness not found")

func Load(name string) (*Harness, error) {
	installDir := InstalledDir(name)
	switch DetectFormat(installDir) {
	case "harness":
		return LoadDir(installDir)
	case "legacy":
		return nil, fmt.Errorf("harness %q: legacy format detected. Consolidate .claude-plugin/plugin.json and metadata.json into .harness.json", name)
	default:
		return nil, fmt.Errorf("harness %q: %w", name, ErrNotFound)
	}
}

func List() ([]string, error) {
	dir := config.HarnessesDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			subDir := filepath.Join(dir, entry.Name())
			if DetectFormat(subDir) == "harness" {
				names = append(names, entry.Name())
			}
		}
	}

	return names, nil
}

func InstalledDir(name string) string {
	return filepath.Join(config.HarnessesDir(), name)
}

// LoadDir loads a harness from a directory containing harness.json.
func LoadDir(dir string) (*Harness, error) {
	hj, err := plugin.LoadHarnessJSON(dir)
	if err != nil {
		return nil, err
	}

	if !validName.MatchString(hj.Name) {
		return nil, fmt.Errorf("invalid harness name %q: must match %s", hj.Name, validName.String())
	}

	p := &Harness{Name: hj.Name, Description: hj.Description}
	p.DefaultVendor = hj.DefaultVendor

	for _, inc := range hj.Includes {
		p.Includes = append(p.Includes, Include{
			GitSource: GitSource{Git: inc.Git, Ref: inc.Ref, Path: inc.Path},
			Pick:      inc.Pick,
		})
	}
	for _, del := range hj.DelegatesTo {
		p.DelegatesTo = append(p.DelegatesTo, Delegate{
			GitSource: GitSource{Git: del.Git, Ref: del.Ref, Path: del.Path},
		})
	}
	if len(hj.Hooks) > 0 {
		p.Hooks = hj.Hooks
	}
	if len(hj.MCPServers) > 0 {
		p.MCPServers = hj.MCPServers
	}
	if len(hj.Profiles) > 0 {
		p.Profiles = hj.Profiles
	}
	if len(hj.Focuses) > 0 {
		p.Focuses = hj.Focuses
	}
	if hj.InstalledFrom != nil {
		prov := hj.InstalledFrom
		p.InstalledFrom = &Provenance{
			SourceType:   prov.SourceType,
			Source:       prov.Source,
			Path:         prov.Path,
			RegistryName: prov.RegistryName,
			InstalledAt:  prov.InstalledAt,
		}
	}

	return p, nil
}

// ResolveProfile returns a copy of the harness with profile settings merged
// into top-level values. MCP servers are deep-merged (profile keys win on
// collision, absent keys inherited; nil pointer removes inherited entry).
// Hooks use per-event replace (if profile declares an event, it replaces
// the default; other events are inherited). Server env maps are deep-merged.
// Returns an error if the profile is not defined.
func ResolveProfile(h *Harness, profileName string) (*Harness, error) {
	if profileName == "" {
		return h, nil
	}

	profile, ok := h.Profiles[profileName]
	if !ok {
		return nil, fmt.Errorf("profile %q not defined in .harness.json", profileName)
	}

	resolved := *h

	// Merge hooks: per-event replace, inherit absent events
	if profile.Hooks != nil {
		merged := make(map[string][]plugin.HookEntry)
		for k, v := range h.Hooks {
			merged[k] = v
		}
		for k, v := range profile.Hooks {
			merged[k] = v
		}
		resolved.Hooks = merged
	}

	// Merge MCP servers: deep merge, nil removes inherited
	if profile.MCPServers != nil {
		merged := make(map[string]plugin.MCPServer)
		for k, v := range h.MCPServers {
			merged[k] = v
		}
		for k, v := range profile.MCPServers {
			if v == nil {
				delete(merged, k)
			} else {
				existing, exists := merged[k]
				if exists {
					// Deep merge env maps
					if v.Command != "" {
						existing.Command = v.Command
					}
					if v.Args != nil {
						existing.Args = v.Args
					}
					if v.URL != "" {
						existing.URL = v.URL
					}
					if v.Headers != nil {
						existing.Headers = v.Headers
					}
					if v.Env != nil {
						if existing.Env == nil {
							existing.Env = make(map[string]string)
						}
						for ek, ev := range v.Env {
							existing.Env[ek] = ev
						}
					}
					merged[k] = existing
				} else {
					merged[k] = *v
				}
			}
		}
		resolved.MCPServers = merged
	}

	return &resolved, nil
}

// LoadFile loads a harness from a file path directly (e.g. .harness.json).
// Unlike LoadDir, name is optional and the validName check is skipped.
func LoadFile(path string) (*Harness, error) {
	hj, err := plugin.LoadHarnessFile(path)
	if err != nil {
		return nil, err
	}

	p := &Harness{Name: hj.Name, Description: hj.Description}
	p.DefaultVendor = hj.DefaultVendor

	for _, inc := range hj.Includes {
		p.Includes = append(p.Includes, Include{
			GitSource: GitSource{Git: inc.Git, Ref: inc.Ref, Path: inc.Path},
			Pick:      inc.Pick,
		})
	}
	for _, del := range hj.DelegatesTo {
		p.DelegatesTo = append(p.DelegatesTo, Delegate{
			GitSource: GitSource{Git: del.Git, Ref: del.Ref, Path: del.Path},
		})
	}
	if len(hj.Hooks) > 0 {
		p.Hooks = hj.Hooks
	}
	if len(hj.MCPServers) > 0 {
		p.MCPServers = hj.MCPServers
	}
	if len(hj.Profiles) > 0 {
		p.Profiles = hj.Profiles
	}
	if len(hj.Focuses) > 0 {
		p.Focuses = hj.Focuses
	}

	return p, nil
}

// Artifacts holds the names of local artifacts found in a harness directory,
// keyed by artifact type (skills, agents, rules, commands).
type Artifacts struct {
	Skills   []string
	Agents   []string
	Rules    []string
	Commands []string
}

// Total returns the total number of local artifacts.
func (a *Artifacts) Total() int {
	return len(a.Skills) + len(a.Agents) + len(a.Rules) + len(a.Commands)
}

// ScanArtifacts discovers local artifacts in a harness's installed directory.
// Skills are directories containing SKILL.md; agents, rules, and commands are .md files.
func ScanArtifacts(name string) (*Artifacts, error) {
	dir := InstalledDir(name)
	a := &Artifacts{}

	// Skills: subdirectories with SKILL.md
	a.Skills = scanSkillDirs(filepath.Join(dir, "skills"))

	// Agents, rules, commands: .md files
	a.Agents = scanMDFiles(filepath.Join(dir, "agents"))
	a.Rules = scanMDFiles(filepath.Join(dir, "rules"))
	a.Commands = scanMDFiles(filepath.Join(dir, "commands"))

	return a, nil
}

// scanSkillDirs returns names of subdirectories that contain a SKILL.md file.
func scanSkillDirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			if _, err := os.Stat(filepath.Join(dir, entry.Name(), "SKILL.md")); err == nil {
				names = append(names, entry.Name())
			}
		}
	}
	sort.Strings(names)
	return names
}

// scanMDFiles returns names (without .md extension) of markdown files in dir.
func scanMDFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			names = append(names, strings.TrimSuffix(entry.Name(), ".md"))
		}
	}
	sort.Strings(names)
	return names
}
