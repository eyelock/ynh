package resolver

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/harness"
)

// gitToplevel returns the root of the git working tree containing dir, with
// symlinks resolved, or "" when dir is not inside one.
func gitToplevel(dir string) string {
	out, err := exec.Command(gitBin, "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	top := strings.TrimSpace(string(out))
	if resolved, err := filepath.EvalSymlinks(top); err == nil {
		return resolved
	}
	return top
}

// IsOwnRepoSource reports whether a git source is the harness's own repo: a
// relative path ("." or "./sub") that GitSourceURL makes absolute and that
// lies in the same git working tree as harnessDir. Such a source is read live
// anyway, so refreshing its cached clone adds no trust. Everything else
// (another repo on disk, an absolute path, file://, a remote URL, or a harness
// directory that is not a git repo, such as a copy installed from a remote) is
// not, and must not be fetched on every run. Apart from the git toplevel
// lookup it is pure.
func IsOwnRepoSource(gs harness.GitSource, harnessDir string) bool {
	if gs.Git == "" || !strings.HasPrefix(gs.Git, ".") || harnessDir == "" {
		return false
	}
	own := gitToplevel(harnessDir)
	if own == "" {
		return false
	}
	return gitToplevel(GitSourceURL(gs.Git, harnessDir)) == own
}

// missingPathError is returned when a source's path is missing from a clone.
type missingPathError struct {
	path, git string
}

func (e *missingPathError) Error() string {
	return fmt.Sprintf("path %q not found in %s", e.path, e.git)
}

// ResolveSourceForRun resolves a delegate's or include's git source for a
// run. The harness's own repo is fetched on every call (cheap, offline, and
// it picks up new commits); any other source is served from the cache so a
// run stays off the network. A path missing from a cached clone that was not just
// fetched gets a hint that the cache may be out of date.
func ResolveSourceForRun(gs harness.GitSource, harnessDir string) (string, *RepoResult, error) {
	return resolveForRunWith(gs, harnessDir, CacheOnlyRepo, EnsureRepo)
}

func resolveForRunWith(gs harness.GitSource, harnessDir string, cached, fetch repoFunc) (string, *RepoResult, error) {
	use := cached
	fresh := false
	if IsOwnRepoSource(gs, harnessDir) {
		use = fetch
		fresh = true
	}
	basePath, res, err := resolveGitSourceWith(gs, harnessDir, func(url, ref string) (RepoResult, error) {
		r, err := use(url, ref)
		if r.Cloned {
			fresh = true
		}
		return r, err
	})
	var missing *missingPathError
	if errors.As(err, &missing) && !fresh {
		return "", nil, fmt.Errorf("%w (the cached copy may be out of date; run `ynh update <harness>` to refresh it)", err)
	}
	return basePath, res, err
}
