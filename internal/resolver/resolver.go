package resolver

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/pathutil"
)

// ResolvedContent represents files extracted from a Git source.
type ResolvedContent struct {
	// BasePath is the root of the cloned/cached repo.
	BasePath string
	// Paths are the specific files/dirs requested via pick.
	// If empty, the entire repo is included.
	Paths []string
}

// ResolveResult pairs a ResolvedContent with metadata about how it was resolved.
type ResolveResult struct {
	Content ResolvedContent
	Source  string // short display name (e.g., "eyelock/assistants")
	Path    string // subpath within repo, if any
	Cloned  bool   // true if freshly cloned (first time)
	Cached  bool   // true if already in cache (not first time)
}

// repoFunc is a function that fetches or looks up a Git repo.
type repoFunc func(gitURL, ref string) (RepoResult, error)

// resolveGitSourceWith resolves a GitSource using the given repo function.
func resolveGitSourceWith(gs harness.GitSource, fetch repoFunc) (string, *RepoResult, error) {
	result, err := fetch(gs.Git, gs.Ref)
	if err != nil {
		return "", nil, fmt.Errorf("resolving %s: %w", gs.Git, err)
	}

	basePath := result.Path
	if gs.Path != "" {
		if err := pathutil.CheckSubpath(gs.Path); err != nil {
			return "", nil, fmt.Errorf("include path: %w", err)
		}
		basePath = filepath.Join(result.Path, gs.Path)
		if _, err := os.Stat(basePath); os.IsNotExist(err) {
			return "", nil, fmt.Errorf("path %q not found in %s", gs.Path, gs.Git)
		}
	}

	return basePath, &result, nil
}

// resolveLocalSource resolves a local filesystem include against the harness
// root. Absolute paths are used as-is; relative paths are joined to harnessDir.
// Subpath (gs.Path) is applied on top if set.
func resolveLocalSource(gs harness.GitSource, harnessDir string) (string, error) {
	local := gs.Local
	if !filepath.IsAbs(local) {
		if err := pathutil.CheckSubpath(local); err != nil {
			return "", fmt.Errorf("local include: %w", err)
		}
		if harnessDir == "" {
			return "", fmt.Errorf("local include %q: harness directory not known, cannot resolve relative path", local)
		}
		local = filepath.Join(harnessDir, local)
	}
	if _, err := os.Stat(local); os.IsNotExist(err) {
		return "", fmt.Errorf("local include %q: path not found", gs.Local)
	}

	basePath := local
	if gs.Path != "" {
		if err := pathutil.CheckSubpath(gs.Path); err != nil {
			return "", fmt.Errorf("local include path: %w", err)
		}
		basePath = filepath.Join(local, gs.Path)
		if _, err := os.Stat(basePath); os.IsNotExist(err) {
			return "", fmt.Errorf("path %q not found in local source %q", gs.Path, gs.Local)
		}
	}
	return basePath, nil
}

// resolveWith fetches all includes using the given repo function.
func resolveWith(p *harness.Harness, cfg *config.Config, fetch repoFunc) ([]ResolveResult, error) {
	var results []ResolveResult

	for _, inc := range p.Includes {
		if inc.IsLocal() {
			basePath, err := resolveLocalSource(inc.GitSource, p.Dir)
			if err != nil {
				return nil, err
			}
			results = append(results, ResolveResult{
				Content: ResolvedContent{
					BasePath: basePath,
					Paths:    inc.Pick,
				},
				Source: inc.Local,
				Path:   inc.Path,
				Cloned: false,
				Cached: true,
			})
			continue
		}

		if cfg != nil {
			if err := cfg.CheckRemoteSource(inc.Git); err != nil {
				return nil, fmt.Errorf("include %q: %w", inc.Git, err)
			}
		}

		basePath, repoResult, err := resolveGitSourceWith(inc.GitSource, fetch)
		if err != nil {
			return nil, err
		}

		results = append(results, ResolveResult{
			Content: ResolvedContent{
				BasePath: basePath,
				Paths:    inc.Pick,
			},
			Source: ShortGitURL(inc.Git),
			Path:   inc.Path,
			Cloned: repoResult.Cloned,
			Cached: !repoResult.Cloned,
		})
	}

	return results, nil
}

// ResolveGitSource clones/updates a GitSource and returns the resolved base path,
// scoped to the optional sub-path within the repo.
func ResolveGitSource(gs harness.GitSource) (string, *RepoResult, error) {
	return resolveGitSourceWith(gs, EnsureRepo)
}

// Resolve fetches all includes for a harness and returns resolved content
// with resolution metadata (cloned vs cached).
// If cfg is non-nil, remote sources are checked against the allowed sources list.
func Resolve(p *harness.Harness, cfg *config.Config) ([]ResolveResult, error) {
	return resolveWith(p, cfg, EnsureRepo)
}

// ShortGitURL abbreviates a git URL for display.
// "github.com/eyelock/assistants" -> "eyelock/assistants"
func ShortGitURL(url string) string {
	// Local paths: keep as-is
	if strings.HasPrefix(url, "/") || strings.HasPrefix(url, ".") {
		return url
	}
	// Strip host prefix
	parts := strings.SplitN(url, "/", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return url
}

// RepoResult describes the outcome of EnsureRepo.
type RepoResult struct {
	Path string // path to the cached repo on disk
	SHA  string // resolved commit SHA at HEAD after fetch/checkout
	// ResolvedRef is the branch name actually tracked by this clone.
	// For non-empty input refs it equals the input ref. For empty input
	// refs it is read from the cache's origin/HEAD symref (the default
	// branch as of clone time, which never auto-updates with upstream
	// default-branch changes). Used by --check-updates to probe the same
	// ref that ynh update tracks, so the two stay consistent.
	ResolvedRef string
	Cloned      bool // true if freshly cloned (not previously cached)
	Changed     bool // true if HEAD moved during update
}

// EnsureRepo clones or updates a Git repo in the cache directory.
func EnsureRepo(gitURL string, ref string) (RepoResult, error) {
	cacheDir := config.CacheDir()
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return RepoResult{}, err
	}

	repoDir := filepath.Join(cacheDir, repoDirName(gitURL, ref))
	fullURL := NormalizeGitURL(gitURL)

	if _, err := os.Stat(filepath.Join(repoDir, ".git")); os.IsNotExist(err) {
		// No .git dir — remove any partial/pre-existing directory before cloning.
		if err := os.RemoveAll(repoDir); err != nil {
			return RepoResult{}, fmt.Errorf("removing incomplete cache dir: %w", err)
		}
		// SHA refs can't go through `clone --branch <sha>` — git rejects them.
		// Use init+fetch+checkout, which works against any server that allows
		// fetch-by-SHA (GitHub does, default uploadpack.allowReachableSHA1InWant).
		if isShaLike(ref) {
			if err := cloneAtSHA(repoDir, fullURL, ref); err != nil {
				return RepoResult{}, err
			}
			return RepoResult{Path: repoDir, SHA: gitHead(repoDir), Cloned: true, Changed: true}, nil
		}
		// Branch/tag: use clone --branch for the depth-1 shortcut.
		if err := shallowCloneWithRetry(fullURL, ref, repoDir); err != nil {
			return RepoResult{}, fmt.Errorf("git clone %s: %w", fullURL, err)
		}
		return RepoResult{Path: repoDir, SHA: gitHead(repoDir), ResolvedRef: effectiveRef(repoDir, ref), Cloned: true, Changed: true}, nil
	}

	// Update existing clone — capture HEAD before and after
	before := gitHead(repoDir)

	var fetchErr error
	if ref != "" {
		fetchErr = gitCmd("-C", repoDir, "fetch", "--depth", "1", "origin", ref)
	} else {
		fetchErr = gitCmd("-C", repoDir, "fetch", "--depth", "1", "origin")
	}

	if fetchErr != nil {
		// Stale lock file from an interrupted fetch — remove it and retry once
		// before falling through to the full re-clone path.
		if strings.Contains(fetchErr.Error(), ".lock") {
			_ = os.Remove(filepath.Join(repoDir, ".git", "shallow.lock"))
			_ = os.Remove(filepath.Join(repoDir, ".git", "index.lock"))
			if ref != "" {
				fetchErr = gitCmd("-C", repoDir, "fetch", "--depth", "1", "origin", ref)
			} else {
				fetchErr = gitCmd("-C", repoDir, "fetch", "--depth", "1", "origin")
			}
		}
	}

	if fetchErr != nil {
		// Any fetch failure on a shallow clone is unrecoverable: the shallow
		// graft history may be inconsistent with what the remote sends (stale
		// grafts, missing objects, changed history). Nuke the cache and
		// re-clone clean — the cache is disposable.
		if err := os.RemoveAll(repoDir); err != nil {
			return RepoResult{}, fmt.Errorf("removing stale registry cache: %w", err)
		}
		if err := shallowCloneWithRetry(fullURL, ref, repoDir); err != nil {
			return RepoResult{}, fmt.Errorf("git clone %s: %w", fullURL, err)
		}
		return RepoResult{Path: repoDir, SHA: gitHead(repoDir), ResolvedRef: effectiveRef(repoDir, ref), Cloned: true, Changed: true}, nil
	}

	if ref != "" {
		if err := gitCmd("-C", repoDir, "checkout", "FETCH_HEAD"); err != nil {
			return RepoResult{}, fmt.Errorf("git checkout FETCH_HEAD in %s: %w", repoDir, err)
		}
	} else {
		if err := gitCmd("-C", repoDir, "reset", "--hard", "origin/HEAD"); err != nil {
			return RepoResult{}, fmt.Errorf("git reset in %s: %w", repoDir, err)
		}
	}

	after := gitHead(repoDir)
	return RepoResult{Path: repoDir, SHA: after, ResolvedRef: effectiveRef(repoDir, ref), Changed: before != after}, nil
}

// CacheOnlyRepo returns a cached repo without hitting the network.
// If the cache entry exists, it is returned as-is. If not, it falls back to
// EnsureRepo (which clones from the network) and prints a warning to stderr.
func CacheOnlyRepo(gitURL string, ref string) (RepoResult, error) {
	if res, ok := LookupCache(gitURL, ref); ok {
		return res, nil
	}
	// Cache miss — fall back to network fetch.
	// Caller can detect this via RepoResult.Cloned.
	return EnsureRepo(gitURL, ref)
}

// LookupCache returns the cached repo state for (gitURL, ref) without hitting
// the network. ok=false if no cache entry exists. Used to backfill harness
// provenance for pre-migration installs whose installed.json predates SHA/ref
// recording — we can still recover the install ref from the cache's pinned
// origin/HEAD symref.
func LookupCache(gitURL string, ref string) (RepoResult, bool) {
	cacheDir := config.CacheDir()
	repoDir := filepath.Join(cacheDir, repoDirName(gitURL, ref))
	if _, err := os.Stat(filepath.Join(repoDir, ".git")); err != nil {
		return RepoResult{}, false
	}
	return RepoResult{Path: repoDir, SHA: gitHead(repoDir), ResolvedRef: effectiveRef(repoDir, ref)}, true
}

// ResolveGitSourceFromCache is like ResolveGitSource but uses CacheOnlyRepo
// to avoid network access when the cache is warm.
func ResolveGitSourceFromCache(gs harness.GitSource) (string, *RepoResult, error) {
	return resolveGitSourceWith(gs, CacheOnlyRepo)
}

// ResolveFromCache is like Resolve but uses CacheOnlyRepo to avoid network
// access when the cache is warm. Falls back to a network fetch on cache miss.
func ResolveFromCache(p *harness.Harness, cfg *config.Config) ([]ResolveResult, error) {
	return resolveWith(p, cfg, CacheOnlyRepo)
}

// gitHead returns the short HEAD SHA for a repo, or empty string on error.
// gitBin is the resolved path to the git binary. Resolved once at startup so
// that callers invoked from GUI apps (which have a minimal PATH) can still
// find git even when PATH doesn't include Homebrew or developer tool dirs.
var gitBin = resolveGitBin()

func resolveGitBin() string {
	if path, err := exec.LookPath("git"); err == nil {
		return path
	}
	// Common macOS locations not always on GUI app PATH
	for _, candidate := range []string{
		"/usr/bin/git",
		"/opt/homebrew/bin/git",
		"/usr/local/bin/git",
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "git" // last resort — let exec fail with a clear error
}

// resolvedBranchName returns the bare branch name that origin/HEAD points to
// in the local cache (e.g. "main" or "develop"). Empty if the symref is not
// set or doesn't point at a remote-tracking branch under origin/.
//
// We capture this so --check-updates probes the same ref that ynh update
// tracks. Git pins this symref at clone time and does not auto-update it
// when upstream changes its default branch — which is exactly the divergence
// that makes phantom drift possible.
func resolvedBranchName(repoDir string) string {
	cmd := exec.Command(gitBin, "-C", repoDir, "symbolic-ref", "refs/remotes/origin/HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	ref := strings.TrimSpace(string(out))
	const prefix = "refs/remotes/origin/"
	if !strings.HasPrefix(ref, prefix) {
		return ""
	}
	return strings.TrimPrefix(ref, prefix)
}

// effectiveRef returns the ref to record alongside this clone. If the caller
// supplied an explicit ref it is echoed; otherwise the cache's resolved
// branch name is returned (empty string if it cannot be determined).
func effectiveRef(repoDir, inputRef string) string {
	if inputRef != "" {
		return inputRef
	}
	return resolvedBranchName(repoDir)
}

func gitHead(repoDir string) string {
	cmd := exec.Command(gitBin, "-C", repoDir, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gitCmd runs a git command, suppressing output unless it fails.
// Replaceable in tests via gitCmdFunc.
var gitCmdFunc = func(args ...string) error {
	cmd := exec.Command(gitBin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w\n%s", err, out)
	}
	return nil
}

func gitCmd(args ...string) error { return gitCmdFunc(args...) }

// LsRemote queries the upstream SHA for ref on gitURL via `git ls-remote`,
// without cloning. Empty ref resolves to HEAD. Returns the resolved SHA or
// an error if the network probe fails or the ref is not present upstream.
//
// Replaceable in tests via LsRemoteFunc.
func LsRemote(gitURL, ref string) (string, error) {
	return LsRemoteFunc(gitURL, ref)
}

// LsRemoteFunc is the implementation behind LsRemote, broken out so tests
// can stub network behaviour without shelling out to git.
var LsRemoteFunc = func(gitURL, ref string) (string, error) {
	fullURL := NormalizeGitURL(gitURL)
	args := []string{"ls-remote", "--exit-code", fullURL}
	if ref != "" {
		args = append(args, ref)
	} else {
		args = append(args, "HEAD")
	}
	cmd := exec.Command(gitBin, args...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git ls-remote %s: %w", fullURL, err)
	}
	// First whitespace-separated token of the first line is the SHA.
	line := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", fmt.Errorf("git ls-remote %s: no output", fullURL)
	}
	return fields[0], nil
}

// PurgeCacheDirsForURL removes all cache directories associated with the given
// Git URL (all refs). It derives the org--repo name prefix from the URL and
// deletes every subdirectory of the cache that starts with that prefix.
func PurgeCacheDirsForURL(gitURL string) error {
	cacheDir := config.CacheDir()
	prefix := repoDirPrefix(gitURL)

	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading cache dir: %w", err)
	}

	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), prefix+"--") {
			if err := os.RemoveAll(filepath.Join(cacheDir, e.Name())); err != nil {
				return fmt.Errorf("removing cache dir %s: %w", e.Name(), err)
			}
		}
	}
	return nil
}

// repoDirPrefix returns the org--repo portion of the cache dir name (without the hash suffix).
func repoDirPrefix(url string) string {
	cleaned := strings.TrimSuffix(url, ".git")
	if strings.HasPrefix(cleaned, "git@") {
		if idx := strings.Index(cleaned, ":"); idx > 0 {
			cleaned = cleaned[idx+1:]
		}
	}
	cleaned = strings.TrimPrefix(cleaned, "https://")
	cleaned = strings.TrimPrefix(cleaned, "http://")
	parts := strings.Split(cleaned, "/")
	if len(parts) >= 2 {
		return fmt.Sprintf("%s--%s", parts[len(parts)-2], parts[len(parts)-1])
	}
	return parts[len(parts)-1]
}

// shaLike matches a 40-character hex commit SHA. Git refuses to take a
// SHA via `clone --branch`, so SHA pinning has to go through init+fetch.
var shaLike = regexp.MustCompile(`^[0-9a-f]{40}$`)

func isShaLike(ref string) bool {
	return shaLike.MatchString(ref)
}

// isTransientShallowErr reports whether err looks like a known transient
// git shallow-clone failure that a clean retry can resolve. Git can
// intermittently produce these on `clone --depth 1` when its own internal
// state races against the just-written shallow file or a stale lock from
// an interrupted prior run.
func isTransientShallowErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "shallow file has changed") ||
		strings.Contains(s, "shallow.lock") ||
		strings.Contains(s, "index.lock")
}

// shallowCloneWithRetry runs `git clone --depth 1 [--branch ref] url dir`,
// retrying once on transient shallow-clone errors. dir is removed between
// attempts so the retry sees a clean slate.
func shallowCloneWithRetry(url, ref, dir string) error {
	args := []string{"clone", "--depth", "1"}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	args = append(args, url, dir)

	err := gitCmd(args...)
	if err == nil || !isTransientShallowErr(err) {
		return err
	}
	if rmErr := os.RemoveAll(dir); rmErr != nil {
		return fmt.Errorf("removing partial clone before retry: %w", rmErr)
	}
	return gitCmd(args...)
}

// cloneAtSHA materialises a shallow checkout of url at sha into dir.
// dir must not yet exist.
func cloneAtSHA(dir, url, sha string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating cache dir: %w", err)
	}
	if err := gitCmd("-C", dir, "init", "--quiet"); err != nil {
		return fmt.Errorf("git init in %s: %w", dir, err)
	}
	if err := gitCmd("-C", dir, "remote", "add", "origin", url); err != nil {
		return fmt.Errorf("git remote add: %w", err)
	}
	if err := gitCmd("-C", dir, "fetch", "--depth", "1", "origin", sha); err != nil {
		return fmt.Errorf("git fetch %s %s: %w", url, sha, err)
	}
	if err := gitCmd("-C", dir, "checkout", "--quiet", sha); err != nil {
		return fmt.Errorf("git checkout %s: %w", sha, err)
	}
	return nil
}

// NormalizeGitURL ensures a full Git URL from shorthand.
// Local paths (starting with / or .) are returned as-is.
// Shorthand like "github.com/user/repo" becomes "git@github.com:user/repo.git" (SSH).
// SSH URLs (git@...), HTTPS URLs, and file:// URLs are passed through unchanged.
// (file:// is a valid git transport for local bare repos and lets the E2E suite
// exercise the cloner against controlled fixtures without going to the network.)
func NormalizeGitURL(url string) string {
	if strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "git@") || strings.HasPrefix(url, "file://") {
		return url
	}
	if strings.HasPrefix(url, "/") || strings.HasPrefix(url, ".") {
		return url
	}
	// Shorthand like "github.com/user/repo" -> SSH URL.
	// SSH works for both public and private repos and is the common dev setup.
	parts := strings.SplitN(url, "/", 2)
	if len(parts) == 2 {
		host := parts[0]
		path := strings.TrimSuffix(parts[1], ".git")
		return fmt.Sprintf("git@%s:%s.git", host, path)
	}
	// Fallback: treat as HTTPS
	return "https://" + url + ".git"
}

// repoDirName creates a deterministic cache directory name from a Git URL and ref.
// Format: org--repo--<hash> with double hyphens for parsibility.
// Including the ref ensures that the same repo at different versions gets separate cache entries.
func repoDirName(url string, ref string) string {
	key := url + "\x00" + ref
	h := sha256.Sum256([]byte(key))
	hash := fmt.Sprintf("%x", h[:4])

	// Strip .git suffix and extract path segments
	cleaned := strings.TrimSuffix(url, ".git")

	// Handle SSH URLs: git@host:org/repo
	if strings.HasPrefix(cleaned, "git@") {
		if idx := strings.Index(cleaned, ":"); idx > 0 {
			cleaned = cleaned[idx+1:]
		}
	}

	// Handle HTTPS URLs: https://host/org/repo
	cleaned = strings.TrimPrefix(cleaned, "https://")
	cleaned = strings.TrimPrefix(cleaned, "http://")

	parts := strings.Split(cleaned, "/")

	// Use last two segments as org--repo when available
	if len(parts) >= 2 {
		org := parts[len(parts)-2]
		repo := parts[len(parts)-1]
		return fmt.Sprintf("%s--%s--%s", org, repo, hash)
	}

	// Fallback: single segment
	return fmt.Sprintf("%s--%s", parts[len(parts)-1], hash)
}
