package main

import (
	"fmt"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/registry"
	"github.com/eyelock/ynh/internal/resolver"
)

func cmdRegistry(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ynh registry <add|list|remove|update> [args]")
	}

	switch args[0] {
	case "add":
		return cmdRegistryAdd(args[1:])
	case "list", "ls":
		return cmdRegistryList()
	case "remove", "rm":
		return cmdRegistryRemove(args[1:])
	case "update":
		return cmdRegistryUpdate()
	default:
		return fmt.Errorf("unknown registry subcommand: %s\nusage: ynh registry <add|list|remove|update>", args[0])
	}
}

func cmdRegistryAdd(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: ynh registry add <url>")
	}

	url := args[0]

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Check for duplicates
	for _, r := range cfg.Registries {
		if r.URL == url {
			return fmt.Errorf("registry %q already configured", url)
		}
	}

	cfg.Registries = append(cfg.Registries, config.RegistrySource{URL: url})
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Printf("Added registry: %s\n", url)
	return nil
}

func cmdRegistryList() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if len(cfg.Registries) == 0 {
		fmt.Println("No registries configured.")
		fmt.Println("Add one with: ynh registry add <url>")
		return nil
	}

	for _, r := range cfg.Registries {
		if r.Ref != "" {
			fmt.Printf("  %s (ref: %s)\n", r.URL, r.Ref)
		} else {
			fmt.Printf("  %s\n", r.URL)
		}
	}

	return nil
}

func cmdRegistryRemove(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: ynh registry remove <url>")
	}

	url := args[0]

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	found := false
	var remaining []config.RegistrySource
	for _, r := range cfg.Registries {
		if r.URL == url {
			found = true
		} else {
			remaining = append(remaining, r)
		}
	}

	if !found {
		return fmt.Errorf("registry %q not found", url)
	}

	cfg.Registries = remaining
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Printf("Removed registry: %s\n", url)
	return nil
}

func cmdRegistryUpdate() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if len(cfg.Registries) == 0 {
		fmt.Println("No registries configured.")
		return nil
	}

	for _, src := range cfg.Registries {
		result, err := resolver.EnsureRepo(src.URL, src.Ref)
		if err != nil {
			fmt.Printf("  %s: error: %v\n", src.URL, err)
			continue
		}

		// Try to load and validate the registry
		reg, err := registry.LoadFromDir(result.Path)
		if err != nil {
			fmt.Printf("  %s: updated but invalid: %v\n", src.URL, err)
			continue
		}

		status := "up to date"
		if result.Cloned {
			status = "cloned"
		} else if result.Changed {
			status = "updated"
		}
		fmt.Printf("  %s (%s, %d entries)\n", reg.Name, status, len(reg.Entries))
	}

	return nil
}
