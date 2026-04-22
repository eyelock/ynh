package sources

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/eyelock/ynh/internal/migration"
	"github.com/eyelock/ynh/internal/plugin"
)

// DiscoveredHarness describes a harness found by walking a local source directory.
type DiscoveredHarness struct {
	Name          string
	Description   string
	Version       string
	DefaultVendor string
	Keywords      []string
	Path          string // absolute path to the harness directory
}

// Discover walks root up to maxDepth levels looking for directories that
// contain a .harness.json file. Returns one entry per discovered harness.
func Discover(root string, maxDepth int) ([]DiscoveredHarness, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, nil
	}

	var results []DiscoveredHarness

	// Check root itself
	if h, ok := loadMinimalHarness(abs); ok {
		results = append(results, h)
	}

	// Walk children up to maxDepth
	if maxDepth > 0 {
		walkDiscover(abs, maxDepth, &results)
	}

	return results, nil
}

func walkDiscover(dir string, remainingDepth int, results *[]DiscoveredHarness) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() || entry.Name()[0] == '.' {
			continue
		}
		child := filepath.Join(dir, entry.Name())
		if h, ok := loadMinimalHarness(child); ok {
			*results = append(*results, h)
		} else if remainingDepth > 1 {
			walkDiscover(child, remainingDepth-1, results)
		}
	}
}

// loadMinimalHarness reads just the identity fields from a harness manifest.
// Migration chain runs first so any legacy format is converted before we read.
// Uses a loose struct (no DisallowUnknownFields) so discovery tolerates newer fields.
func loadMinimalHarness(dir string) (DiscoveredHarness, bool) {
	if _, err := migration.FormatChain().Run(dir); err != nil {
		return DiscoveredHarness{}, false
	}

	manifestPath := filepath.Join(dir, plugin.PluginDir, plugin.PluginFile)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return DiscoveredHarness{}, false
	}

	var manifest struct {
		Name          string   `json:"name"`
		Description   string   `json:"description"`
		Version       string   `json:"version"`
		DefaultVendor string   `json:"default_vendor"`
		Keywords      []string `json:"keywords"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return DiscoveredHarness{}, false
	}

	if manifest.Name == "" {
		return DiscoveredHarness{}, false
	}

	return DiscoveredHarness{
		Name:          manifest.Name,
		Description:   manifest.Description,
		Version:       manifest.Version,
		DefaultVendor: manifest.DefaultVendor,
		Keywords:      manifest.Keywords,
		Path:          dir,
	}, true
}
