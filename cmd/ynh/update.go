package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/eyelock/ynh/internal/assembler"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/harness"
	"github.com/eyelock/ynh/internal/plugin"
	"github.com/eyelock/ynh/internal/resolver"
)

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

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	checked := 0
	updated := 0
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
				newSrcDir := result.Path
				if p.InstalledFrom.Path != "" {
					newSrcDir = filepath.Join(result.Path, p.InstalledFrom.Path)
				}
				if _, statErr := os.Stat(newSrcDir); statErr == nil {
					if err := assembler.CopyDir(newSrcDir, p.Dir); err != nil {
						fmt.Fprintf(os.Stderr, "  Warning: copying refreshed harness: %v\n", err)
					}
					if reloaded, reloadErr := harness.LoadDir(p.Dir); reloadErr == nil {
						p = reloaded
					}
				}
			} else if shaAdvanced {
				updated++
				fmt.Printf("  Updated.\n")
			} else {
				fmt.Printf("  Already up to date.\n")
			}
		}
	}
	resolvedSources := make([]plugin.ResolvedSourceJSON, 0, len(p.Includes)+len(p.DelegatesTo))
	for _, inc := range p.Includes {
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
