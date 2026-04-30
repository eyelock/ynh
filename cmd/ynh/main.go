package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/migration"
	"github.com/eyelock/ynh/internal/pathutil"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/resolver"
	"github.com/eyelock/ynh/internal/symlink"
	"github.com/eyelock/ynh/internal/vendor"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "init":
		err = cmdInit()
	case "install":
		err = cmdInstall(os.Args[2:])
	case "uninstall", "remove":
		err = cmdUninstall(os.Args[2:])
	case "update":
		err = cmdUpdate(os.Args[2:])
	case "run":
		err = cmdRun(os.Args[2:])
	case "ls", "list":
		err = cmdList(os.Args[2:])
	case "info":
		err = cmdInfo(os.Args[2:])
	case "vendors":
		err = cmdVendors(os.Args[2:])
	case "sources":
		err = cmdSources(os.Args[2:])
	case "paths":
		err = cmdPaths(os.Args[2:])
	case "status":
		err = cmdStatus()
	case "search":
		err = cmdSearch(os.Args[2:])
	case "registry":
		err = cmdRegistry(os.Args[2:])
	case "delegate":
		err = cmdDelegate(os.Args[2:])
	case "fork":
		err = cmdFork(os.Args[2:])
	case "include":
		err = cmdInclude(os.Args[2:])
	case "image":
		err = cmdImage(os.Args[2:])
	case "prune":
		err = cmdPrune()
	case "version", "--version":
		err = cmdVersion(os.Args[2:])
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		// errStructuredReported means the command has already emitted a JSON
		// error envelope to stderr — print nothing more to keep structured
		// consumer stdout/stderr clean. errors.Is so a wrapped sentinel still
		// suppresses the "Error: ..." line.
		if !errors.Is(err, errStructuredReported) {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Printf(`ynh - ynh harness template manager (%s)

Usage:
  ynh <command> [arguments]

Commands:
  init                       Show ynh home path and setup instructions
  install <source> [--path <subdir>]  Install a harness from Git URL or local path
  uninstall <name>           Remove an installed harness and its launcher
  update <name>              Refresh cached Git repos for a harness
  run <name> [flags] [prompt]  Launch a harness session
  ls                           List installed harnesses (supports --format json)
  info <name>                  Show detailed harness information (supports --format json)
  vendors                      List supported vendor adapters (supports --format json)
  search <term>                Search registries and sources (supports --format json)
  sources add <path>           Add a local harness source directory
  sources list                 Show configured sources (supports --format json)
  sources remove <name>        Remove a source
  fork <name> [--to <path>]    Fork an installed harness to a local directory
  delegate add <harness> <url>     Add a Git delegate to a harness
  delegate remove <harness> <url>  Remove a Git delegate from a harness
  delegate update <harness> <url>  Update a Git delegate's options
  include add <harness> <url>  Add a Git include to a harness
  include remove <harness> <url>  Remove a Git include from a harness
  include update <harness> <url>  Update a Git include's options
  registry add <url>           Add a harness registry
  registry list                Show configured registries (supports --format json)
  registry remove <url>        Remove a registry
  registry update              Refresh all cached registries
  image <name> [flags]         Build a Docker image with a harness baked in
  paths                        Show resolved path roots (supports --format json)
  status                       Show symlink installations across projects
  prune                        Clean orphaned symlink installations
  version                      Print version
  help                         Show this help

Run flags:
  -v <vendor>                  Override vendor (claude, codex, cursor)
  --session-name <name>        Session label (recorded by ynh, not forwarded to vendor CLI)
  --install                    Install symlinks for the vendor in current project
  --clean                      Remove symlinks for the vendor in current project
  All other flags are passed through to the vendor CLI.
  Use -- to separate vendor flags from the prompt.

Examples:
  ynh init
  ynh install github.com/myorg/david
  ynh install ./my-local-harness
  ynh install github.com/org/monorepo --path harnesses/david
  ynh run david
  ynh run david "review this PR"
  ynh run david -v codex
  ynh run david --model opus -- "fix this bug"
  ynh run david -v codex -- "refactor auth"
  ynh run david -v cursor --install
  ynh run david -v cursor --clean
  ynh search "go development"
  ynh registry add github.com/org/registry
  ynh install david
  ynh install david@my-registry
`, config.Version)
}

func cmdInit() error {
	if err := config.EnsureDirs(); err != nil {
		return err
	}

	// Save default config if none exists
	if _, err := os.Stat(config.ConfigPath()); os.IsNotExist(err) {
		cfg := &config.Config{
			DefaultVendor: "claude",
		}
		if err := cfg.Save(); err != nil {
			return err
		}
	}

	fmt.Printf("ynh home: %s\n", config.HomeDir())
	fmt.Printf("\nAdd this to your shell profile if not already present:\n")
	fmt.Printf("  export PATH=\"%s:$PATH\"\n", config.BinDir())

	return nil
}

func cmdInstall(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: ynh install <git-url|local-path> [--path <subdir>]")
	}

	if err := config.EnsureDirs(); err != nil {
		return err
	}

	// Parse --path flag from args
	var pathFlag string
	var remaining []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--path" && i+1 < len(args) {
			pathFlag = args[i+1]
			i++ // skip value
		} else {
			remaining = append(remaining, args[i])
		}
	}
	if len(remaining) < 1 {
		return fmt.Errorf("usage: ynh install <git-url|local-path> [--path <subdir>]")
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

	if resolved.localPath != "" {
		srcDir = resolved.localPath
	} else if isLocalPath(source) {
		absPath, err := filepath.Abs(source)
		if err != nil {
			return fmt.Errorf("resolving absolute path for %s: %w", source, err)
		}
		srcDir = absPath
	} else {
		// Check remote source against allow-list
		if err := cfg.CheckRemoteSource(source); err != nil {
			return err
		}

		// Resolve from Git via cache. When the source came from a registry
		// entry that pinned a ref, honor it so the on-disk checkout matches
		// what the marketplace declared. If a sha is also declared, verify
		// it against the fetched HEAD.
		result, err := resolver.EnsureRepo(source, resolved.ref)
		if err != nil {
			return fmt.Errorf("resolving %s: %w", source, err)
		}
		if err := verifyResolvedSHA(result.Path, resolved.sha); err != nil {
			return err
		}
		srcDir = result.Path
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

	// Install dir: namespaced when a registry namespace was resolved, else flat.
	var installDir string
	if resolved.namespace != "" {
		installDir = harness.InstalledDirNS(resolved.namespace, p.Name)
	} else {
		installDir = harness.InstalledDir(p.Name)
	}

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

	// Migrate format if needed (converts .harness.json → .ynh-plugin/plugin.json)
	if _, err := migration.FormatChain().Run(installDir); err != nil {
		return fmt.Errorf("migrating installed harness format: %w", err)
	}

	// Write install provenance to .ynh-plugin/installed.json (separate from plugin.json)
	provSource := source
	if resolved.sourceType == "local" {
		provSource = originalSource
	} else if resolved.localPath != "" {
		provSource = resolved.localPath
	}

	// Carry forward forked_from when installing from a previously forked local directory.
	var forkedFrom *plugin.ForkedFromJSON
	if srcIns, loadErr := plugin.LoadInstalledJSON(srcDir); loadErr == nil && srcIns.ForkedFrom != nil {
		forkedFrom = srcIns.ForkedFrom
	}

	ins := &plugin.InstalledJSON{
		SourceType:   resolved.sourceType,
		Source:       provSource,
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
			Ref:  inc.Ref,
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
			Ref:  del.Ref,
			Path: del.Path,
			SHA:  res.SHA,
		})
		fmt.Printf("  Fetched %s\n", resolver.ShortGitURL(del.Git))
	}

	if err := plugin.SaveInstalledJSON(installDir, ins); err != nil {
		return fmt.Errorf("saving provenance: %w", err)
	}

	// Generate launcher script (skip for reserved names that conflict with the binary)
	if !reservedName {
		if err := generateLauncher(p.Name); err != nil {
			return err
		}
	}

	fmt.Printf("Installed harness %q\n", p.Name)
	fmt.Printf("  Location: %s\n", installDir)
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

func cmdUninstall(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: ynh uninstall <harness-name>")
	}

	ref := args[0]

	// Load to resolve the actual on-disk directory (may be namespaced).
	// Accepts plain "name" or qualified "name@org/repo".
	p, err := harness.LoadQualified(ref)
	if err != nil {
		return fmt.Errorf("harness %q is not installed", ref)
	}
	installDir := p.Dir
	bareName := p.Name // bare name for launcher/run/sources cleanup

	// Remove harness directory
	if err := os.RemoveAll(installDir); err != nil {
		return fmt.Errorf("removing harness: %w", err)
	}

	// Remove launcher script
	launcherPath := filepath.Join(config.BinDir(), bareName)
	_ = os.Remove(launcherPath) // ignore error if launcher doesn't exist

	// Remove run directory
	runDir := filepath.Join(config.RunDir(), bareName)
	_ = os.RemoveAll(runDir) // ignore error if not present

	// Remove matching sources entry if present
	if cfg, err := config.Load(); err == nil {
		remaining := make([]config.Source, 0, len(cfg.Sources))
		for _, s := range cfg.Sources {
			if s.Name != bareName {
				remaining = append(remaining, s)
			}
		}
		cfg.Sources = remaining
		if err := cfg.Save(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not update config after uninstall: %v\n", err)
		}
	}

	fmt.Printf("Uninstalled harness %q\n", bareName)
	return nil
}

func cmdUpdate(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: ynh update <harness-name>")
	}

	name := args[0]

	p, err := harness.Load(name)
	if err != nil {
		return err
	}

	if p.InstalledFrom != nil && p.InstalledFrom.ForkedFrom != nil {
		return fmt.Errorf("harness %q is a fork — ynh update cannot pull upstream changes for a fork\n"+
			"  To update includes within the fork, edit .ynh-plugin/plugin.json directly\n"+
			"  To incorporate upstream changes, fork again from the re-installed original", name)
	}

	if len(p.Includes) == 0 && len(p.DelegatesTo) == 0 {
		fmt.Printf("Harness %q has no Git sources to update.\n", name)
		return nil
	}

	// Load config for remote source checking
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	checked := 0
	updated := 0
	resolvedSources := make([]plugin.ResolvedSourceJSON, 0, len(p.Includes)+len(p.DelegatesTo))
	for _, inc := range p.Includes {
		// Local-path includes have no cache entry to refresh.
		if inc.IsLocal() {
			continue
		}
		if err := cfg.CheckRemoteSource(inc.Git); err != nil {
			return fmt.Errorf("include %q: %w", inc.Git, err)
		}
		fmt.Printf("Checking %s...\n", inc.Git)
		result, err := resolver.EnsureRepo(inc.Git, inc.Ref)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: %v\n", err)
			continue
		}
		checked++
		resolvedSources = append(resolvedSources, plugin.ResolvedSourceJSON{
			Git:  inc.Git,
			Ref:  inc.Ref,
			Path: inc.Path,
			SHA:  result.SHA,
		})
		if result.Changed {
			updated++
			fmt.Printf("  Updated.\n")
		} else {
			fmt.Printf("  Already up to date.\n")
		}
	}
	for _, del := range p.DelegatesTo {
		if err := cfg.CheckRemoteSource(del.Git); err != nil {
			return fmt.Errorf("delegate %q: %w", del.Git, err)
		}
		fmt.Printf("Checking delegate %s...\n", del.Git)
		result, err := resolver.EnsureRepo(del.Git, del.Ref)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: %v\n", err)
			continue
		}
		checked++
		resolvedSources = append(resolvedSources, plugin.ResolvedSourceJSON{
			Git:  del.Git,
			Ref:  del.Ref,
			Path: del.Path,
			SHA:  result.SHA,
		})
		if result.Changed {
			updated++
			fmt.Printf("  Updated.\n")
		} else {
			fmt.Printf("  Already up to date.\n")
		}
	}

	// Persist resolved SHAs back to installed.json so subsequent --check-updates
	// queries can compare against the recorded SHA without re-fetching.
	if ins, loadErr := plugin.LoadInstalledJSON(p.Dir); loadErr == nil && ins != nil {
		ins.Resolved = resolvedSources
		if err := plugin.SaveInstalledJSON(p.Dir, ins); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not update installed.json: %v\n", err)
		}
	}

	fmt.Printf("Checked %d source(s) for harness %q, %d updated.\n", checked, name, updated)
	return nil
}

func cmdRun(args []string) error {
	if err := config.EnsureDirs(); err != nil {
		return fmt.Errorf("ensuring directories: %w", err)
	}

	ra := parseRunArgs(args)

	// Mutual exclusivity: --harness-file + harness name
	if ra.HarnessFile != "" && ra.HarnessName != "" {
		return fmt.Errorf("cannot specify both a harness name and --harness-file")
	}

	// Mutual exclusivity: --focus + --profile
	profileName := ra.ProfileFlag
	if profileName == "" {
		profileName = os.Getenv("YNH_PROFILE")
	}
	if ra.FocusFlag != "" && profileName != "" {
		return fmt.Errorf("cannot use --focus and --profile together (focus includes a profile)")
	}

	// Mutual exclusivity: --focus + trailing prompt
	if ra.FocusFlag != "" && ra.Prompt != "" {
		return fmt.Errorf("cannot use --focus and a trailing prompt together (focus includes a prompt)")
	}

	// Resolve harness source: name > --harness-file > .harness.json in cwd > error
	var p *harness.Harness
	var name string
	var harnessDir string // directory containing harness content (for local artifacts)
	var err error

	switch {
	case ra.HarnessName != "":
		name = ra.HarnessName
		p, err = harness.Load(name)
		if err != nil {
			return err
		}
		harnessDir = p.Dir

	case ra.HarnessFile != "":
		p, err = harness.LoadFile(ra.HarnessFile)
		if err != nil {
			return err
		}
		name = p.Name
		if name == "" {
			name = "(inline)"
		}
		harnessDir = filepath.Dir(ra.HarnessFile)

	default:
		// Auto-discover a harness in cwd. The migration chain converts any
		// legacy format transparently so we only need to load the new format.
		cwd, wdErr := os.Getwd()
		if wdErr != nil {
			return wdErr
		}
		if _, err := migration.FormatChain().Run(cwd); err != nil {
			return fmt.Errorf("migrating harness in cwd: %w", err)
		}
		if !plugin.IsPluginDir(cwd) {
			return fmt.Errorf("usage: ynh run <harness-name> [-v vendor] [--focus name] [--harness-file path] [-- prompt]")
		}
		p, err = harness.LoadDir(cwd)
		if err != nil {
			return err
		}
		name = p.Name
		if name == "" {
			name = "(inline)"
		}
		harnessDir = cwd
	}

	// Resolve focus → profile + prompt
	if ra.FocusFlag != "" {
		focus, ok := p.Focuses[ra.FocusFlag]
		if !ok {
			return fmt.Errorf("focus %q not defined in harness", ra.FocusFlag)
		}
		if focus.Profile != "" {
			profileName = focus.Profile
		}
		ra.Prompt = focus.Prompt
	}

	// Resolve profile
	if profileName != "" {
		p, err = harness.ResolveProfile(p, profileName)
		if err != nil {
			return err
		}
	}

	prompt := ra.Prompt
	vendorArgs := ra.VendorArgs
	action := ra.Action

	// Determine vendor
	vendorName, err := resolveVendor(ra.VendorFlag, p)
	if err != nil {
		return err
	}

	adapter, err := vendor.Get(vendorName)
	if err != nil {
		return err
	}

	// Load config for remote source checking
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Resolve Git includes from cache (no network access unless cache miss)
	if len(p.Includes) > 0 {
		fmt.Fprintf(os.Stderr, "Resolving %d include(s)...\n", len(p.Includes))
	}
	resolved, err := resolver.ResolveFromCache(p, cfg)
	if err != nil {
		return fmt.Errorf("resolving includes: %w", err)
	}

	// Print per-source status
	for _, r := range resolved {
		source := r.Source
		if r.Path != "" {
			source += " → " + r.Path
		}
		if len(r.Content.Paths) > 0 {
			source += " [" + strings.Join(r.Content.Paths, ", ") + "]"
		}
		if r.Cloned {
			fmt.Fprintf(os.Stderr, "  Cloned %s\n", source)
		} else {
			fmt.Fprintf(os.Stderr, "  Cached %s\n", source)
		}
	}

	// Extract ResolvedContent for the assembler
	var content []resolver.ResolvedContent
	for _, r := range resolved {
		content = append(content, r.Content)
	}

	// Also include any local content from the harness directory
	localContent := resolver.ResolvedContent{
		BasePath: harnessDir,
	}
	content = append(content, localContent)

	// Assemble vendor config into deterministic run dir.
	// We use a stable path instead of a temp dir because syscall.Exec
	// replaces this process — deferred cleanup would never run.
	runDirName := name
	if ra.HarnessFile != "" || ra.HarnessName == "" {
		// Inline/discovered harness: use a hash-based stable dir name
		h := fmt.Sprintf("%x", hashString(harnessDir))
		runDirName = "_inline-" + h[:8]
	}
	runDir := filepath.Join(config.RunDir(), runDirName)
	vendorRunDir := filepath.Join(runDir, vendorName)
	if info, err := os.Stat(vendorRunDir); err == nil && info.IsDir() {
		// Pre-assembled layout (baked harness image) — use directly.
		// Skip AssembleTo, delegate allow-list check, AND AssembleDelegates —
		// everything was vetted and assembled at image build time.
		runDir = vendorRunDir
	} else {
		// Normal host flow — assemble now
		if err := assembler.AssembleTo(runDir, adapter, content); err != nil {
			return fmt.Errorf("assembling config: %w", err)
		}

		// Check delegates against remote source allow-list
		for _, del := range p.DelegatesTo {
			if err := cfg.CheckRemoteSource(del.Git); err != nil {
				return fmt.Errorf("delegate %q: %w", del.Git, err)
			}
		}

		// Assemble delegate harnesses as agent files
		if err := assembler.AssembleDelegates(runDir, adapter, p.DelegatesTo); err != nil {
			return fmt.Errorf("assembling delegates: %w", err)
		}

		// Generate vendor-native hook config files
		if len(p.Hooks) > 0 {
			hookFiles, err := adapter.GenerateHookConfig(p.Hooks)
			if err != nil {
				return fmt.Errorf("generating hook config: %w", err)
			}
			for relPath, content := range hookFiles {
				absPath := filepath.Join(runDir, relPath)
				if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
					return fmt.Errorf("creating hook config dir: %w", err)
				}
				if err := os.WriteFile(absPath, content, 0o644); err != nil {
					return fmt.Errorf("writing hook config %s: %w", relPath, err)
				}
			}
		}

		// Generate vendor-native MCP config files
		if len(p.MCPServers) > 0 {
			mcpFiles, err := adapter.GenerateMCPConfig(p.MCPServers)
			if err != nil {
				return fmt.Errorf("generating MCP config: %w", err)
			}
			for relPath, content := range mcpFiles {
				absPath := filepath.Join(runDir, relPath)
				if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
					return fmt.Errorf("creating MCP config dir: %w", err)
				}
				if err := os.WriteFile(absPath, content, 0o644); err != nil {
					return fmt.Errorf("writing MCP config %s: %w", relPath, err)
				}
			}
		}

		// Generate vendor plugin manifest (after hooks/MCP so path pointers are accurate)
		pj := &plugin.HarnessJSON{Name: p.Name, Version: "0.0.0", Description: p.Description}
		manifestFiles, mErr := adapter.GeneratePluginManifest(pj, runDir)
		if mErr != nil {
			return fmt.Errorf("writing plugin manifest: %w", mErr)
		}
		for relPath, content := range manifestFiles {
			absPath := filepath.Join(runDir, relPath)
			if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
				return fmt.Errorf("creating manifest dir: %w", err)
			}
			if err := os.WriteFile(absPath, content, 0o644); err != nil {
				return fmt.Errorf("writing manifest %s: %w", relPath, err)
			}
		}
	}

	// Dispatch based on action.
	switch action {
	case "install":
		if !adapter.NeedsSymlinks() {
			fmt.Printf("%s uses native plugin loading - no symlink installation needed.\n", adapter.Name())
			return nil
		}
		return cmdInstallVendor(adapter, runDir, p.Name)
	case "clean":
		if !adapter.NeedsSymlinks() {
			fmt.Printf("%s uses native plugin loading - no symlinks to clean.\n", adapter.Name())
			return nil
		}
		return cmdCleanVendor(adapter, p.Name)
	default:
		// For vendors that need symlinks, check if they're installed in cwd.
		if adapter.NeedsSymlinks() {
			projectDir, err := os.Getwd()
			if err != nil {
				return err
			}
			log, err := symlink.LoadLog()
			if err != nil {
				return err
			}
			inst := log.FindInstallation(p.Name, adapter.Name(), projectDir)
			if inst != nil && !symlinkIntact(inst) {
				// Log says installed but symlinks are gone — clean up stale entry.
				log.RemoveInstallation(p.Name, adapter.Name(), projectDir)
				_ = log.Save()
				inst = nil
			}
			if inst == nil {
				planned, err := vendor.PlanSymlinks(runDir, projectDir, adapter.ConfigDir(), adapter.ArtifactDirs())
				if err != nil {
					return err
				}
				if len(planned) > 0 {
					fmt.Printf("%s requires symlinks in your project directory.\n", adapter.Name())
					fmt.Printf("The following symlinks will be created in %s:\n\n", projectDir)
					for _, entry := range planned {
						rel, _ := filepath.Rel(projectDir, entry.Link)
						fmt.Printf("  %s -> %s\n", rel, entry.Target)
					}
					fmt.Printf("\nInstall %d symlinks? [Y/n] ", len(planned))
					reader := bufio.NewReader(os.Stdin)
					answer, _ := reader.ReadString('\n')
					answer = strings.TrimSpace(strings.ToLower(answer))
					if answer == "" || answer == "y" || answer == "yes" {
						if err := cmdInstallVendor(adapter, runDir, p.Name); err != nil {
							return err
						}
					}
				}
			}
		}

		// Launch
		fmt.Fprintf(os.Stderr, "Launching %s...\n", adapter.CLIName())
		if prompt != "" {
			return adapter.LaunchNonInteractive(runDir, prompt, vendorArgs)
		}
		return adapter.LaunchInteractive(runDir, vendorArgs)
	}
}

func cmdVendors(args []string) error {
	return cmdVendorsTo(args, os.Stdout, os.Stderr)
}

// vendorEntry is the JSON shape for a single vendor in `ynh vendors --format json`.
type vendorEntry struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	CLI         string `json:"cli"`
	ConfigDir   string `json:"config_dir"`
	Available   bool   `json:"available"`
}

func cmdVendorsTo(args []string, stdout, stderr io.Writer) error {
	structured := detectJSONFormat(args)

	format := "text"
	i := 0
	for i < len(args) {
		switch args[i] {
		case "--format":
			if i+1 >= len(args) {
				return cliError(stderr, structured, errCodeInvalidInput, "--format requires a value")
			}
			i++
			format = args[i]
		default:
			if strings.HasPrefix(args[i], "-") {
				return cliError(stderr, structured, errCodeInvalidInput,
					fmt.Sprintf("unknown flag: %s", args[i]))
			}
			return cliError(stderr, structured, errCodeInvalidInput,
				fmt.Sprintf("unexpected argument: %s", args[i]))
		}
		i++
	}

	switch format {
	case "text":
		return printVendorsText(stdout)
	case "json":
		return printVendorsJSON(stdout)
	default:
		return cliError(stderr, structured, errCodeInvalidInput,
			fmt.Sprintf("invalid --format value %q (want text or json)", format))
	}
}

func printVendorsText(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tDISPLAY NAME\tCLI\tCONFIG DIR\tAVAILABLE")

	for _, name := range vendor.Available() {
		adapter, err := vendor.Get(name)
		if err != nil {
			return fmt.Errorf("loading vendor %s: %w", name, err)
		}
		available := "false"
		if _, err := exec.LookPath(adapter.CLIName()); err == nil {
			available = "true"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			adapter.Name(), adapter.DisplayName(), adapter.CLIName(), adapter.ConfigDir(), available)
	}

	return tw.Flush()
}

func printVendorsJSON(w io.Writer) error {
	entries := make([]vendorEntry, 0, len(vendor.Available()))
	for _, name := range vendor.Available() {
		adapter, err := vendor.Get(name)
		if err != nil {
			return fmt.Errorf("loading vendor %s: %w", name, err)
		}
		_, lookErr := exec.LookPath(adapter.CLIName())
		entries = append(entries, vendorEntry{
			Name:        adapter.Name(),
			DisplayName: adapter.DisplayName(),
			CLI:         adapter.CLIName(),
			ConfigDir:   adapter.ConfigDir(),
			Available:   lookErr == nil,
		})
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding vendors: %w", err)
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}

// resolveVendor picks the vendor: CLI flag > YNH_VENDOR env > harness default > global config.
func resolveVendor(flag string, p *harness.Harness) (string, error) {
	if flag != "" {
		return flag, nil
	}
	if v := os.Getenv("YNH_VENDOR"); v != "" {
		return v, nil
	}
	if p.DefaultVendor != "" {
		return p.DefaultVendor, nil
	}

	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	if cfg.DefaultVendor != "" {
		return cfg.DefaultVendor, nil
	}

	return "", fmt.Errorf("no vendor specified (use -v flag, YNH_VENDOR env var, harness default_vendor, or global config)")
}

// parseRunArgs separates ynh's own flags from vendor pass-through args and the prompt.
//
// ynh flags consumed:
//   - -v <vendor>  : override vendor
//   - --install    : install symlinks for the vendor
//   - --clean      : remove symlinks for the vendor
//
// All other arguments are passed through to the vendor CLI verbatim.
// Use -- to separate vendor flags from the prompt when vendor flags take values:
//
//	ynh run david "simple prompt"
//	ynh run david --verbose "simple prompt"
//	ynh run david --model opus -- "fix this bug"
//	ynh run david -v codex -- "deploy it"
//	ynh run david -v cursor --install
//
// Without --, the first non-flag argument is treated as the prompt. Flag values
// like "opus" in "--model opus" would be mistaken for the prompt, so use -- when
// vendor flags take values.
// runArgs holds parsed arguments for ynh run.
type runArgs struct {
	HarnessName string   // positional name, if given
	HarnessFile string   // --harness-file or YNH_HARNESS_FILE
	VendorFlag  string   // -v or YNH_VENDOR
	ProfileFlag string   // --profile or YNH_PROFILE
	FocusFlag   string   // --focus or YNH_FOCUS
	SessionName string   // --session-name: consumed by ynh, not forwarded to vendor
	Prompt      string   // trailing prompt after --
	VendorArgs  []string // passthrough args for vendor CLI
	Action      string   // "install", "clean", or ""
}

func parseRunArgs(args []string) runArgs {
	var ra runArgs
	flagArgs := args

	// First pass: find -- separator and extract prompt
	for i, arg := range args {
		if arg == "--" {
			flagArgs = args[:i]
			if i+1 < len(args) {
				ra.Prompt = args[i+1]
			}
			break
		}
	}

	// Second pass: process flags
	firstPositional := true
	for i := 0; i < len(flagArgs); i++ {
		switch {
		case flagArgs[i] == "-v" && i+1 < len(flagArgs):
			ra.VendorFlag = flagArgs[i+1]
			i++
		case flagArgs[i] == "--profile" && i+1 < len(flagArgs):
			ra.ProfileFlag = flagArgs[i+1]
			i++
		case flagArgs[i] == "--focus" && i+1 < len(flagArgs):
			ra.FocusFlag = flagArgs[i+1]
			i++
		case flagArgs[i] == "--harness-file" && i+1 < len(flagArgs):
			ra.HarnessFile = flagArgs[i+1]
			i++
		case flagArgs[i] == "--session-name" && i+1 < len(flagArgs):
			ra.SessionName = flagArgs[i+1]
			i++
		case flagArgs[i] == "--install":
			ra.Action = "install"
		case flagArgs[i] == "--clean":
			ra.Action = "clean"
		case !strings.HasPrefix(flagArgs[i], "-"):
			if firstPositional {
				// First positional arg is the harness name
				ra.HarnessName = flagArgs[i]
				firstPositional = false
			} else if ra.Prompt == "" {
				ra.Prompt = flagArgs[i]
			} else {
				ra.VendorArgs = append(ra.VendorArgs, flagArgs[i])
			}
		default:
			ra.VendorArgs = append(ra.VendorArgs, flagArgs[i])
		}
	}

	// Env var fallbacks
	if ra.FocusFlag == "" {
		ra.FocusFlag = os.Getenv("YNH_FOCUS")
	}
	if ra.HarnessFile == "" {
		ra.HarnessFile = os.Getenv("YNH_HARNESS_FILE")
	}

	return ra
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

func generateLauncher(name string) error {
	binDir := config.BinDir()
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}

	launcherPath := filepath.Join(binDir, name)
	script := fmt.Sprintf(`#!/bin/bash
# Generated by ynh - do not edit
exec ynh run %q "$@"
`, name)

	if err := os.WriteFile(launcherPath, []byte(script), 0o755); err != nil {
		return err
	}

	return nil
}

func cmdInstallVendor(adapter vendor.Adapter, stagingDir string, harnessName string) error {
	projectDir, err := os.Getwd()
	if err != nil {
		return err
	}

	entries, err := adapter.Install(stagingDir, projectDir)
	if err != nil {
		return err
	}

	if len(entries) > 0 {
		log, err := symlink.LoadLog()
		if err != nil {
			return err
		}
		log.Record(harnessName, adapter.Name(), projectDir, entries)
		if err := log.Save(); err != nil {
			return err
		}
		fmt.Printf("Installed %d symlinks for %s (%s) in %s:\n\n", len(entries), harnessName, adapter.Name(), projectDir)
		for _, entry := range entries {
			rel, _ := filepath.Rel(projectDir, entry.Link)
			fmt.Printf("  %s -> %s\n", rel, entry.Target)
		}
	}
	return nil
}

func cmdCleanVendor(adapter vendor.Adapter, harnessName string) error {
	projectDir, err := os.Getwd()
	if err != nil {
		return err
	}

	log, err := symlink.LoadLog()
	if err != nil {
		return err
	}

	installation := log.FindInstallation(harnessName, adapter.Name(), projectDir)
	if installation == nil {
		fmt.Printf("No %s installation found for harness %q in %s\n", adapter.Name(), harnessName, projectDir)
		return nil
	}

	if err := adapter.Clean(installation.Symlinks); err != nil {
		return err
	}

	log.RemoveInstallation(harnessName, adapter.Name(), projectDir)
	if err := log.Save(); err != nil {
		return err
	}

	fmt.Printf("Cleaned %s symlinks for harness %q in %s\n", adapter.Name(), harnessName, projectDir)
	return nil
}

func cmdStatus() error {
	log, err := symlink.LoadLog()
	if err != nil {
		return err
	}

	if len(log.Installations) == 0 {
		fmt.Println("No symlink installations found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "HARNESS\tVENDOR\tPROJECT\tSYMLINKS")

	for _, inst := range log.Installations {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%d\n", inst.Harness, inst.Vendor, inst.Project, len(inst.Symlinks))
	}

	return w.Flush()
}

func cmdPrune() error {
	log, err := symlink.LoadLog()
	if err != nil {
		return err
	}

	orphans := log.Prune()
	for _, inst := range orphans {
		fmt.Printf("Removing orphaned installation: %s (%s) in %s\n", inst.Harness, inst.Vendor, inst.Project)
	}

	if len(orphans) > 0 {
		log.RemoveOrphans(orphans)
		if err := log.Save(); err != nil {
			return err
		}
	}

	// Scan for stale launcher scripts in ~/.ynh/bin/
	staleLaunchers := 0
	binDir := config.BinDir()
	entries, err := os.ReadDir(binDir)
	if err == nil {
		for _, entry := range entries {
			name := entry.Name()
			if name == "ynh" || name == "ynd" {
				continue
			}
			if _, err := harness.Load(name); err == nil {
				continue
			}
			launcherPath := filepath.Join(binDir, name)
			data, err := os.ReadFile(launcherPath)
			if err != nil {
				continue
			}
			if !strings.Contains(string(data), "exec ynh run") {
				continue
			}
			_ = os.Remove(launcherPath)
			fmt.Printf("Removed stale launcher: %s\n", launcherPath)
			staleLaunchers++
		}
	}

	// Scan for stale run directories in ~/.ynh/run/
	staleRuns := 0
	runDir := config.RunDir()
	runEntries, err := os.ReadDir(runDir)
	if err == nil {
		for _, entry := range runEntries {
			name := entry.Name()
			if _, err := harness.Load(name); err == nil {
				continue
			}
			staleRun := filepath.Join(runDir, name)
			_ = os.RemoveAll(staleRun)
			fmt.Printf("Removed stale run dir: %s\n", staleRun)
			staleRuns++
		}
	}

	if len(orphans) == 0 && staleLaunchers == 0 && staleRuns == 0 {
		fmt.Println("No orphaned installations found.")
	}

	return nil
}

// symlinkIntact returns true if at least one symlink from the installation
// still exists on disk. Returns false if all symlinks are missing (e.g. the
// user deleted the vendor config directory).
func symlinkIntact(inst *symlink.Installation) bool {
	for _, entry := range inst.Symlinks {
		info, err := os.Lstat(entry.Link)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return true
		}
	}
	return false
}
