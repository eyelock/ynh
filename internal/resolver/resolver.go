package resolver

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
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
		basePath = filepath.Join(result.Path, gs.Path)
		if _, err := os.Stat(basePath); os.IsNotExist(err) {
			return "", nil, fmt.Errorf("path %q not found in %s", gs.Path, gs.Git)
		}
	}

	return basePath, &result, nil
}

// resolveWith fetches all includes using the given repo function.
func resolveWith(p *harness.Harness, cfg *config.Config, fetch repoFunc) ([]ResolveResult, error) {
	var results []ResolveResult

	for _, inc := range p.Includes {
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
	Path    string // path to the cached repo on disk
	Cloned  bool   // true if freshly cloned (not previously cached)
	Changed bool   // true if HEAD moved during update
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
		// Clone
		args := []string{"clone", "--depth", "1"}
		if ref != "" {
			args = append(args, "--branch", ref)
		}
		args = append(args, fullURL, repoDir)
		if err := gitCmd(args...); err != nil {
			return RepoResult{}, fmt.Errorf("git clone %s: %w", fullURL, err)
		}
		return RepoResult{Path: repoDir, Cloned: true, Changed: true}, nil
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
		errStr := fetchErr.Error()

		// Stale lock file from an interrupted fetch — remove it and retry once.
		// This is the most common cause of "first call fails, retry succeeds".
		if strings.Contains(errStr, ".lock") {
			_ = os.Remove(filepath.Join(repoDir, ".git", "shallow.lock"))
			_ = os.Remove(filepath.Join(repoDir, ".git", "index.lock"))
			if ref != "" {
				fetchErr = gitCmd("-C", repoDir, "fetch", "--depth", "1", "origin", ref)
			} else {
				fetchErr = gitCmd("-C", repoDir, "fetch", "--depth", "1", "origin")
			}
			errStr = ""
			if fetchErr != nil {
				errStr = fetchErr.Error()
			}
		}

		if fetchErr != nil {
			// Shallow clone corruption — delete the stale cache and re-clone clean.
			if strings.Contains(errStr, "shallow file has changed") {
				if err := os.RemoveAll(repoDir); err != nil {
					return RepoResult{}, fmt.Errorf("removing corrupt registry cache: %w", err)
				}
				args := []string{"clone", "--depth", "1"}
				if ref != "" {
					args = append(args, "--branch", ref)
				}
				args = append(args, fullURL, repoDir)
				if err := gitCmd(args...); err != nil {
					return RepoResult{}, fmt.Errorf("git clone %s: %w", fullURL, err)
				}
				return RepoResult{Path: repoDir, Cloned: true, Changed: true}, nil
			}
			if ref != "" {
				return RepoResult{}, fmt.Errorf("git fetch %s ref %s: %w", fullURL, ref, fetchErr)
			}
			return RepoResult{}, fmt.Errorf("git fetch %s: %w", fullURL, fetchErr)
		}
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
	return RepoResult{Path: repoDir, Changed: before != after}, nil
}

// CacheOnlyRepo returns a cached repo without hitting the network.
// If the cache entry exists, it is returned as-is. If not, it falls back to
// EnsureRepo (which clones from the network) and prints a warning to stderr.
func CacheOnlyRepo(gitURL string, ref string) (RepoResult, error) {
	cacheDir := config.CacheDir()
	repoDir := filepath.Join(cacheDir, repoDirName(gitURL, ref))

	if _, err := os.Stat(filepath.Join(repoDir, ".git")); err == nil {
		return RepoResult{Path: repoDir}, nil
	}

	// Cache miss — fall back to network fetch.
	// Caller can detect this via RepoResult.Cloned.
	return EnsureRepo(gitURL, ref)
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

// NormalizeGitURL ensures a full Git URL from shorthand.
// Local paths (starting with / or .) are returned as-is.
// Shorthand like "github.com/user/repo" becomes "git@github.com:user/repo.git" (SSH).
// SSH URLs (git@...) and full HTTPS URLs are passed through unchanged.
func NormalizeGitURL(url string) string {
	if strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "git@") {
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
