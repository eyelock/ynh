package main

import (
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/eyelock/ynh/internal/config"
)

func TestCmdRegistryAddAndList(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())

	// Initialize config
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{DefaultVendor: "claude"}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	// Add a registry
	err := cmdRegistryAdd([]string{"github.com/test/registry"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	// Verify config was updated
	cfg, err = config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Registries) != 1 {
		t.Fatalf("registries = %d, want 1", len(cfg.Registries))
	}
	if cfg.Registries[0].URL != "github.com/test/registry" {
		t.Errorf("url = %q", cfg.Registries[0].URL)
	}

	// List should work
	err = cmdRegistryList(nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
}

func TestCmdRegistryAddDuplicate(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())

	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		DefaultVendor: "claude",
		Registries:    []config.RegistrySource{{URL: "github.com/test/registry"}},
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	err := cmdRegistryAdd([]string{"github.com/test/registry"})
	if err == nil {
		t.Fatal("expected error for duplicate")
	}
}

func TestCmdRegistryRemove(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())

	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		DefaultVendor: "claude",
		Registries: []config.RegistrySource{
			{URL: "github.com/test/reg1"},
			{URL: "github.com/test/reg2"},
		},
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	err := cmdRegistryRemove([]string{"github.com/test/reg1"})
	if err != nil {
		t.Fatalf("remove: %v", err)
	}

	cfg, err = config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Registries) != 1 {
		t.Fatalf("registries = %d, want 1", len(cfg.Registries))
	}
	if cfg.Registries[0].URL != "github.com/test/reg2" {
		t.Errorf("remaining url = %q", cfg.Registries[0].URL)
	}
}

func TestCmdRegistryRemoveNotFound(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())

	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{DefaultVendor: "claude"}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	err := cmdRegistryRemove([]string{"github.com/test/nonexistent"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCmdRegistryListEmpty(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())

	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{DefaultVendor: "claude"}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	// Should not error, just print message
	err := cmdRegistryList(nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
}

func TestCmdRegistryMissingArgs(t *testing.T) {
	err := cmdRegistry([]string{})
	if err == nil {
		t.Fatal("expected error")
	}

	err = cmdRegistryAdd([]string{})
	if err == nil {
		t.Fatal("expected error for add without url")
	}

	err = cmdRegistryRemove([]string{})
	if err == nil {
		t.Fatal("expected error for remove without url")
	}
}

func TestCmdRegistryUnknownSubcommand(t *testing.T) {
	// Ensure HOME dir exists for config loading
	t.Setenv("YNH_HOME", t.TempDir())
	_ = os.MkdirAll(config.HomeDir(), 0o755)

	err := cmdRegistry([]string{"destroy"})
	if err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
}

func TestCmdRegistryListJSON(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())

	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		DefaultVendor: "claude",
		Registries: []config.RegistrySource{
			{URL: "github.com/org/registry-a"},
			{URL: "github.com/org/registry-b", Ref: "v2"},
		},
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	// Capture stdout
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w

	err := cmdRegistryList([]string{"--format", "json"})

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = old

	if err != nil {
		t.Fatalf("list --format json: %v", err)
	}

	out, err2 := io.ReadAll(r)
	if err2 != nil {
		t.Fatal(err2)
	}

	var got []registryListEntry
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("invalid json: %v\noutput: %s", err, out)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	if got[0].URL != "github.com/org/registry-a" {
		t.Errorf("entry 0 url = %q", got[0].URL)
	}
	if got[1].URL != "github.com/org/registry-b" || got[1].Ref != "v2" {
		t.Errorf("entry 1 = %+v", got[1])
	}
}

func TestCmdRegistryListJSONEmpty(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())

	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{DefaultVendor: "claude"}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w

	err := cmdRegistryList([]string{"--format", "json"})

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = old

	if err != nil {
		t.Fatalf("list --format json empty: %v", err)
	}

	out, err2 := io.ReadAll(r)
	if err2 != nil {
		t.Fatal(err2)
	}

	var got []registryListEntry
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty array, got %d entries", len(got))
	}
}

func TestCmdRegistryListInvalidFormat(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{DefaultVendor: "claude"}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	err := cmdRegistryList([]string{"--format", "yaml"})
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
}
