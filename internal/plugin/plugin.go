package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// PluginJSON represents the Claude Code plugin.json schema.
// Only fields that Claude Code recognizes are included.
type PluginJSON struct {
	Name        string      `json:"name"`
	Version     string      `json:"version"`
	Description string      `json:"description,omitempty"`
	Author      *AuthorInfo `json:"author,omitempty"`
	Keywords    []string    `json:"keywords,omitempty"`
}

// AuthorInfo holds plugin author information.
type AuthorInfo struct {
	Name string `json:"name"`
}

// MetadataJSON represents the metadata.json sidecar file.
// The "ynh" key holds ynh-specific configuration.
type MetadataJSON struct {
	YNH *YNHMetadata `json:"ynh,omitempty"`
}

// YNHMetadata holds ynh-specific persona configuration.
type YNHMetadata struct {
	DefaultVendor string          `json:"default_vendor,omitempty"`
	Includes      []IncludeMeta   `json:"includes,omitempty"`
	DelegatesTo   []DelegateMeta  `json:"delegates_to,omitempty"`
	InstalledFrom *ProvenanceMeta `json:"installed_from,omitempty"`
}

// ProvenanceMeta records where a persona was installed from.
type ProvenanceMeta struct {
	SourceType   string `json:"source_type"`
	Source       string `json:"source"`
	Path         string `json:"path,omitempty"`
	RegistryName string `json:"registry_name,omitempty"`
	InstalledAt  string `json:"installed_at"`
}

// IncludeMeta is the JSON representation of a Git include.
type IncludeMeta struct {
	Git  string   `json:"git"`
	Ref  string   `json:"ref,omitempty"`
	Path string   `json:"path,omitempty"`
	Pick []string `json:"pick,omitempty"`
}

// DelegateMeta is the JSON representation of a delegate reference.
type DelegateMeta struct {
	Git  string `json:"git"`
	Ref  string `json:"ref,omitempty"`
	Path string `json:"path,omitempty"`
}

// IsPluginDir returns true if the directory contains a Claude Code plugin manifest.
func IsPluginDir(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	return err == nil
}

// LoadPluginJSON reads and parses .claude-plugin/plugin.json from dir.
func LoadPluginJSON(dir string) (*PluginJSON, error) {
	data, err := os.ReadFile(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	if err != nil {
		return nil, fmt.Errorf("reading plugin.json: %w", err)
	}

	var pj PluginJSON
	if err := json.Unmarshal(data, &pj); err != nil {
		return nil, fmt.Errorf("parsing plugin.json: %w", err)
	}

	if pj.Name == "" {
		return nil, fmt.Errorf("plugin.json missing required field: name")
	}

	return &pj, nil
}

// LoadMetadataJSON reads and parses metadata.json from dir.
// Returns nil with no error if the file doesn't exist.
func LoadMetadataJSON(dir string) (*MetadataJSON, error) {
	data, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading metadata.json: %w", err)
	}

	var meta MetadataJSON
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parsing metadata.json: %w", err)
	}

	return &meta, nil
}

// SaveMetadataJSON merges ynh metadata into the existing metadata.json at dir,
// preserving any non-ynh keys. Creates the file if it doesn't exist.
func SaveMetadataJSON(dir string, ynh *YNHMetadata) error {
	path := filepath.Join(dir, "metadata.json")

	// Read existing file into a raw map to preserve non-ynh keys.
	existing := make(map[string]any)
	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &existing); err != nil {
			return fmt.Errorf("parsing existing metadata.json: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("reading metadata.json: %w", err)
	}

	// Marshal the typed YNHMetadata to a raw value, then inject into the map.
	ynhBytes, err := json.Marshal(ynh)
	if err != nil {
		return fmt.Errorf("marshaling ynh metadata: %w", err)
	}
	var ynhRaw any
	if err := json.Unmarshal(ynhBytes, &ynhRaw); err != nil {
		return fmt.Errorf("converting ynh metadata: %w", err)
	}
	existing["ynh"] = ynhRaw

	out, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling metadata.json: %w", err)
	}
	out = append(out, '\n')

	if err := os.WriteFile(path, out, 0o644); err != nil {
		return fmt.Errorf("writing metadata.json: %w", err)
	}

	return nil
}
