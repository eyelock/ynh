package migration

import (
	"bytes"
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
	if _, err := os.Stat(filepath.Join(dir, plugin.PluginDir, plugin.MarketplaceFile)); err == nil {
		return false
	}
	_, err := readYnhRegistry(dir)
	return err == nil
}

// readYnhRegistry reads dir/registry.json and returns it only if it is one
// ynh wrote.
//
// "registry.json" is not an ynh-specific name: npm, Docker and others use it.
// Applies used to match on the filename alone, and Run decoded whatever it
// found with a plain Unmarshal, so a foreign object became zero values, was
// overwritten with an empty marketplace, and the original was deleted. An
// unrelated file with no path back (#348).
//
// This is the predicate half of that fix, and it is deliberately a pure read:
// per .claude/rules/destructive-operations.md the code that decides is kept
// separate from the code that deletes, so a file this cannot identify never
// reaches os.Remove.
func readYnhRegistry(dir string) (*oldRegistry, error) {
	path := filepath.Join(dir, "registry.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Strictness at the top level only. An ynh registry has exactly name,
	// description and entries there, so an npm-style {"registries":…,
	// "authToken":…} is rejected outright rather than decoding to zero
	// values. Entries themselves are decoded leniently: a registry written by
	// an older ynh carries per-entry fields this struct does not model
	// (vendors, ref, sha, namespace), and rejecting those would refuse to
	// migrate the very files this exists to migrate.
	var envelope struct {
		Schema string `json:"$schema"`
		Name   string `json:"name"`
		// Namespace is derived from the registry URL and marshalled into the
		// file by internal/registry. Unmodelled here it would be an unknown
		// field, and a genuine ynh registry would be refused.
		Namespace   string            `json:"namespace"`
		Description string            `json:"description"`
		Entries     []json.RawMessage `json:"entries"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&envelope); err != nil {
		return nil, fmt.Errorf("%s is not an ynh registry: %w", path, err)
	}

	old := oldRegistry{Name: envelope.Name, Description: envelope.Description}
	for i, raw := range envelope.Entries {
		var e oldEntry
		if err := json.Unmarshal(raw, &e); err != nil {
			return nil, fmt.Errorf("%s entry %d: %w", path, i, err)
		}
		old.Entries = append(old.Entries, e)
	}

	// A registry ynh wrote has a name or entries. One with neither carries
	// nothing to migrate, and converting it would replace a file we cannot
	// identify with an empty marketplace.
	if old.Name == "" && len(old.Entries) == 0 {
		return nil, fmt.Errorf("%s has no name and no entries; not an ynh registry", path)
	}
	return &old, nil
}

func (RegistryFormatMigrator) Run(dir string) error {
	// Re-read through the same predicate Applies used, so Run cannot delete a
	// file that would not have qualified. The two must not be able to
	// disagree.
	oldPtr, err := readYnhRegistry(dir)
	if err != nil {
		return err
	}
	old := *oldPtr

	mj := &plugin.MarketplaceJSON{
		Schema: "https://eyelock.github.io/ynh/schema/marketplace.schema.json",
		Name:   old.Name,
		Owner:  &plugin.OwnerInfo{Name: old.Name},
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
