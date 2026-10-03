package migration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eyelock/ynh/internal/config"
	"github.com/eyelock/ynh/internal/plugin"
)

// legacySchemaSuffix is the tail of the 0.1 $schema URL. When a migrated
// manifest still points at harness.schema.json, we rewrite it to the 0.2
// plugin.schema.json so IDE validation reflects the new format.
const legacySchemaSuffix = "/harness.schema.json"

// pluginSchemaSuffix is the 0.2 replacement. We keep the host/prefix from
// whatever the manifest declared so self-hosted schema repos keep working.
const pluginSchemaSuffix = "/plugin.schema.json"

// pluginSchemaURL is written when the legacy manifest declared no $schema.
const pluginSchemaURL = "https://eyelock.github.io/ynh/schema/plugin.schema.json"

// HarnessFormatMigrator converts .harness.json to .agents/harness/plugin.json.
//
// It extracts installed_from into .agents/harness/installed.json, writes
// plugin.json without that field, then removes .harness.json.
// Safe to run multiple times: Applies returns false once the new format exists.
//
// Where it converts is decided by SourceTrees, as for ManifestDirMigrator:
//
//   - The zero value, in FormatChain, converts only installs under
//     config.HarnessesDir(), ynh's own copies. Anywhere else Run changes
//     nothing and returns LegacyHarnessManifest's error, so a read command
//     on a legacy tree fails with the fix instead of rewriting it (#406).
//   - SourceTrees is set only by MigrateChain, which `ynd migrate` runs
//     behind its confirmation.
type HarnessFormatMigrator struct {
	// SourceTrees allows the conversion outside config.HarnessesDir().
	SourceTrees bool
}

func (HarnessFormatMigrator) Description() string {
	return "harness format: .harness.json → .agents/harness/plugin.json"
}

// Applies reports whether dir's only harness manifest is .harness.json,
// wherever dir is. In FormatChain that is what makes Run refuse a source
// tree: a legacy tree must fail loudly, not be skipped as manifest-less.
func (HarnessFormatMigrator) Applies(dir string) bool {
	return hasLegacyHarnessManifest(dir)
}

func (m HarnessFormatMigrator) Run(dir string) error {
	// Decide again rather than trusting an earlier Applies, so the code
	// below that deletes only ever sees a tree cleared just now.
	if !hasLegacyHarnessManifest(dir) {
		return fmt.Errorf("not converting %s: no %s without a %s", dir, plugin.HarnessFile, plugin.PluginFile)
	}
	if !m.SourceTrees && !insideHarnessesDir(dir) {
		return LegacyHarnessManifest(dir)
	}

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
	// A 0.1 manifest could omit $schema; a plugin.json must declare one, and
	// ynd validate rejects it otherwise. Converting is the one chance to add
	// it, or `ynd migrate` would hand back a tree validate calls invalid.
	if hj.Schema == "" {
		hj.Schema = pluginSchemaURL
	}

	if err := plugin.SavePluginJSON(dir, hj); err != nil {
		return fmt.Errorf("writing plugin.json: %w", err)
	}

	if err := os.Remove(filepath.Join(dir, plugin.HarnessFile)); err != nil {
		return fmt.Errorf("removing .harness.json: %w", err)
	}

	return nil
}

// hasLegacyHarnessManifest reports whether dir has a .harness.json and no
// plugin.json in either manifest directory. A pure predicate: it only stats.
func hasLegacyHarnessManifest(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, plugin.HarnessFile))
	return err == nil && !plugin.IsPluginDir(dir)
}

// LegacyHarnessManifest returns the error a read command gives for a tree
// whose only harness manifest is .harness.json, or nil when it has none.
// ynh no longer reads that file anywhere but `ynd migrate`, and no longer
// converts it as a side effect of reading (#406). A pure predicate.
func LegacyHarnessManifest(dir string) error {
	if !hasLegacyHarnessManifest(dir) {
		return nil
	}
	return legacyManifestError(dir, plugin.HarnessFile+" manifest")
}

// legacyManifestError names the tree and the fix. A cached clone of a remote
// harness is someone else's repository, and converting it would be undone by
// the next fetch, so the fix there is its maintainer's.
func legacyManifestError(dir, what string) error {
	// Name the tree absolutely: "." is no help once the error is read out
	// of context.
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	if within(config.CacheDir(), dir) {
		return fmt.Errorf("%s uses the legacy %s, which ynh no longer reads. It is a cached copy of a remote repository, so the manifest is its upstream's: ask its maintainer to run `ynd migrate` and publish the result", dir, what)
	}
	return fmt.Errorf("%s uses the legacy %s, which ynh no longer reads; convert it with: ynd migrate %s", dir, what, dir)
}
