package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/eyelock/ynh/internal/backend"
	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/vendor"
)

func cmdBackend(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: ynh backend <add|list|remove> [args]")
	}

	switch args[0] {
	case "add":
		return cmdBackendAdd(args[1:])
	case "list", "ls":
		return cmdBackendList(args[1:])
	case "remove", "rm":
		return cmdBackendRemove(args[1:])
	default:
		return fmt.Errorf("unknown backend subcommand: %s\nusage: ynh backend <add|list|remove>", args[0])
	}
}

func cmdBackendAdd(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: ynh backend add <name> <vendor> --base-url <url> [--auth-token <token>] [--type <type>] [--env KEY=VALUE]")
	}
	name := args[0]
	vendorName := args[1]
	rest := args[2:]

	var baseURL, authToken, backendType string
	env := map[string]string{}
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--base-url":
			if i+1 >= len(rest) {
				return fmt.Errorf("--base-url requires a value")
			}
			i++
			baseURL = rest[i]
		case "--auth-token":
			if i+1 >= len(rest) {
				return fmt.Errorf("--auth-token requires a value")
			}
			i++
			authToken = rest[i]
		case "--type":
			if i+1 >= len(rest) {
				return fmt.Errorf("--type requires a value")
			}
			i++
			backendType = rest[i]
		case "--env":
			if i+1 >= len(rest) {
				return fmt.Errorf("--env requires a KEY=VALUE value")
			}
			i++
			kv := rest[i]
			eq := strings.IndexByte(kv, '=')
			if eq < 0 {
				return fmt.Errorf("--env value %q must be KEY=VALUE", kv)
			}
			env[kv[:eq]] = kv[eq+1:]
		default:
			return fmt.Errorf("unknown flag: %s", rest[i])
		}
	}

	if baseURL == "" {
		return fmt.Errorf("--base-url is required")
	}
	if _, err := vendor.Get(vendorName); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if cfg.Backends == nil {
		cfg.Backends = map[string]config.BackendDef{}
	}

	def, exists := cfg.Backends[name]
	if !exists {
		def = config.BackendDef{Vendors: map[string]config.BackendConnection{}}
	}
	if def.Vendors == nil {
		def.Vendors = map[string]config.BackendConnection{}
	}
	if _, dup := def.Vendors[vendorName]; dup {
		return fmt.Errorf("backend %q already has a connection for vendor %q; remove it first with: ynh backend remove %s %s", name, vendorName, name, vendorName)
	}
	if backendType != "" {
		def.Type = backendType
	}

	conn := config.BackendConnection{BaseURL: baseURL, AuthToken: authToken}
	if len(env) > 0 {
		conn.Env = env
	}
	def.Vendors[vendorName] = conn
	cfg.Backends[name] = def

	if err := cfg.Save(); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Printf("Added backend %q for vendor %q (base_url=%s)\n", name, vendorName, baseURL)
	fmt.Printf("Use it with: ynh run -v %s/%s\n", name, vendorName)
	return nil
}

type backendListEntry struct {
	Backend      string   `json:"backend"`
	Vendor       string   `json:"vendor"`
	Type         string   `json:"type,omitempty"`
	BaseURL      string   `json:"base_url"`
	HasAuthToken bool     `json:"has_auth_token"`
	Models       []string `json:"models,omitempty"`
}

func cmdBackendList(args []string) error {
	format := "text"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--format":
			if i+1 >= len(args) {
				return fmt.Errorf("--format requires a value")
			}
			i++
			format = args[i]
		default:
			return fmt.Errorf("unknown flag: %s", args[i])
		}
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	var backendNames []string
	for name := range cfg.Backends {
		backendNames = append(backendNames, name)
	}
	sort.Strings(backendNames)

	var entries []backendListEntry
	for _, name := range backendNames {
		def := cfg.Backends[name]
		// Best-effort: an unreachable/untyped backend just lists without models.
		models, _ := backend.ListModels(cfg, name)

		var vendorNames []string
		for vn := range def.Vendors {
			vendorNames = append(vendorNames, vn)
		}
		sort.Strings(vendorNames)

		for _, vn := range vendorNames {
			conn := def.Vendors[vn]
			entries = append(entries, backendListEntry{
				Backend:      name,
				Vendor:       vn,
				Type:         def.Type,
				BaseURL:      conn.BaseURL,
				HasAuthToken: conn.AuthToken != "",
				Models:       models,
			})
		}
	}

	switch format {
	case "json":
		if entries == nil {
			entries = []backendListEntry{}
		}
		data, err := json.MarshalIndent(entries, "", "  ")
		if err != nil {
			return fmt.Errorf("encoding json: %w", err)
		}
		_, err = fmt.Fprintf(os.Stdout, "%s\n", data)
		return err
	case "text":
		if len(entries) == 0 {
			fmt.Println("No backends configured.")
			fmt.Println("Add one with: ynh backend add <name> <vendor> --base-url <url>")
			return nil
		}
		for _, e := range entries {
			spec := e.Backend + "/" + e.Vendor
			typ := e.Type
			if typ == "" {
				typ = "-"
			}
			if len(e.Models) > 0 {
				fmt.Printf("  %-30s type=%-8s base_url=%-35s %d model(s) installed\n", spec, typ, e.BaseURL, len(e.Models))
			} else {
				fmt.Printf("  %-30s type=%-8s base_url=%s\n", spec, typ, e.BaseURL)
			}
		}
		return nil
	default:
		return fmt.Errorf("invalid --format value %q (want text or json)", format)
	}
}

func cmdBackendRemove(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: ynh backend remove <name> [<vendor>]")
	}
	name := args[0]

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	def, ok := cfg.Backends[name]
	if !ok {
		return fmt.Errorf("backend %q not found", name)
	}

	if len(args) >= 2 {
		vendorName := args[1]
		if _, ok := def.Vendors[vendorName]; !ok {
			return fmt.Errorf("backend %q has no connection for vendor %q", name, vendorName)
		}
		delete(def.Vendors, vendorName)
		if len(def.Vendors) == 0 {
			delete(cfg.Backends, name)
		} else {
			cfg.Backends[name] = def
		}
		if err := cfg.Save(); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}
		fmt.Printf("Removed backend %q vendor %q\n", name, vendorName)
		return nil
	}

	delete(cfg.Backends, name)
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}
	fmt.Printf("Removed backend %q\n", name)
	return nil
}
