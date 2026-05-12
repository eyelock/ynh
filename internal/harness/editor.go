package harness

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eyelock/ynh/internal/migration"
	"github.com/eyelock/ynh/internal/namespace"
	"github.com/eyelock/ynh/internal/plugin"
)

// loadManifest runs the migration chain and loads the manifest from the new path.
func loadManifest(dir string) (*plugin.HarnessJSON, error) {
	if _, err := migration.FormatChain().Run(dir); err != nil {
		return nil, fmt.Errorf("migrating harness manifest: %w", err)
	}
	return plugin.LoadPluginJSON(dir)
}

// ResolveEditTarget resolves a harness reference to the directory where
// `ynh include/delegate` edits land. Refs are classified lexically by
// namespace.Classify:
//   - RefPath ("./", "../", "/", "~/", drive-letter): filesystem path
//   - RefID (slash-bearing canonical id): installed harness, looked up by id
//   - RefInvalid (bare names, "name@org/repo"): rejected with hint
//
// For installed (RefID) harnesses the resolution mirrors LoadByID: a
// pointer file (pointer-form install) wins over a tree under
// HarnessesDir (tree-form). This guarantees reads and writes land at the
// same place — no divergence between the install copy and the source
// tree, which was the read/write split that motivated schema 3 (see
// topology.go).
func ResolveEditTarget(ref string) (dir string, installed bool, err error) {
	switch namespace.Classify(ref) {
	case namespace.RefPath:
		abs, absErr := filepath.Abs(ref)
		if absErr != nil {
			return "", false, fmt.Errorf("resolving path %q: %w", ref, absErr)
		}
		if DetectFormat(abs) == "" {
			return "", false, fmt.Errorf("no harness manifest found at %q", abs)
		}
		return abs, false, nil
	case namespace.RefID:
		if ptr, _ := LoadPointerByID(ref); ptr != nil {
			return localLoadDir(&ptr.InstalledJSON), true, nil
		}
		// Schema-1 fallback: fork created before schema-2 pointer writer landed.
		if name, ok := strings.CutPrefix(ref, "local/"); ok {
			if ptr, _ := LoadPointer(name); ptr != nil {
				return localLoadDir(&ptr.InstalledJSON), true, nil
			}
		}
		treeDir := InstalledDirByID(ref)
		if DetectFormat(treeDir) == "plugin" {
			return treeDir, true, nil
		}
		return "", false, fmt.Errorf("harness %q: %w", ref, ErrNotFound)
	default:
		return "", false, BadRefError(ref)
	}
}

// AddOptions controls ynh include add behaviour.
type AddOptions struct {
	Path    string
	Pick    []string
	Ref     string
	Replace bool
}

// RemoveOptions controls ynh include remove behaviour.
type RemoveOptions struct {
	Path string // optional disambiguation when URL matches multiple includes
}

// UpdateOptions controls ynh include update behaviour.
type UpdateOptions struct {
	FromPath string  // disambiguation key (required if URL matches multiple includes)
	NewPath  *string // non-nil → update the path field to this value
	Pick     []string
	SetPick  bool    // true when --pick was explicitly given (even for empty)
	Ref      *string // non-nil → update the ref field to this value
}

// AddInclude adds a new include to a harness directory.
// If the URL+path already exists and Replace is false, it returns an error.
// Network operations (pre-fetch, pick validation) are the caller's responsibility.
func AddInclude(dir, url string, opts AddOptions) error {
	hj, err := loadManifest(dir)
	if err != nil {
		return err
	}

	idx := findInclude(hj.Includes, url, opts.Path)
	if idx >= 0 {
		if !opts.Replace {
			msg := fmt.Sprintf("include %q already present in harness %q", url, hj.Name)
			if opts.Path != "" {
				msg = fmt.Sprintf("include %q (path: %q) already present in harness %q", url, opts.Path, hj.Name)
			}
			return fmt.Errorf("%s.\nUse 'ynh include update' to change its options, or pass --replace to overwrite", msg)
		}
		hj.Includes[idx] = plugin.IncludeMeta{Git: url, Ref: opts.Ref, Path: opts.Path, Pick: opts.Pick}
	} else {
		hj.Includes = append(hj.Includes, plugin.IncludeMeta{Git: url, Ref: opts.Ref, Path: opts.Path, Pick: opts.Pick})
	}

	return plugin.SavePluginJSON(dir, hj)
}

// RemoveInclude removes an include identified by URL and optional path.
// If the URL matches multiple includes and no path is given, an error is
// returned listing the paths that would disambiguate.
func RemoveInclude(dir, url string, opts RemoveOptions) error {
	hj, err := loadManifest(dir)
	if err != nil {
		return err
	}

	matches := findAllIncludes(hj.Includes, url)
	if len(matches) == 0 {
		return fmt.Errorf("include %q not found in harness %q", url, hj.Name)
	}

	if opts.Path == "" && len(matches) > 1 {
		return ambiguousIncludeError(url, hj.Includes, matches)
	}

	keep := hj.Includes[:0]
	for _, inc := range hj.Includes {
		if inc.Git == url && (opts.Path == "" || inc.Path == opts.Path) {
			continue
		}
		keep = append(keep, inc)
	}

	if len(keep) == len(hj.Includes) {
		return fmt.Errorf("include %q (path: %q) not found in harness %q", url, opts.Path, hj.Name)
	}

	hj.Includes = keep
	return plugin.SavePluginJSON(dir, hj)
}

// UpdateInclude updates fields on an existing include.
// The include is looked up by URL; if multiple includes share the same URL,
// FromPath must be set to disambiguate. Supplied fields (NewPath, Pick, Ref)
// are updated; omitted fields are left unchanged.
// Network operations (pre-fetch, pick validation) are the caller's responsibility.
func UpdateInclude(dir, url string, opts UpdateOptions) error {
	hj, err := loadManifest(dir)
	if err != nil {
		return err
	}

	targetIdx, findErr := findUpdateTarget(hj.Includes, url, opts.FromPath, hj.Name)
	if findErr != nil {
		return findErr
	}

	inc := &hj.Includes[targetIdx]
	if opts.NewPath != nil {
		inc.Path = *opts.NewPath
	}
	if opts.SetPick {
		inc.Pick = opts.Pick
	}
	if opts.Ref != nil {
		inc.Ref = *opts.Ref
	}

	return plugin.SavePluginJSON(dir, hj)
}

// FindUpdateTarget loads the harness from dir and returns the include that
// would be updated by UpdateOptions. Used by callers that need to inspect the
// final state before writing (e.g. to pre-fetch the right ref/path).
func FindUpdateTarget(dir, url string, opts UpdateOptions) (plugin.IncludeMeta, error) {
	hj, err := loadManifest(dir)
	if err != nil {
		return plugin.IncludeMeta{}, err
	}

	targetIdx, findErr := findUpdateTarget(hj.Includes, url, opts.FromPath, hj.Name)
	if findErr != nil {
		return plugin.IncludeMeta{}, findErr
	}

	inc := hj.Includes[targetIdx]
	if opts.NewPath != nil {
		inc.Path = *opts.NewPath
	}
	if opts.SetPick {
		inc.Pick = opts.Pick
	}
	if opts.Ref != nil {
		inc.Ref = *opts.Ref
	}
	return inc, nil
}

// ValidatePicks checks that every pick names an artifact that exists in basePath.
//
// Each pick must be in canonical type/name form. Directory-style types carry
// no extension (skills/<name>); flat-file types carry .md:
//
//   - Skills:   "skills/<name>"        (a skill directory containing SKILL.md)
//   - Agents:   "agents/<name>.md"
//   - Rules:    "rules/<name>.md"
//   - Commands: "commands/<name>.md"
//
// This is the same format the assembler expects (see assembler.CopyPicked), so
// a pick that passes validation here is guaranteed to work end-to-end. The
// type/name prefix also disambiguates a skill named "foo" from an agent named
// "foo.md" — both can coexist and be picked independently.
//
// On unknown picks the error lists the canonical candidates. When a user
// submitted a bare basename that resolves to exactly one canonical entry,
// the error leads with a "Did you mean <X>?" suggestion; when it resolves
// to several (e.g. a skill and an agent share a basename) all candidates
// are suggested; when it matches none the full available list is shown.
func ValidatePicks(basePath string, picks []string) error {
	artifacts, err := ScanArtifactsDir(basePath)
	if err != nil {
		return fmt.Errorf("scanning artifacts for pick validation: %w", err)
	}

	known := make(map[string]bool)
	// byBasename indexes canonical entries by their basename (sans .md) so we
	// can offer "did you mean ...?" suggestions when a user submits a bare name.
	byBasename := make(map[string][]string)

	for _, n := range artifacts.Skills {
		canon := "skills/" + n
		known[canon] = true
		byBasename[n] = append(byBasename[n], canon)
	}
	for _, n := range artifacts.Agents {
		canon := "agents/" + n + ".md"
		known[canon] = true
		byBasename[n] = append(byBasename[n], canon)
	}
	for _, n := range artifacts.Rules {
		canon := "rules/" + n + ".md"
		known[canon] = true
		byBasename[n] = append(byBasename[n], canon)
	}
	for _, n := range artifacts.Commands {
		canon := "commands/" + n + ".md"
		known[canon] = true
		byBasename[n] = append(byBasename[n], canon)
	}

	var unknown []string
	for _, pick := range picks {
		if !known[pick] {
			unknown = append(unknown, pick)
		}
	}

	if len(unknown) == 0 {
		return nil
	}

	// Build suggestion lines. For each unknown pick try basename → canonical
	// resolution; if that fails, fall back to dropping any type/ prefix and
	// retrying (so "skill/foo" with a typo'd type still hints).
	var suggestions []string
	for _, bad := range unknown {
		candidates := suggestionsFor(bad, byBasename)
		if len(candidates) > 0 {
			suggestions = append(suggestions, fmt.Sprintf("  %s → did you mean %s?", bad, strings.Join(candidates, " or ")))
		}
	}

	available := make([]string, 0, len(known))
	for n := range known {
		available = append(available, n)
	}

	msg := fmt.Sprintf("unknown pick name(s): %s", strings.Join(unknown, ", "))
	if len(suggestions) > 0 {
		msg += "\n" + strings.Join(suggestions, "\n")
	}
	msg += fmt.Sprintf("\nAvailable: %s\n(Picks must use type/name form: skills/<name>, agents/<name>.md, rules/<name>.md, commands/<name>.md)", formatAvailable(available))
	return fmt.Errorf("%s", msg)
}

// suggestionsFor returns canonical entries that share the unknown pick's
// basename. Strips any leading "type/" and trailing ".md" before lookup
// so "foo", "skill/foo", and "foo.md" all resolve to the same candidates.
func suggestionsFor(bad string, byBasename map[string][]string) []string {
	bare := bad
	if idx := strings.Index(bare, "/"); idx >= 0 {
		bare = bare[idx+1:]
	}
	bare = strings.TrimSuffix(bare, ".md")
	return byBasename[bare]
}

func findInclude(includes []plugin.IncludeMeta, url, path string) int {
	for i, inc := range includes {
		if inc.Git == url && inc.Path == path {
			return i
		}
	}
	return -1
}

func findAllIncludes(includes []plugin.IncludeMeta, url string) []int {
	var out []int
	for i, inc := range includes {
		if inc.Git == url {
			out = append(out, i)
		}
	}
	return out
}

func findUpdateTarget(includes []plugin.IncludeMeta, url, fromPath, harnessName string) (int, error) {
	matches := findAllIncludes(includes, url)
	if len(matches) == 0 {
		return -1, fmt.Errorf("include %q not found in harness %q", url, harnessName)
	}

	if len(matches) == 1 {
		return matches[0], nil
	}

	if fromPath == "" {
		return -1, ambiguousIncludeError(url, includes, matches)
	}

	for _, i := range matches {
		if includes[i].Path == fromPath {
			return i, nil
		}
	}
	return -1, fmt.Errorf("include %q with --from-path %q not found in harness %q", url, fromPath, harnessName)
}

func ambiguousIncludeError(url string, includes []plugin.IncludeMeta, indices []int) error {
	var paths []string
	for _, i := range indices {
		p := includes[i].Path
		if p == "" {
			p = "(root)"
		}
		paths = append(paths, "  "+p)
	}
	return fmt.Errorf(
		"include %q matches multiple entries:\n%s\nUse --path (remove) or --from-path (update) to disambiguate",
		url, strings.Join(paths, "\n"),
	)
}

// DelegateAddOptions controls ynh delegate add behaviour.
type DelegateAddOptions struct {
	Ref  string
	Path string
}

// DelegateRemoveOptions controls ynh delegate remove behaviour.
type DelegateRemoveOptions struct {
	Path string // optional disambiguation when URL matches multiple delegates
}

// DelegateUpdateOptions controls ynh delegate update behaviour.
type DelegateUpdateOptions struct {
	FromPath string  // disambiguation key (required if URL matches multiple delegates)
	NewPath  *string // non-nil → update the path field to this value
	Ref      *string // non-nil → update the ref field to this value
}

// AddDelegate adds a new delegate to a harness directory.
// Returns an error if the same URL+path combination already exists.
func AddDelegate(dir, url string, opts DelegateAddOptions) error {
	hj, err := loadManifest(dir)
	if err != nil {
		return err
	}

	if idx := findDelegate(hj.DelegatesTo, url, opts.Path); idx >= 0 {
		msg := fmt.Sprintf("delegate %q already present in harness %q", url, hj.Name)
		if opts.Path != "" {
			msg = fmt.Sprintf("delegate %q (path: %q) already present in harness %q", url, opts.Path, hj.Name)
		}
		return fmt.Errorf("%s.\nUse 'ynh delegate update' to change its options", msg)
	}

	hj.DelegatesTo = append(hj.DelegatesTo, plugin.DelegateMeta{Git: url, Ref: opts.Ref, Path: opts.Path})
	return plugin.SavePluginJSON(dir, hj)
}

// RemoveDelegate removes a delegate identified by URL and optional path.
// If the URL matches multiple delegates and no path is given, an error is
// returned listing the paths that would disambiguate.
func RemoveDelegate(dir, url string, opts DelegateRemoveOptions) error {
	hj, err := loadManifest(dir)
	if err != nil {
		return err
	}

	matches := findAllDelegates(hj.DelegatesTo, url)
	if len(matches) == 0 {
		return fmt.Errorf("delegate %q not found in harness %q", url, hj.Name)
	}

	if opts.Path == "" && len(matches) > 1 {
		return ambiguousDelegateError(url, hj.DelegatesTo, matches)
	}

	keep := hj.DelegatesTo[:0]
	for _, del := range hj.DelegatesTo {
		if del.Git == url && (opts.Path == "" || del.Path == opts.Path) {
			continue
		}
		keep = append(keep, del)
	}

	if len(keep) == len(hj.DelegatesTo) {
		return fmt.Errorf("delegate %q (path: %q) not found in harness %q", url, opts.Path, hj.Name)
	}

	hj.DelegatesTo = keep
	return plugin.SavePluginJSON(dir, hj)
}

// UpdateDelegate updates fields on an existing delegate.
// The delegate is looked up by URL; if multiple delegates share the same URL,
// FromPath must be set to disambiguate. Supplied fields (NewPath, Ref) are
// updated; omitted fields are left unchanged.
func UpdateDelegate(dir, url string, opts DelegateUpdateOptions) error {
	hj, err := loadManifest(dir)
	if err != nil {
		return err
	}

	targetIdx, findErr := findDelegateUpdateTarget(hj.DelegatesTo, url, opts.FromPath, hj.Name)
	if findErr != nil {
		return findErr
	}

	del := &hj.DelegatesTo[targetIdx]
	if opts.NewPath != nil {
		del.Path = *opts.NewPath
	}
	if opts.Ref != nil {
		del.Ref = *opts.Ref
	}

	return plugin.SavePluginJSON(dir, hj)
}

// FindDelegateUpdateTarget loads the harness from dir and returns the delegate
// that would be updated by DelegateUpdateOptions. Used by callers that need to
// inspect the final state before writing (e.g. to pre-fetch the right ref).
func FindDelegateUpdateTarget(dir, url string, opts DelegateUpdateOptions) (plugin.DelegateMeta, error) {
	hj, err := loadManifest(dir)
	if err != nil {
		return plugin.DelegateMeta{}, err
	}

	targetIdx, findErr := findDelegateUpdateTarget(hj.DelegatesTo, url, opts.FromPath, hj.Name)
	if findErr != nil {
		return plugin.DelegateMeta{}, findErr
	}

	del := hj.DelegatesTo[targetIdx]
	if opts.NewPath != nil {
		del.Path = *opts.NewPath
	}
	if opts.Ref != nil {
		del.Ref = *opts.Ref
	}
	return del, nil
}

func findDelegate(delegates []plugin.DelegateMeta, url, path string) int {
	for i, del := range delegates {
		if del.Git == url && del.Path == path {
			return i
		}
	}
	return -1
}

func findAllDelegates(delegates []plugin.DelegateMeta, url string) []int {
	var out []int
	for i, del := range delegates {
		if del.Git == url {
			out = append(out, i)
		}
	}
	return out
}

func findDelegateUpdateTarget(delegates []plugin.DelegateMeta, url, fromPath, harnessName string) (int, error) {
	matches := findAllDelegates(delegates, url)
	if len(matches) == 0 {
		return -1, fmt.Errorf("delegate %q not found in harness %q", url, harnessName)
	}

	if len(matches) == 1 {
		return matches[0], nil
	}

	if fromPath == "" {
		return -1, ambiguousDelegateError(url, delegates, matches)
	}

	for _, i := range matches {
		if delegates[i].Path == fromPath {
			return i, nil
		}
	}
	return -1, fmt.Errorf("delegate %q with --from-path %q not found in harness %q", url, fromPath, harnessName)
}

func ambiguousDelegateError(url string, delegates []plugin.DelegateMeta, indices []int) error {
	var paths []string
	for _, i := range indices {
		p := delegates[i].Path
		if p == "" {
			p = "(root)"
		}
		paths = append(paths, "  "+p)
	}
	return fmt.Errorf(
		"delegate %q matches multiple entries:\n%s\nUse --path (remove) or --from-path (update) to disambiguate",
		url, strings.Join(paths, "\n"),
	)
}

// ---- focus ----------------------------------------------------------

// FocusAddOptions controls ynh focus add behaviour.
type FocusAddOptions struct {
	Profile string
}

// FocusUpdateOptions controls ynh focus update behaviour.
// Non-nil pointer fields are applied; nil leaves the field unchanged.
// A non-nil empty Profile clears the focus's profile binding.
type FocusUpdateOptions struct {
	Prompt  *string
	Profile *string
}

// AddFocus adds a new focus to a harness directory.
func AddFocus(dir, name, prompt string, opts FocusAddOptions) error {
	if name == "" {
		return fmt.Errorf("focus name must not be empty")
	}
	if prompt == "" {
		return fmt.Errorf("focus prompt must not be empty")
	}
	hj, err := loadManifest(dir)
	if err != nil {
		return err
	}
	if _, exists := hj.Focuses[name]; exists {
		return fmt.Errorf("focus %q already exists in harness %q.\nUse 'ynh focus update' to change it", name, hj.Name)
	}
	if opts.Profile != "" {
		if _, ok := hj.Profiles[opts.Profile]; !ok {
			return fmt.Errorf("focus %q references unknown profile %q", name, opts.Profile)
		}
	}
	if hj.Focuses == nil {
		hj.Focuses = make(map[string]plugin.Focus)
	}
	hj.Focuses[name] = plugin.Focus{Profile: opts.Profile, Prompt: prompt}
	return plugin.SavePluginJSON(dir, hj)
}

// RemoveFocus removes a focus by name.
func RemoveFocus(dir, name string) error {
	hj, err := loadManifest(dir)
	if err != nil {
		return err
	}
	if _, ok := hj.Focuses[name]; !ok {
		return fmt.Errorf("focus %q not found in harness %q", name, hj.Name)
	}
	delete(hj.Focuses, name)
	if len(hj.Focuses) == 0 {
		hj.Focuses = nil
	}
	return plugin.SavePluginJSON(dir, hj)
}

// UpdateFocus mutates fields on an existing focus.
func UpdateFocus(dir, name string, opts FocusUpdateOptions) error {
	hj, err := loadManifest(dir)
	if err != nil {
		return err
	}
	f, ok := hj.Focuses[name]
	if !ok {
		return fmt.Errorf("focus %q not found in harness %q", name, hj.Name)
	}
	if opts.Prompt != nil {
		if *opts.Prompt == "" {
			return fmt.Errorf("focus prompt must not be empty")
		}
		f.Prompt = *opts.Prompt
	}
	if opts.Profile != nil {
		if *opts.Profile != "" {
			if _, pok := hj.Profiles[*opts.Profile]; !pok {
				return fmt.Errorf("focus %q references unknown profile %q", name, *opts.Profile)
			}
		}
		f.Profile = *opts.Profile
	}
	hj.Focuses[name] = f
	return plugin.SavePluginJSON(dir, hj)
}

// ---- profile --------------------------------------------------------

// AddProfile creates a new empty profile.
func AddProfile(dir, name string) error {
	if name == "" {
		return fmt.Errorf("profile name must not be empty")
	}
	hj, err := loadManifest(dir)
	if err != nil {
		return err
	}
	if _, exists := hj.Profiles[name]; exists {
		return fmt.Errorf("profile %q already exists in harness %q", name, hj.Name)
	}
	if hj.Profiles == nil {
		hj.Profiles = make(map[string]plugin.Profile)
	}
	hj.Profiles[name] = plugin.Profile{}
	return plugin.SavePluginJSON(dir, hj)
}

// RemoveProfile removes a profile by name. Fails if any focus still
// references the profile, so the caller can fix those first.
func RemoveProfile(dir, name string) error {
	hj, err := loadManifest(dir)
	if err != nil {
		return err
	}
	if _, ok := hj.Profiles[name]; !ok {
		return fmt.Errorf("profile %q not found in harness %q", name, hj.Name)
	}
	var blockers []string
	for fname, f := range hj.Focuses {
		if f.Profile == name {
			blockers = append(blockers, fname)
		}
	}
	if len(blockers) > 0 {
		sort.Strings(blockers)
		return fmt.Errorf("profile %q is referenced by focus(es): %s.\nUpdate or remove those focuses first", name, strings.Join(blockers, ", "))
	}
	delete(hj.Profiles, name)
	if len(hj.Profiles) == 0 {
		hj.Profiles = nil
	}
	return plugin.SavePluginJSON(dir, hj)
}

// ---- profile hooks --------------------------------------------------

// ProfileHookAddOptions controls ynh profile hook add behaviour.
type ProfileHookAddOptions struct {
	Matcher string
}

// AddProfileHook appends a hook entry to an event in a profile.
func AddProfileHook(dir, profileName, event, command string, opts ProfileHookAddOptions) error {
	if !plugin.ValidHookEvents[event] {
		return fmt.Errorf("unknown hook event %q (valid: before_tool, after_tool, before_prompt, on_stop)", event)
	}
	if command == "" {
		return fmt.Errorf("hook command must not be empty")
	}
	hj, err := loadManifest(dir)
	if err != nil {
		return err
	}
	p, ok := hj.Profiles[profileName]
	if !ok {
		return fmt.Errorf("profile %q not found in harness %q", profileName, hj.Name)
	}
	if p.Hooks == nil {
		p.Hooks = make(map[string][]plugin.HookEntry)
	}
	p.Hooks[event] = append(p.Hooks[event], plugin.HookEntry{Matcher: opts.Matcher, Command: command})
	hj.Profiles[profileName] = p
	return plugin.SavePluginJSON(dir, hj)
}

// RemoveProfileHook removes a single hook entry by zero-based index. When
// the last entry for an event is removed, the event key is also deleted.
func RemoveProfileHook(dir, profileName, event string, index int) error {
	hj, err := loadManifest(dir)
	if err != nil {
		return err
	}
	p, ok := hj.Profiles[profileName]
	if !ok {
		return fmt.Errorf("profile %q not found in harness %q", profileName, hj.Name)
	}
	entries, ok := p.Hooks[event]
	if !ok {
		return fmt.Errorf("profile %q has no hooks for event %q", profileName, event)
	}
	if index < 0 || index >= len(entries) {
		return fmt.Errorf("hook index %d out of range [0, %d) for event %q in profile %q", index, len(entries), event, profileName)
	}
	entries = append(entries[:index], entries[index+1:]...)
	if len(entries) == 0 {
		delete(p.Hooks, event)
	} else {
		p.Hooks[event] = entries
	}
	if len(p.Hooks) == 0 {
		p.Hooks = nil
	}
	hj.Profiles[profileName] = p
	return plugin.SavePluginJSON(dir, hj)
}

// ---- profile mcp ----------------------------------------------------

// ProfileMCPAddOptions controls ynh profile mcp add behaviour.
type ProfileMCPAddOptions struct {
	Command string
	Args    []string
	Env     map[string]string
	URL     string
	Headers map[string]string
	Null    bool // explicitly remove an inherited server (null JSON entry)
}

// ProfileMCPUpdateOptions controls ynh profile mcp update behaviour.
// Pointer fields and Set* booleans disambiguate "not provided" from "empty".
type ProfileMCPUpdateOptions struct {
	Command    *string
	Args       []string
	SetArgs    bool
	Env        map[string]string
	SetEnv     bool
	URL        *string
	Headers    map[string]string
	SetHeaders bool
}

// AddProfileMCP adds a new MCP server entry to a profile.
// Null=true writes a JSON null (suppresses an inherited server during merge).
func AddProfileMCP(dir, profileName, serverName string, opts ProfileMCPAddOptions) error {
	if serverName == "" {
		return fmt.Errorf("mcp server name must not be empty")
	}
	if opts.Null {
		if opts.Command != "" || opts.URL != "" {
			return fmt.Errorf("--null cannot be combined with --command or --url")
		}
	} else {
		if opts.Command == "" && opts.URL == "" {
			return fmt.Errorf("mcp add requires --command, --url, or --null")
		}
		if opts.Command != "" && opts.URL != "" {
			return fmt.Errorf("mcp add cannot have both --command and --url")
		}
	}
	hj, err := loadManifest(dir)
	if err != nil {
		return err
	}
	p, ok := hj.Profiles[profileName]
	if !ok {
		return fmt.Errorf("profile %q not found in harness %q", profileName, hj.Name)
	}
	if _, exists := p.MCPServers[serverName]; exists {
		return fmt.Errorf("mcp server %q already present in profile %q.\nUse 'ynh profile mcp update' to change it", serverName, profileName)
	}
	if p.MCPServers == nil {
		p.MCPServers = make(map[string]*plugin.MCPServer)
	}
	if opts.Null {
		p.MCPServers[serverName] = nil
	} else {
		p.MCPServers[serverName] = &plugin.MCPServer{
			Command: opts.Command,
			Args:    opts.Args,
			Env:     opts.Env,
			URL:     opts.URL,
			Headers: opts.Headers,
		}
	}
	hj.Profiles[profileName] = p
	return plugin.SavePluginJSON(dir, hj)
}

// RemoveProfileMCP deletes the named MCP server entry from a profile.
// This is different from a null entry — remove drops the key entirely,
// so the inherited server (if any) is no longer suppressed.
func RemoveProfileMCP(dir, profileName, serverName string) error {
	hj, err := loadManifest(dir)
	if err != nil {
		return err
	}
	p, ok := hj.Profiles[profileName]
	if !ok {
		return fmt.Errorf("profile %q not found in harness %q", profileName, hj.Name)
	}
	if _, exists := p.MCPServers[serverName]; !exists {
		return fmt.Errorf("mcp server %q not found in profile %q", serverName, profileName)
	}
	delete(p.MCPServers, serverName)
	if len(p.MCPServers) == 0 {
		p.MCPServers = nil
	}
	hj.Profiles[profileName] = p
	return plugin.SavePluginJSON(dir, hj)
}

// UpdateProfileMCP mutates fields on an existing MCP server in a profile.
// Null entries cannot be updated — remove and re-add instead.
func UpdateProfileMCP(dir, profileName, serverName string, opts ProfileMCPUpdateOptions) error {
	if opts.Command == nil && !opts.SetArgs && !opts.SetEnv && opts.URL == nil && !opts.SetHeaders {
		return fmt.Errorf("ynh profile mcp update: at least one flag must be specified")
	}
	hj, err := loadManifest(dir)
	if err != nil {
		return err
	}
	p, ok := hj.Profiles[profileName]
	if !ok {
		return fmt.Errorf("profile %q not found in harness %q", profileName, hj.Name)
	}
	srv, exists := p.MCPServers[serverName]
	if !exists {
		return fmt.Errorf("mcp server %q not found in profile %q", serverName, profileName)
	}
	if srv == nil {
		return fmt.Errorf("mcp server %q in profile %q is a null entry; remove it and add a new one to replace", serverName, profileName)
	}
	if opts.Command != nil {
		srv.Command = *opts.Command
	}
	if opts.SetArgs {
		srv.Args = opts.Args
	}
	if opts.SetEnv {
		srv.Env = opts.Env
	}
	if opts.URL != nil {
		srv.URL = *opts.URL
	}
	if opts.SetHeaders {
		srv.Headers = opts.Headers
	}
	if srv.Command == "" && srv.URL == "" {
		return fmt.Errorf("mcp server %q must have either command or url after update", serverName)
	}
	if srv.Command != "" && srv.URL != "" {
		return fmt.Errorf("mcp server %q cannot have both command and url after update", serverName)
	}
	p.MCPServers[serverName] = srv
	hj.Profiles[profileName] = p
	return plugin.SavePluginJSON(dir, hj)
}

// ---- profile includes -----------------------------------------------

// AddProfileInclude mirrors AddInclude but targets a profile's includes slice.
func AddProfileInclude(dir, profileName, url string, opts AddOptions) error {
	hj, err := loadManifest(dir)
	if err != nil {
		return err
	}
	p, ok := hj.Profiles[profileName]
	if !ok {
		return fmt.Errorf("profile %q not found in harness %q", profileName, hj.Name)
	}
	idx := findInclude(p.Includes, url, opts.Path)
	if idx >= 0 {
		if !opts.Replace {
			msg := fmt.Sprintf("include %q already present in profile %q", url, profileName)
			if opts.Path != "" {
				msg = fmt.Sprintf("include %q (path: %q) already present in profile %q", url, opts.Path, profileName)
			}
			return fmt.Errorf("%s.\nUse 'ynh profile include update' to change its options, or pass --replace to overwrite", msg)
		}
		p.Includes[idx] = plugin.IncludeMeta{Git: url, Ref: opts.Ref, Path: opts.Path, Pick: opts.Pick}
	} else {
		p.Includes = append(p.Includes, plugin.IncludeMeta{Git: url, Ref: opts.Ref, Path: opts.Path, Pick: opts.Pick})
	}
	hj.Profiles[profileName] = p
	return plugin.SavePluginJSON(dir, hj)
}

// RemoveProfileInclude mirrors RemoveInclude but targets a profile's includes.
func RemoveProfileInclude(dir, profileName, url string, opts RemoveOptions) error {
	hj, err := loadManifest(dir)
	if err != nil {
		return err
	}
	p, ok := hj.Profiles[profileName]
	if !ok {
		return fmt.Errorf("profile %q not found in harness %q", profileName, hj.Name)
	}
	matches := findAllIncludes(p.Includes, url)
	if len(matches) == 0 {
		return fmt.Errorf("include %q not found in profile %q", url, profileName)
	}
	if opts.Path == "" && len(matches) > 1 {
		return ambiguousIncludeError(url, p.Includes, matches)
	}
	keep := p.Includes[:0]
	for _, inc := range p.Includes {
		if inc.Git == url && (opts.Path == "" || inc.Path == opts.Path) {
			continue
		}
		keep = append(keep, inc)
	}
	if len(keep) == len(p.Includes) {
		return fmt.Errorf("include %q (path: %q) not found in profile %q", url, opts.Path, profileName)
	}
	p.Includes = keep
	hj.Profiles[profileName] = p
	return plugin.SavePluginJSON(dir, hj)
}

// UpdateProfileInclude mirrors UpdateInclude but targets a profile's includes.
func UpdateProfileInclude(dir, profileName, url string, opts UpdateOptions) error {
	hj, err := loadManifest(dir)
	if err != nil {
		return err
	}
	p, ok := hj.Profiles[profileName]
	if !ok {
		return fmt.Errorf("profile %q not found in harness %q", profileName, hj.Name)
	}
	targetIdx, findErr := findUpdateTarget(p.Includes, url, opts.FromPath, profileName)
	if findErr != nil {
		return findErr
	}
	inc := &p.Includes[targetIdx]
	if opts.NewPath != nil {
		inc.Path = *opts.NewPath
	}
	if opts.SetPick {
		inc.Pick = opts.Pick
	}
	if opts.Ref != nil {
		inc.Ref = *opts.Ref
	}
	hj.Profiles[profileName] = p
	return plugin.SavePluginJSON(dir, hj)
}

// FindProfileIncludeUpdateTarget mirrors FindUpdateTarget for profile includes.
func FindProfileIncludeUpdateTarget(dir, profileName, url string, opts UpdateOptions) (plugin.IncludeMeta, error) {
	hj, err := loadManifest(dir)
	if err != nil {
		return plugin.IncludeMeta{}, err
	}
	p, ok := hj.Profiles[profileName]
	if !ok {
		return plugin.IncludeMeta{}, fmt.Errorf("profile %q not found in harness %q", profileName, hj.Name)
	}
	targetIdx, findErr := findUpdateTarget(p.Includes, url, opts.FromPath, profileName)
	if findErr != nil {
		return plugin.IncludeMeta{}, findErr
	}
	inc := p.Includes[targetIdx]
	if opts.NewPath != nil {
		inc.Path = *opts.NewPath
	}
	if opts.SetPick {
		inc.Pick = opts.Pick
	}
	if opts.Ref != nil {
		inc.Ref = *opts.Ref
	}
	return inc, nil
}

func formatAvailable(names []string) string {
	if len(names) == 0 {
		return "(none)"
	}
	out := make([]string, 0, len(names))
	for i, n := range names {
		if i >= 10 {
			out = append(out, fmt.Sprintf("… (%d more)", len(names)-10))
			break
		}
		out = append(out, n)
	}
	return strings.Join(out, ", ")
}
