package main

import (
	"fmt"
	"os"
	"path/filepath"
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

	// Parse --path and --ref flags from args.
	var pathFlag, refFlag string
	var remaining []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--path" && i+1 < len(args) {
			pathFlag = args[i+1]
			i++
		} else if args[i] == "--ref" && i+1 < len(args) {
			refFlag = args[i+1]
			i++
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
		resolved.sha = "" // user-provided ref takes precedence
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
		// gitURL (registry lookup OR canonical-id normalisation), use that.
		cloneURL := source
		if resolved.gitURL != "" {
			cloneURL = resolved.gitURL
		}

		if err := cfg.CheckRemoteSource(cloneURL); err != nil {
			return err
		}

		result, err := resolver.EnsureRepo(cloneURL, resolved.ref)
		if err != nil {
			return fmt.Errorf("resolving %s: %w", cloneURL, err)
		}
		if err := verifyResolvedSHA(result.Path, resolved.sha); err != nil {
			return err
		}
		srcDir = result.Path
		harnessSHA = result.SHA
		harnessResolvedRef = result.ResolvedRef
	}

	// Canonical-id installs with a name-hint don't know where in the
	// cloned repo the harness lives — discover by name.
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

	// Scope to subdirectory if --path was specified.
	if pathFlag != "" {
		if err := pathutil.CheckSubpath(pathFlag); err != nil {
			return fmt.Errorf("invalid --path: %w", err)
		}
		srcDir = filepath.Join(srcDir, pathFlag)
		if _, err := os.Stat(srcDir); os.IsNotExist(err) {
			return fmt.Errorf("path %q not found in source", pathFlag)
		}
	}

	// Load harness.
	p, err := loadOrSynthesizeHarness(srcDir)
	if err != nil {
		return err
	}

	// Reserved name: "ynh" can be installed but gets no launcher script.
	reservedName := p.Name == "ynh"

	sourceForID := resolved.gitURL
	if sourceForID == "" && resolved.sourceType == "git" {
		sourceForID = source
	}
	canonID := namespace.CanonicalID(sourceForID, p.Name)
	installDir := harness.InstalledDirByID(canonID)

	isLocal := resolved.sourceType == "local" || resolved.sourceType == "source"

	if isLocal {
		absSrc, srcErr := filepath.Abs(srcDir)
		absInstall, instErr := filepath.Abs(installDir)
		if srcErr == nil && instErr == nil && absSrc != absInstall {
			if err := os.RemoveAll(installDir); err != nil {
				return fmt.Errorf("cleaning stale install copy: %w", err)
			}
		}
		if _, err := migration.FormatChain().Run(srcDir); err != nil {
			return fmt.Errorf("migrating source harness format: %w", err)
		}
	} else {
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

	provSource := source
	if resolved.gitURL != "" {
		provSource = resolved.gitURL
	}
	if resolved.sourceType == "local" {
		provSource = originalSource
	} else if resolved.localPath != "" {
		provSource = resolved.localPath
	}

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

	if len(p.Includes) > 0 || len(p.DelegatesTo) > 0 {
		fmt.Printf("Fetching %d include(s) and %d delegate(s)...\n", len(p.Includes), len(p.DelegatesTo))
	}
	for _, inc := range p.Includes {
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

	if !reservedName {
		if err := generateLauncher(p.Name, canonID); err != nil {
			return err
		}
	}

	if migration.ReadSchemaVersion(config.HomeDir()) < migration.CurrentSchemaVersion {
		if err := migration.WriteSchemaVersion(config.HomeDir(), migration.CurrentSchemaVersion); err != nil {
			return fmt.Errorf("stamping schema version: %w", err)
		}
	}

	fmt.Printf("Installed harness %q\n", p.Name)
	locationDir := installDir
	if isLocal {
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
