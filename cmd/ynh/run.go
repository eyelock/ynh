package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/migration"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/resolver"
	"github.com/eyelock/ynh/internal/symlink"
	"github.com/eyelock/ynh/internal/vendor"
)

func cmdRun(args []string) error {
	if err := config.EnsureDirs(); err != nil {
		return fmt.Errorf("ensuring directories: %w", err)
	}

	ra, err := parseRunArgs(args)
	if err != nil {
		return err
	}

	// Mutual exclusivity: --harness-file + harness name
	if ra.HarnessFile != "" && ra.HarnessName != "" {
		return fmt.Errorf("cannot specify both a harness name and --harness-file")
	}

	// Agent mode short-circuits the vendor pipeline: no profile/focus merge,
	// no symlink install, no exec into vendor. Agent loop owns its own
	// assembly inside internal/agent.
	if ra.Agent {
		return runAgentMode(ra)
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

	// Resolve harness source.
	var p *harness.Harness
	var name string
	var harnessDir string

	switch {
	case ra.HarnessName != "":
		name = ra.HarnessName
		p, err = harness.LoadQualified(name)
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
	_ = name

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

	if profileName != "" {
		p, err = harness.ResolveProfile(p, profileName)
		if err != nil {
			return err
		}
	}

	prompt := ra.Prompt
	vendorArgs := ra.VendorArgs
	action := ra.Action

	vendorName, err := resolveVendor(ra.VendorFlag, p)
	if err != nil {
		return err
	}

	adapter, err := vendor.Get(vendorName)
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if len(p.Includes) > 0 {
		fmt.Fprintf(os.Stderr, "Resolving %d include(s)...\n", len(p.Includes))
	}
	resolved, err := resolver.ResolveFromCache(p, cfg)
	if err != nil {
		return fmt.Errorf("resolving includes: %w", err)
	}

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

	var content []resolver.ResolvedContent
	for _, r := range resolved {
		content = append(content, r.Content)
	}

	localContent := resolver.ResolvedContent{
		BasePath: harnessDir,
	}
	content = append(content, localContent)

	// Assemble vendor config into deterministic run dir.
	runDirName := p.Name
	if ra.HarnessFile != "" || ra.HarnessName == "" {
		h := fmt.Sprintf("%x", hashString(harnessDir))
		runDirName = "_inline-" + h[:8]
	}
	runDir := filepath.Join(config.RunDir(), runDirName)
	vendorRunDir := filepath.Join(runDir, vendorName)
	if info, err := os.Stat(vendorRunDir); err == nil && info.IsDir() {
		runDir = vendorRunDir
	} else {
		if err := assembler.AssembleTo(runDir, adapter, content); err != nil {
			return fmt.Errorf("assembling config: %w", err)
		}

		for _, del := range p.DelegatesTo {
			if err := cfg.CheckRemoteSource(del.Git); err != nil {
				return fmt.Errorf("delegate %q: %w", del.Git, err)
			}
		}

		if err := assembler.AssembleDelegates(runDir, adapter, p.DelegatesTo); err != nil {
			return fmt.Errorf("assembling delegates: %w", err)
		}

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

	if ra.Instructions != "" {
		extraArgs, err := adapter.ApplyRuntimeInstructions(runDir, ra.Instructions)
		if err != nil {
			return fmt.Errorf("applying runtime instructions: %w", err)
		}
		vendorArgs = append(vendorArgs, extraArgs...)
	}

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

		fmt.Fprintf(os.Stderr, "Launching %s...\n", adapter.CLIName())
		if prompt != "" {
			if ra.Interactive {
				return adapter.LaunchWithInitialPrompt(runDir, prompt, vendorArgs)
			}
			return adapter.LaunchNonInteractive(runDir, prompt, vendorArgs)
		}
		return adapter.LaunchInteractive(runDir, vendorArgs)
	}
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
// The launcher delegates to `ynh run <canonical-id>`.
func generateLauncher(name, canonicalID string) error {
	binDir := config.BinDir()
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}

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

// symlinkIntact returns true if at least one symlink from the installation
// still exists on disk.
func symlinkIntact(inst *symlink.Installation) bool {
	for _, entry := range inst.Symlinks {
		info, err := os.Lstat(entry.Link)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return true
		}
	}
	return false
}
