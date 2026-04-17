package harness

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/plugin"
)

// ResolveEditTarget resolves a harness reference to a directory.
// If ref contains a path separator or starts with '.', it is treated as a
// filesystem path and must contain .harness.json. Otherwise it is looked up
// as an installed harness name. Returns the directory and whether the harness
// is installed (i.e. lives under the ynh harnesses directory).
func ResolveEditTarget(ref string) (dir string, installed bool, err error) {
	if strings.ContainsRune(ref, filepath.Separator) || strings.HasPrefix(ref, ".") {
		abs, absErr := filepath.Abs(ref)
		if absErr != nil {
			return "", false, fmt.Errorf("resolving path %q: %w", ref, absErr)
		}
		if !plugin.IsHarnessDir(abs) {
			return "", false, fmt.Errorf("no .harness.json found at %q", abs)
		}
		return abs, false, nil
	}

	installDir := InstalledDir(ref)
	if DetectFormat(installDir) == "harness" {
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
	hj, err := plugin.LoadHarnessJSON(dir)
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

	return plugin.SaveHarnessJSON(dir, hj)
}

// RemoveInclude removes an include identified by URL and optional path.
// If the URL matches multiple includes and no path is given, an error is
// returned listing the paths that would disambiguate.
func RemoveInclude(dir, url string, opts RemoveOptions) error {
	hj, err := plugin.LoadHarnessJSON(dir)
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
	return plugin.SaveHarnessJSON(dir, hj)
}

// UpdateInclude updates fields on an existing include.
// The include is looked up by URL; if multiple includes share the same URL,
// FromPath must be set to disambiguate. Supplied fields (NewPath, Pick, Ref)
// are updated; omitted fields are left unchanged.
// Network operations (pre-fetch, pick validation) are the caller's responsibility.
func UpdateInclude(dir, url string, opts UpdateOptions) error {
	hj, err := plugin.LoadHarnessJSON(dir)
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

	return plugin.SaveHarnessJSON(dir, hj)
}

// FindUpdateTarget loads the harness from dir and returns the include that
// would be updated by UpdateOptions. Used by callers that need to inspect the
// final state before writing (e.g. to pre-fetch the right ref/path).
func FindUpdateTarget(dir, url string, opts UpdateOptions) (plugin.IncludeMeta, error) {
	hj, err := plugin.LoadHarnessJSON(dir)
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

// ValidatePicks checks that every named pick exists as an artifact in basePath.
// Returns an error listing any unrecognised names.
func ValidatePicks(basePath string, picks []string) error {
	artifacts, err := ScanArtifactsDir(basePath)
	if err != nil {
		return fmt.Errorf("scanning artifacts for pick validation: %w", err)
	}

	known := make(map[string]bool, len(artifacts.Skills)+len(artifacts.Agents)+len(artifacts.Rules)+len(artifacts.Commands))
	for _, n := range artifacts.Skills {
		known[n] = true
	}
	for _, n := range artifacts.Agents {
		known[n] = true
	}
	for _, n := range artifacts.Rules {
		known[n] = true
	}
	for _, n := range artifacts.Commands {
		known[n] = true
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

	available := make([]string, 0, len(known))
	for n := range known {
		available = append(available, n)
	}

	return fmt.Errorf(
		"unknown pick name(s): %s\nAvailable: %s",
		strings.Join(unknown, ", "),
		formatAvailable(available),
	)
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
