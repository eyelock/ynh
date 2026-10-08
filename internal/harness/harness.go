package harness

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/eyelock/ynh/internal/agentplugin"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/migration"
	"github.com/eyelock/ynh/internal/namespace"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/vendor"
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
	Format       string
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
	Name        string
	Version     string
	Description string
	// Author and Keywords exist so an assembled vendor manifest can carry what
	// the source manifest declared. Without them a *Harness cannot reproduce
	// its own plugin.json, and every path that assembles from one — `ynh run`
	// and `ynd preview` — silently ships less than `ynd export` does.
	Author        *plugin.AuthorInfo
	Keywords      []string
	DefaultVendor string
	Namespace     string // e.g. "eyelock/assistants"; empty for local/unqualified installs
	Dir           string // absolute path to the harness directory, the base for relative local includes
	Includes      []Include
	DelegatesTo   []Delegate
	Hooks         map[string][]plugin.HookEntry
	MCPServers    map[string]plugin.MCPServer
	// MCPRemovals names servers inherited from included harnesses that this
	// harness drops: the null entries of mcp_servers, and, once a profile is
	// resolved, the nulls of the profile. ComposeMCPServers applies them after
	// the includes' servers are merged in.
	MCPRemovals     []string
	EnvPassthrough  []string
	MCPIsolation    bool // run with only this harness's MCP servers (mcp_isolation)
	Agent           *plugin.AgentConfig
	Profiles        map[string]plugin.Profile
	Focuses         map[string]plugin.Focus
	Sensors         map[string]plugin.Sensor
	SensorOverrides map[string]plugin.SensorOverride
	InstalledFrom   *Provenance

	// Manifest is the manifest this harness was loaded from: the parsed
	// .ynh-plugin/plugin.json, or the one derived in memory from an Agent
	// Plugins package. Export and info read it rather than the file, so a
	// derived harness has one to give.
	Manifest *plugin.HarnessJSON
	// Format is agentplugin.Format when the harness was derived from an
	// Agent Plugins package at load time, and "" for a ynh harness.
	Format string
	// ImportedExtensions names the client namespaces a derived harness's
	// package carried under extensions. ynh does not interpret them and
	// re-export does not reproduce them.
	ImportedExtensions []string
	// Diagnostics is what a derived harness's loader skipped or ignored,
	// for the command that installs it to report.
	Diagnostics []string
}

// ListEntry is one installed harness with its namespace.
type ListEntry struct {
	Name      string
	Namespace string // e.g. "eyelock/assistants"; empty for flat/local installs
	Dir       string // absolute path to the harness directory
}

// DetectFormat reports what manifest format a directory holds, after
// running the migration chain. Returns "plugin" (new format present),
// agentplugin.Format (an Agent Plugins package, derived at load time),
// "legacy" (unsupported pre-0.1 .claude-plugin format), or "" (nothing).
//
// The chain converts an install ynh owns; for any other tree whose manifest
// is a format ynh no longer reads (.harness.json, registry.json) it returns
// an error naming `ynd migrate`, which DetectFormat passes on rather than
// reporting the tree as manifest-less. "legacy" signals a format that
// predates ynh and has no migration path.
func DetectFormat(dir string) (string, error) {
	if _, err := migration.FormatChain().Run(dir); err != nil {
		return "", err
	}
	if plugin.IsPluginDir(dir) {
		return "plugin", nil
	}
	if agentplugin.IsPluginRoot(dir) {
		return agentplugin.Format, nil
	}
	if plugin.IsClaudePluginDir(dir) {
		return "legacy", nil
	}
	return "", nil
}

// isHarnessDir reports whether DetectFormat finds any manifest in dir. Used
// when listing installs, where an unreadable entry is skipped.
func isHarnessDir(dir string) bool {
	f, err := DetectFormat(dir)
	return err == nil && f != ""
}

// IsHarnessDir reports whether dir is something LoadDir can load: a ynh
// harness, or an Agent Plugins package that LoadDir derives one from.
func IsHarnessDir(dir string) bool {
	return plugin.IsPluginDir(dir) || agentplugin.IsPluginRoot(dir)
}

// PluginDataDir is the client-managed, persistent, writable directory the
// Agent Plugins specification (§9.1) requires for a plugin's stdio servers,
// keyed by the harness's canonical id so it survives updates and is not
// shared between harnesses. It is not created here; the caller that is
// about to launch something creates it.
func PluginDataDir(p *Harness) string {
	id := "local/" + p.Name
	if p.Namespace != "" {
		id = p.Namespace + "/" + p.Name
	}
	return filepath.Join(config.HomeDir(), "plugin-data", namespace.IDToFSName(id))
}

// AssembleMCPServers returns the servers as a vendor config should carry
// them for this harness, resolved the way the Agent Plugins specification
// asks of a client: ${PLUGIN_ROOT} and ${PLUGIN_DATA} expanded against the
// harness directory and dataDir, plugin-relative ./ paths made absolute,
// and, for a harness derived from an Agent Plugin, PLUGIN_ROOT and
// PLUGIN_DATA supplied in each stdio server's env.
//
// Credential references (${VAR} resolved through env_passthrough) apply to
// a ynh harness only. An Agent Plugin has no allowlist and the specification
// says a client expands nothing but the two placeholders, so for a derived
// harness any other reference reaches the server literally, as the package
// author was told at export time.
func AssembleMCPServers(p *Harness, dataDir string, lookup func(string) (string, bool)) (map[string]plugin.MCPServer, error) {
	derived := p.Format == agentplugin.Format
	servers := plugin.ExpandPluginPlaceholders(p.MCPServers, p.Dir, dataDir, derived)
	if derived {
		return servers, nil
	}
	return plugin.ExpandMCPEnv(servers, p.EnvPassthrough, lookup)
}

// artifactNamespaces lists the client namespace directories that hold
// artifacts in ynh's layout, as the vendor adapters declare them.
func artifactNamespaces() []string {
	var dirs []string
	for _, name := range vendor.Available() {
		a, err := vendor.Get(name)
		if err != nil {
			continue
		}
		if d := a.AgentPluginLayout().ArtifactDir; d != "" && d != "." {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

// ErrNotFound is returned when a harness is not installed.
var ErrNotFound = errors.New("harness not found")

// LoadQualified loads an installed harness by canonical id. Schema 2 only:
// bare names and the legacy "name@org/repo" form are hard-rejected with a
// hint pointing at the canonical id.
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

// LoadIDOrPath resolves the harness argument of a command that takes either
// an installed id or a local harness directory (`ynh check`, `ynh run`, `ynh
// agent run`), so all of them accept and refuse exactly the same refs.
//
// A path needs no prior install. It goes through the format chain, which
// refuses a tree whose manifest ynh no longer reads (a legacy .harness.json)
// with the `ynd migrate` fix and writes nothing. Anything that is neither an
// id nor a path gets the hint that names both forms. The returned harness's
// Dir is absolute for a path.
func LoadIDOrPath(ref string) (*Harness, error) {
	switch namespace.Classify(ref) {
	case namespace.RefID:
		return LoadByID(ref)
	case namespace.RefPath:
		// Resolved below.
	default:
		return nil, BadRefOrPathError(ref)
	}

	dir := ref
	if strings.HasPrefix(dir, "~/") {
		if home, hErr := os.UserHomeDir(); hErr == nil {
			dir = filepath.Join(home, dir[2:])
		}
	}
	abs, absErr := filepath.Abs(dir)
	if absErr != nil {
		return nil, fmt.Errorf("resolving harness path %q: %w", ref, absErr)
	}
	if _, statErr := os.Stat(abs); statErr != nil {
		return nil, fmt.Errorf("no harness at %s: %w", abs, statErr)
	}
	if _, mErr := migration.FormatChain().Run(abs); mErr != nil {
		return nil, mErr
	}
	if !IsHarnessDir(abs) {
		return nil, fmt.Errorf(
			"no harness at %s: expected %s. Run `ynd create harness <name>` to make one, "+
				"or pass an installed id (`ynh ls` lists them)",
			abs, plugin.PluginFile)
	}
	return LoadDir(abs)
}

// BadRefError formats the rejection message for a ref that is not a valid
// canonical id, for a command that takes only an installed id. Its hint
// lists only the forms such a command accepts: a command that also takes a
// local harness directory uses BadRefOrPathError instead, so no command
// offers a form it then refuses (#448).
func BadRefError(ref string) error {
	return badRef(ref, false)
}

// BadRefOrPathError is BadRefError for a command that takes either an
// installed id or a local harness directory, and its hint names both.
func BadRefOrPathError(ref string) error {
	return badRef(ref, true)
}

// badRef builds both rejections. The message is multi-line; lint suppresses
// the trailing-punctuation check via the nolint directive, as the hint
// trailer is intentionally human-readable.
//
//nolint:staticcheck // ST1005: multi-line user-facing hint
func badRef(ref string, pathOK bool) error {
	if ref == "" {
		return fmt.Errorf("missing harness reference")
	}
	forms := "Use a canonical id like 'github.com/<org>/<repo>/<name>' or 'local/<name>'. "
	if pathOK {
		forms = "Use a canonical id like 'github.com/<org>/<repo>/<name>' or 'local/<name>', " +
			"or './<path>' for a local harness directory. "
	}
	return fmt.Errorf("%q is not a valid harness id. %sRun 'ynh ls' to see installed ids", ref, forms)
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
			if isHarnessDir(entryPath) {
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
				if isHarnessDir(childDir) {
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
			if isHarnessDir(entryPath) {
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

// LoadDir loads a harness from a directory. The format chain runs first:
// it converts an install ynh owns and refuses, with the `ynd migrate` fix,
// any other tree whose manifest ynh no longer reads, so callers never handle
// legacy formats themselves.
//
// Tree-form installs (see topology.go) store their provenance in
// <dir>/.agents/harness/installed.json; LoadDir reads it from there.
// Pointer-form installs carry their provenance on the pointer file and
// must use loadDirWithProvenance to supply it explicitly — otherwise the
// source tree would need a redundant installed.json.
func LoadDir(dir string) (*Harness, error) {
	return loadDirWithProvenance(dir, nil)
}

// loadDirWithProvenance is the implementation of LoadDir with an explicit
// provenance record. When ins is nil it is read from
// <contentDir>/.agents/harness/installed.json (tree-form behaviour). When ins
// is supplied (pointer-form) the contentDir need not carry installed.json.
func loadDirWithProvenance(contentDir string, ins *plugin.InstalledJSON) (*Harness, error) {
	dir := contentDir
	if _, err := migration.FormatChain().Run(dir); err != nil {
		return nil, err
	}

	// An Agent Plugins package is derived in memory, every time, and the
	// package is never written to: it stays a clean Agent Plugin whether it
	// sits in the user's own tree (pointer-form) or in ynh's copy.
	var derived *agentplugin.Derivation
	if !plugin.IsPluginDir(dir) && agentplugin.IsPluginRoot(dir) {
		var err error
		if derived, err = agentplugin.DeriveHarness(dir, artifactNamespaces()); err != nil {
			return nil, fmt.Errorf("loading Agent Plugin: %w", err)
		}
	}

	if derived == nil && plugin.IsClaudePluginDir(dir) && !plugin.IsPluginDir(dir) {
		return nil, fmt.Errorf("legacy .claude-plugin format is not supported; migrate to .agents/harness/plugin.json")
	}
	if derived == nil && !plugin.IsPluginDir(dir) {
		return nil, fmt.Errorf("no harness manifest found in %s", dir)
	}

	if ins == nil {
		// Tree-form: read provenance from disk. Tolerate absence; the
		// fields default to zero and callers that need them check
		// p.InstalledFrom for nil.
		if disk, err := plugin.LoadInstalledJSON(dir); err == nil {
			ins = disk
		}
	}

	var hj *plugin.HarnessJSON
	if derived != nil {
		hj = derived.Manifest
	} else {
		var err error
		if hj, err = plugin.LoadPluginJSON(dir); err != nil {
			return nil, err
		}
	}

	if !validName.MatchString(hj.Name) {
		return nil, fmt.Errorf("invalid harness name %q: must match %s", hj.Name, validName.String())
	}

	p := &Harness{Name: hj.Name, Version: hj.Version, Description: hj.Description,
		Author: hj.Author, Keywords: hj.Keywords, Manifest: hj}
	if derived != nil {
		p.Format = agentplugin.Format
		p.ImportedExtensions = derived.Extensions
		for _, d := range derived.Diagnostics {
			p.Diagnostics = append(p.Diagnostics, d.String())
		}
	}
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
	if ins != nil && len(ins.Resolved) > 0 {
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
	} else if fallback, err := plugin.LoadMCPJSON(dir); err == nil && len(fallback) > 0 {
		p.MCPServers = fallback
	}
	p.MCPRemovals = hj.MCPRemovals
	// env_passthrough and agent are not MCP settings and must not be
	// conditional on MCP servers existing. They were loaded inside that branch,
	// so a harness declaring env_passthrough or agent budgets but no MCP server
	// silently lost both: the worker allowlist had nothing to admit, and
	// manifest budget defaults were ignored in favour of the built-in ones.
	if len(hj.EnvPassthrough) > 0 {
		p.EnvPassthrough = hj.EnvPassthrough
	}
	p.MCPIsolation = hj.MCPIsolation
	if hj.Agent != nil {
		p.Agent = hj.Agent
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
	if len(hj.SensorOverrides) > 0 {
		p.SensorOverrides = hj.SensorOverrides
	}

	// Provenance: use the supplied/loaded record; fall back to InstalledFrom
	// in manifest (legacy) is handled by the caller path that left ins nil.
	if ins != nil {
		p.InstalledFrom = &Provenance{
			SourceType:   ins.SourceType,
			Source:       ins.Source,
			Ref:          ins.Ref,
			SHA:          ins.SHA,
			Path:         ins.Path,
			Namespace:    ins.Namespace,
			RegistryName: ins.RegistryName,
			InstalledAt:  ins.InstalledAt,
			Format:       ins.Format,
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
		// Name what is declared, sorted so the list does not change order
		// between calls (#520).
		if len(h.Profiles) == 0 {
			return nil, fmt.Errorf("profile %q not defined in harness manifest (the harness declares no profiles)", profileName)
		}
		return nil, fmt.Errorf("profile %q not defined in harness manifest (available: %v)", profileName, slices.Sorted(maps.Keys(h.Profiles)))
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

	// Replace the env allowlist rather than union it. A profile that exists to
	// restrict what an agent can see has to be able to; a union could only ever
	// widen, which is the wrong direction for a containment declaration.
	if profile.EnvPassthrough != nil {
		resolved.EnvPassthrough = profile.EnvPassthrough
	}

	if profile.MCPIsolation != nil {
		resolved.MCPIsolation = *profile.MCPIsolation
	}

	// Merge MCP servers: deep merge, nil removes inherited. A null also goes
	// on the removal list, because the server it names may come from an
	// included harness, which is only merged in after the profile is resolved.
	if profile.MCPServers != nil {
		merged := make(map[string]plugin.MCPServer)
		for k, v := range h.MCPServers {
			merged[k] = copyMCPServer(v)
		}
		removals := slices.Clone(h.MCPRemovals)
		for _, k := range slices.Sorted(maps.Keys(profile.MCPServers)) {
			v := profile.MCPServers[k]
			if v != nil {
				// A profile that declares a server again overrides a removal.
				removals = slices.DeleteFunc(removals, func(r string) bool { return r == k })
			}
			if v == nil {
				delete(merged, k)
				if !slices.Contains(removals, k) {
					removals = append(removals, k)
				}
				continue
			}
			existing, exists := merged[k]
			if !exists {
				merged[k] = copyMCPServer(*v)
				continue
			}
			// Deep merge: scalar fields replace when set, env merges per key.
			if v.Type != "" {
				existing.Type = v.Type
			}
			if v.Command != "" {
				existing.Command = v.Command
			}
			if v.Args != nil {
				existing.Args = slices.Clone(v.Args)
			}
			if v.Cwd != "" {
				existing.Cwd = v.Cwd
			}
			if v.URL != "" {
				existing.URL = v.URL
			}
			if v.Headers != nil {
				existing.Headers = maps.Clone(v.Headers)
			}
			if v.Env != nil {
				if existing.Env == nil {
					existing.Env = make(map[string]string, len(v.Env))
				}
				maps.Copy(existing.Env, v.Env)
			}
			merged[k] = existing
		}
		resolved.MCPServers = merged
		resolved.MCPRemovals = removals
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

// legacyHarnessFile refuses a --harness-file that names a legacy
// .harness.json, as every other read has since #417: only `ynd migrate` reads
// that file (#449). It goes by the name because the contents cannot tell the
// two apart: a single-file manifest has the same shape. A pure predicate.
func legacyHarnessFile(path string) error {
	if filepath.Base(path) != plugin.HarnessFile {
		return nil
	}
	dir := filepath.Dir(path)
	if err := migration.LegacyHarnessManifest(dir); err != nil {
		return err
	}
	// The tree has already been converted and the old file left behind:
	// point at the manifest ynh does read.
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return fmt.Errorf("%s is the legacy %s manifest, which ynh no longer reads; %s is already converted, so run it with: ynh run %s",
		filepath.Join(dir, plugin.HarnessFile), plugin.HarnessFile, dir, dir)
}

// LoadFile loads a harness from a single manifest file given by path
// (--harness-file), whatever it is named, except a legacy .harness.json.
// Unlike LoadDir, name is optional and the validName check is skipped.
func LoadFile(path string) (*Harness, error) {
	if err := legacyHarnessFile(path); err != nil {
		return nil, err
	}
	hj, err := plugin.LoadHarnessFile(path)
	if err != nil {
		return nil, err
	}

	p := &Harness{Name: hj.Name, Version: hj.Version, Description: hj.Description,
		Author: hj.Author, Keywords: hj.Keywords, Manifest: hj}
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
	p.MCPRemovals = hj.MCPRemovals
	// See above: neither of these is an MCP setting.
	if len(hj.EnvPassthrough) > 0 {
		p.EnvPassthrough = hj.EnvPassthrough
	}
	p.MCPIsolation = hj.MCPIsolation
	if hj.Agent != nil {
		p.Agent = hj.Agent
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
	if len(hj.SensorOverrides) > 0 {
		p.SensorOverrides = hj.SensorOverrides
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
