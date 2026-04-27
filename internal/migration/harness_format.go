package migration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/plugin"
)

// legacySchemaSuffix is the tail of the 0.1 $schema URL. When a migrated
// manifest still points at harness.schema.json, we rewrite it to the 0.2
// plugin.schema.json so IDE validation reflects the new format.
const legacySchemaSuffix = "/harness.schema.json"

// pluginSchemaURL is the 0.2 replacement. We keep the host/prefix from
// whatever the manifest declared so self-hosted schema repos keep working.
const pluginSchemaSuffix = "/plugin.schema.json"

// HarnessFormatMigrator converts .harness.json → .ynh-plugin/plugin.json.
//
// It extracts installed_from into .ynh-plugin/installed.json, writes
// plugin.json without that field, then removes .harness.json.
// Safe to run multiple times — Applies returns false once the new format exists.
type HarnessFormatMigrator struct{}

func (HarnessFormatMigrator) Description() string {
	return "harness format: .harness.json → .ynh-plugin/plugin.json"
}

func (HarnessFormatMigrator) Applies(dir string) bool {
	_, oldErr := os.Stat(filepath.Join(dir, plugin.HarnessFile))
	_, newErr := os.Stat(filepath.Join(dir, plugin.PluginDir, plugin.PluginFile))
	return oldErr == nil && newErr != nil
}

func (HarnessFormatMigrator) Run(dir string) error {
	hj, err := plugin.LoadHarnessJSON(dir)
	if err != nil {
		return fmt.Errorf("reading .harness.json: %w", err)
	}

	if hj.InstalledFrom != nil {
		ins := &plugin.InstalledJSON{
			SourceType:   hj.InstalledFrom.SourceType,
			Source:       hj.InstalledFrom.Source,
			Path:         hj.InstalledFrom.Path,
			RegistryName: hj.InstalledFrom.RegistryName,
			InstalledAt:  hj.InstalledFrom.InstalledAt,
		}
		if err := plugin.SaveInstalledJSON(dir, ins); err != nil {
			return fmt.Errorf("writing installed.json: %w", err)
		}
	}

	// Rewrite the $schema URL to point at plugin.schema.json so IDE validation
	// matches the new format. Only touches URLs that end with the old suffix
	// — custom/self-hosted schemas pass through unchanged.
	if strings.HasSuffix(hj.Schema, legacySchemaSuffix) {
		hj.Schema = strings.TrimSuffix(hj.Schema, legacySchemaSuffix) + pluginSchemaSuffix
	}

	if err := plugin.SavePluginJSON(dir, hj); err != nil {
		return fmt.Errorf("writing plugin.json: %w", err)
	}

	if err := os.Remove(filepath.Join(dir, plugin.HarnessFile)); err != nil {
		return fmt.Errorf("removing .harness.json: %w", err)
	}

	return nil
}
