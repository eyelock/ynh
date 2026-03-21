package main

import (
	"testing"

	"github.com/eyelock/ynh/internal/config"
)

func TestCmdSearchNoRegistries(t *testing.T) {
	t.Setenv("YNH_HOME", t.TempDir())
	if err := config.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{DefaultVendor: "claude"}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	err := cmdSearch([]string{"something"})
	if err == nil {
		t.Fatal("expected error for no registries")
	}
}

func TestCmdSearchMissingArgs(t *testing.T) {
	err := cmdSearch([]string{})
	if err == nil {
		t.Fatal("expected error for missing search term")
	}
}
