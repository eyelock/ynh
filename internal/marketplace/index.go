package marketplace

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/eyelock/ynh/internal/vendor"
)

// GenerateIndex writes a vendor-native marketplace index into the appropriate
// vendor manifest directory (e.g., .claude-plugin/marketplace.json).
func GenerateIndex(cfg *MarketplaceConfig, plugins []pluginInfo, outputDir string, vendorName string) error {
	adapter, err := vendor.Get(vendorName)
	if err != nil {
		return fmt.Errorf("marketplace index not supported for vendor %q: %w", vendorName, err)
	}

	manifestDir := adapter.MarketplaceManifestDir()
	if manifestDir == "" {
		// Vendor has no marketplace system — skip silently
		return nil
	}

	indexCfg := vendor.MarketplaceIndexConfig{
		Name:        cfg.Name,
		Description: cfg.Description,
		OwnerName:   cfg.Owner.Name,
		OwnerEmail:  cfg.Owner.Email,
	}

	var vendorPlugins []vendor.MarketplacePluginInfo
	for _, p := range plugins {
		vendorPlugins = append(vendorPlugins, vendor.MarketplacePluginInfo{
			Name:        p.Name,
			Description: p.Description,
			Version:     p.Version,
		})
	}

	data, err := adapter.GenerateMarketplaceIndex(indexCfg, vendorPlugins)
	if err != nil {
		return fmt.Errorf("generating %s marketplace index: %w", vendorName, err)
	}
	if data == nil {
		return nil
	}

	absDir := filepath.Join(outputDir, manifestDir)
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(absDir, "marketplace.json"), data, 0o644)
}
