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
	DefaultVendor string         `json:"default_vendor,omitempty"`
	Includes      []IncludeMeta  `json:"includes,omitempty"`
	DelegatesTo   []DelegateMeta `json:"delegates_to,omitempty"`
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
