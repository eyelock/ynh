package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// MarketplaceFile is the registry index filename inside PluginDir.
const MarketplaceFile = "marketplace.json"

// MarketplaceJSON is the root structure of .ynh-plugin/marketplace.json.
type MarketplaceJSON struct {
	Schema    string           `json:"$schema,omitempty"`
	Name      string           `json:"name"`
	Owner     *OwnerInfo       `json:"owner"`
	Metadata  *MarketplaceMeta `json:"metadata,omitempty"`
	Harnesses []HarnessEntry   `json:"harnesses"`
}

// OwnerInfo describes the registry owner.
type OwnerInfo struct {
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
}

// MarketplaceMeta holds optional registry-level metadata.
type MarketplaceMeta struct {
	Description string `json:"description,omitempty"`
	Version     string `json:"version,omitempty"`
	HarnessRoot string `json:"harnessRoot,omitempty"`
}

// HarnessEntry describes one harness in a marketplace index.
// Source is either a JSON string (relative path) or a RemoteSource object.
type HarnessEntry struct {
	Name        string          `json:"name"`
	Source      json.RawMessage `json:"source"`
	Description string          `json:"description,omitempty"`
	Version     string          `json:"version,omitempty"`
	Author      *AuthorInfo     `json:"author,omitempty"`
	Keywords    []string        `json:"keywords,omitempty"`
	Category    string          `json:"category,omitempty"`
	Tags        []string        `json:"tags,omitempty"`
}

// SourcePath returns the relative path string if source is a plain string.
func (e *HarnessEntry) SourcePath() (string, bool) {
	var s string
	if err := json.Unmarshal(e.Source, &s); err == nil {
		return s, true
	}
	return "", false
}

// SourceRemote returns the remote source object if source is an object.
func (e *HarnessEntry) SourceRemote() (*RemoteSource, bool) {
	var r RemoteSource
	if err := json.Unmarshal(e.Source, &r); err == nil && r.Type != "" {
		return &r, true
	}
	return nil, false
}

// RemoteSource is the object form of a HarnessEntry source.
// Type is the discriminator: "github", "url", or "git-subdir".
type RemoteSource struct {
	Type string `json:"type"`
	Repo string `json:"repo,omitempty"` // github only
	URL  string `json:"url,omitempty"`  // url and git-subdir
	Path string `json:"path,omitempty"`
	Ref  string `json:"ref,omitempty"`
	SHA  string `json:"sha,omitempty"`
}

// LoadMarketplaceJSON reads and parses .ynh-plugin/marketplace.json from dir.
func LoadMarketplaceJSON(dir string) (*MarketplaceJSON, error) {
	data, err := os.ReadFile(filepath.Join(dir, PluginDir, MarketplaceFile))
	if err != nil {
		return nil, fmt.Errorf("reading marketplace.json: %w", err)
	}

	var mj MarketplaceJSON
	if err := json.Unmarshal(data, &mj); err != nil {
		return nil, fmt.Errorf("invalid marketplace.json: %w", err)
	}

	return &mj, nil
}

// SaveMarketplaceJSON writes mj to .ynh-plugin/marketplace.json in dir.
func SaveMarketplaceJSON(dir string, mj *MarketplaceJSON) error {
	if err := os.MkdirAll(filepath.Join(dir, PluginDir), 0o755); err != nil {
		return fmt.Errorf("creating .ynh-plugin dir: %w", err)
	}

	data, err := json.MarshalIndent(mj, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling marketplace.json: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(filepath.Join(dir, PluginDir, MarketplaceFile), data, 0o644); err != nil {
		return fmt.Errorf("writing marketplace.json: %w", err)
	}

	return nil
}
