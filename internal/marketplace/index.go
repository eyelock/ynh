package marketplace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// pluginInfo holds resolved metadata for a plugin in the marketplace.
type pluginInfo struct {
	Name        string
	Description string
	Version     string
}

// marketplaceJSON is the vendor-native marketplace index format.
// Used for both .claude-plugin/marketplace.json and .cursor-plugin/marketplace.json.
type marketplaceJSON struct {
	Name        string              `json:"name"`
	Owner       MarketplaceOwner    `json:"owner"`
	Description string              `json:"description,omitempty"`
	Plugins     []marketplacePlugin `json:"plugins"`
}

// marketplacePlugin is one entry in the vendor marketplace index.
type marketplacePlugin struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Version     string `json:"version,omitempty"`
	Source      string `json:"source"`
}

// GenerateIndex writes a vendor-native marketplace.json into the appropriate
// vendor manifest directory (e.g., .claude-plugin/marketplace.json).
func GenerateIndex(cfg *MarketplaceConfig, plugins []pluginInfo, outputDir string, vendorName string) error {
	var manifestDirName string
	switch vendorName {
	case "claude":
		manifestDirName = ".claude-plugin"
	case "cursor":
		manifestDirName = ".cursor-plugin"
	default:
		return fmt.Errorf("marketplace index not supported for vendor %q", vendorName)
	}

	idx := marketplaceJSON{
		Name:        cfg.Name,
		Owner:       cfg.Owner,
		Description: cfg.Description,
	}

	for _, p := range plugins {
		idx.Plugins = append(idx.Plugins, marketplacePlugin{
			Name:        p.Name,
			Description: p.Description,
			Version:     p.Version,
			Source:      "./plugins/" + p.Name,
		})
	}

	manifestDir := filepath.Join(outputDir, manifestDirName)
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	return os.WriteFile(filepath.Join(manifestDir, "marketplace.json"), data, 0o644)
}
