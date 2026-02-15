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

// ResolveGitSource clones/updates a GitSource and returns the resolved base path,
// scoped to the optional sub-path within the repo.
func ResolveGitSource(gs persona.GitSource) (string, error) {
	repoPath, err := EnsureRepo(gs.Git, gs.Ref)
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", gs.Git, err)
	}

	basePath := repoPath
	if gs.Path != "" {
		basePath = filepath.Join(repoPath, gs.Path)
		if _, err := os.Stat(basePath); os.IsNotExist(err) {
			return "", fmt.Errorf("path %q not found in %s", gs.Path, gs.Git)
		}
	}

	return basePath, nil
}

// Resolve fetches all includes for a persona and returns resolved content.
func Resolve(p *persona.Persona) ([]ResolvedContent, error) {
	var results []ResolvedContent

	for _, inc := range p.Includes {
		basePath, err := ResolveGitSource(inc.GitSource)
		if err != nil {
			return nil, err
		}

		results = append(results, ResolvedContent{
			BasePath: basePath,
			Paths:    inc.Pick,
		})
	}

	return results, nil
}

// EnsureRepo clones or updates a Git repo in the cache directory.
// Returns the path to the cached repo on disk.
func EnsureRepo(gitURL string, ref string) (string, error) {
	cacheDir := config.CacheDir()
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", err
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
			return "", fmt.Errorf("git clone %s: %w", fullURL, err)
		}
	} else {
		// Update existing clone
		if ref != "" {
			if err := gitCmd("-C", repoDir, "fetch", "--depth", "1", "origin", ref); err != nil {
				return "", fmt.Errorf("git fetch %s ref %s: %w", fullURL, ref, err)
			}
			if err := gitCmd("-C", repoDir, "checkout", "FETCH_HEAD"); err != nil {
				return "", fmt.Errorf("git checkout FETCH_HEAD in %s: %w", repoDir, err)
			}
		} else {
			if err := gitCmd("-C", repoDir, "fetch", "--depth", "1", "origin"); err != nil {
				return "", fmt.Errorf("git fetch %s: %w", fullURL, err)
			}
			if err := gitCmd("-C", repoDir, "reset", "--hard", "origin/HEAD"); err != nil {
				return "", fmt.Errorf("git reset in %s: %w", repoDir, err)
			}
		}
	}

	return repoDir, nil
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
// Including the ref ensures that the same repo at different versions gets separate cache entries.
func repoDirName(url string, ref string) string {
	key := url + "\x00" + ref
	h := sha256.Sum256([]byte(key))
	// Use the last path segment + short hash for readability
	parts := strings.Split(strings.TrimSuffix(url, ".git"), "/")
	name := parts[len(parts)-1]
	return fmt.Sprintf("%s-%x", name, h[:4])
}
