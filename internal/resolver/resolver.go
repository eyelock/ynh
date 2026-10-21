package resolver

import (
	"crypto/sha256"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/pathutil"
	"github.com/eyelock/ynh/internal/plugin"
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

	// Harness is the harness the include resolved to, when its directory
	// holds a manifest, and nil for a plain artifact package. Its MCP servers
	// reach a run through harness.ComposeMCPServers.
	Harness *harness.Harness
	// Chain is where the include came from, for provenance: Source plus
	// "//Path", and for an include reached through another harness the chain
	// of includes that led to it, "eyelock/a > eyelock/b".
	Chain string
	// Namespace is what the harness's focuses and profiles are selected
	// under: the "as" alias of the include, or the harness's own name. Empty
	// for a plain artifact package.
	Namespace string
	// HooksActive is whether the harness's hooks may run: every include on
	// the way to it, from the root's, says "hooks": true. Without it the
	// hooks are declared but not carried.
	HooksActive bool
}

// repoFunc is a function that fetches or looks up a Git repo.
type repoFunc func(gitURL, ref string) (RepoResult, error)

// GitSourceURL returns the URL a git source is fetched from. A relative local
// path ("./inc", "../inc") belongs to the harness that names it, so it is
// joined to harnessDir, the directory the harness was loaded from, and made
// absolute. The same harness then fetches the same repo wherever ynh runs,
// the fetch agrees with the allow-list (config.CheckSource joins the same
// directory), and the absolute path is the cache key, so two harnesses that
// each name "./inc" never share a cache entry. With no harnessDir the path
// is taken from the working directory. Any other source is returned as is.
func GitSourceURL(gitURL, harnessDir string) string {
	if !strings.HasPrefix(gitURL, ".") {
		return gitURL
	}
	joined := filepath.Join(harnessDir, gitURL)
	abs, err := filepath.Abs(joined)
	if err != nil {
		return joined
	}
	return abs
}

// resolveGitSourceWith resolves a GitSource using the given repo function. A
// relative source is resolved against harnessDir (see GitSourceURL).
func resolveGitSourceWith(gs harness.GitSource, harnessDir string, fetch repoFunc) (string, *RepoResult, error) {
	result, err := fetch(GitSourceURL(gs.Git, harnessDir), gs.Ref)
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

// CheckLocalInclude checks a "local" include against the allow-list. A
// relative one is confined to the harness directory (resolveLocalSource
// refuses an escape), so it is part of the harness and not a source. An
// absolute one can point anywhere and is read on every run, so it is
// governed like any other source. A nil cfg allows everything.
func CheckLocalInclude(cfg *config.Config, inc harness.Include, harnessDir string) error {
	if cfg == nil || !filepath.IsAbs(inc.Local) {
		return nil
	}
	if err := cfg.CheckSource(inc.Local, harnessDir); err != nil {
		return fmt.Errorf("include %q: %w", inc.Local, err)
	}
	return nil
}

// chainLink is one step on the include chain being resolved, kept to catch a
// harness that includes itself, directly or through others.
type chainLink struct {
	id    string // identity, see includeIdentity
	label string // display name for the error message
}

// includeResolver carries what resolving one harness's includes shares across
// the recursion into included harnesses.
type includeResolver struct {
	cfg   *config.Config
	fetch repoFunc
	// done holds the identity of every harness include already resolved, so a
	// harness reached by two routes (a diamond) contributes once.
	done map[string]bool
	// selected holds the namespaces a --profile or --focus named: the value
	// is the profile applied to that included harness, empty when the
	// namespace is only used for its focus.
	selected map[string]string
	// seen records every harness include met, by namespace, with the
	// identity and display chain of each distinct harness, so a namespace
	// two harnesses share is caught when something uses it.
	seen map[string][]seenHarness
	// profileErr holds a failed profile lookup per namespace. It is raised
	// by check, after ambiguity, since a namespace two harnesses share fails
	// as ambiguous whichever of them lacked the profile.
	profileErr map[string]error
}

// seenHarness is one harness met under a namespace.
type seenHarness struct {
	id      string
	chain   string
	harness *harness.Harness
}

// resolveWith fetches all includes using the given repo function.
//
// An include whose directory holds a harness manifest is loaded, and its own
// includes are resolved in turn unless the include picks artifacts. The
// result lists dependencies before the harness that includes them, and p's
// own includes in order, so that later content keeps overriding earlier.
func resolveWith(p *harness.Harness, cfg *config.Config, fetch repoFunc) ([]ResolveResult, error) {
	results, _, err := resolveSelected(p, cfg, fetch, harness.Selection{})
	return results, err
}

// resolveSelected is resolveWith with the profiles and focus of sel applied
// to the included harnesses they name. A namespaced focus is returned: its
// profile, if it has one, is applied to its harness, which is only known
// once the graph has been walked, so the walk runs again with the profile.
func resolveSelected(p *harness.Harness, cfg *config.Config, fetch repoFunc, sel harness.Selection) ([]ResolveResult, *plugin.Focus, error) {
	selected := make(map[string]string, len(sel.Included)+1)
	for ns, profile := range sel.Included {
		selected[ns] = profile
	}
	if sel.FocusNS != "" {
		if _, ok := selected[sel.FocusNS]; !ok {
			selected[sel.FocusNS] = ""
		}
	}

	results, r, err := walkIncludes(p, cfg, fetch, selected)
	if err != nil {
		return nil, nil, err
	}
	if sel.FocusNS == "" {
		return results, nil, nil
	}

	inner := r.seen[sel.FocusNS][0].harness
	focus, ok := inner.Focuses[sel.FocusName]
	if !ok {
		return nil, nil, fmt.Errorf("focus %q not defined in included harness %q (%s)", sel.FocusName, sel.FocusNS, availableNames(inner.Focuses, "focuses"))
	}
	if focus.Profile != "" && selected[sel.FocusNS] != focus.Profile {
		selected[sel.FocusNS] = focus.Profile
		if results, _, err = walkIncludes(p, cfg, fetch, selected); err != nil {
			return nil, nil, err
		}
	}
	return results, &focus, nil
}

func walkIncludes(p *harness.Harness, cfg *config.Config, fetch repoFunc, selected map[string]string) ([]ResolveResult, *includeResolver, error) {
	r := &includeResolver{
		cfg: cfg, fetch: fetch, done: map[string]bool{},
		selected: selected, seen: map[string][]seenHarness{}, profileErr: map[string]error{},
	}
	chain := []chainLink{{id: dirIdentity(p.Dir), label: "root"}}
	results, err := r.resolve(p, chain, "", true)
	if err != nil {
		return nil, nil, err
	}
	if err := r.check(); err != nil {
		return nil, nil, err
	}
	return results, r, nil
}

// availableNames words the names a map declares for an error message.
func availableNames[V any](m map[string]V, what string) string {
	if len(m) == 0 {
		return "the harness declares no " + what
	}
	return fmt.Sprintf("available: %v", slices.Sorted(maps.Keys(m)))
}

// register records a harness met under namespace ns. A harness reached twice
// is the same harness and is recorded once.
func (r *includeResolver) register(ns, id, chain string, h *harness.Harness) {
	if slices.ContainsFunc(r.seen[ns], func(s seenHarness) bool { return s.id == id }) {
		return
	}
	r.seen[ns] = append(r.seen[ns], seenHarness{id: id, chain: chain, harness: h})
}

// applyProfile resolves the profile selected for namespace ns on an included
// harness. An undefined profile is held in profileErr, not returned: see
// check.
func (r *includeResolver) applyProfile(ns string, inner *harness.Harness) *harness.Harness {
	profile := r.selected[ns]
	if profile == "" {
		return inner
	}
	if _, ok := inner.Profiles[profile]; !ok {
		if _, held := r.profileErr[ns]; !held {
			r.profileErr[ns] = fmt.Errorf("profile %q not defined in included harness %q (%s)", profile, ns, availableNames(inner.Profiles, "profiles"))
		}
		return inner
	}
	resolved, err := harness.ResolveProfile(inner, profile)
	if err != nil {
		if _, held := r.profileErr[ns]; !held {
			r.profileErr[ns] = fmt.Errorf("profile %q of included harness %q: %w", profile, ns, err)
		}
		return inner
	}
	return resolved
}

// check reports what a namespaced --profile or --focus got wrong once the
// whole graph is known: a namespace no harness answers to, one that more
// than one does, or a profile its harness does not define. A namespace that
// nothing selected is never an error, however many harnesses share it.
func (r *includeResolver) check() error {
	for _, ns := range slices.Sorted(maps.Keys(r.selected)) {
		switch found := r.seen[ns]; {
		case len(found) == 0:
			if len(r.seen) == 0 {
				return fmt.Errorf("no included harness has namespace %q (the harness includes no other harnesses)", ns)
			}
			return fmt.Errorf("no included harness has namespace %q (available: %s)", ns, strings.Join(slices.Sorted(maps.Keys(r.seen)), ", "))
		case len(found) > 1:
			chains := make([]string, len(found))
			for i, f := range found {
				chains[i] = f.chain
			}
			return fmt.Errorf("namespace %q is ambiguous: %s; give one include an \"as\" alias", ns, strings.Join(chains, " and "))
		}
		if err := r.profileErr[ns]; err != nil {
			return err
		}
	}
	return nil
}

// withoutMCPServers applies an included harness's removals to the harnesses
// it includes: a null in its mcp_servers drops the server whichever of its
// dependencies declared it, as it does for the root.
func withoutMCPServers(deps []ResolveResult, removals []string) {
	if len(removals) == 0 {
		return
	}
	for i := range deps {
		h := deps[i].Harness
		if h == nil {
			continue
		}
		view := *h
		view.MCPServers = make(map[string]plugin.MCPServer, len(h.MCPServers))
		for name, s := range h.MCPServers {
			if !slices.Contains(removals, name) {
				view.MCPServers[name] = s
			}
		}
		deps[i].Harness = &view
	}
}

// resolve resolves p's includes. via is the display chain of the harness p was
// reached through, empty for the root. consent is whether every include on the
// way to p consented to hooks, true for the root itself.
func (r *includeResolver) resolve(p *harness.Harness, chain []chainLink, via string, consent bool) ([]ResolveResult, error) {
	var results []ResolveResult

	for _, inc := range p.Includes {
		res, id, err := r.resolveOne(p, inc)
		if err != nil {
			return nil, err
		}
		res.Chain = res.Source
		if res.Path != "" {
			res.Chain += "//" + res.Path
		}
		label := res.Chain
		if via != "" {
			res.Chain = via + " > " + res.Chain
		}

		if !harness.IsHarnessDir(res.Content.BasePath) {
			results = append(results, res)
			continue
		}

		inner, err := harness.LoadDir(res.Content.BasePath)
		if err != nil {
			return nil, fmt.Errorf("loading included harness %s: %w", res.Chain, err)
		}

		ns := inc.As
		if ns == "" {
			ns = inner.Name
		}
		res.Namespace = ns
		hooksConsent := consent && inc.Hooks
		res.HooksActive = hooksConsent
		r.register(ns, id, res.Chain, inner)
		inner = r.applyProfile(ns, inner)
		res.Harness = inner

		if len(inc.Pick) > 0 {
			// A picked include contributes its picked artifacts and its MCP
			// servers; its own includes are not followed.
			if key := id + pickKey(inc.Pick); !r.done[key] {
				r.done[key] = true
				results = append(results, res)
			}
			continue
		}

		for _, link := range chain {
			if link.id == id {
				return nil, fmt.Errorf("include cycle: %s -> %s", chainLabels(chain), link.label)
			}
		}
		if r.done[id] {
			continue
		}
		deps, err := r.resolve(inner, append(chain[:len(chain):len(chain)], chainLink{id: id, label: label}), res.Chain, hooksConsent)
		if err != nil {
			return nil, err
		}
		r.done[id] = true
		withoutMCPServers(deps, inner.MCPRemovals)
		results = append(results, deps...)
		results = append(results, res)
	}

	return results, nil
}

func chainLabels(chain []chainLink) string {
	labels := make([]string, len(chain))
	for i, l := range chain {
		labels[i] = l.label
	}
	return strings.Join(labels, " -> ")
}

func pickKey(pick []string) string {
	sorted := slices.Sorted(slices.Values(pick))
	return "|" + strings.Join(sorted, ",")
}

// dirIdentity is the identity of a local directory: its absolute path with
// symlinks resolved where the path exists.
func dirIdentity(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = filepath.Clean(dir)
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	return abs
}

// resolveOne resolves a single include of p to a ResolveResult, applying the
// allow-list, and returns the include's identity for cycle detection.
func (r *includeResolver) resolveOne(p *harness.Harness, inc harness.Include) (ResolveResult, string, error) {
	if inc.IsLocal() {
		if err := CheckLocalInclude(r.cfg, inc, p.Dir); err != nil {
			return ResolveResult{}, "", err
		}
		basePath, err := resolveLocalSource(inc.GitSource, p.Dir)
		if err != nil {
			return ResolveResult{}, "", err
		}
		return ResolveResult{
			Content: ResolvedContent{
				BasePath: basePath,
				Paths:    inc.Pick,
			},
			Source: inc.Local,
			Path:   inc.Path,
			Cloned: false,
			Cached: true,
		}, dirIdentity(basePath), nil
	}

	if r.cfg != nil {
		if err := r.cfg.CheckSource(inc.Git, p.Dir); err != nil {
			return ResolveResult{}, "", fmt.Errorf("include %q: %w", inc.Git, err)
		}
	}

	basePath, repoResult, err := resolveGitSourceWith(inc.GitSource, p.Dir, r.fetch)
	if err != nil {
		return ResolveResult{}, "", err
	}

	id := ShortGitURL(GitSourceURL(inc.Git, p.Dir)) + "@" + inc.Ref + "//" + inc.Path
	return ResolveResult{
		Content: ResolvedContent{
			BasePath: basePath,
			Paths:    inc.Pick,
		},
		Source: ShortGitURL(inc.Git),
		Path:   inc.Path,
		Cloned: repoResult.Cloned,
		Cached: !repoResult.Cloned,
	}, id, nil
}

// IncludedHarnesses lists the harnesses among resolved, in content order, for
// harness.ComposeMCPServers and harness.ComposeMCPServersForExport.
func IncludedHarnesses(resolved []ResolveResult) []harness.IncludedHarness {
	var out []harness.IncludedHarness
	for _, r := range resolved {
		if r.Harness != nil {
			out = append(out, harness.IncludedHarness{Harness: r.Harness, Source: r.Chain})
		}
	}
	return out
}

// ResolveGitSource clones/updates a GitSource and returns the resolved base path,
// scoped to the optional sub-path within the repo. harnessDir is the directory
// of the harness that names the source; a relative source resolves against it.
func ResolveGitSource(gs harness.GitSource, harnessDir string) (string, *RepoResult, error) {
	return resolveGitSourceWith(gs, harnessDir, EnsureRepo)
}

// Resolve fetches all includes for a harness and returns resolved content
// with resolution metadata (cloned vs cached).
// If cfg is non-nil, remote sources are checked against the allowed sources list.
func Resolve(p *harness.Harness, cfg *config.Config) ([]ResolveResult, error) {
	return resolveWith(p, cfg, EnsureRepo)
}

// ShortGitURL abbreviates a git URL for display: the host, scheme, user and
// port are dropped, along with a trailing ".git", so every spelling of one
// repo reads the same.
//
//	"github.com/eyelock/assistants"              -> "eyelock/assistants"
//	"https://github.com/eyelock/assistants.git"  -> "eyelock/assistants"
//	"git@github.com:eyelock/assistants.git"      -> "eyelock/assistants"
//	"ssh://git@github.com/eyelock/assistants"    -> "eyelock/assistants"
//	"file:///tmp/repos/inc"                      -> "/tmp/repos/inc"
//
// Local paths are kept as written.
func ShortGitURL(url string) string {
	if strings.HasPrefix(url, "/") || strings.HasPrefix(url, ".") {
		return url
	}
	if p, ok := strings.CutPrefix(url, "file://"); ok {
		return p
	}

	var repoPath string
	if _, rest, ok := strings.Cut(url, "://"); ok {
		// scheme://[user@]host[:port]/org/repo
		_, repoPath, ok = strings.Cut(rest, "/")
		if !ok {
			return rest
		}
	} else if colon, slash := strings.Index(url, ":"), strings.Index(url, "/"); colon >= 0 && (slash < 0 || colon < slash) {
		// scp-like ssh: [user@]host:org/repo
		repoPath = url[colon+1:]
	} else if slash >= 0 {
		// shorthand: host/org/repo
		repoPath = url[slash+1:]
	} else {
		return url
	}

	repoPath = strings.Trim(repoPath, "/")
	repoPath = strings.TrimSuffix(repoPath, ".git")
	if repoPath == "" {
		return url
	}
	return repoPath
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

	// Serialize concurrent ynh processes against this same cache entry —
	// without this, racing RemoveAll/clone calls produced "directory not
	// empty" and "could not lock config file" errors when e.g. a TUI fired
	// multiple ynh invocations in parallel.
	var result RepoResult
	lockErr := withRepoLock(repoDir+".lock", func() error {
		r, err := ensureRepoLocked(repoDir, fullURL, ref)
		result = r
		return err
	})
	return result, lockErr
}

func ensureRepoLocked(repoDir, fullURL, ref string) (RepoResult, error) {
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
func ResolveGitSourceFromCache(gs harness.GitSource, harnessDir string) (string, *RepoResult, error) {
	return resolveGitSourceWith(gs, harnessDir, CacheOnlyRepo)
}

// ResolveFromCache is like Resolve but uses CacheOnlyRepo to avoid network
// access when the cache is warm. Falls back to a network fetch on cache miss.
func ResolveFromCache(p *harness.Harness, cfg *config.Config) ([]ResolveResult, error) {
	return resolveWith(p, cfg, CacheOnlyRepo)
}

// ResolveSelected is Resolve with the selections of sel applied to the
// included harnesses: each namespaced profile to the harness it names, and a
// namespaced focus's profile likewise. The focus itself comes back as well,
// nil when sel names none.
func ResolveSelected(p *harness.Harness, cfg *config.Config, sel harness.Selection) ([]ResolveResult, *plugin.Focus, error) {
	return resolveSelected(p, cfg, EnsureRepo, sel)
}

// ResolveSelectedFromCache is ResolveSelected over the cache, as
// ResolveFromCache is to Resolve.
func ResolveSelectedFromCache(p *harness.Harness, cfg *config.Config, sel harness.Selection) ([]ResolveResult, *plugin.Focus, error) {
	return resolveSelected(p, cfg, CacheOnlyRepo, sel)
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
	// A ref beginning with "-" is read by git as an option, not a ref:
	// "--upload-pack=..." would run a command of the caller's choosing.
	// NormalizeGitURL already forces a scheme on the URL, closing the same
	// hole for that argument; the ref had no equivalent guard.
	if strings.HasPrefix(ref, "-") {
		return "", fmt.Errorf("git ref %q may not begin with %q", ref, "-")
	}
	args := []string{"ls-remote", "--exit-code"}
	// Everything after this is positional, whatever it looks like.
	args = append(args, "--end-of-options", fullURL)
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

	// git ls-remote does suffix pattern matching: passing "main" also matches
	// branches like "daisy/caffeinate/main". Parse all lines and return the
	// SHA whose refname is an exact match on the intended canonical name.
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		sha, refName := fields[0], fields[1]
		if ref == "" {
			// HEAD probe: accept "HEAD" or any refs/heads/<branch>.
			if refName == "HEAD" || strings.HasPrefix(refName, "refs/heads/") {
				return sha, nil
			}
			continue
		}
		// Named ref: accept an already-qualified ref as-is; otherwise require
		// an exact refs/heads/<ref> or refs/tags/<ref> match so partial-name
		// branches (e.g. foo/main) don't shadow the intended target.
		if strings.HasPrefix(ref, "refs/") {
			if refName == ref {
				return sha, nil
			}
		} else if refName == "refs/heads/"+ref || refName == "refs/tags/"+ref {
			return sha, nil
		}
	}
	return "", fmt.Errorf("git ls-remote %s: ref %q not found in remote", fullURL, ref)
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
