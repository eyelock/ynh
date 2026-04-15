package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

var Version = "dev"

const (
	DefaultDirName = ".ynh"
	ConfigFile     = "config.json"
	DefaultVendor  = "claude"
)

// RegistrySource points to a Git repo containing a registry.json.
type RegistrySource struct {
	URL string `json:"url"`
	Ref string `json:"ref,omitempty"`
}

type Config struct {
	DefaultVendor        string           `json:"default_vendor,omitempty"`
	AllowedRemoteSources []string         `json:"allowed_remote_sources,omitempty"`
	Registries           []RegistrySource `json:"registries,omitempty"`
}

// HomeDir returns the ynh home directory.
// Uses YNH_HOME env var if set, otherwise ~/.ynh.
func HomeDir() string {
	if env := os.Getenv("YNH_HOME"); env != "" {
		return env
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, DefaultDirName)
}

func HarnessesDir() string {
	return filepath.Join(HomeDir(), "harnesses")
}

func CacheDir() string {
	return filepath.Join(HomeDir(), "cache")
}

func BinDir() string {
	return filepath.Join(HomeDir(), "bin")
}

func RunDir() string {
	return filepath.Join(HomeDir(), "run")
}

func ConfigPath() string {
	return filepath.Join(HomeDir(), ConfigFile)
}

// SymlinksPath returns the path to the symlink installation log.
// Kept here alongside the other path accessors so every ynh-resolved path
// has a single, authoritative home.
func SymlinksPath() string {
	return filepath.Join(HomeDir(), "symlinks.json")
}

// EnsureDirs creates the ynh directory structure if it doesn't exist.
func EnsureDirs() error {
	dirs := []string{
		HomeDir(),
		HarnessesDir(),
		CacheDir(),
		BinDir(),
		RunDir(),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating directory %s: %w", dir, err)
		}
	}
	return nil
}

func Load() (*Config, error) {
	cfg := &Config{
		DefaultVendor: DefaultVendor,
	}

	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	return cfg, nil
}

func (c *Config) Save() error {
	if err := os.MkdirAll(HomeDir(), 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling config: %w", err)
	}

	if err := os.WriteFile(ConfigPath(), data, 0o644); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	return nil
}
