package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Version is the release version, injected by goreleaser via ldflags.
// "dev" when building from a developer checkout.
var Version = "dev"

// CapabilitiesVersion declares the TermQ-facing wire-contract version of this
// ynh build. Unlike Version it is a source constant, so developer builds
// (`make install` from any branch) honestly report what the contract supports
// without needing a release tag.
//
// Bump when the JSON shapes consumers decode, the commands they invoke, or
// the manifest fields they rely on change in a way that requires downstream
// code to adapt. Do NOT bump for internal refactors, bug fixes, or additive
// fields older clients can ignore.
//
// Consumers (e.g. TermQ) read this via `ynh version --format json` and gate
// features on it with their own `minimumYNHCapabilities` constant.
// 0.6.0: local model backends (`ynh backend`, `-v <backend>/<vendor>[/<model>]`
// redirection) — `ynh vendors` gained extra rows per configured backend
// connection.
//
// 0.7.0 (this branch): the surface refactor — `ynh agent run` collapsed into
// `ynh run --agent`, `ynh installed` into `ynh info --installed`, `ynh prune`
// into `ynh status --prune`, `ynd validate-output` into `ynd validate --schema`,
// `ynd marketplace build` into `ynd marketplace`, `ynd migrate` renamed to
// `ynd migrate-manifest`. `ynh profile {hook,mcp,include}` sub-trees folded
// into top-level `ynh {hook,mcp,include}` with `--profile <name>`. Flag
// renames: `--json` → `--format json`, `--output-dir` / `--to` → `-o, --output`,
// four `--clear-*` collapsed into `--clear <field>`.
const CapabilitiesVersion = "0.7.0"

// SchemaVersion declares the on-disk format version of the YNH home
// directory (~/.ynh). Distinct from CapabilitiesVersion: capabilities is
// the wire-contract version (what JSON shapes / commands this binary
// speaks), schema_version is the on-disk format version of the user's
// YNH home that this binary expects.
//
// Bumped when the on-disk layout of installed/, harnesses/, cache/, or
// any associated metadata file changes in a way that requires migration.
//
// Schema 1: name-keyed pointer files, host-stripped namespaces
// (`<org>/<repo>`), no `id` field on disk — `id` is derived on-read.
//
// Future schema 2 (planned in PR-canonical-2/3): id-keyed pointer files
// at `~/.ynh/installed/<host--org--repo--name>.json`, canonical id
// stored explicitly in installed.json, host-prefixed namespaces.
//
// Emitted as a top-level sibling on harness-centric envelopes (ls, info,
// search, fork) so consumers detect format-level capability in the same
// round-trip they use for listing.
const SchemaVersion = 1

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

// Source points to a local directory tree containing harnesses.
type Source struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Description string `json:"description,omitempty"`
}

// BackendConnection holds the connection details for redirecting one vendor
// CLI at one local model backend (e.g. Ollama) instead of its default cloud
// API. Keyed two levels deep in Config.Backends — backend name, then vendor
// name — because the connection details (notably base_url's wire format)
// differ per vendor even against the same backend server. The model is
// deliberately not stored here: it's supplied per invocation via the vendor
// spec string (e.g. "-v ollama/claude/qwen3"), not pinned in config.
type BackendConnection struct {
	BaseURL   string            `json:"base_url"`
	AuthToken string            `json:"auth_token,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
}

// BackendDef declares one local model backend: what kind of server it is
// (Type, used to decide how to query it for installed models) and its
// per-vendor connection details.
type BackendDef struct {
	// Type identifies the backend server so ynh knows how to query it for
	// installed models — e.g. "ollama" queries Ollama's native /api/tags.
	// Empty (or unrecognized) means model discovery isn't available; the
	// backend still works for launching, just without live enumeration.
	Type    string                       `json:"type,omitempty"`
	Vendors map[string]BackendConnection `json:"vendors"`
}

type Config struct {
	DefaultVendor        string                `json:"default_vendor,omitempty"`
	AllowedRemoteSources []string              `json:"allowed_remote_sources,omitempty"`
	Registries           []RegistrySource      `json:"registries,omitempty"`
	Sources              []Source              `json:"sources,omitempty"`
	Backends             map[string]BackendDef `json:"backends,omitempty"`
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

// PointersDir is where pointer files for local-fork installs live.
// Each file is <name>.json and points at a user-owned source tree —
// edits to that tree are live to ynh run with no copy step.
//
// Distinct from HarnessesDir(): tree-shaped installs (git/registry) live
// under HarnessesDir(); pointer-shaped installs (local forks) live here.
func PointersDir() string {
	return filepath.Join(HomeDir(), "installed")
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
		PointersDir(),
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
