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

// IsValidName reports whether s is a syntactically valid harness name.
// Same rule LoadDir applies internally — exposed so callers (e.g. the
// `ynh fork --name <new>` validator) can reject invalid input up front
// instead of producing a half-installed harness.
func IsValidName(s string) bool {
	return validName.MatchString(s)
}

// ValidNamePattern returns the regex source string used to validate
// harness names. Useful for error messages that want to show the
// permitted shape.
func ValidNamePattern() string {
	return validName.String()
}

// GitSource holds the common fields for any include or delegate source.
// Exactly one of Git (remote) or Local (filesystem path) is set at any
// given time. Path is a subdirectory scoped within the source; Ref
// applies to Git sources only.
type GitSource struct {
	Git   string
	Local string
	Ref   string
	Path  string
}

// IsLocal reports whether the source is a filesystem path (Local set,
// Git empty). Callers use this to skip git fetch and resolve directly.
func (g GitSource) IsLocal() bool { return g.Local != "" && g.Git == "" }

type Include struct {
	GitSource
	Pick []string
	// SHA is the resolved commit at install/update time, populated from
	// installed.json's resolved slice. Empty for local-path includes and for
	// pre-migration installs that predate SHA recording.
	SHA string
	// ResolvedRef is the branch name actually tracked at install/update time.
	// For non-empty manifest refs it equals the manifest ref. For empty
	// manifest refs it is the cache's resolved default branch (e.g. "main")
	// captured at clone time. Used by --check-updates so probe targets the
	// same ref that ynh update tracks. Empty for pre-migration installs.
	ResolvedRef string
}

type Delegate struct {
	GitSource
	// SHA is the resolved commit at install/update time, populated from
	// installed.json's resolved slice. Empty for pre-migration installs.
	SHA string
	// ResolvedRef — see Include.ResolvedRef.
	ResolvedRef string
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
	ForkedFrom   *ForkedFrom
}

// ForkedFrom records the upstream that a local harness was forked from.
type ForkedFrom struct {
	SourceType   string
	Source       string
	Ref          string
	SHA          string
	Path         string
	RegistryName string
	Version      string
}

// pinnedRefRe matches a Git SHA (full or short) — 7 to 40 lowercase hex chars.
// Used to classify an include's ref as pinned (SHA) vs floating (tag/branch).
var pinnedRefRe = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// IsPinnedRef reports whether ref looks like a resolved Git SHA.
// Pinned refs identify a single immutable commit; floating refs (tags,
// branches, "main", "HEAD") track moving targets. Empty refs are floating.
func IsPinnedRef(ref string) bool {
	return ref != "" && pinnedRefRe.MatchString(ref)
}

type Harness struct {
	Name          string
	Version       string
	Description   string
	DefaultVendor string
	Namespace     string // e.g. "eyelock/assistants"; empty for local/unqualified installs
	Dir           string // absolute path to the harness directory — the base for relative local includes
	Includes      []Include
	DelegatesTo   []Delegate
	Hooks         map[string][]plugin.HookEntry
	MCPServers    map[string]plugin.MCPServer
	Profiles      map[string]plugin.Profile
	Focuses       map[string]plugin.Focus
	Sensors       map[string]plugin.Sensor
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

// LoadQualified loads an installed harness by canonical id. Schema 2 only:
// bare names and the legacy "name@org/repo" form are hard-rejected with a
// hint pointing at the canonical id and the local path alternative.
//
// Auto-migration runs before any command (see cmd/ynh autoMigrate), so by
// the time this function is reached the home is at schema 2 — every
// installed harness has an id-keyed pointer or tree-shaped install dir.
// No fallback path: there is exactly one valid ref shape for installed
// harnesses, which is the whole point of the canonical-id rule.
func LoadQualified(ref string) (*Harness, error) {
	if namespace.Classify(ref) != namespace.RefID {
		return nil, BadRefError(ref)
	}
	return LoadByID(ref)
}

// BadRefError formats the rejection message for refs that aren't a valid
// canonical id. Exported so cmd/ynh callers that pre-classify refs (e.g.
// to decide between an id and a path) can emit the same hint. The message
// is multi-line; lint suppresses the trailing-punctuation check via the
// nolint directive — the hint trailer is intentionally human-readable.
//
//nolint:staticcheck // ST1005: multi-line user-facing hint
func BadRefError(ref string) error {
	if ref == "" {
		return fmt.Errorf("missing harness reference")
	}
	return fmt.Errorf(
		"%q is not a valid harness id. "+
			"Use a canonical id like 'github.com/<org>/<repo>/<name>' or 'local/<name>', "+
			"or './<path>' for a local harness directory. "+
			"Run 'ynh ls' to see installed ids",
		ref)
}

// LoadNS loads an installed harness by namespace-qualified name.
func LoadNS(ns, name string) (*Harness, error) {
	dir := InstalledDirNS(ns, name)
	if _, err := os.Stat(dir); err != nil {
		return nil, fmt.Errorf("harness %q@%q: %w", name, ns, ErrNotFound)
	}
	return LoadDir(dir)
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

// ListAll returns all installed harnesses with namespace and directory
// information. Unions pointer-shaped installs (local forks) with
// tree-shaped installs (git/registry). Pointer entries take precedence
// over a flat/local tree with the same canonical id — a pre-1.0 invariant
// for the case where ynh fork co-existed with a local tree install. A
// pointer and a remote registry install can share the same leaf name but
// have distinct canonical ids (e.g. "local/foo" vs
// "github.com/org/repo/foo") and must both appear.
func ListAll() ([]ListEntry, error) {
	pointers, err := ListPointers()
	if err != nil {
		return nil, err
	}
	// Key by canonical id, not bare name, so a fork and a registry install
	// that share only the leaf name are not incorrectly deduplicated.
	seen := make(map[string]bool, len(pointers))
	for _, p := range pointers {
		seen["local/"+p.Name] = true
	}

	harnessesDir := config.HarnessesDir()
	entries, err := os.ReadDir(harnessesDir)
	if err != nil {
		if os.IsNotExist(err) {
			sort.Slice(pointers, func(i, j int) bool {
				if pointers[i].Namespace != pointers[j].Namespace {
					return pointers[i].Namespace < pointers[j].Namespace
				}
				return pointers[i].Name < pointers[j].Name
			})
			return pointers, nil
		}
		return nil, err
	}

	results := append([]ListEntry(nil), pointers...)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		entryPath := filepath.Join(harnessesDir, entry.Name())
		if strings.Contains(entry.Name(), "--") {
			// Schema-2 install: the dir itself is the harness, named by
			// its id-fsname (e.g. "local--demo" or
			// "github.com--eyelock--assistants--planner"). Detected by the
			// presence of a manifest at the top level — distinguishes from
			// schema-1 namespace directories which only contain children.
			if DetectFormat(entryPath) != "" {
				id := namespace.FSNameToID(entry.Name())
				name := entry.Name()
				if i := strings.LastIndex(id, "/"); i >= 0 {
					name = id[i+1:]
				}
				if seen[id] {
					continue
				}
				ns, _ := namespace.SplitID(id)
				// "local" id namespace is an internal sentinel; downstream
				// schema-2 emitters override this from the canonical id, but
				// for schema-1 consumers (text format, namespace==flat) keep
				// it empty when the id is "local/<name>".
				if ns == "local" {
					ns = ""
				}
				results = append(results, ListEntry{
					Name:      name,
					Namespace: ns,
					Dir:       entryPath,
				})
				continue
			}
			// Schema-1 namespace directory: walk children
			ns := namespace.FromFSName(entry.Name())
			children, err := os.ReadDir(entryPath)
			if err != nil {
				continue
			}
			for _, child := range children {
				if !child.IsDir() {
					continue
				}
				if seen[ns+"/"+child.Name()] {
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
			if seen["local/"+entry.Name()] {
				continue
			}
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

// InstalledDirByID returns the schema-2 install directory for a harness with
// the given canonical id. The directory name is the id with "/" replaced by
// "--" — same transliteration as PointerPathByID and the cache.
//
//	"github.com/eyelock/assistants/planner" → "<HarnessesDir>/github.com--eyelock--assistants--planner"
//	"local/planner"                         → "<HarnessesDir>/local--planner"
//
// This replaces the schema-1 split between InstalledDir (flat) and
// InstalledDirNS (two-level) with a single id-keyed layout. After schema-2
// migration, every install lives at InstalledDirByID(id).
func InstalledDirByID(id string) string {
	return filepath.Join(config.HarnessesDir(), namespace.IDToFSName(id))
}

// LoadByID loads an installed harness by its canonical id. Resolution
// precedence under schema 2:
//  1. Pointer file at ~/.ynh/installed/<id-fsname>.json (local fork / alias)
//  2. Schema-1 fallback: for "local/<name>" ids, name-keyed pointer at
//     ~/.ynh/installed/<name>.json — handles forks created before the
//     schema-2 pointer writer landed (or in homes already stamped schema-2
//     when fork wrote a schema-1 file).
//  3. Tree at ~/.ynh/harnesses/<id-fsname>/
//
// Returns ErrNotFound if none match. Callers that received a user-typed
// ref must Classify first and only call LoadByID for RefID kinds.
func LoadByID(id string) (*Harness, error) {
	if id == "" {
		return nil, fmt.Errorf("harness id %q: %w", id, ErrNotFound)
	}
	if ptr, err := LoadPointerByID(id); err != nil {
		return nil, err
	} else if ptr != nil {
		return loadFromPointer(ptr)
	}
	// Schema-1 fallback: a fork created by an older binary (or by a binary
	// that wrote schema-1 into an already-schema-2 home) stores its pointer
	// as <name>.json rather than local--<name>.json. Try it before giving up.
	if name, ok := strings.CutPrefix(id, "local/"); ok {
		if ptr, err := LoadPointer(name); err != nil {
			return nil, err
		} else if ptr != nil {
			return loadFromPointer(ptr)
		}
	}
	dir := InstalledDirByID(id)
	if _, err := os.Stat(dir); err == nil {
		return LoadDir(dir)
	}
	return nil, fmt.Errorf("harness %q: %w", id, ErrNotFound)
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
	if abs, err := filepath.Abs(dir); err == nil {
		p.Dir = abs
	} else {
		p.Dir = dir
	}

	for _, inc := range hj.Includes {
		p.Includes = append(p.Includes, Include{
			GitSource: GitSource{Git: inc.Git, Local: inc.Local, Ref: inc.Ref, Path: inc.Path},
			Pick:      inc.Pick,
		})
	}
	for _, del := range hj.DelegatesTo {
		p.DelegatesTo = append(p.DelegatesTo, Delegate{
			GitSource: GitSource{Git: del.Git, Ref: del.Ref, Path: del.Path},
		})
	}

	// Backfill resolved SHAs and resolved refs from installed.json onto
	// includes/delegates so downstream consumers (list, info,
	// --check-updates) have both a recorded commit and the ref that was
	// actually tracked, even for floating manifest refs.
	//
	// Matching: prefer an exact (git, ref, path) match against the manifest
	// ref. If none, fall back to (git, path). The fallback covers the
	// floating-ref case where the manifest ref is empty but the resolved
	// entry's ref records the cache's default branch — e.g. "main" — that
	// the install actually tracked.
	if ins, err := plugin.LoadInstalledJSON(dir); err == nil && ins != nil && len(ins.Resolved) > 0 {
		find := func(git, ref, path string) (sha, resolvedRef string) {
			for _, r := range ins.Resolved {
				if r.Git == git && r.Ref == ref && r.Path == path {
					return r.SHA, r.Ref
				}
			}
			for _, r := range ins.Resolved {
				if r.Git == git && r.Path == path {
					return r.SHA, r.Ref
				}
			}
			return "", ""
		}
		for i := range p.Includes {
			if p.Includes[i].Git == "" {
				continue
			}
			p.Includes[i].SHA, p.Includes[i].ResolvedRef = find(p.Includes[i].Git, p.Includes[i].Ref, p.Includes[i].Path)
		}
		for i := range p.DelegatesTo {
			p.DelegatesTo[i].SHA, p.DelegatesTo[i].ResolvedRef = find(p.DelegatesTo[i].Git, p.DelegatesTo[i].Ref, p.DelegatesTo[i].Path)
		}
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
	if len(hj.Sensors) > 0 {
		p.Sensors = hj.Sensors
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
		if ins.ForkedFrom != nil {
			ff := ins.ForkedFrom
			p.InstalledFrom.ForkedFrom = &ForkedFrom{
				SourceType:   ff.SourceType,
				Source:       ff.Source,
				Ref:          ff.Ref,
				SHA:          ff.SHA,
				Path:         ff.Path,
				RegistryName: ff.RegistryName,
				Version:      ff.Version,
			}
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

	// Append profile-level includes to the harness's base includes. Profile
	// includes cannot remove base includes — they only add. Order: base first,
	// then profile entries, so a later profile pick can shadow a base pick
	// when the assembler resolves collisions.
	if len(profile.Includes) > 0 {
		merged := make([]Include, 0, len(h.Includes)+len(profile.Includes))
		merged = append(merged, h.Includes...)
		for _, inc := range profile.Includes {
			merged = append(merged, Include{
				GitSource: GitSource{
					Git:   inc.Git,
					Local: inc.Local,
					Ref:   inc.Ref,
					Path:  inc.Path,
				},
				Pick: inc.Pick,
			})
		}
		resolved.Includes = merged
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
			GitSource: GitSource{Git: inc.Git, Local: inc.Local, Ref: inc.Ref, Path: inc.Path},
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
	if len(hj.Sensors) > 0 {
		p.Sensors = hj.Sensors
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

// ArtifactTypeDirs lists the directory-style artifact roots — each entry
// is a subdirectory of the harness root whose children are themselves
// directories, each holding a manifest file (SKILL.md today).
//
// Keep this list in lock-step with the pick.items pattern in
// docs/schema/plugin.schema.json. The TestArtifactTypes_SchemaAgreement
// test in this package asserts they match.
var ArtifactTypeDirs = []string{"skills"}

// ArtifactTypeFiles lists the flat-file artifact roots — each entry is a
// subdirectory of the harness root whose children are individual .md files.
//
// Keep this list in lock-step with the pick.items pattern in
// docs/schema/plugin.schema.json.
var ArtifactTypeFiles = []string{"agents", "rules", "commands"}

// ScanArtifactsDir discovers artifacts in an arbitrary directory.
func ScanArtifactsDir(dir string) (*Artifacts, error) {
	a := &Artifacts{}
	// ArtifactTypeDirs[0] is "skills" — update if more directory-style types are added.
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
