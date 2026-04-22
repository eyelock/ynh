package harness

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/migration"
	"github.com/eyelock/ynh/internal/plugin"
)

// loadManifest runs the migration chain and loads the manifest from the new path.
func loadManifest(dir string) (*plugin.HarnessJSON, error) {
	if _, err := migration.FormatChain().Run(dir); err != nil {
		return nil, fmt.Errorf("migrating harness manifest: %w", err)
	}
	return plugin.LoadPluginJSON(dir)
}

// ResolveEditTarget resolves a harness reference to a directory.
// If ref contains a path separator or starts with '.', it is treated as a
// filesystem path and must contain a harness manifest. Otherwise it is looked up
// as an installed harness name. Returns the directory and whether the harness
// is installed (i.e. lives under the ynh harnesses directory).
func ResolveEditTarget(ref string) (dir string, installed bool, err error) {
	if strings.ContainsRune(ref, filepath.Separator) || strings.HasPrefix(ref, ".") {
		abs, absErr := filepath.Abs(ref)
		if absErr != nil {
			return "", false, fmt.Errorf("resolving path %q: %w", ref, absErr)
		}
		if DetectFormat(abs) == "" {
			return "", false, fmt.Errorf("no harness manifest found at %q", abs)
		}
		return abs, false, nil
	}

	installDir := InstalledDir(ref)
	if DetectFormat(installDir) == "plugin" {
		return installDir, true, nil
	}
	return "", false, fmt.Errorf("harness %q: %w", ref, ErrNotFound)
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
