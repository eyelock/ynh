package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/persona"
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
		err = cmdList()
	case "vendors":
		cmdVendors()
	case "status":
		err = cmdStatus()
	case "prune":
		err = cmdPrune()
	case "version", "--version":
		fmt.Printf("ynh %s\n", config.Version)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Printf(`ynh - ynh persona manager (%s)

Usage:
  ynh <command> [arguments]

Commands:
  init                       Show ynh home path and setup instructions
  install <source> [--path <subdir>]  Install a persona from Git URL or local path
  uninstall <name>           Remove an installed persona and its launcher
  update <name>              Refresh cached Git repos for a persona
  run <name> [flags] [prompt]  Launch a persona session
  ls                           List installed personas
  vendors                      List supported vendor adapters
  status                       Show symlink installations across projects
  prune                        Clean orphaned symlink installations
  version                      Print version
  help                         Show this help

Run flags:
  -v <vendor>                  Override vendor (claude, codex, cursor)
  --install                    Install symlinks for the vendor in current project
  --clean                      Remove symlinks for the vendor in current project
  All other flags are passed through to the vendor CLI.
  Use -- to separate vendor flags from the prompt.

Examples:
  ynh init
  ynh install github.com/david/my-persona
  ynh install ./my-local-persona
  ynh install github.com/org/monorepo --path personas/david
  ynh run david
  ynh run david "review this PR"
  ynh run david -v codex
  ynh run david --model opus -- "fix this bug"
  ynh run david -v codex -- "refactor auth"
  ynh run david -v cursor --install
  ynh run david -v cursor --clean
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

	// Determine if local path or Git URL
	var srcDir string
	var cloneTmpDir string // track for cleanup
	if isLocalPath(source) {
		absPath, err := filepath.Abs(source)
		if err != nil {
			return err
		}
		srcDir = absPath
	} else {
		// Check remote source against allow-list
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		if err := cfg.CheckRemoteSource(source); err != nil {
			return err
		}

		// Resolve from Git
		repoPath, err := cloneForInstall(source)
		if err != nil {
			return err
		}
		cloneTmpDir = repoPath
		srcDir = repoPath
	}

	// Scope to subdirectory if --path was specified
	if pathFlag != "" {
		srcDir = filepath.Join(srcDir, pathFlag)
		if _, err := os.Stat(srcDir); os.IsNotExist(err) {
			if cloneTmpDir != "" {
				_ = os.RemoveAll(cloneTmpDir)
			}
			return fmt.Errorf("path %q not found in source", pathFlag)
		}
	}

	// Load persona from plugin format
	p, err := persona.LoadPluginDir(srcDir)
	if err != nil {
		if cloneTmpDir != "" {
			_ = os.RemoveAll(cloneTmpDir)
		}
		return err
	}

	// Reserved name: "ynh" can be installed but gets no launcher script
	// (it would overwrite the ynh binary in ~/.ynh/bin/).
	// Users invoke it with: ynh run ynh
	reservedName := p.Name == "ynh"

	// Copy persona to installed directory (clean first to remove stale artifacts)
	installDir := persona.InstalledDir(p.Name)
	if err := os.RemoveAll(installDir); err != nil {
		return fmt.Errorf("cleaning install dir: %w", err)
	}
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return err
	}

	if err := copyTree(srcDir, installDir); err != nil {
		return err
	}

	// Clean up temp clone directory (only set for Git installs)
	if cloneTmpDir != "" {
		_ = os.RemoveAll(cloneTmpDir)
	}

	// Generate launcher script (skip for reserved names that conflict with the binary)
	if !reservedName {
		if err := generateLauncher(p.Name); err != nil {
			return err
		}
	}

	fmt.Printf("Installed persona %q\n", p.Name)
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
		return fmt.Errorf("usage: ynh uninstall <persona-name>")
	}

	name := args[0]

	// Verify persona exists (either format)
	installDir := persona.InstalledDir(name)
	if persona.DetectFormat(installDir) == "" {
		return fmt.Errorf("persona %q is not installed", name)
	}

	// Remove persona directory
	if err := os.RemoveAll(installDir); err != nil {
		return fmt.Errorf("removing persona: %w", err)
	}

	// Remove launcher script
	launcherPath := filepath.Join(config.BinDir(), name)
	_ = os.Remove(launcherPath) // ignore error if launcher doesn't exist

	// Remove run directory
	runDir := filepath.Join(config.RunDir(), name)
	_ = os.RemoveAll(runDir) // ignore error if not present

	fmt.Printf("Uninstalled persona %q\n", name)
	return nil
}

func cmdUpdate(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: ynh update <persona-name>")
	}

	name := args[0]

	p, err := persona.Load(name)
	if err != nil {
		return fmt.Errorf("persona %q not found: %w", name, err)
	}

	if len(p.Includes) == 0 && len(p.DelegatesTo) == 0 {
		fmt.Printf("Persona %q has no Git sources to update.\n", name)
		return nil
	}

	// Load config for remote source checking
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	checked := 0
	updated := 0
	for _, inc := range p.Includes {
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
		if result.Changed {
			updated++
			fmt.Printf("  Updated.\n")
		} else {
			fmt.Printf("  Already up to date.\n")
		}
	}

	fmt.Printf("Checked %d source(s) for persona %q, %d updated.\n", checked, name, updated)
	return nil
}

func cmdRun(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: ynh run <persona-name> [-v vendor] [vendor-flags...] [prompt]")
	}

	if err := config.EnsureDirs(); err != nil {
		return err
	}

	name := args[0]
	vendorFlag, prompt, vendorArgs, action := parseRunArgs(args[1:])

	// Load persona
	p, err := persona.Load(name)
	if err != nil {
		return fmt.Errorf("persona %q not found: %w", name, err)
	}

	// Determine vendor
	vendorName, err := resolveVendor(vendorFlag, p)
	if err != nil {
		return err
	}

	adapter, err := vendor.Get(vendorName)
	if err != nil {
		return err
	}

	// Show progress when there are Git sources to resolve
	sources := len(p.Includes) + len(p.DelegatesTo)
	if sources > 0 {
		fmt.Fprintf(os.Stderr, "Assembling %d source(s)...\n", sources)
	}

	// Load config for remote source checking
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Resolve Git includes (with remote source allow-list check)
	content, err := resolver.Resolve(p, cfg)
	if err != nil {
		return fmt.Errorf("resolving includes: %w", err)
	}

	// Also include any local content from the persona's installed directory
	localContent := resolver.ResolvedContent{
		BasePath: persona.InstalledDir(name),
	}
	content = append(content, localContent)

	// Assemble vendor config into deterministic run dir.
	// We use a stable path instead of a temp dir because syscall.Exec
	// replaces this process — deferred cleanup would never run.
	runDir := filepath.Join(config.RunDir(), name)
	if err := assembler.AssembleTo(runDir, adapter, content); err != nil {
		return fmt.Errorf("assembling config: %w", err)
	}

	// Check delegates against remote source allow-list
	for _, del := range p.DelegatesTo {
		if err := cfg.CheckRemoteSource(del.Git); err != nil {
			return fmt.Errorf("delegate %q: %w", del.Git, err)
		}
	}

	// Assemble delegate personas as agent files
	if err := assembler.AssembleDelegates(runDir, adapter, p.DelegatesTo); err != nil {
		return fmt.Errorf("assembling delegates: %w", err)
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
		if prompt != "" {
			return adapter.LaunchNonInteractive(runDir, prompt, vendorArgs)
		}
		return adapter.LaunchInteractive(runDir, vendorArgs)
	}
}

func cmdList() error {
	names, err := persona.List()
	if err != nil {
		return err
	}

	if len(names) == 0 {
		fmt.Println("No personas installed.")
		fmt.Println("Install one with: ynh install <git-url|path>")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tVENDOR\tINCLUDES\tDELEGATES TO")

	for _, name := range names {
		p, err := persona.Load(name)
		if err != nil {
			_, _ = fmt.Fprintf(w, "%s\t(error: %v)\t\t\n", name, err)
			continue
		}

		vendorName := p.DefaultVendor
		if vendorName == "" {
			vendorName = "-"
		}

		includes := formatSources(len(p.Includes), gitURLs(p.Includes, func(i persona.Include) string { return i.Git }))
		delegates := formatSources(len(p.DelegatesTo), gitURLs(p.DelegatesTo, func(d persona.Delegate) string { return d.Git }))

		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", p.Name, vendorName, includes, delegates)
	}

	_ = w.Flush()
	return nil
}

// gitURLs extracts Git URLs from a slice using the given accessor.
func gitURLs[T any](items []T, getGit func(T) string) []string {
	urls := make([]string, 0, len(items))
	for _, item := range items {
		urls = append(urls, getGit(item))
	}
	return urls
}

// formatSources formats a count with abbreviated git URLs.
func formatSources(count int, urls []string) string {
	if count == 0 {
		return "0"
	}
	short := make([]string, 0, len(urls))
	for _, u := range urls {
		short = append(short, shortGitURL(u))
	}
	return strings.Join(short, ", ")
}

// shortGitURL abbreviates a git URL for display.
// "github.com/eyelock/ynh" -> "eyelock/ynh"
// "/tmp/ynh-walkthrough/foo" -> "/tmp/ynh-walkthrough/foo"
func shortGitURL(url string) string {
	// Local paths: keep as-is
	if strings.HasPrefix(url, "/") || strings.HasPrefix(url, ".") {
		return url
	}
	// Strip host prefix: "github.com/user/repo" -> "user/repo"
	parts := strings.SplitN(url, "/", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return url
}

func cmdVendors() {
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tCLI\tCONFIG DIR")

	for _, name := range vendor.Available() {
		adapter, _ := vendor.Get(name)
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n", adapter.Name(), adapter.CLIName(), adapter.ConfigDir())
	}

	_ = w.Flush()
}

// resolveVendor picks the vendor: CLI flag > persona default > global config.
func resolveVendor(flag string, p *persona.Persona) (string, error) {
	if flag != "" {
		return flag, nil
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

	return "", fmt.Errorf("no vendor specified (use -v flag, persona default_vendor, or global config)")
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
func parseRunArgs(args []string) (vendorFlag, prompt string, vendorArgs []string, action string) {
	flagArgs := args

	// First pass: find -- separator and extract prompt
	for i, arg := range args {
		if arg == "--" {
			flagArgs = args[:i]
			if i+1 < len(args) {
				prompt = args[i+1]
			}
			break
		}
	}

	// Second pass: process flags
	for i := 0; i < len(flagArgs); i++ {
		switch {
		case flagArgs[i] == "-v" && i+1 < len(flagArgs):
			vendorFlag = flagArgs[i+1]
			i++
		case flagArgs[i] == "--install":
			action = "install"
		case flagArgs[i] == "--clean":
			action = "clean"
		case !strings.HasPrefix(flagArgs[i], "-"):
			if prompt == "" {
				// No -- separator found; first non-flag arg is the prompt
				prompt = flagArgs[i]
			} else {
				// -- separator found; non-flag args before it are flag values
				vendorArgs = append(vendorArgs, flagArgs[i])
			}
		default:
			vendorArgs = append(vendorArgs, flagArgs[i])
		}
	}
	return
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

func cmdInstallVendor(adapter vendor.Adapter, stagingDir string, personaName string) error {
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
		log.Record(personaName, adapter.Name(), projectDir, entries)
		if err := log.Save(); err != nil {
			return err
		}
		fmt.Printf("Installed %d symlinks for %s (%s) in %s:\n\n", len(entries), personaName, adapter.Name(), projectDir)
		for _, entry := range entries {
			rel, _ := filepath.Rel(projectDir, entry.Link)
			fmt.Printf("  %s -> %s\n", rel, entry.Target)
		}
	}
	return nil
}

func cmdCleanVendor(adapter vendor.Adapter, personaName string) error {
	projectDir, err := os.Getwd()
	if err != nil {
		return err
	}

	log, err := symlink.LoadLog()
	if err != nil {
		return err
	}

	installation := log.FindInstallation(personaName, adapter.Name(), projectDir)
	if installation == nil {
		fmt.Printf("No %s installation found for persona %q in %s\n", adapter.Name(), personaName, projectDir)
		return nil
	}

	if err := adapter.Clean(installation.Symlinks); err != nil {
		return err
	}

	log.RemoveInstallation(personaName, adapter.Name(), projectDir)
	if err := log.Save(); err != nil {
		return err
	}

	fmt.Printf("Cleaned %s symlinks for persona %q in %s\n", adapter.Name(), personaName, projectDir)
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
	_, _ = fmt.Fprintln(w, "PERSONA\tVENDOR\tPROJECT\tSYMLINKS")

	for _, inst := range log.Installations {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%d\n", inst.Persona, inst.Vendor, inst.Project, len(inst.Symlinks))
	}

	return w.Flush()
}

func cmdPrune() error {
	log, err := symlink.LoadLog()
	if err != nil {
		return err
	}

	orphans := log.Prune()
	if len(orphans) == 0 {
		fmt.Println("No orphaned installations found.")
		return nil
	}

	for _, inst := range orphans {
		fmt.Printf("Removing orphaned installation: %s (%s) in %s\n", inst.Persona, inst.Vendor, inst.Project)
	}

	log.RemoveOrphans(orphans)
	return log.Save()
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

func cloneForInstall(gitURL string) (string, error) {
	fullURL := resolver.NormalizeGitURL(gitURL)

	tmpDir, err := os.MkdirTemp("", "ynh-install-*")
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
