package migration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/eyelock/ynh/internal/plugin"
)

// RegistryFormatMigrator converts registry.json → .ynh-plugin/marketplace.json.
// Safe to run multiple times — Applies returns false once the new format exists.
type RegistryFormatMigrator struct{}

func (RegistryFormatMigrator) Description() string {
	return "registry format: registry.json → .ynh-plugin/marketplace.json"
}

func (RegistryFormatMigrator) Applies(dir string) bool {
	_, oldErr := os.Stat(filepath.Join(dir, "registry.json"))
	_, newErr := os.Stat(filepath.Join(dir, plugin.PluginDir, plugin.MarketplaceFile))
	return oldErr == nil && newErr != nil
}

func (RegistryFormatMigrator) Run(dir string) error {
	data, err := os.ReadFile(filepath.Join(dir, "registry.json"))
	if err != nil {
		return fmt.Errorf("reading registry.json: %w", err)
	}

	var old oldRegistry
	if err := json.Unmarshal(data, &old); err != nil {
		return fmt.Errorf("parsing registry.json: %w", err)
	}

	mj := &plugin.MarketplaceJSON{
		Name:  old.Name,
		Owner: &plugin.OwnerInfo{Name: old.Name},
	}
	if old.Description != "" {
		mj.Metadata = &plugin.MarketplaceMeta{Description: old.Description}
	}

	for i, e := range old.Entries {
		src := plugin.RemoteSource{
			Type: "github",
			Repo: e.Repo,
			Path: e.Path,
		}
		srcData, err := json.Marshal(src)
		if err != nil {
			return fmt.Errorf("entry %d: marshaling source: %w", i, err)
		}
		mj.Harnesses = append(mj.Harnesses, plugin.HarnessEntry{
			Name:        e.Name,
			Source:      json.RawMessage(srcData),
			Description: e.Description,
			Version:     e.Version,
			Keywords:    e.Keywords,
		})
	}

	if err := plugin.SaveMarketplaceJSON(dir, mj); err != nil {
		return fmt.Errorf("writing marketplace.json: %w", err)
	}

	if err := os.Remove(filepath.Join(dir, "registry.json")); err != nil {
		return fmt.Errorf("removing registry.json: %w", err)
	}

	return nil
}

type oldRegistry struct {
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Entries     []oldEntry `json:"entries"`
}

type oldEntry struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Keywords    []string `json:"keywords,omitempty"`
	Repo        string   `json:"repo"`
	Path        string   `json:"path,omitempty"`
	Version     string   `json:"version,omitempty"`
}
