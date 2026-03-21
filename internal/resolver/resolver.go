package resolver

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/persona"
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

// ResolveGitSource clones/updates a GitSource and returns the resolved base path,
// scoped to the optional sub-path within the repo.
func ResolveGitSource(gs persona.GitSource) (string, *RepoResult, error) {
	result, err := EnsureRepo(gs.Git, gs.Ref)
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

// Resolve fetches all includes for a persona and returns resolved content
// with resolution metadata (cloned vs cached).
// If cfg is non-nil, remote sources are checked against the allowed sources list.
func Resolve(p *persona.Persona, cfg *config.Config) ([]ResolveResult, error) {
	var results []ResolveResult

	for _, inc := range p.Includes {
		if cfg != nil {
			if err := cfg.CheckRemoteSource(inc.Git); err != nil {
				return nil, fmt.Errorf("include %q: %w", inc.Git, err)
			}
		}

		basePath, repoResult, err := ResolveGitSource(inc.GitSource)
		if err != nil {
			return nil, err
		}

		results = append(results, ResolveResult{
			Content: ResolvedContent{
				BasePath: basePath,
				Paths:    inc.Pick,
			},
			Source: shortGitURL(inc.Git),
			Path:   inc.Path,
			Cloned: repoResult.Cloned,
			Cached: !repoResult.Cloned,
		})
	}

	return results, nil
}

// shortGitURL abbreviates a git URL for display.
// "github.com/eyelock/assistants" -> "eyelock/assistants"
func shortGitURL(url string) string {
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

// CloneToTemp clones a Git repo to a temporary directory (shallow, depth 1).
// The caller is responsible for removing the returned directory when done.
func CloneToTemp(gitURL string) (string, error) {
	fullURL := NormalizeGitURL(gitURL)

	tmpDir, err := os.MkdirTemp("", "ynh-clone-*")
	if err != nil {
		return "", err
	}

	cmd := exec.Command("git", "clone", "--depth", "1", fullURL, tmpDir)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", fmt.Errorf("git clone %s: %w", fullURL, err)
	}

	return tmpDir, nil
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

	if ref != "" {
		if err := gitCmd("-C", repoDir, "fetch", "--depth", "1", "origin", ref); err != nil {
			return RepoResult{}, fmt.Errorf("git fetch %s ref %s: %w", fullURL, ref, err)
		}
		if err := gitCmd("-C", repoDir, "checkout", "FETCH_HEAD"); err != nil {
			return RepoResult{}, fmt.Errorf("git checkout FETCH_HEAD in %s: %w", repoDir, err)
		}
	} else {
		if err := gitCmd("-C", repoDir, "fetch", "--depth", "1", "origin"); err != nil {
			return RepoResult{}, fmt.Errorf("git fetch %s: %w", fullURL, err)
		}
		if err := gitCmd("-C", repoDir, "reset", "--hard", "origin/HEAD"); err != nil {
			return RepoResult{}, fmt.Errorf("git reset in %s: %w", repoDir, err)
		}
	}

	after := gitHead(repoDir)
	return RepoResult{Path: repoDir, Changed: before != after}, nil
}

// gitHead returns the short HEAD SHA for a repo, or empty string on error.
func gitHead(repoDir string) string {
	cmd := exec.Command("git", "-C", repoDir, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gitCmd runs a git command, suppressing output unless it fails.
func gitCmd(args ...string) error {
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w\n%s", err, out)
	}
	return nil
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
