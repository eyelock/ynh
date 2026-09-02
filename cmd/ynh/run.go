// `ynh run` — assemble a harness and launch the vendor CLI.

package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/backend"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/migration"
	"github.com/eyelock/ynh/internal/namespace"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/resolver"
	"github.com/eyelock/ynh/internal/symlink"
	"github.com/eyelock/ynh/internal/vendor"
)

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

// initialPrompter is an optional capability interface for vendors that support
// starting an interactive session with an initial prompt pre-loaded.
type initialPrompter interface {
	SupportsInitialPrompt() bool
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
