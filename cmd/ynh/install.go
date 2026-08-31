// `ynh install` and the launcher plumbing it writes.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/migration"
	"github.com/eyelock/ynh/internal/namespace"
	"github.com/eyelock/ynh/internal/pathutil"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/resolver"
	"github.com/eyelock/ynh/internal/sources"
)

func cmdInstall(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: ynh install <git-url|local-path> [--path <subdir>] [--ref <commit|tag|branch>]")
	}

	if err := config.EnsureDirs(); err != nil {
		return err
	}

	// Parse --path and --ref flags from args
	var pathFlag, refFlag string
	var remaining []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--path" && i+1 < len(args) {
			pathFlag = args[i+1]
			i++ // skip value
		} else if args[i] == "--ref" && i+1 < len(args) {
			refFlag = args[i+1]
			i++ // skip value
		} else {
			remaining = append(remaining, args[i])
		}
	}
	if len(remaining) < 1 {
		return fmt.Errorf("usage: ynh install <git-url|local-path> [--path <subdir>] [--ref <commit|tag|branch>]")
	}

	source := remaining[0]
	originalSource := source

	// Determine source type using disambiguation rules:
	// 1. Starts with . or / → local path
	// 2. Starts with git@ → Git SSH URL
	// 3. Starts with https:// or http:// → Git HTTPS URL
	// 4. Contains @ (not matching 2/3) → registry lookup name@registry-name
	// 5. Contains / → Git URL shorthand
	// 6. Plain word → registry search
	var srcDir string

	// Captured from EnsureRepo when the install resolves to a git/registry
	// source. Used to record harness-level provenance into installed.json so
	// --check-updates can detect drift between the installed harness and
	// upstream (symmetric to per-include resolved tracking).
	var harnessSHA, harnessResolvedRef string

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	resolved, err := resolveInstallSource(source, pathFlag, cfg)
	if err != nil {
		return err
	}
	if resolved.gitURL != "" {
		source = resolved.gitURL
		if resolved.path != "" {
			pathFlag = resolved.path
		}
	}
	// --ref overrides any registry-resolved ref. Allowed only when a git
	// fetch will actually happen; noisily refused for local installs to
	// avoid silent confusion.
	if refFlag != "" {
		if resolved.sourceType == "local" || resolved.localPath != "" {
			return fmt.Errorf("--ref is not valid for local-path installs")
		}
		resolved.ref = refFlag
		resolved.sha = "" // user-provided ref takes precedence; don't verify against a stale registry SHA
	}

	if resolved.localPath != "" {
		srcDir = resolved.localPath
	} else if isLocalPath(source) {
		absPath, err := filepath.Abs(source)
		if err != nil {
			return fmt.Errorf("resolving absolute path for %s: %w", source, err)
		}
		srcDir = absPath
	} else {
		// Clone-URL precedence: when resolveInstallSource synthesised a
		// gitURL (registry lookup OR canonical-id normalisation), use that
		// — it's the real repo URL, not the user-typed shape. Fall back
		// to the original source for direct Git URLs (https://, git@).
		cloneURL := source
		if resolved.gitURL != "" {
			cloneURL = resolved.gitURL
		}

		// Check remote source against allow-list
		if err := cfg.CheckRemoteSource(cloneURL); err != nil {
			return err
		}

		// Resolve from Git via cache. When the source came from a registry
		// entry that pinned a ref, honor it so the on-disk checkout matches
		// what the marketplace declared. If a sha is also declared, verify
		// it against the fetched HEAD.
		result, err := resolver.EnsureRepo(cloneURL, resolved.ref)
		if err != nil {
			return fmt.Errorf("resolving %s: %w", cloneURL, err)
		}
		if err := verifyResolvedSHA(result.Path, resolved.sha); err != nil {
			return err
		}
		srcDir = result.Path
		// Capture the resolved harness-source SHA and ref so installed.json
		// can record them. Empty for local installs (set in the local branch).
		harnessSHA = result.SHA
		harnessResolvedRef = result.ResolvedRef
	}

	// Canonical-id installs with a name-hint (e.g.
	// github.com/eyelock/assistants/researcher) don't know where in the
	// cloned repo the harness lives — the trailing "researcher" segment
	// is a name to find, not a path. Discover the matching harness by name
	// and use its directory as the source. The user's explicit --path
	// takes precedence; we only run discovery when --path is absent.
	if pathFlag == "" && resolved.nameHint != "" {
		discovered, derr := sources.Discover(srcDir, 4)
		if derr == nil {
			for _, h := range discovered {
				if h.Name == resolved.nameHint {
					rel, relErr := filepath.Rel(srcDir, h.Path)
					if relErr == nil && rel != "." {
						pathFlag = rel
					}
					break
				}
			}
		}
		if pathFlag == "" {
			// Last resort: see if there's a harness at the repo root with
			// the right name. loadOrSynthesizeHarness below handles that
			// implicitly, but only when the manifest's name matches the
			// hint. If discovery didn't find anything and the root manifest
			// (if any) doesn't match, error out with a clear hint instead
			// of silently installing a different harness.
			rootHarness, rerr := plugin.LoadHarnessJSON(srcDir)
			rootMatches := rerr == nil && rootHarness != nil && rootHarness.Name == resolved.nameHint
			if !rootMatches && plugin.IsPluginDir(srcDir) {
				if hj, perr := plugin.LoadPluginJSON(srcDir); perr == nil && hj != nil && hj.Name == resolved.nameHint {
					rootMatches = true
				}
			}
			if !rootMatches {
				return fmt.Errorf(
					"no harness named %q found in %s; the canonical id ends in %q but the cloned repo has no matching manifest. "+
						"Pass --path <subdir> if the harness lives at a non-default location",
					resolved.nameHint, source, resolved.nameHint)
			}
		}
	}

	// Scope to subdirectory if --path was specified
	if pathFlag != "" {
		if err := pathutil.CheckSubpath(pathFlag); err != nil {
			return fmt.Errorf("invalid --path: %w", err)
		}
		srcDir = filepath.Join(srcDir, pathFlag)
		if _, err := os.Stat(srcDir); os.IsNotExist(err) {
			return fmt.Errorf("path %q not found in source", pathFlag)
		}
	}

	// Load harness: try plugin format first, then bare AGENTS.md directory
	p, err := loadOrSynthesizeHarness(srcDir)
	if err != nil {
		return err
	}

	// Reserved name: "ynh" can be installed but gets no launcher script
	// (it would overwrite the ynh binary in ~/.ynh/bin/).
	// Users invoke it with: ynh run ynh
	reservedName := p.Name == "ynh"

	// Install dir: schema 2 — id-keyed flat layout under HarnessesDir.
	// The canonical id is derived from the recorded source URL plus the
	// harness name. Source URL precedence:
	//  1. Registry-resolved gitURL (registry installs)
	//  2. The original `source` arg if it's a remote URL (direct git installs)
	//  3. Empty (local installs → "local/<name>")
	sourceForID := resolved.gitURL
	if sourceForID == "" && resolved.sourceType == "git" {
		sourceForID = source
	}
	canonID := namespace.CanonicalID(sourceForID, p.Name)
	installDir := harness.InstalledDirByID(canonID)

	// Topology branch (see internal/harness/topology.go). Pointer-form
	// installs (local path, sources: lookup) leave content in the user's
	// source tree — no copy. Tree-form installs (git, registry) copy
	// content into installDir as before.
	isLocal := resolved.sourceType == "local" || resolved.sourceType == "source"

	if isLocal {
		// Pre-schema-3 binaries left a copy dir at installDir for the
		// same canonical id; remove it so reads land on the source tree.
		// Skip when the user pointed install at the install dir itself
		// (rare but possible — e.g. browsing an already-installed copy)
		// since removing it would delete the source we're about to use.
		absSrc, srcErr := filepath.Abs(srcDir)
		absInstall, instErr := filepath.Abs(installDir)
		if srcErr == nil && instErr == nil && absSrc != absInstall {
			if err := os.RemoveAll(installDir); err != nil {
				return fmt.Errorf("cleaning stale install copy: %w", err)
			}
		}
		// Run the format migration against the source tree so the
		// include/delegate pre-fetch below sees the new plugin.json layout.
		if _, err := migration.FormatChain().Run(srcDir); err != nil {
			return fmt.Errorf("migrating source harness format: %w", err)
		}
	} else {
		// If source == install dir, skip the clean+copy (already in place).
		// Otherwise remove stale artifacts and copy fresh.
		absSrc, srcErr := filepath.Abs(srcDir)
		absInstall, instErr := filepath.Abs(installDir)
		alreadyInstalled := srcErr == nil && instErr == nil && absSrc == absInstall
		if !alreadyInstalled {
			if err := os.RemoveAll(installDir); err != nil {
				return fmt.Errorf("cleaning install dir: %w", err)
			}
			if err := os.MkdirAll(installDir, 0o755); err != nil {
				return fmt.Errorf("creating install directory: %w", err)
			}
			if err := assembler.CopyDir(srcDir, installDir); err != nil {
				return fmt.Errorf("copying harness to install directory: %w", err)
			}
		}
		if _, err := migration.FormatChain().Run(installDir); err != nil {
			return fmt.Errorf("migrating installed harness format: %w", err)
		}
	}

	// Write install provenance to .ynh-plugin/installed.json (separate from plugin.json)
	// For canonical-id installs (e.g. `ynh install github.com/org/repo/name`),
	// resolved.gitURL holds the synthesized clone URL — record THAT as the
	// provenance source, not the canonical id, so re-cloning works.
	provSource := source
	if resolved.gitURL != "" {
		provSource = resolved.gitURL
	}
	if resolved.sourceType == "local" {
		// Resolve to absolute path so the pointer record stays valid
		// regardless of the cwd of the consumer (CLI, daemon, embedding
		// host) that later loads it. Relative paths persisted here
		// silently break with a misleading "manifest not found" error.
		absSource, absErr := filepath.Abs(originalSource)
		if absErr != nil {
			return fmt.Errorf("resolving absolute path for %s: %w", originalSource, absErr)
		}
		provSource = absSource
	} else if resolved.localPath != "" {
		provSource = resolved.localPath
	}

	// Carry forward forked_from when installing from a previously forked
	// local directory. Two sources to check:
	//  - Schema-3+: an existing pointer at this canonical id (ynh fork
	//    writes forked_from onto the pointer, nothing into the source tree).
	//  - Pre-schema-3: a leftover <srcDir>/.ynh-plugin/installed.json
	//    written by an older ynh fork — the schema-3 migration absorbs
	//    these but a freshly-built source tree may still have one.
	var forkedFrom *plugin.ForkedFromJSON
	if existing, loadErr := harness.LoadPointerByID(canonID); loadErr == nil && existing != nil && existing.ForkedFrom != nil {
		forkedFrom = existing.ForkedFrom
	}
	if forkedFrom == nil {
		if srcIns, loadErr := plugin.LoadInstalledJSON(srcDir); loadErr == nil && srcIns.ForkedFrom != nil {
			forkedFrom = srcIns.ForkedFrom
		}
	}

	ins := &plugin.InstalledJSON{
		SourceType:   resolved.sourceType,
		Source:       provSource,
		Ref:          harnessResolvedRef,
		SHA:          harnessSHA,
		Path:         pathFlag,
		Namespace:    resolved.namespace,
		RegistryName: resolved.registryName,
		InstalledAt:  time.Now().UTC().Format(time.RFC3339),
		ForkedFrom:   forkedFrom,
	}

	// Pre-fetch includes and delegates so ynh run works offline.
	// Capture each resolved SHA into ins.Resolved so floating-ref entries
	// have a recorded commit for later update detection.
	if len(p.Includes) > 0 || len(p.DelegatesTo) > 0 {
		fmt.Printf("Fetching %d include(s) and %d delegate(s)...\n", len(p.Includes), len(p.DelegatesTo))
	}
	for _, inc := range p.Includes {
		// Local-path includes are resolved on-demand from the harness dir
		// — there's nothing to pre-fetch. Skip the allow-list check and the
		// EnsureRepo clone; the resolver will hit the filesystem at run time.
		if inc.IsLocal() {
			fmt.Printf("  Local  %s\n", inc.Local)
			continue
		}
		if !isLocalPath(inc.Git) {
			if err := cfg.CheckRemoteSource(inc.Git); err != nil {
				return fmt.Errorf("include %q: %w", inc.Git, err)
			}
		}
		res, err := resolver.EnsureRepo(inc.Git, inc.Ref)
		if err != nil {
			return fmt.Errorf("fetching include %s: %w", inc.Git, err)
		}
		ins.Resolved = append(ins.Resolved, plugin.ResolvedSourceJSON{
			Git:  inc.Git,
			Ref:  res.ResolvedRef,
			Path: inc.Path,
			SHA:  res.SHA,
		})
		fmt.Printf("  Fetched %s\n", resolver.ShortGitURL(inc.Git))
	}
	for _, del := range p.DelegatesTo {
		if !isLocalPath(del.Git) {
			if err := cfg.CheckRemoteSource(del.Git); err != nil {
				return fmt.Errorf("delegate %q: %w", del.Git, err)
			}
		}
		res, err := resolver.EnsureRepo(del.Git, del.Ref)
		if err != nil {
			return fmt.Errorf("fetching delegate %s: %w", del.Git, err)
		}
		ins.Resolved = append(ins.Resolved, plugin.ResolvedSourceJSON{
			Git:  del.Git,
			Ref:  res.ResolvedRef,
			Path: del.Path,
			SHA:  res.SHA,
		})
		fmt.Printf("  Fetched %s\n", resolver.ShortGitURL(del.Git))
	}

	if isLocal {
		// Pointer-form: the install record lives in PointersDir, never in
		// the user's source tree — the source stays free of ynh metadata.
		// Drop any stale id-keyed pointer from a prior install of the same
		// canonical id pointing at a different path.
		if err := harness.RemovePointerByID(canonID); err != nil {
			return fmt.Errorf("removing stale pointer: %w", err)
		}
		ptr := &harness.Pointer{
			ID:            canonID,
			Name:          p.Name,
			InstalledJSON: *ins,
		}
		if err := harness.SavePointerByID(ptr); err != nil {
			return fmt.Errorf("saving pointer: %w", err)
		}
	} else {
		if err := plugin.SaveInstalledJSON(installDir, ins); err != nil {
			return fmt.Errorf("saving provenance: %w", err)
		}
	}

	// Generate launcher script (skip for reserved names that conflict with the binary)
	if !reservedName {
		if err := generateLauncher(p.Name, canonID); err != nil {
			return err
		}
	}

	// Stamp the home as schema 2 if absent. Fresh installs always produce
	// canonical-id layout, so the auto-migration gate has nothing to do
	// and shouldn't re-walk the install dir on the next command.
	if migration.ReadSchemaVersion(config.HomeDir()) < migration.CurrentSchemaVersion {
		if err := migration.WriteSchemaVersion(config.HomeDir(), migration.CurrentSchemaVersion); err != nil {
			return fmt.Errorf("stamping schema version: %w", err)
		}
	}

	fmt.Printf("Installed harness %q\n", p.Name)
	locationDir := installDir
	if isLocal {
		// Pointer-form: report the user's source tree, which is where
		// edits and ynh run both land.
		locationDir = srcDir
	}
	fmt.Printf("  Location: %s\n", locationDir)
	if reservedName {
		fmt.Printf("  Launcher: (skipped — conflicts with ynh binary, use \"ynh run %s\")\n", p.Name)
	} else {
		fmt.Printf("  Launcher: %s/%s\n", config.BinDir(), p.Name)
	}

	if p.DefaultVendor != "" {
		fmt.Printf("  Vendor:   %s\n", p.DefaultVendor)
	}

	return nil
}

// bareNameSurvivors returns the canonical ids of installed harnesses that
// still claim bareName after an uninstall. Runs post-removal, so the removed
// install is already absent from ListAll; uninstalledIDs additionally drops
// stale duplicate registrations of the just-removed canonical id (e.g. an
// unmigrated schema-1 flat tree shadowing the schema-2 dir that was removed).
// Results keep ListAll order (namespace, then name) so callers that pick one
// get a deterministic choice.
func bareNameSurvivors(bareName string, uninstalledIDs map[string]bool) ([]string, error) {
	entries, err := harness.ListAll()
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if e.Name != bareName {
			continue
		}
		if id := canonicalIDForEntry(e); !uninstalledIDs[id] {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// repointLauncher keeps the bare-name launcher usable for the installs that
// still claim the name after an uninstall. If the launcher already targets
// one of the survivors it is left untouched; otherwise it is regenerated for
// the first survivor. Returns a user-facing note for the uninstall summary,
// or "" when there is nothing to report (reserved names never get a
// launcher — see cmdInstall).
func repointLauncher(bareName string, survivors []string) string {
	if bareName == "ynh" {
		return ""
	}
	launcherPath := filepath.Join(config.BinDir(), bareName)
	if body, err := os.ReadFile(launcherPath); err == nil {
		for _, id := range survivors {
			if strings.Contains(string(body), fmt.Sprintf("ynh run %q", id)) {
				return fmt.Sprintf("Launcher kept: targets surviving install %s", id)
			}
		}
	}
	if err := generateLauncher(bareName, survivors[0]); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not regenerate launcher for %s: %v\n", survivors[0], err)
		return ""
	}
	return fmt.Sprintf("Launcher repointed to surviving install %s", survivors[0])
}

// repointLegacyRunAlias keeps the legacy bare-name run path (run/<name>,
// a symlink to an id-keyed run dir — see updateLegacyRunAlias) coherent
// after an uninstall with survivors. An alias targeting a removed install's
// run dir is repointed to the sole survivor, or removed when several
// survivors make the bare name ambiguous. A real directory (stale
// pre-re-key assembly) or an alias already targeting a survivor is left
// alone. Best-effort: failures leave a dangling alias, which run repairs.
func repointLegacyRunAlias(bareName string, uninstalledIDs map[string]bool, survivors []string) {
	alias := filepath.Join(config.RunDir(), bareName)
	target, err := os.Readlink(alias)
	if err != nil {
		return // no alias (or a real dir) — nothing to repoint
	}
	removed := false
	for id := range uninstalledIDs {
		if target == namespace.IDToFSName(id) {
			removed = true
			break
		}
	}
	if !removed {
		return
	}
	_ = os.Remove(alias)
	if len(survivors) == 1 {
		_ = os.Symlink(namespace.IDToFSName(survivors[0]), alias)
	}
}

// harnessHasRemoteSource reports whether the harness was installed from a
// git or registry source we can re-pull. Local installs and forks are
// excluded — those have no upstream to track.
func harnessHasRemoteSource(p *harness.Harness) bool {
	if p.InstalledFrom == nil {
		return false
	}
	if p.InstalledFrom.ForkedFrom != nil {
		return false
	}
	switch p.InstalledFrom.SourceType {
	case "git", "registry":
		return p.InstalledFrom.Source != ""
	}
	return false
}

// hashString returns a stable hash of s for use in directory names.
func hashString(s string) uint64 {
	var h uint64 = 14695981039346656037 // FNV-1a offset basis
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 1099511628211 // FNV-1a prime
	}
	return h
}

// generateLauncher writes the per-harness launcher at ~/.ynh/bin/<name>.
// The launcher delegates to `ynh run <canonical-id>` rather than the bare
// name — schema 2 rejects bare names at the resolver, so the launcher must
// pass the same canonical id form a CLI user would type.
func generateLauncher(name, canonicalID string) error {
	binDir := config.BinDir()
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}

	// Filename stays the bare name so users invoke the harness as
	// `~/.ynh/bin/<name>` (and bare on PATH). Only the embedded `ynh run`
	// arg is the canonical id.
	launcherPath := filepath.Join(binDir, name)
	script := fmt.Sprintf(`#!/bin/bash
# Generated by ynh - do not edit
exec ynh run %q "$@"
`, canonicalID)

	if err := os.WriteFile(launcherPath, []byte(script), 0o755); err != nil {
		return err
	}

	return nil
}
