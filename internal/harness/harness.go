package harness

import (
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
	InstalledFrom *Provenance
}

// DetectFormat returns "plugin" if dir contains .claude-plugin/plugin.json,
// or "" if not found.
func DetectFormat(dir string) string {
	if plugin.IsPluginDir(dir) {
		return "plugin"
	}
	return ""
}

func Load(name string) (*Harness, error) {
	installDir := InstalledDir(name)
	if DetectFormat(installDir) != "plugin" {
		return nil, fmt.Errorf("harness %q: no .claude-plugin/plugin.json found", name)
	}
	return LoadPluginDir(installDir)
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
			if DetectFormat(subDir) == "plugin" {
				names = append(names, entry.Name())
			}
		}
	}

	return names, nil
}

func InstalledDir(name string) string {
	return filepath.Join(config.HarnessesDir(), name)
}

// LoadPluginDir loads a harness from a plugin-format directory.
func LoadPluginDir(dir string) (*Harness, error) {
	pj, err := plugin.LoadPluginJSON(dir)
	if err != nil {
		return nil, err
	}

	if !validName.MatchString(pj.Name) {
		return nil, fmt.Errorf("invalid harness name %q: must match %s", pj.Name, validName.String())
	}

	p := &Harness{Name: pj.Name, Description: pj.Description}

	meta, err := plugin.LoadMetadataJSON(dir)
	if err != nil {
		return nil, err
	}
	if meta != nil && meta.YNH != nil {
		p.DefaultVendor = meta.YNH.DefaultVendor
		for _, inc := range meta.YNH.Includes {
			p.Includes = append(p.Includes, Include{
				GitSource: GitSource{Git: inc.Git, Ref: inc.Ref, Path: inc.Path},
				Pick:      inc.Pick,
			})
		}
		for _, del := range meta.YNH.DelegatesTo {
			p.DelegatesTo = append(p.DelegatesTo, Delegate{
				GitSource: GitSource{Git: del.Git, Ref: del.Ref, Path: del.Path},
			})
		}
		if len(meta.YNH.Hooks) > 0 {
			p.Hooks = meta.YNH.Hooks
		}
		if len(meta.YNH.MCPServers) > 0 {
			p.MCPServers = meta.YNH.MCPServers
		}
		if meta.YNH.InstalledFrom != nil {
			prov := meta.YNH.InstalledFrom
			p.InstalledFrom = &Provenance{
				SourceType:   prov.SourceType,
				Source:       prov.Source,
				Path:         prov.Path,
				RegistryName: prov.RegistryName,
				InstalledAt:  prov.InstalledAt,
			}
		}
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
