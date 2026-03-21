package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

var Version = "dev"

const (
	DefaultDirName = ".ynh"
	ConfigFile     = "config.json"
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
	home, _ := os.UserHomeDir()
	return filepath.Join(home, DefaultDirName)
}

func PersonasDir() string {
	return filepath.Join(HomeDir(), "personas")
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

// EnsureDirs creates the ynh directory structure if it doesn't exist.
func EnsureDirs() error {
	dirs := []string{
		HomeDir(),
		PersonasDir(),
		CacheDir(),
		BinDir(),
		RunDir(),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func Load() (*Config, error) {
	cfg := &Config{
		DefaultVendor: "claude",
	}

	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) Save() error {
	if err := os.MkdirAll(HomeDir(), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(ConfigPath(), data, 0o644)
}
