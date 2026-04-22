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
	"github.com/eyelock/ynh/internal/migration"
	"github.com/eyelock/ynh/internal/namespace"
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
	Ref          string
	SHA          string
	Path         string
	Namespace    string
	RegistryName string
	InstalledAt  string
}

type Harness struct {
	Name          string
	Version       string
	Description   string
	DefaultVendor string
	Namespace     string // e.g. "eyelock/assistants"; empty for local/unqualified installs
	Includes      []Include
	DelegatesTo   []Delegate
	Hooks         map[string][]plugin.HookEntry
	MCPServers    map[string]plugin.MCPServer
	Profiles      map[string]plugin.Profile
	Focuses       map[string]plugin.Focus
	InstalledFrom *Provenance
}

// ListEntry is one installed harness with its namespace.
type ListEntry struct {
	Name      string
	Namespace string // e.g. "eyelock/assistants"; empty for flat/local installs
	Dir       string // absolute path to the harness directory
}

// DetectFormat reports what manifest format a directory holds, after
// running the migration chain. Returns "plugin" (new format present),
// "legacy" (unsupported pre-0.1 .claude-plugin format), or "" (nothing).
//
// The migration chain converts any supported legacy format to the new
// format before detection, so callers only ever see "plugin" or "" for
// valid harnesses. Legacy detection is the one exception — it signals a
// format that predates ynh and has no migration path.
func DetectFormat(dir string) string {
	if _, err := migration.FormatChain().Run(dir); err != nil {
		return ""
	}
	if plugin.IsPluginDir(dir) {
		return "plugin"
	}
	if plugin.IsLegacyPluginDir(dir) {
		return "legacy"
	}
	return ""
}

// ErrNotFound is returned when a harness is not installed.
var ErrNotFound = errors.New("harness not found")

// Load finds and loads an installed harness by name. The migration chain
// runs inside LoadDir so no legacy handling is needed here. If the flat
// layout has the harness, use that; otherwise scan the namespaced layout.
func Load(name string) (*Harness, error) {
	flatDir := InstalledDir(name)
	if _, err := os.Stat(flatDir); err == nil {
		return LoadDir(flatDir)
	}
	return findInNamespacedDirs(name)
}

// LoadNS loads an installed harness by namespace-qualified name.
func LoadNS(ns, name string) (*Harness, error) {
	dir := InstalledDirNS(ns, name)
	if _, err := os.Stat(dir); err != nil {
		return nil, fmt.Errorf("harness %q@%q: %w", name, ns, ErrNotFound)
	}
	return LoadDir(dir)
}

// findInNamespacedDirs scans ~/.ynh/harnesses/<ns>/<name>/ for a matching harness.
func findInNamespacedDirs(name string) (*Harness, error) {
	harnessesDir := config.HarnessesDir()
	entries, err := os.ReadDir(harnessesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("harness %q: %w", name, ErrNotFound)
		}
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.Contains(e.Name(), "--") {
			continue
		}
		candidate := filepath.Join(harnessesDir, e.Name(), name)
		if DetectFormat(candidate) != "" {
			return LoadDir(candidate)
		}
	}
	return nil, fmt.Errorf("harness %q: %w", name, ErrNotFound)
}

// List returns the names of all installed harnesses across all namespaces.
func List() ([]string, error) {
	entries, err := ListAll()
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, nil
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name
	}
	return names, nil
}

// ListAll returns all installed harnesses with namespace and directory information.
func ListAll() ([]ListEntry, error) {
	harnessesDir := config.HarnessesDir()
	entries, err := os.ReadDir(harnessesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var results []ListEntry
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		entryPath := filepath.Join(harnessesDir, entry.Name())
		if strings.Contains(entry.Name(), "--") {
			// Namespace directory: walk children
			ns := namespace.FromFSName(entry.Name())
			children, err := os.ReadDir(entryPath)
			if err != nil {
				continue
			}
			for _, child := range children {
				if !child.IsDir() {
					continue
				}
				childDir := filepath.Join(entryPath, child.Name())
				if DetectFormat(childDir) != "" {
					results = append(results, ListEntry{
						Name:      child.Name(),
						Namespace: ns,
						Dir:       childDir,
					})
				}
			}
		} else {
			// Flat entry (unmigrated or local install)
			if DetectFormat(entryPath) != "" {
				results = append(results, ListEntry{
					Name: entry.Name(),
					Dir:  entryPath,
				})
			}
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Namespace != results[j].Namespace {
			return results[i].Namespace < results[j].Namespace
		}
		return results[i].Name < results[j].Name
	})
	return results, nil
}

// InstalledDirNS returns the namespaced install path for a harness.
func InstalledDirNS(ns, name string) string {
	return filepath.Join(config.HarnessesDir(), namespace.ToFSName(ns), name)
}

// NamespacedDir is an alias for InstalledDirNS.
func NamespacedDir(ns, name string) string {
	return InstalledDirNS(ns, name)
}

func InstalledDir(name string) string {
	return filepath.Join(config.HarnessesDir(), name)
}

// LoadDir loads a harness from a directory. The migration chain runs
// transparently, so callers never need to handle legacy formats themselves.
func LoadDir(dir string) (*Harness, error) {
	if _, err := migration.FormatChain().Run(dir); err != nil {
		return nil, fmt.Errorf("migrating harness manifest: %w", err)
	}

	if plugin.IsLegacyPluginDir(dir) && !plugin.IsPluginDir(dir) {
		return nil, fmt.Errorf("legacy .claude-plugin format is not supported; migrate to .ynh-plugin/plugin.json")
	}
	if !plugin.IsPluginDir(dir) {
		return nil, fmt.Errorf("no harness manifest found in %s", dir)
	}

	hj, err := plugin.LoadPluginJSON(dir)
	if err != nil {
		return nil, err
	}

	if !validName.MatchString(hj.Name) {
		return nil, fmt.Errorf("invalid harness name %q: must match %s", hj.Name, validName.String())
	}

	p := &Harness{Name: hj.Name, Version: hj.Version, Description: hj.Description}
	p.DefaultVendor = hj.DefaultVendor
	p.Namespace = inferNamespace(dir)

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

	// Provenance: prefer installed.json (new format), fall back to InstalledFrom in manifest (legacy).
	if ins, err := plugin.LoadInstalledJSON(dir); err == nil {
		p.InstalledFrom = &Provenance{
			SourceType:   ins.SourceType,
			Source:       ins.Source,
			Ref:          ins.Ref,
			SHA:          ins.SHA,
			Path:         ins.Path,
			Namespace:    ins.Namespace,
			RegistryName: ins.RegistryName,
			InstalledAt:  ins.InstalledAt,
		}
		if p.Namespace == "" && ins.Namespace != "" {
			p.Namespace = ins.Namespace
		}
	} else if hj.InstalledFrom != nil {
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

// inferNamespace derives namespace from dir path if it is under ~/.ynh/harnesses/<ns>/<name>/.
func inferNamespace(dir string) string {
	harnessesDir := config.HarnessesDir()
	parent := filepath.Dir(dir)
	if filepath.Dir(parent) == harnessesDir && strings.Contains(filepath.Base(parent), "--") {
		return namespace.FromFSName(filepath.Base(parent))
	}
	return ""
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
		return nil, fmt.Errorf("profile %q not defined in harness manifest", profileName)
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

	p := &Harness{Name: hj.Name, Version: hj.Version, Description: hj.Description}
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
	return ScanArtifactsDir(InstalledDir(name))
}

// ScanArtifactsDir discovers artifacts in an arbitrary directory.
func ScanArtifactsDir(dir string) (*Artifacts, error) {
	a := &Artifacts{}
	a.Skills = scanSkillDirs(filepath.Join(dir, "skills"))
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
