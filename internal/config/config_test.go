package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("YNH_HOME", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.DefaultVendor != "claude" {
		t.Errorf("DefaultVendor = %q, want %q", cfg.DefaultVendor, "claude")
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("YNH_HOME", "")

	cfg := &Config{
		DefaultVendor: "codex",
	}

	if err := cfg.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify file exists
	configPath := filepath.Join(dir, DefaultDirName, ConfigFile)
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("config file not created")
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.DefaultVendor != "codex" {
		t.Errorf("DefaultVendor = %q, want %q", loaded.DefaultVendor, "codex")
	}
}

func TestDirPaths(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("YNH_HOME", "")

	home := HomeDir()
	want := filepath.Join(dir, DefaultDirName)
	if home != want {
		t.Errorf("HomeDir() = %q, want %q", home, want)
	}

	if HarnessesDir() != filepath.Join(want, "harnesses") {
		t.Errorf("HarnessesDir() = %q, want %q", HarnessesDir(), filepath.Join(want, "harnesses"))
	}

	if CacheDir() != filepath.Join(want, "cache") {
		t.Errorf("CacheDir() = %q, want %q", CacheDir(), filepath.Join(want, "cache"))
	}

	if BinDir() != filepath.Join(want, "bin") {
		t.Errorf("BinDir() = %q, want %q", BinDir(), filepath.Join(want, "bin"))
	}

	if RunDir() != filepath.Join(want, "run") {
		t.Errorf("RunDir() = %q, want %q", RunDir(), filepath.Join(want, "run"))
	}
}

func TestYNHHomeEnvOverride(t *testing.T) {
	customHome := t.TempDir()
	t.Setenv("YNH_HOME", customHome)

	if HomeDir() != customHome {
		t.Errorf("HomeDir() = %q, want %q", HomeDir(), customHome)
	}

	if HarnessesDir() != filepath.Join(customHome, "harnesses") {
		t.Errorf("HarnessesDir() = %q, want %q", HarnessesDir(), filepath.Join(customHome, "harnesses"))
	}

	if CacheDir() != filepath.Join(customHome, "cache") {
		t.Errorf("CacheDir() = %q, want %q", CacheDir(), filepath.Join(customHome, "cache"))
	}

	if BinDir() != filepath.Join(customHome, "bin") {
		t.Errorf("BinDir() = %q, want %q", BinDir(), filepath.Join(customHome, "bin"))
	}

	if ConfigPath() != filepath.Join(customHome, ConfigFile) {
		t.Errorf("ConfigPath() = %q, want %q", ConfigPath(), filepath.Join(customHome, ConfigFile))
	}
}

func TestEnsureDirs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("YNH_HOME", filepath.Join(dir, "custom-ynh"))

	if err := EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs failed: %v", err)
	}

	// Verify all directories were created
	for _, sub := range []string{"", "harnesses", "cache", "bin", "run"} {
		path := filepath.Join(dir, "custom-ynh", sub)
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			t.Errorf("directory not created: %s", path)
		} else if !info.IsDir() {
			t.Errorf("not a directory: %s", path)
		}
	}
}

func TestEnsureDirsIdempotent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("YNH_HOME", filepath.Join(dir, "ynh"))

	// Call twice - should not error on second call
	if err := EnsureDirs(); err != nil {
		t.Fatalf("first EnsureDirs failed: %v", err)
	}
	if err := EnsureDirs(); err != nil {
		t.Fatalf("second EnsureDirs failed: %v", err)
	}
}

func TestSaveAndLoadWithYNHHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("YNH_HOME", dir)

	cfg := &Config{
		DefaultVendor: "cursor",
	}

	if err := cfg.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Config should be at YNH_HOME/config.json, not ~/.ynh/config.json
	configPath := filepath.Join(dir, ConfigFile)
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("config file not created at YNH_HOME location")
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if loaded.DefaultVendor != "cursor" {
		t.Errorf("DefaultVendor = %q, want %q", loaded.DefaultVendor, "cursor")
	}
}
