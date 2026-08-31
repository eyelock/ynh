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
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/backend"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/migration"
	"github.com/eyelock/ynh/internal/namespace"
	"github.com/eyelock/ynh/internal/pathutil"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/resolver"
	"github.com/eyelock/ynh/internal/sources"
	"github.com/eyelock/ynh/internal/symlink"
	"github.com/eyelock/ynh/internal/vendor"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	// Help is answered before anything else happens — before auto-migration,
	// before dispatch, before any command sees its arguments.
	//
	// `--help` used to be unimplemented on all 31 commands: an error on 27,
	// and on four the flag was ignored and the command ran. `ynh prune --help`
	// deleted run directories; `ynh run <name> --help` started a vendor
	// session. Asking what a command does must never be the same as doing it.
	//
	// Intercepting here rather than in each command is the whole point: with
	// 31 commands there is no version of "remember to check --help first" that
	// stays true, and the next command added would inherit the bug. Control
	// never reaching the action is not something an author can get wrong.
	//
	// It also precedes auto-migration deliberately. Reading help must not
	// rewrite the user's home directory.
	if len(os.Args) > 2 && wantsHelp(os.Args[2:]) {
		if printCommandHelp(os.Stdout, os.Args[1]) {
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "Error: %s\n", helpForUnknown(os.Args[1]))
		os.Exit(1)
	}

	// Auto-migration gate: every command except a few that must remain
	// callable on a legacy home (migrate itself, version, help, paths)
	// runs schema-2 migration first if the home is at schema 1. This is
	// the SINGLE place legacy schema-1 layout is touched; all other code
	// speaks schema 2 only. Failure here aborts the command unless
	// --skip-broken was passed (handled inside cmdMigrate's own path).
	if needsAutoMigrate(os.Args[1]) {
		if err := autoMigrate(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
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
	case "installed":
		err = cmdInstalled(os.Args[2:])
	case "schema":
		err = cmdSchema(os.Args[2:])
	case "vendors":
		err = cmdVendors(os.Args[2:])
	case "sources":
		err = cmdSources(os.Args[2:])
	case "paths":
		err = cmdPaths(os.Args[2:])
	case "status":
		err = cmdStatus(os.Args[2:])
	case "search":
		err = cmdSearch(os.Args[2:])
	case "registry":
		err = cmdRegistry(os.Args[2:])
	case "backend":
		err = cmdBackend(os.Args[2:])
	case "delegate":
		err = cmdDelegate(os.Args[2:])
	case "fork":
		err = cmdFork(os.Args[2:])
	case "include":
		err = cmdInclude(os.Args[2:])
	case "focus":
		err = cmdFocus(os.Args[2:])
	case "profile":
		err = cmdProfile(os.Args[2:])
	case "hook":
		err = cmdHook(os.Args[2:])
	case "doctor":
		err = cmdDoctor(os.Args[2:])
	case "mcp":
		err = cmdMCP(os.Args[2:])
	case "sensors":
		err = cmdSensors(os.Args[2:])
	case "check":
		err = cmdCheck(os.Args[2:], os.Stdout, os.Stderr)
	case "baseline":
		err = cmdBaseline(os.Args[2:], os.Stdout, os.Stderr)
	case "agent":
		err = cmdAgent(os.Args[2:])
	case "image":
		err = cmdImage(os.Args[2:])
	case "prune":
		err = cmdPrune()
	case "migrate":
		err = cmdMigrate(os.Args[2:])
	case "quarantine":
		err = cmdQuarantine(os.Args[2:])
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
		// `ynh check` distinguishes its two failure modes by exit code so a
		// gate is unambiguous: 1 means a blocking sensor failed (the report
		// is already on stdout), 2 means ynh could not run the check.
		if errors.Is(err, errCheckBlocked) {
			os.Exit(1)
		}
		if errors.Is(err, errCheckExec) {
			if !errors.Is(err, errStructuredReported) {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			}
			os.Exit(2)
		}
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
	printUsageTo(os.Stdout)
}

// printUsageTo writes the usage page to w. Split from printUsage so a test can
// assert every dispatched command is listed — the check that would have caught
// `migrate` and `quarantine` being dispatched for releases while the page never
// mentioned them. Mirrors the cmdXTo pattern used elsewhere in this package.
func printUsageTo(w io.Writer) {
	// Usage goes to a terminal or a test buffer; a write failure means stdout
	// is gone and there is nowhere left to report it.
	_, _ = fmt.Fprintf(w, `ynh - ynh harness template manager (%s)

Usage:
  ynh <command> [arguments]

Commands:

  Getting a harness
    install <source> [--path <subdir>] [--ref <ref>]  Install from a Git URL or local path
    uninstall <name>                       Remove an installed harness and its launcher
    update <name>                          Refresh a harness's cached Git repos
    fork <name> [--to <path>]              Copy an installed harness somewhere you can edit
    search <term>                          Search registries and sources
    registry <add|list|remove|update>      Manage harness registries
    sources <add|list|remove>              Manage local harness source directories

  Using one
    run <name> [flags] [prompt]            Launch a harness session
    agent run --task <text> [flags]        Run an autonomous agent loop
    ls                                     List installed harnesses
    info <name>                            Show a harness's resolved configuration
    installed <name>                       Show where a harness was installed from
    vendors                                List vendor adapters, and which CLIs are on PATH

  Authoring one
    focus <ls|add|remove|update>           Named prompts, each optionally applying a profile
    profile <ls|add|remove|hook|mcp|include>  Named configuration overlays
    hook <add|remove|export>               Commands the vendor runs at lifecycle events
    mcp <add|remove|update>                MCP servers the harness declares
    include <add|remove|update>            Artifacts pulled in from Git or a local path
    delegate <add|remove|update>           Harnesses this one hands off to
    sensors <ls|show|run>                  Observation surfaces a loop driver consumes

  Gating
    check <harness-id|path>                Run every declared sensor and gate on the result
    baseline <harness>                     Report what the ratchet currently forgives

  Operating
    init                                   Show the ynh home path and setup instructions
    status                                 Show symlink installations across projects
    paths                                  Show resolved path roots
    doctor                                 Check Claude hook wiring in this project
    prune                                  Clean orphaned symlinks and stale run dirs
    image <name> [flags]                   Build a Docker image with a harness baked in
    backend <add|list|remove>              Local model backends a vendor CLI can target
    migrate                                Migrate the ynh home to the current schema
    quarantine <list|restore|drop>         Harnesses a failed migration set aside
    schema <name> | --all                  Print a published JSON schema
    version                                Print version
    help                                   Show this help

Run 'ynh <command> --help' for one command's detail, and 'ynh schema --all'
for the published JSON shape of every command that emits structured output.

Run flags:
  -v <vendor>                  Override vendor (claude, codex, cursor, copilot), or "<backend>/<vendor>[/<model>]" to redirect at a local model backend (see: ynh backend)
  --focus <name>               Load a named focus (sets prompt and profile; implies non-interactive)
  --profile <name>             Apply a named profile overlay (with a prompt, implies non-interactive)
  --interactive                Override non-interactive default — stay in session after focus or prompt
  --instructions "<text>"      Inject per-invocation context after harness instructions
  --session-name <name>        Session label (recorded by ynh, not forwarded to vendor CLI)
  --resume                     Continue the previous session in this directory
  --resume=<id>                Continue one specific session by id
  --install                    Install symlinks for the vendor in current project
  --clean                      Remove symlinks for the vendor in current project
  All other flags are passed through to the vendor CLI.
  Use -- to separate vendor flags from the prompt.

Examples:
  ynh init
  ynh install github.com/myorg/david
  ynh install ./my-local-harness
  ynh install github.com/org/monorepo --path harnesses/david
  ynh install github.com/org/repo --ref v1.2.0
  ynh run david
  ynh run david "review this PR"
  ynh run david --focus code-review
  ynh run david --focus code-review --interactive
  ynh run david --profile thorough -- "audit this module"
  ynh run david --resume
  ynh run david --resume=6187c041-ee79-4646-b101-82f89c3e50ca
  ynh run david --instructions "PR #22 in eyelock/assistants"
  ynh run david --focus code-review --instructions "PR #22 in eyelock/assistants"
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

func cmdUninstall(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: ynh uninstall <harness-name>")
	}

	ref := args[0]

	// Pointer-shaped install: take the pointer path first, before attempting
	// to load the manifest. Removing a pointer is a metadata operation; it
	// must succeed even when the pointed-to source tree is missing — that's
	// the exact case where users most need to uninstall.
	//
	// Resolution mirrors LoadByID: try schema-2 (id-keyed) first, then fall
	// back to schema-1 (name-keyed) for "local/<name>" canonical IDs.
	var bareName, pointerSource string
	ptr, ptrErr := harness.LoadPointerByID(ref)
	if ptrErr != nil {
		return fmt.Errorf("checking pointer: %w", ptrErr)
	}
	if ptr == nil {
		if name, ok := strings.CutPrefix(ref, "local/"); ok {
			var err error
			ptr, err = harness.LoadPointer(name)
			if err != nil {
				return fmt.Errorf("checking pointer: %w", err)
			}
		}
	}
	if ptr != nil {
		bareName = ptr.Name
		pointerSource = ptr.Source
		// Remove both schemas — RemovePointer* silently no-ops on missing files.
		if err := harness.RemovePointer(bareName); err != nil {
			return fmt.Errorf("removing pointer: %w", err)
		}
		if err := harness.RemovePointerByID(ref); err != nil {
			return fmt.Errorf("removing id-keyed pointer: %w", err)
		}
	} else {
		// Tree-shaped install: resolve the on-disk directory (may be flat or
		// namespaced) via the manifest, then remove the directory.
		p, err := harness.LoadQualified(ref)
		if err != nil {
			return fmt.Errorf("harness %q is not installed", ref)
		}
		bareName = p.Name
		if err := os.RemoveAll(p.Dir); err != nil {
			return fmt.Errorf("removing harness: %w", err)
		}
	}

	// Run dirs are keyed by canonical id ("local--foo"), so the removed
	// install's run dirs are exclusively its own — remove them outright.
	// Both id forms in uninstalledIDs may have dirs; removing a missing one
	// is a no-op.
	uninstalledIDs := map[string]bool{ref: true}
	if ptr != nil {
		// Pointer registrations are listed under "local/<name>" regardless
		// of the ref form used to remove them.
		uninstalledIDs["local/"+bareName] = true
	}
	for id := range uninstalledIDs {
		_ = os.RemoveAll(filepath.Join(config.RunDir(), namespace.IDToFSName(id)))
	}

	// The launcher (~/.ynh/bin/<name>), legacy bare-name run path
	// (~/.ynh/run/<name>) and sources entry are keyed by bare name and
	// therefore shared across installs whose canonical ids differ only in
	// namespace (e.g. "local/foo" vs "github.com/org/repo/foo"). Remove
	// them only when no other install still claims the name; otherwise
	// leave them for the survivor, repointing the launcher and the legacy
	// run alias if they targeted the install just removed.
	var launcherNote string
	survivors, surErr := bareNameSurvivors(bareName, uninstalledIDs)
	switch {
	case surErr != nil:
		// Can't tell whether the name is still claimed — leave the shared
		// resources in place rather than risk orphaning a surviving install.
		fmt.Fprintf(os.Stderr, "warning: could not check for other installs named %q: %v\n", bareName, surErr)
		fmt.Fprintf(os.Stderr, "  launcher, run alias and sources entry left in place\n")
	case len(survivors) == 0:
		// Remove launcher script
		launcherPath := filepath.Join(config.BinDir(), bareName)
		_ = os.Remove(launcherPath) // ignore error if launcher doesn't exist

		// Remove legacy bare-name run path (pre-re-key real dir or alias
		// symlink — RemoveAll removes a symlink without following it)
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
	default:
		launcherNote = repointLauncher(bareName, survivors)
		repointLegacyRunAlias(bareName, uninstalledIDs, survivors)
	}

	fmt.Printf("Uninstalled harness %q\n", bareName)
	if launcherNote != "" {
		fmt.Printf("  %s\n", launcherNote)
	}
	if pointerSource != "" {
		fmt.Printf("  Source tree left in place: %s\n", pointerSource)
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

func cmdUpdate(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: ynh update <harness-name>")
	}

	name := args[0]

	p, err := harness.LoadQualified(name)
	if err != nil {
		return err
	}

	if p.InstalledFrom != nil && p.InstalledFrom.ForkedFrom != nil {
		return fmt.Errorf("harness %q is a fork — ynh update cannot pull upstream changes for a fork\n"+
			"  To update includes within the fork, edit .ynh-plugin/plugin.json directly\n"+
			"  To incorporate upstream changes, fork again from the re-installed original", name)
	}

	// A harness is updateable if it has a remote source (git/registry) OR
	// any remote includes/delegates. Pointer-form installs (local source
	// or sources: entry) have no upstream to fetch for the harness body
	// itself — the user's source tree is authoritative; edits are live to
	// ynh run without any sync step. Any remote includes/delegates the
	// harness references still get refreshed below.
	hasHarnessSource := harnessHasRemoteSource(p)
	isLocalBody := p.InstalledFrom != nil && (p.InstalledFrom.SourceType == "local" || p.InstalledFrom.SourceType == "source")
	if len(p.Includes) == 0 && len(p.DelegatesTo) == 0 && !hasHarnessSource {
		if isLocalBody {
			fmt.Printf("Harness %q is local — edits at %s are live; nothing to fetch.\n", name, p.Dir)
		} else {
			fmt.Printf("Harness %q has no Git sources to update.\n", name)
		}
		return nil
	}
	if isLocalBody {
		fmt.Printf("Harness %q is local at %s — refreshing remote includes/delegates only.\n", name, p.Dir)
	}

	// Load config for remote source checking
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	checked := 0
	updated := 0
	// Re-pull the harness source itself first so any newly-added includes
	// or delegates upstream are visible to the include walk below.
	var harnessSHA, harnessResolvedRef string
	if hasHarnessSource {
		gitURL := p.InstalledFrom.Source
		if err := cfg.CheckRemoteSource(gitURL); err != nil {
			return fmt.Errorf("harness source %q: %w", gitURL, err)
		}
		fmt.Printf("Checking harness source %s...\n", gitURL)
		ref := p.InstalledFrom.Ref
		result, err := resolver.EnsureRepo(gitURL, ref)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: %v\n", err)
		} else {
			checked++
			harnessSHA = result.SHA
			harnessResolvedRef = result.ResolvedRef
			shaAdvanced := harnessSHA != "" && p.InstalledFrom != nil && harnessSHA != p.InstalledFrom.SHA
			if result.Changed {
				updated++
				fmt.Printf("  Updated.\n")
				// Sync the installed harness directory with the new content.
				// Skip when source path is empty (whole-repo install) or path
				// doesn't exist in the new tree.
				newSrcDir := result.Path
				if p.InstalledFrom.Path != "" {
					newSrcDir = filepath.Join(result.Path, p.InstalledFrom.Path)
				}
				if _, statErr := os.Stat(newSrcDir); statErr == nil {
					if err := assembler.CopyDir(newSrcDir, p.Dir); err != nil {
						fmt.Fprintf(os.Stderr, "  Warning: copying refreshed harness: %v\n", err)
					}
					// Reload after content refresh so include/delegate slices
					// reflect any newly-added entries from upstream.
					if reloaded, reloadErr := harness.LoadDir(p.Dir); reloadErr == nil {
						p = reloaded
					}
				}
			} else if shaAdvanced {
				// Cache was already current but the recorded SHA in installed.json
				// was stale (cache was advanced by a different operation). The SHA
				// is being written below; count it as updated so callers know
				// ref_installed changed.
				updated++
				fmt.Printf("  Updated.\n")
			} else {
				fmt.Printf("  Already up to date.\n")
			}
		}
	}
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
			Ref:  result.ResolvedRef,
			Path: inc.Path,
			SHA:  result.SHA,
		})
		if result.Changed || result.SHA != inc.SHA {
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
			Ref:  result.ResolvedRef,
			Path: del.Path,
			SHA:  result.SHA,
		})
		if result.Changed || result.SHA != del.SHA {
			updated++
			fmt.Printf("  Updated.\n")
		} else {
			fmt.Printf("  Already up to date.\n")
		}
	}

	// Persist resolved SHAs back to the install record so subsequent
	// --check-updates queries can compare against the recorded SHA without
	// re-fetching. Also refresh the harness-level SHA/Ref when re-pulled
	// so the harness's own drift signal stays accurate.
	//
	// LoadInstalledRecord / SaveInstalledRecord are topology-aware (see
	// internal/harness/topology.go): pointer-form installs route to the
	// pointer file, tree-form to <p.Dir>/.ynh-plugin/installed.json.
	if ins, loadErr := harness.LoadInstalledRecord(name, p); loadErr == nil && ins != nil {
		ins.Resolved = resolvedSources
		if hasHarnessSource && harnessSHA != "" {
			ins.SHA = harnessSHA
			if harnessResolvedRef != "" {
				ins.Ref = harnessResolvedRef
			}
		}
		if err := harness.SaveInstalledRecord(name, p, ins); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not update install record: %v\n", err)
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
	var harnessDir string // directory containing harness content (for local artifacts)
	var err error

	switch {
	case ra.HarnessName != "":
		p, err = harness.LoadQualified(ra.HarnessName)
		if err != nil {
			return err
		}
		harnessDir = p.Dir

	case ra.HarnessFile != "":
		p, err = harness.LoadFile(ra.HarnessFile)
		if err != nil {
			return err
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

	// Load config for vendor resolution and remote source checking
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Determine vendor. The resolved spec may itself be a plain vendor name
	// ("claude") or a backend redirection ("ollama/claude/qwen3") — both
	// travel through the same -v flag / YNH_VENDOR / default_vendor
	// precedence chain, so a local model backend is just another vendor
	// spec, not a separate flag.
	spec, err := resolveVendor(ra.VendorFlag, p, cfg)
	if err != nil {
		return err
	}
	vs, err := backend.ParseSpec(spec)
	if err != nil {
		return err
	}
	vendorName := vs.Vendor

	adapter, err := vendor.Get(vendorName)
	if err != nil {
		return err
	}

	// Redirect the vendor CLI at the backend named in the spec, if any.
	// No-op otherwise — vendor talks to its normal cloud API.
	if vs.Backend != "" {
		conn, err := backend.Lookup(cfg, vs)
		if err != nil {
			return err
		}
		extra, err := backend.Apply(vendorName, vs.Backend, conn, vs.Model)
		if err != nil {
			return err
		}
		vendorArgs = append(extra, vendorArgs...)
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
	//
	// Run-dir naming uses the canonical id's fs name ("local/foo" →
	// "local--foo") so same-named installs with distinct canonical ids get
	// distinct run dirs and can't clobber each other's live sessions.
	// LoadQualified already enforced that ra.HarnessName is a canonical id.
	// Inline/discovered harnesses have no canonical id and use a hash-based
	// name instead.
	runDirName := namespace.IDToFSName(ra.HarnessName)
	if ra.HarnessFile != "" || ra.HarnessName == "" {
		// Inline/discovered harness: use a hash-based stable dir name
		h := fmt.Sprintf("%x", hashString(harnessDir))
		runDirName = "_inline-" + h[:8]
	}
	runDir := filepath.Join(config.RunDir(), runDirName)
	preassembledDir, preassembled := findPreassembledVendorDir(runDirName, p.Name, vendorName)
	if preassembled {
		// Pre-assembled layout (baked harness image) — use directly.
		// Skip AssembleTo, delegate allow-list check, AND AssembleDelegates —
		// everything was vetted and assembled at image build time.
		runDir = preassembledDir
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
			servers, expErr := plugin.ExpandMCPEnv(p.MCPServers, p.EnvPassthrough, os.LookupEnv)
			if expErr != nil {
				return expErr
			}
			mcpFiles, err := adapter.GenerateMCPConfig(servers)
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
		// The real manifest, not a synthesized one. h.Version is populated and
		// correct here; hardcoding "0.0.0" made this path disagree with
		// `ynd export` and with what actually ships, for the same harness.
		pj := &plugin.HarnessJSON{
			Name:        p.Name,
			Version:     p.Version,
			Description: p.Description,
			Author:      p.Author,
			Keywords:    p.Keywords,
		}
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

		// Keep the legacy bare-name run path resolving for project symlinks
		// planted before run dirs were keyed by canonical id.
		if ra.HarnessName != "" {
			updateLegacyRunAlias(p.Name, runDirName)
		}
	}

	// Inject per-invocation instructions into the vendor's pipeline.
	if ra.Instructions != "" {
		extraArgs, err := adapter.ApplyRuntimeInstructions(runDir, ra.Instructions)
		if err != nil {
			return fmt.Errorf("applying runtime instructions: %w", err)
		}
		vendorArgs = append(vendorArgs, extraArgs...)
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
		if ra.Resume {
			if !adapter.SupportsResume() {
				return fmt.Errorf("%s does not support resuming sessions", adapter.Name())
			}
			sessionID, resumed := resolveResumeSession(adapter, ra.ResumeID, os.Stderr)
			if resumed {
				return adapter.LaunchResume(runDir, sessionID, vendorArgs)
			}
			// Nothing to resume: fall through to a normal cold launch rather
			// than failing. A card relaunching for the first time must still
			// start.
		}
		if prompt != "" {
			if ra.Interactive {
				return adapter.LaunchWithInitialPrompt(runDir, prompt, vendorArgs)
			}
			return adapter.LaunchNonInteractive(runDir, prompt, vendorArgs)
		}
		return adapter.LaunchInteractive(runDir, vendorArgs)
	}
}

// resolveResumeSession decides which session `--resume` should continue.
//
// It returns the session id to resume and whether to resume at all. An empty id
// with resume==true means "use the vendor's continue-last form" — which is a
// real, working resume, not a fallback to nothing.
//
// The three outcomes:
//
//   - explicit id given            → resume that session
//   - vendor cannot look up ids    → resume with an empty id (continue-last)
//   - store readable, nothing here → do not resume; the caller launches cold
//
// The last case is deliberately not an error. Relaunching a terminal whose
// first session never existed must still start the CLI, and a hard failure
// there would strand the caller. Warn instead.
func resolveResumeSession(adapter vendor.Adapter, explicitID string, stderr io.Writer) (string, bool) {
	if explicitID != "" {
		return explicitID, true
	}

	cwd, err := os.Getwd()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "resume: cannot determine working directory (%v); starting a new session\n", err)
		return "", false
	}

	sessionID, err := adapter.ResolveLastSession(cwd, time.Time{})
	switch {
	case err == nil:
		return sessionID, true
	case errors.Is(err, vendor.ErrSessionLookupUnavailable):
		// The vendor keeps no id we can read, but can still continue its own
		// most recent session.
		return "", true
	case errors.Is(err, vendor.ErrNoResumableSession):
		_, _ = fmt.Fprintf(stderr, "resume: no previous session found for %s; starting a new session\n", cwd)
		return "", false
	default:
		// A malformed or unreadable store is not worth failing a launch over.
		_, _ = fmt.Fprintf(stderr, "resume: could not read session history (%v); starting a new session\n", err)
		return "", false
	}
}

// findPreassembledVendorDir looks for a baked vendor layout (harness image)
// under the id-keyed run dir first, then under the legacy bare-name run dir
// for images assembled by older ynh. Host-assembled run dirs never contain a
// bare vendor-name subdirectory (vendor config dirs are dot-prefixed:
// .claude, .codex, .cursor), so a match is always a baked layout.
func findPreassembledVendorDir(runDirName, bareName, vendorName string) (string, bool) {
	candidates := []string{filepath.Join(config.RunDir(), runDirName, vendorName)}
	if bareName != "" && bareName != runDirName {
		candidates = append(candidates, filepath.Join(config.RunDir(), bareName, vendorName))
	}
	for _, dir := range candidates {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir, true
		}
	}
	return "", false
}

// updateLegacyRunAlias maintains a bare-name symlink run/<name> →
// <id-fsname> beside the id-keyed run dir. Project symlinks planted by
// older ynh point through the bare-name path; the alias keeps them
// resolving after the id-keyed re-key. When several installs claim the
// name the alias is removed instead — a bare-name path can't say which
// install it means, and a dangling project link surfaces loudly (run
// re-plants it, see symlinkIntact) where a silently re-bound one wouldn't.
// Best-effort: alias failures degrade to the dangling-link path, never
// fail the run.
func updateLegacyRunAlias(bareName, runDirName string) {
	if bareName == "" || bareName == runDirName {
		return
	}
	alias := filepath.Join(config.RunDir(), bareName)
	entries, err := harness.ListAll()
	if err != nil {
		return // can't tell who claims the name — leave the alias alone
	}
	// Count distinct canonical ids, not entries: a stale schema-1 duplicate
	// of the same install (flat tree beside the id-keyed tree) is one
	// logical claimant, not two.
	claimants := map[string]bool{}
	for _, e := range entries {
		if e.Name == bareName {
			claimants[canonicalIDForEntry(e)] = true
		}
	}
	if len(claimants) > 1 {
		// Only remove an alias we own (a symlink); never a real directory.
		if fi, err := os.Lstat(alias); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			_ = os.Remove(alias)
		}
		return
	}
	// Unambiguous: (re)point the alias. A real directory here is a stale
	// pre-re-key assembly — the old flow clobbered it via AssembleTo on
	// every run anyway, so replacing it with the alias loses nothing.
	if err := os.RemoveAll(alias); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not refresh legacy run alias %s: %v\n", alias, err)
		return
	}
	if err := os.Symlink(runDirName, alias); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not create legacy run alias %s: %v\n", alias, err)
	}
}

func cmdVendors(args []string) error {
	return cmdVendorsTo(args, os.Stdout, os.Stderr)
}

// vendorEntry is the JSON shape for a single vendor in `ynh vendors --format json`.
type vendorEntry struct {
	Name                  string `json:"name"`
	DisplayName           string `json:"display_name"`
	CLI                   string `json:"cli"`
	ConfigDir             string `json:"config_dir"`
	Available             bool   `json:"available"`
	SupportsInitialPrompt bool   `json:"supports_initial_prompt"`
	SupportsResume        bool   `json:"supports_resume"`
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
	entries, err := vendorEntries()
	if err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NAME\tDISPLAY NAME\tCLI\tCONFIG DIR\tAVAILABLE")

	for _, e := range entries {
		available := "false"
		if e.Available {
			available = "true"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			e.Name, e.DisplayName, e.CLI, e.ConfigDir, available)
	}

	return tw.Flush()
}

// initialPrompter is an optional capability interface for vendors that support
// starting an interactive session with an initial prompt pre-loaded.
type initialPrompter interface {
	SupportsInitialPrompt() bool
}

// vendorEntries lists the registered vendor adapters, plus one extra row per
// (backend, vendor) pair declared in ~/.ynh/config.json's "backends" map.
// Backend rows use the same "<backend>/<vendor>" spec accepted by -v (see
// backend.ParseSpec) as their name — discoverable without guessing at
// installed models, which config deliberately doesn't store (the model is
// supplied live in the -v string, not pinned in config).
func vendorEntries() ([]vendorEntry, error) {
	entries := make([]vendorEntry, 0, len(vendor.Available()))
	for _, name := range vendor.Available() {
		adapter, err := vendor.Get(name)
		if err != nil {
			return nil, fmt.Errorf("loading vendor %s: %w", name, err)
		}
		_, lookErr := exec.LookPath(adapter.CLIName())
		supportsIP := false
		if ip, ok := adapter.(initialPrompter); ok {
			supportsIP = ip.SupportsInitialPrompt()
		}
		entries = append(entries, vendorEntry{
			Name:                  adapter.Name(),
			DisplayName:           adapter.DisplayName(),
			CLI:                   adapter.CLIName(),
			ConfigDir:             adapter.ConfigDir(),
			Available:             lookErr == nil,
			SupportsInitialPrompt: supportsIP,
			SupportsResume:        adapter.SupportsResume(),
		})
	}

	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}

	var backendNames []string
	for name := range cfg.Backends {
		backendNames = append(backendNames, name)
	}
	sort.Strings(backendNames)

	for _, backendName := range backendNames {
		var vendorNames []string
		for name := range cfg.Backends[backendName].Vendors {
			vendorNames = append(vendorNames, name)
		}
		sort.Strings(vendorNames)

		// Query the backend for installed models, if it declares a type ynh
		// knows how to ask (currently just "ollama"). This is best-effort:
		// an unreachable server or unrecognized type just falls back to a
		// bare "<backend>/<vendor>" row instead of failing the listing.
		models, _ := backend.ListModels(cfg, backendName)

		for _, vn := range vendorNames {
			adapter, err := vendor.Get(vn)
			if err != nil {
				// Config names a vendor ynh doesn't know; skip it rather
				// than failing the whole listing over one bad entry.
				continue
			}
			_, lookErr := exec.LookPath(adapter.CLIName())
			supportsIP := false
			if ip, ok := adapter.(initialPrompter); ok {
				supportsIP = ip.SupportsInitialPrompt()
			}

			if len(models) == 0 {
				entries = append(entries, vendorEntry{
					Name:                  backendName + "/" + vn,
					DisplayName:           fmt.Sprintf("%s (%s)", adapter.DisplayName(), backendName),
					CLI:                   adapter.CLIName(),
					ConfigDir:             adapter.ConfigDir(),
					Available:             lookErr == nil,
					SupportsInitialPrompt: supportsIP,
					SupportsResume:        adapter.SupportsResume(),
				})
				continue
			}

			for _, model := range models {
				entries = append(entries, vendorEntry{
					Name:                  backendName + "/" + vn + "/" + model,
					DisplayName:           fmt.Sprintf("%s (%s · %s)", adapter.DisplayName(), backendName, model),
					CLI:                   adapter.CLIName(),
					ConfigDir:             adapter.ConfigDir(),
					Available:             lookErr == nil,
					SupportsInitialPrompt: supportsIP,
					SupportsResume:        adapter.SupportsResume(),
				})
			}
		}
	}

	return entries, nil
}

func printVendorsJSON(w io.Writer) error {
	entries, err := vendorEntries()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding vendors: %w", err)
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}

// resolveVendor determines the vendor spec string to launch — a plain
// vendor name ("claude") or a backend redirection ("ollama/claude/qwen3");
// see backend.ParseSpec. Both forms flow through the same precedence chain:
// CLI flag (-v) > YNH_VENDOR env > harness default > global config.
func resolveVendor(flag string, p *harness.Harness, cfg *config.Config) (string, error) {
	if flag != "" {
		return flag, nil
	}
	if v := os.Getenv("YNH_VENDOR"); v != "" {
		return v, nil
	}
	if p.DefaultVendor != "" {
		return p.DefaultVendor, nil
	}
	if cfg.DefaultVendor != "" {
		return cfg.DefaultVendor, nil
	}

	return "", fmt.Errorf("no vendor specified (use -v flag, YNH_VENDOR env var, harness default_vendor, or global config)")
}

// parseRunArgs separates ynh's own flags from vendor pass-through args and the prompt.
//
// ynh flags consumed:
//   - -v <vendor>   : override vendor. Also accepts a backend-redirected
//     spec — "<backend>/<vendor>" or "<backend>/<vendor>/<model>" — to
//     launch that vendor against a local model backend (e.g. Ollama)
//     instead of its normal cloud API. See docs/vendors.md.
//   - --install     : install symlinks for the vendor
//   - --clean       : remove symlinks for the vendor
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
	HarnessName  string   // positional name, if given
	HarnessFile  string   // --harness-file or YNH_HARNESS_FILE
	VendorFlag   string   // -v or YNH_VENDOR
	ProfileFlag  string   // --profile or YNH_PROFILE
	FocusFlag    string   // --focus or YNH_FOCUS
	SessionName  string   // --session-name: consumed by ynh, not forwarded to vendor
	Instructions string   // --instructions: per-invocation context injected into vendor pipeline
	Prompt       string   // trailing prompt after --
	VendorArgs   []string // passthrough args for vendor CLI
	Action       string   // "install", "clean", or ""
	Interactive  bool     // --interactive: stay in session after initial prompt
	Resume       bool     // --resume: continue a previous session
	ResumeID     string   // --resume=<id>: continue one specific session
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
		case flagArgs[i] == "--instructions" && i+1 < len(flagArgs):
			ra.Instructions = flagArgs[i+1]
			i++
		// Only the "=" form takes an id: `--resume <id>` would be ambiguous with
		// the positional harness name. This also matches how Copilot's own CLI
		// spells it (`copilot --resume=<session-id>`).
		case flagArgs[i] == "--resume":
			ra.Resume = true
		case strings.HasPrefix(flagArgs[i], "--resume="):
			ra.Resume = true
			ra.ResumeID = strings.TrimPrefix(flagArgs[i], "--resume=")
		case flagArgs[i] == "--install":
			ra.Action = "install"
		case flagArgs[i] == "--clean":
			ra.Action = "clean"
		case flagArgs[i] == "--interactive":
			ra.Interactive = true
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

func cmdStatus(args []string) error {
	return cmdStatusTo(args, os.Stdout, os.Stderr)
}

// statusInstallation is the JSON shape for a single symlink installation
// in `ynh status --format json`. Mirrors symlink.Installation; redeclared
// here so the wire contract is owned by cmd/ynh and not coupled to the
// internal package's struct evolution.
type statusInstallation struct {
	Harness   string                `json:"harness"`
	Vendor    string                `json:"vendor"`
	Project   string                `json:"project"`
	Timestamp string                `json:"timestamp"`
	Symlinks  []vendor.SymlinkEntry `json:"symlinks"`
}

func cmdStatusTo(args []string, stdout, stderr io.Writer) error {
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
		return printStatusText(stdout)
	case "json":
		return printStatusJSON(stdout)
	default:
		return cliError(stderr, structured, errCodeInvalidInput,
			fmt.Sprintf("invalid --format value %q (want text or json)", format))
	}
}

func printStatusText(w io.Writer) error {
	log, err := symlink.LoadLog()
	if err != nil {
		return err
	}
	if len(log.Installations) == 0 {
		_, _ = fmt.Fprintln(w, "No symlink installations found.")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "HARNESS\tVENDOR\tPROJECT\tSYMLINKS")
	for _, inst := range log.Installations {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%d\n", inst.Harness, inst.Vendor, inst.Project, len(inst.Symlinks))
	}
	return tw.Flush()
}

func printStatusJSON(w io.Writer) error {
	log, err := symlink.LoadLog()
	if err != nil {
		return err
	}
	entries := make([]statusInstallation, 0, len(log.Installations))
	for _, inst := range log.Installations {
		entries = append(entries, statusInstallation{
			Harness:   inst.Harness,
			Vendor:    inst.Vendor,
			Project:   inst.Project,
			Timestamp: inst.Timestamp.UTC().Format(time.RFC3339),
			Symlinks:  inst.Symlinks,
		})
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding status: %w", err)
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
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

	// Scan for orphan pointer files: pointer exists but its source tree is
	// gone. The user owned the source — they likely deleted it without
	// uninstalling first. Removing the pointer is a metadata operation, so
	// we can do it without consent prompts.
	orphanPointers := 0
	if pointers, err := harness.ListPointers(); err == nil {
		for _, e := range pointers {
			if _, err := os.Stat(e.Dir); err == nil {
				continue
			} else if !os.IsNotExist(err) {
				continue
			}
			if err := harness.RemovePointer(e.Name); err != nil {
				fmt.Fprintf(os.Stderr, "warning: removing pointer %q: %v\n", e.Name, err)
				continue
			}
			fmt.Printf("Removed orphan pointer: %s (source missing: %s)\n", e.Name, e.Dir)
			orphanPointers++
		}
	}

	// Build a set of installed harness names so the launcher / run-dir
	// scanners can decide "stale" by membership rather than per-entry
	// Load lookups. Schema 2's id-keyed install layout means
	// harness.Load(<bare-name>) no longer resolves an install whose
	// canonical id is e.g. "local/<name>" — using ListAll names directly
	// is both correct under schema 2 and faster than N Load calls.
	//
	// Membership covers both name forms: bare names (launchers, legacy run
	// dirs, and the bare-name run alias — load-bearing for project symlinks
	// planted before the id-keyed re-key) and id fs-names (run dirs keyed
	// by canonical id, e.g. "local--foo").
	installedNames := map[string]bool{}
	if installs, err := harness.ListAll(); err == nil {
		for _, e := range installs {
			installedNames[e.Name] = true
			installedNames[namespace.IDToFSName(canonicalIDForEntry(e))] = true
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
			if installedNames[name] {
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
			if installedNames[name] {
				continue
			}
			staleRun := filepath.Join(runDir, name)
			_ = os.RemoveAll(staleRun)
			fmt.Printf("Removed stale run dir: %s\n", staleRun)
			staleRuns++
		}
	}

	if len(orphans) == 0 && orphanPointers == 0 && staleLaunchers == 0 && staleRuns == 0 {
		fmt.Println("No orphaned installations found.")
	}

	return nil
}

// symlinkIntact returns true if at least one symlink from the installation
// still exists on disk AND resolves to a live target. Returns false if all
// symlinks are missing (e.g. the user deleted the vendor config directory)
// or dangling (e.g. the run dir they point into was removed or re-keyed) —
// either way the installation needs re-planting against the current run dir.
func symlinkIntact(inst *symlink.Installation) bool {
	for _, entry := range inst.Symlinks {
		info, err := os.Lstat(entry.Link)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		// Stat follows the link: an error here means the target is gone —
		// a dangling link is not intact.
		if _, err := os.Stat(entry.Link); err == nil {
			return true
		}
	}
	return false
}
